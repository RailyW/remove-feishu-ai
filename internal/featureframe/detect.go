package featureframe

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"remove-feishu-ai/internal/config"
	"remove-feishu-ai/internal/feature"
	"remove-feishu-ai/internal/peutil"
	"remove-feishu-ai/internal/search"
)

const (
	searchChunkSize = 1024 * 1024

	searchModeCacheExact  = "cache_exact"
	searchModeCacheWindow = "cache_window"
	searchModePESections  = "pe_sections"
	searchModeFullFile    = "full_file"
)

// DetectMeta 描述一次 frame.dll 检测的命中详情。
//
// OldOffsets 与 NewOffsets 分别记录原始 pattern 和补丁 pattern 的实际文件偏移。
// SearchMode 记录最终成功给出非 unknown 判定的扫描层级，便于调用方观察缓存是否命中。
type DetectMeta struct {
	OldOffsets []int64
	NewOffsets []int64
	SearchMode string
}

// ID 返回该功能在配置、日志和后续缓存 map 中使用的稳定标识。
func (f *Feature) ID() string {
	return "featureframe"
}

// DisplayName 返回面向用户展示的功能名称。
func (f *Feature) DisplayName() string {
	return "知识库 AI"
}

// Detect 根据安装目录定位 frame.dll，并执行无写入的状态检测。
//
// 该方法不接收缓存参数，会委托 DetectWithCache 使用空缓存完成检测。
func (f *Feature) Detect(ctx context.Context, env feature.Env) (feature.State, error) {
	state, _, err := f.DetectWithCache(ctx, env, config.OffsetCache{})
	return state, err
}

// DetectWithCache 根据安装目录定位 frame.dll，并在只读模式下结合偏移缓存执行检测。
//
// cache 仅用于加速检测；当缓存的文件大小或修改时间与当前文件不匹配时，缓存层只允许
// 对 mixed 状态早退，不能仅凭缓存中单类 pattern 命中就判定 original 或 patched。
func (f *Feature) DetectWithCache(ctx context.Context, env feature.Env, cache config.OffsetCache) (feature.State, DetectMeta, error) {
	select {
	case <-ctx.Done():
		return feature.State{Internal: feature.StateUnknown}, DetectMeta{}, ctx.Err()
	default:
	}

	path := filepath.Join(env.InstallPath(), f.rule.FileRelativePath)
	return f.detect(path, cache)
}

