package featurejs

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"remove-feishu-ai/internal/feature"
)

// DetectMeta 描述一次 JS bundle 检测的定位与命中详情。
//
// BundlePath 与 RelativePath 用于向上层暴露最终命中的哈希文件。
// Score 是定位阶段的 marker 打分结果。
// OriginalPatternHits / PatchedPatternHits 分别记录每个 exact pattern 的单独命中次数。
// OriginalCount / PatchedCount 是上述命中切片的总和，便于 mixed/unknown 等聚合判断。
// LocateMode 标记本次使用缓存路径命中还是目录扫描命中。
// PatternVariantName / PatternVariantIndex 标记最终采用的规则变体，供写入阶段按
// 同一组 exact pattern 执行 Remove 或 Restore。
type DetectMeta struct {
	BundlePath          string
	RelativePath        string
	Score               int
	OriginalPatternHits []int
	PatchedPatternHits  []int
	OriginalCount       int
	PatchedCount        int
	LocateMode          string
	PatternVariantName  string
	PatternVariantIndex int
}

// Detect 根据安装根目录定位 bundle 目录，并执行无写入状态检测。
func (f *Feature) Detect(ctx context.Context, env feature.Env) (feature.State, error) {
	state, _, err := f.DetectWithCache(ctx, env, "")
	return state, err
}

// DetectWithCache 根据安装根目录与缓存相对路径定位 bundle，并执行无写入状态检测。
func (f *Feature) DetectWithCache(ctx context.Context, env feature.Env, cachedRelativePath string) (feature.State, DetectMeta, error) {
	select {
	case <-ctx.Done():
		return feature.State{Internal: feature.StateUnknown}, DetectMeta{}, ctx.Err()
	default:
	}

	commonDir := filepath.Join(env.InstallPath(), f.rule.BundleDir)
	return f.detect(commonDir, cachedRelativePath)
}

// detect 在 commonDir 下执行候选定位与状态检测。
//
// 若无法找到满足全部 strong marker 的 bundle，不视为错误，而是安全返回 unknown。
func (f *Feature) detect(commonDir string, cachedRelativePath string) (feature.State, DetectMeta, error) {
	locateMode := locateModeScan
	normalizedCachedPath, cachedPathOK := f.cachedCommonRelativePath(cachedRelativePath)
	if cachedPathOK {
		locateMode = locateModeCachePath
	}

	candidate, err := f.locateBundle(commonDir, cachedRelativePath)
	if err != nil {
		if os.IsNotExist(err) {
			return feature.State{Internal: feature.StateUnknown}, DetectMeta{}, nil
		}
		return feature.State{Internal: feature.StateUnknown}, DetectMeta{}, err
	}
	if candidate.Path == "" {
		return feature.State{Internal: feature.StateUnknown}, DetectMeta{}, nil
	}

	if !cachedPathOK || filepath.ToSlash(candidate.RelativePath) != normalizedCachedPath {
		locateMode = locateModeScan
	}

	content, err := os.ReadFile(candidate.Path)
	if err != nil {
		return feature.State{Internal: feature.StateUnknown}, DetectMeta{}, err
	}
	meta := f.detectPatternVariant(string(content))
	meta.BundlePath = candidate.Path
	meta.RelativePath = candidate.RelativePath
	meta.Score = candidate.Score
	meta.LocateMode = locateMode

	return f.classify(meta), meta, nil
}

// detectPatternVariant 按规则声明顺序检测当前 bundle 命中的 exact pattern 变体。
//
// 同一功能可能在不同飞书版本中出现不同压缩变量名或渲染结构，因此 Rule 允许声明
// 多个 PatternVariant。这里的选择策略是“优先级 + 保守停止”：
//  1. 如果某个变体能明确归类为 original、patched 或 mixed，立即采用该变体。
//  2. 如果某个变体出现任何 original/patched 片段但数量不足以归类，也立即采用该
//     unknown 结果，避免跳到备用变体后在部分修改文件上继续写入。
//  3. 只有当前变体完全没有命中任何片段时，才继续尝试低优先级变体。
//  4. 所有变体都没有命中时，返回第一变体的零命中结果，供上层展示 unknown。
func (f *Feature) detectPatternVariant(content string) DetectMeta {
	variants := f.patternVariants()
	var firstMeta DetectMeta
	for index, variant := range variants {
		originalPatternHits := patternHits(content, variant.OriginalPatterns)
		patchedPatternHits := patternHits(content, variant.PatchedPatterns)
		meta := DetectMeta{
			OriginalPatternHits: originalPatternHits,
			PatchedPatternHits:  patchedPatternHits,
			OriginalCount:       countPatternHits(originalPatternHits),
			PatchedCount:        countPatternHits(patchedPatternHits),
			PatternVariantName:  variant.Name,
			PatternVariantIndex: index,
		}
		if index == 0 {
			firstMeta = meta
		}

		state := f.classify(meta).Normalized().Internal
		if state != feature.StateUnknown || meta.OriginalCount > 0 || meta.PatchedCount > 0 {
			return meta
		}
	}

	return firstMeta
}

// classify 根据每个 exact pattern 的单独命中结果归类 bundle 当前状态。
//
// original 要求每个 original pattern 恰好命中一次，且所有 patched pattern 命中 0 次；
// patched 规则对称；mixed 则继续使用“original 总命中 > 0 且 patched 总命中 > 0”。
// 其他情况，包括单个 pattern 重复多次而另一个缺失，统一归类为 unknown。
func (f *Feature) classify(meta DetectMeta) feature.State {
	switch {
	case allPatternHitsEqual(meta.OriginalPatternHits, 1) && allPatternHitsEqual(meta.PatchedPatternHits, 0):
		return feature.State{Internal: feature.StateOriginal}
	case allPatternHitsEqual(meta.OriginalPatternHits, 0) && allPatternHitsEqual(meta.PatchedPatternHits, 1):
		return feature.State{Internal: feature.StatePatched}
	case meta.OriginalCount > 0 && meta.PatchedCount > 0:
		return feature.State{Internal: feature.StateMixed}
	default:
		return feature.State{Internal: feature.StateUnknown}
	}
}

// patternHits 统计每个 pattern 在 content 中的独立命中次数。
//
// 这里保留逐项命中数组，是为了区分“两个 pattern 各命中一次”和“同一个 pattern 重复
// 两次、另一个完全缺失”这两种总数相同但语义不同的内容。
func patternHits(content string, patterns []string) []int {
	hits := make([]int, 0, len(patterns))
	for _, pattern := range patterns {
		hits = append(hits, strings.Count(content, pattern))
	}

	return hits
}

// countPatternHits 汇总逐 pattern 命中切片的总数。
//
// mixed 判定仍然只需要“是否存在任意 original 命中”和“是否存在任意 patched 命中”，
// 因此保留总数作为辅助字段，避免上层重复求和。
func countPatternHits(hits []int) int {
	count := 0
	for _, hit := range hits {
		count += hit
	}

	return count
}

// allPatternHitsEqual 判断一组 pattern 是否全部恰好命中指定次数。
//
// 对 original/patched 的严格判定来说，所有 exact pattern 都必须满足相同命中次数。
// 只要某一个 pattern 重复、缺失或数量漂移，就不能视为明确可写状态。
func allPatternHitsEqual(hits []int, want int) bool {
	if len(hits) == 0 {
		return false
	}

	for _, hit := range hits {
		if hit != want {
			return false
		}
	}

	return true
}
