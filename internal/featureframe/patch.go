package featureframe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"remove-feishu-ai/internal/config"
	"remove-feishu-ai/internal/feature"
	"remove-feishu-ai/internal/search"
)

var (
	// ErrFrameActionNotAllowed 表示当前 frame.dll 状态不允许执行请求的补丁动作。
	//
	// 该错误用于区分“文件存在但状态不满足前置条件”和“文件读取/写入失败”两类问题。
	ErrFrameActionNotAllowed = errors.New("frame action not allowed")

	// ErrFrameVerifyFailed 表示写前或写后校验未通过，无法确认目标文件处于期望字节状态。
	//
	// 该错误主要覆盖扫描后文件被外部修改、偏移不再匹配，或写入完成后状态未转为目标状态的场景。
	ErrFrameVerifyFailed = errors.New("frame verify failed")
)

// Remove 在 frame.dll 处于 original 状态时，将所有 old pattern 定点写为 new pattern。
//
// 实现严格遵循“先检测、再备份、写前复核、WriteAt 定点写入、写后重检”的顺序；
// 若当前状态不是 original，或任一校验失败，则不会执行目标字节写入。
func (f *Feature) Remove(ctx context.Context, env feature.Env, tx feature.Tx) error {
	return f.patch(
		ctx,
		env,
		tx,
		feature.StateOriginal,
		feature.StatePatched,
		f.rule.OldPattern,
		f.rule.NewPattern,
		"remove",
	)
}

// Restore 在 frame.dll 处于 patched 状态时，将所有 new pattern 定点恢复为 old pattern。
//
// 该方法与 Remove 共用同一套保护措施，避免在 mixed、unknown 或文件漂移状态下盲写。
func (f *Feature) Restore(ctx context.Context, env feature.Env, tx feature.Tx) error {
	return f.patch(
		ctx,
		env,
		tx,
		feature.StatePatched,
		feature.StateOriginal,
		f.rule.NewPattern,
		f.rule.OldPattern,
		"restore",
	)
}

// patch 执行一次受保护的 frame.dll 定点写入。
//
// allowedState 定义执行前必须满足的检测状态；expectedPattern 是写前必须再次校验的旧字节；
// replacementPattern 是将通过 WriteAt 写入目标偏移的新字节；targetState 则是写后重检的目标状态。
func (f *Feature) patch(
	ctx context.Context,
	env feature.Env,
	tx feature.Tx,
	allowedState feature.InternalState,
	targetState feature.InternalState,
	expectedPattern []byte,
	replacementPattern []byte,
	action string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	targetPath := filepath.Join(env.InstallPath(), f.rule.FileRelativePath)
	state, meta, err := f.detect(targetPath, config.OffsetCache{})
	if err != nil {
		return err
	}

	normalizedState := state.Normalized().Internal
	if normalizedState != allowedState {
		return fmt.Errorf("%w: %s requires %s state, got %s", ErrFrameActionNotAllowed, action, allowedState, normalizedState)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	offsets := offsetsForPattern(meta, expectedPattern, f.rule)
	if len(offsets) != f.rule.ExpectedCount {
		return fmt.Errorf("%w: %s expected %d offsets, got %d", ErrFrameVerifyFailed, action, f.rule.ExpectedCount, len(offsets))
	}

	if err := tx.BackupFile(f.rule.FileRelativePath, targetPath); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	file, err := os.OpenFile(targetPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, offset := range offsets {
		if err := ctx.Err(); err != nil {
			return err
		}

		matched, verifyErr := search.VerifyAt(file, offset, expectedPattern)
		if verifyErr != nil {
			return fmt.Errorf("%w: %s verify offset %d: %v", ErrFrameVerifyFailed, action, offset, verifyErr)
		}
		if !matched {
			return fmt.Errorf("%w: %s verify offset %d mismatch", ErrFrameVerifyFailed, action, offset)
		}

		written, writeErr := file.WriteAt(replacementPattern, offset)
		if writeErr != nil {
			return writeErr
		}
		if written != len(replacementPattern) {
			return fmt.Errorf("%w: %s short write at offset %d: wrote %d bytes", ErrFrameVerifyFailed, action, offset, written)
		}
	}

	if err := file.Sync(); err != nil {
		return err
	}

	verifiedState, _, err := f.detect(targetPath, config.OffsetCache{})
	if err != nil {
		return err
	}
	if verifiedState.Normalized().Internal != targetState {
		return fmt.Errorf("%w: %s result state = %s, want %s", ErrFrameVerifyFailed, action, verifiedState.Normalized().Internal, targetState)
	}

	return nil
}

// offsetsForPattern 从检测元数据中选出与当前补丁方向对应的一组偏移。
//
// Remove 使用 OldOffsets，Restore 使用 NewOffsets。比较依据是规则中的 pattern 内容本身，
// 这样调用方不需要再显式传入“读取 old 还是 new offsets”的布尔标记。
func offsetsForPattern(meta DetectMeta, expectedPattern []byte, rule Rule) []int64 {
	switch {
	case bytesEqual(expectedPattern, rule.OldPattern):
		return append([]int64{}, meta.OldOffsets...)
	case bytesEqual(expectedPattern, rule.NewPattern):
		return append([]int64{}, meta.NewOffsets...)
	default:
		return []int64{}
	}
}

// bytesEqual 对两个模式字节切片执行长度和值完全相等判断。
//
// 本地定义该辅助函数是为了避免在 patch 流程中引入整包 bytes，仅保留最小依赖面。
func bytesEqual(left []byte, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
