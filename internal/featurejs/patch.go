package featurejs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"remove-feishu-ai/internal/feature"
)

var (
	// ErrJSActionNotAllowed 表示当前 JS bundle 状态不允许执行请求的补丁动作。
	//
	// Remove 只允许 original，Restore 只允许 patched；mixed、unknown 或未找到
	// bundle 都会归入“不允许写入”，以避免在规则失效时误改前端资源。
	ErrJSActionNotAllowed = errors.New("featurejs action not allowed")

	// ErrJSVerifyFailed 表示写入后重检未达到期望状态。
	//
	// 该错误通常代表 bundle 内容在写入过程中发生漂移、规则配置不完整，或写入
	// 替换没有覆盖所有期望 pattern。
	ErrJSVerifyFailed = errors.New("featurejs verify failed")
)

// Remove 在 JS bundle 处于 original 状态时，将所有原始 pattern 替换为补丁 pattern。
//
// 写入前会重新定位并检测 bundle，只有 exact pattern 命中数被归类为 original 时才
// 允许继续；写入前必须先调用 tx.BackupFile 备份相对安装根目录的 bundle 路径。
func (f *Feature) Remove(ctx context.Context, env feature.Env, tx feature.Tx) error {
	return f.patch(
		ctx,
		env,
		tx,
		feature.StateOriginal,
		feature.StatePatched,
		"remove",
	)
}

// Restore 在 JS bundle 处于 patched 状态时，将所有补丁 pattern 恢复为原始 pattern。
//
// Restore 与 Remove 共用同一套检测、备份、临时文件替换和写后重检流程，确保 mixed、
// unknown 或未找到 bundle 时不会执行任何写入。
func (f *Feature) Restore(ctx context.Context, env feature.Env, tx feature.Tx) error {
	return f.patch(
		ctx,
		env,
		tx,
		feature.StatePatched,
		feature.StateOriginal,
		"restore",
	)
}

// patch 执行一次受保护的 JS bundle 全文件替换。
//
// allowedState 是写入前必须满足的状态，targetState 是写入后必须重检达到的状态。
// 函数会读取 detect 阶段命中的 PatternVariant，按动作方向取出一一对应的
// fromPatterns / toPatterns，再读取目标 bundle 到内存后执行成对 ReplaceAll，并通过
// 临时文件替换原文件来减少半写入风险。
func (f *Feature) patch(
	ctx context.Context,
	env feature.Env,
	tx feature.Tx,
	allowedState feature.InternalState,
	targetState feature.InternalState,
	action string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	commonDir := filepath.Join(env.InstallPath(), f.rule.BundleDir)
	state, meta, err := f.detect(commonDir, "")
	if err != nil {
		return err
	}
	normalizedState := state.Normalized().Internal
	if normalizedState != allowedState || meta.BundlePath == "" {
		return fmt.Errorf("%w: %s requires %s state, got %s", ErrJSActionNotAllowed, action, allowedState, normalizedState)
	}
	fromPatterns, toPatterns, err := f.patchPatternsFor(meta.PatternVariantIndex, allowedState)
	if err != nil {
		return fmt.Errorf("%w: %s %v", ErrJSVerifyFailed, action, err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	relativePathFromInstallRoot := filepath.Join(f.rule.BundleDir, filepath.FromSlash(meta.RelativePath))
	if err := tx.BackupFile(relativePathFromInstallRoot, meta.BundlePath); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	contentBytes, err := os.ReadFile(meta.BundlePath)
	if err != nil {
		return err
	}
	updatedContent := replacePatternPairs(string(contentBytes), fromPatterns, toPatterns)
	if err := replaceFileWithTemp(meta.BundlePath, []byte(updatedContent)); err != nil {
		return err
	}

	verifiedState, _, err := f.detect(commonDir, "")
	if err != nil {
		return err
	}
	if verifiedState.Normalized().Internal != targetState {
		return fmt.Errorf("%w: %s result state = %s, want %s", ErrJSVerifyFailed, action, verifiedState.Normalized().Internal, targetState)
	}

	return nil
}

// patchPatternsFor 根据检测命中的变体和动作方向返回需要替换的一组 pattern。
//
// Remove 方向要求当前状态为 original，因此把该变体的 OriginalPatterns 替换为
// PatchedPatterns；Restore 方向相反。函数会校验变体下标和成对 pattern 数量，
// 避免规则配置不完整时写出不可恢复内容。
func (f *Feature) patchPatternsFor(variantIndex int, allowedState feature.InternalState) ([]string, []string, error) {
	variants := f.patternVariants()
	if variantIndex < 0 || variantIndex >= len(variants) {
		return nil, nil, fmt.Errorf("pattern variant index %d out of range", variantIndex)
	}

	variant := variants[variantIndex]
	if len(variant.OriginalPatterns) != len(variant.PatchedPatterns) {
		return nil, nil, fmt.Errorf("pattern pair count mismatch in variant %q", variant.Name)
	}

	switch allowedState {
	case feature.StateOriginal:
		return variant.OriginalPatterns, variant.PatchedPatterns, nil
	case feature.StatePatched:
		return variant.PatchedPatterns, variant.OriginalPatterns, nil
	default:
		return nil, nil, fmt.Errorf("unsupported allowed state %s", allowedState)
	}
}

// replacePatternPairs 将 fromPatterns 中的每个 pattern 按相同下标替换为 toPatterns。
//
// 调用方已经通过状态检测确认 exact pattern 命中数满足规则，因此这里不再用 marker
// 作为写入依据，只执行明确的 pattern 成对替换。
func replacePatternPairs(content string, fromPatterns []string, toPatterns []string) string {
	result := content
	for index, fromPattern := range fromPatterns {
		result = strings.ReplaceAll(result, fromPattern, toPatterns[index])
	}

	return result
}

// replaceFileWithTemp 先写入同目录临时文件，再以原子替换语义覆盖目标文件。
//
// 与“先删除原文件再 Rename”不同，这里要求替换失败时原目标文件仍保持存在，从而
// 避免把飞书安装目录留在“bundle 被删没”的破坏状态。
func replaceFileWithTemp(targetPath string, content []byte) error {
	return replaceFileWithTempUsing(targetPath, content, replaceFileAtomically)
}

// replaceFileWithTempUsing 把“写临时文件”和“替换目标文件”拆成两步，便于测试。
//
// 调用方可以注入一个失败的 replacer，验证在最终替换失败时：
// 1. 原目标文件仍保持原状。
// 2. `.tmp` 文件会被清理，不在 common 目录留下垃圾文件。
func replaceFileWithTempUsing(targetPath string, content []byte, replacer func(tmpPath string, targetPath string) error) error {
	tmpPath := targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o600); err != nil {
		return err
	}

	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if replacer == nil {
		replacer = replaceFileAtomically
	}

	if err := replacer(tmpPath, targetPath); err != nil {
		return err
	}
	cleanupTmp = false

	return nil
}