// detect 按缓存直验、缓存邻域、PE 可执行节区、全文件扫描的优先级检测 frame.dll 状态。
//
// 函数只打开并读取 path 指向的文件，不写入目标文件，也不访问真实安装目录之外的路径。
// 对非 PE 测试样本或 PE 解析失败场景，会自动回退到全文件扫描。
func (f *Feature) detect(path string, cache config.OffsetCache) (feature.State, DetectMeta, error) {
	file, err := os.Open(path)
	if err != nil {
		return feature.State{Internal: feature.StateUnknown}, DetectMeta{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return feature.State{Internal: feature.StateUnknown}, DetectMeta{}, err
	}
	fileSize := info.Size()
	cacheTrusted := cacheMatchesFile(cache, info)

	if hasCachedOffsets(cache) {
		meta, err := f.detectCacheExact(file, cache)
		if err != nil {
			return feature.State{Internal: feature.StateUnknown}, meta, err
		}
		if state := f.classify(meta); canReturnFromCache(state, cacheTrusted) {
			return state, meta, nil
		}

		meta, err = f.detectCacheWindow(file, cache, fileSize)
		if err != nil {
			return feature.State{Internal: feature.StateUnknown}, meta, err
		}
		if state := f.classify(meta); canReturnFromCache(state, cacheTrusted) {
			return state, meta, nil
		}
	}

	meta, err := f.detectPESections(file, path)
	if err == nil {
		if state := f.classify(meta); state.Internal != feature.StateUnknown {
			return state, meta, nil
		}
	}

	meta, err = f.detectFullFile(file, fileSize)
	if err != nil {
		return feature.State{Internal: feature.StateUnknown}, meta, err
	}

	return f.classify(meta), meta, nil
}

// detectCacheExact 只验证缓存中记录的精确偏移，不做额外扫描。
//
// 命中的 offset 才会写入 DetectMeta；失效或越界的缓存 offset 会被忽略。
func (f *Feature) detectCacheExact(file *os.File, cache config.OffsetCache) (DetectMeta, error) {
	meta := DetectMeta{SearchMode: searchModeCacheExact}

	oldOffsets, err := verifyOffsets(file, cache.OldPatternOffsets, f.rule.OldPattern)
	if err != nil {
		return meta, err
	}
	newOffsets, err := verifyOffsets(file, cache.NewPatternOffsets, f.rule.NewPattern)
	if err != nil {
		return meta, err
	}

	meta.OldOffsets = oldOffsets
	meta.NewOffsets = newOffsets
	return normalizeMeta(meta), nil
}

// detectCacheWindow 以缓存偏移为中心，在固定半径窗口内重新查找新旧 pattern。
//
// 该层用于处理 frame.dll 小幅变化导致偏移漂移的情况；所有窗口命中会去重并排序。
func (f *Feature) detectCacheWindow(file *os.File, cache config.OffsetCache, fileSize int64) (DetectMeta, error) {
	meta := DetectMeta{SearchMode: searchModeCacheWindow}
	centers := append([]int64{}, cache.OldPatternOffsets...)
	centers = append(centers, cache.NewPatternOffsets...)
	centers = uniqueSortedOffsets(centers)

	oldOffsets, newOffsets, err := f.findInCenteredWindows(file, centers, fileSize)
	if err != nil {
		return meta, err
	}

	meta.OldOffsets = oldOffsets
	meta.NewOffsets = newOffsets
	return normalizeMeta(meta), nil
}

// detectPESections 只扫描 PE 文件内带可执行标志的节区。
//
// 如果 path 不是 PE 文件，peutil.ExecutableRanges 会返回错误，调用方会继续回退全文件扫描。
func (f *Feature) detectPESections(file *os.File, path string) (DetectMeta, error) {
	meta := DetectMeta{SearchMode: searchModePESections}

	ranges, err := peutil.ExecutableRanges(path)
	if err != nil {
		return meta, err
	}

	for _, executableRange := range ranges {
		oldOffsets, err := search.FindAllInWindow(file, executableRange.Start, executableRange.End, f.rule.OldPattern, searchChunkSize)
		if err != nil {
			return meta, err
		}
		newOffsets, err := search.FindAllInWindow(file, executableRange.Start, executableRange.End, f.rule.NewPattern, searchChunkSize)
		if err != nil {
			return meta, err
		}
		meta.OldOffsets = append(meta.OldOffsets, oldOffsets...)
		meta.NewOffsets = append(meta.NewOffsets, newOffsets...)
	}

	return normalizeMeta(meta), nil
}

// detectFullFile 扫描整个文件，是缓存和 PE 节区都无法判定时的最终兜底。
func (f *Feature) detectFullFile(file *os.File, fileSize int64) (DetectMeta, error) {
	meta := DetectMeta{SearchMode: searchModeFullFile}

	oldOffsets, err := search.FindAllInReader(file, fileSize, f.rule.OldPattern, searchChunkSize)
	if err != nil {
		return meta, err
	}
	newOffsets, err := search.FindAllInReader(file, fileSize, f.rule.NewPattern, searchChunkSize)
	if err != nil {
		return meta, err
	}

	meta.OldOffsets = oldOffsets
	meta.NewOffsets = newOffsets
	return normalizeMeta(meta), nil
}

// findInCenteredWindows 在多个缓存中心点的邻域窗口中查找新旧 pattern。
func (f *Feature) findInCenteredWindows(file *os.File, centers []int64, fileSize int64) ([]int64, []int64, error) {
	oldOffsets := make([]int64, 0)
	newOffsets := make([]int64, 0)

	for _, center := range centers {
		start := center - f.rule.SearchRadius
		if start < 0 {
			start = 0
		}
		end := center + f.rule.SearchRadius
		if end > fileSize {
			end = fileSize
		}

		windowOldOffsets, err := search.FindAllInWindow(file, start, end, f.rule.OldPattern, searchChunkSize)
		if err != nil {
			return nil, nil, err
		}
		windowNewOffsets, err := search.FindAllInWindow(file, start, end, f.rule.NewPattern, searchChunkSize)
		if err != nil {
			return nil, nil, err
		}

		oldOffsets = append(oldOffsets, windowOldOffsets...)
		newOffsets = append(newOffsets, windowNewOffsets...)
	}

	return uniqueSortedOffsets(oldOffsets), uniqueSortedOffsets(newOffsets), nil
}

// classify 根据新旧 pattern 命中数归类 frame.dll 当前状态。
func (f *Feature) classify(meta DetectMeta) feature.State {
	oldCount := len(meta.OldOffsets)
	newCount := len(meta.NewOffsets)

	switch {
	case oldCount == f.rule.ExpectedCount && newCount == 0:
		return feature.State{Internal: feature.StateOriginal}
	case oldCount == 0 && newCount == f.rule.ExpectedCount:
		return feature.State{Internal: feature.StatePatched}
	case oldCount > 0 && newCount > 0:
		return feature.State{Internal: feature.StateMixed}
	default:
		return feature.State{Internal: feature.StateUnknown}
	}
}

// verifyOffsets 逐个使用 search.VerifyAt 验证指定偏移是否完整匹配 pattern。
func verifyOffsets(file *os.File, offsets []int64, pattern []byte) ([]int64, error) {
	matchedOffsets := make([]int64, 0, len(offsets))
	for _, offset := range offsets {
		matched, err := search.VerifyAt(file, offset, pattern)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if err != nil && errors.Is(err, io.EOF) {
			continue
		}
		if matched {
			matchedOffsets = append(matchedOffsets, offset)
		}
	}

	return uniqueSortedOffsets(matchedOffsets), nil
}

// hasCachedOffsets 判断缓存中是否至少包含一个可尝试验证或邻域扫描的偏移。
func hasCachedOffsets(cache config.OffsetCache) bool {
	return len(cache.OldPatternOffsets) > 0 || len(cache.NewPatternOffsets) > 0
}

// cacheMatchesFile 判断偏移缓存是否属于当前打开的文件。
//
// 当前可信性只由文件大小和 UTC RFC3339Nano 修改时间共同决定，不引入额外 hash。
func cacheMatchesFile(cache config.OffsetCache, info os.FileInfo) bool {
	if cache.FileSize != info.Size() {
		return false
	}

	return cache.MTime == info.ModTime().UTC().Format(time.RFC3339Nano)
}

// canReturnFromCache 判断缓存扫描结果是否可以作为最终状态直接返回。
//
// mixed 表示本次读取已经观察到新旧两类 pattern 共存，即使缓存身份不可信也足够判定。
// original 与 patched 都依赖“另一类 pattern 不存在”的负向结论，只有缓存身份可信时才可早退。
func canReturnFromCache(state feature.State, cacheTrusted bool) bool {
	switch state.Normalized().Internal {
	case feature.StateMixed:
		return true
	case feature.StateOriginal, feature.StatePatched:
		return cacheTrusted
	default:
		return false
	}
}

// normalizeMeta 对 DetectMeta 中的 offset 做去重排序，保证测试和日志输出稳定。
func normalizeMeta(meta DetectMeta) DetectMeta {
	meta.OldOffsets = uniqueSortedOffsets(meta.OldOffsets)
	meta.NewOffsets = uniqueSortedOffsets(meta.NewOffsets)
	return meta
}

// uniqueSortedOffsets 返回升序且无重复的 offset 列表。
func uniqueSortedOffsets(offsets []int64) []int64 {
	if len(offsets) == 0 {
		return []int64{}
	}

	sort.Slice(offsets, func(i, j int) bool {
		return offsets[i] < offsets[j]
	})

	writeIndex := 1
	for readIndex := 1; readIndex < len(offsets); readIndex++ {
		if offsets[readIndex] == offsets[writeIndex-1] {
			continue
		}
		offsets[writeIndex] = offsets[readIndex]
		writeIndex++
	}

	return offsets[:writeIndex]
}
