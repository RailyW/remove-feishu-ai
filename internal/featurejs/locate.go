package featurejs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	locateModeCachePath = "cache_path"
	locateModeScan      = "scan"
)

// BundleCandidate 描述通过 marker 命中的 JS bundle 候选文件。
//
// Path 是候选文件的绝对或调用方传入目录下的完整路径。
// RelativePath 是候选文件相对于 commonDir 的路径，用于后续缓存。
// Score 是按照 strong/medium/weak marker 权重计算的分数。
// Size 是文件大小，用于扫描时按大文件优先处理哈希 bundle。
type BundleCandidate struct {
	Path         string
	RelativePath string
	Score        int
	Size         int64
}

// locateBundle 在 commonDir 下定位满足当前规则 strong marker 的目标 JS bundle。
//
// 若 cachedRelativePath 非空，会优先验证缓存路径对应文件是否存在且仍满足 strong marker。
// 缓存失效时会扫描 commonDir 下所有 .js 文件，并按文件大小降序逐个读取打分。
// 只有全部 strong marker 都命中的文件才会作为有效候选返回。
func (f *Feature) locateBundle(commonDir string, cachedRelativePath string) (BundleCandidate, error) {
	var cachedCandidate BundleCandidate
	hasCachedCandidate := false

	if normalizedCachedPath, ok := f.cachedCommonRelativePath(cachedRelativePath); ok {
		candidate, candidateOK, err := f.scoreCachedCandidate(commonDir, normalizedCachedPath)
		if err != nil {
			// 缓存路径只承担加速职责；读取失败或瞬时异常时应继续扫描其他 .js，而不是
			// 直接返回空结果，否则会把单个缓存文件的问题误判成“未找到 bundle”。
		} else if candidateOK {
			cachedCandidate = candidate
			hasCachedCandidate = true
		}
	}

	files, err := collectJSFiles(commonDir)
	if err != nil {
		if os.IsNotExist(err) {
			return BundleCandidate{}, nil
		}
		return BundleCandidate{}, err
	}

	candidates := make([]BundleCandidate, 0, len(files))
	if hasCachedCandidate {
		candidates = append(candidates, cachedCandidate)
	}

	for _, file := range files {
		if hasCachedCandidate && filepath.ToSlash(file.info.Name()) == cachedCandidate.RelativePath {
			continue
		}

		candidate, ok, err := f.scoreFileCandidate(commonDir, file.path, file.info)
		if err != nil {
			return BundleCandidate{}, err
		}
		if ok {
			candidates = append(candidates, candidate)
		}
	}

	return chooseBestCandidate(candidates), nil
}

// scoreCachedCandidate 读取并验证缓存相对路径指向的 JS 文件。
func (f *Feature) scoreCachedCandidate(commonDir string, cachedRelativePath string) (BundleCandidate, bool, error) {
	path := filepath.Join(commonDir, cachedRelativePath)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return BundleCandidate{}, false, nil
		}
		return BundleCandidate{}, false, err
	}
	if info.IsDir() {
		return BundleCandidate{}, false, nil
	}

	return f.scoreFileCandidate(commonDir, path, info)
}

// cachedCommonRelativePath 将上层缓存路径规整为相对 commonDir 的安全 JS 文件路径。
//
// 支持两种输入语义：
// 1. 直接相对 commonDir，例如 `a1.js`。
// 2. 相对安装根目录且带 BundleDir 前缀，例如 `app\webcontent\messenger-vc\common\a1.js`。
//
// 为避免缓存路径越界，绝对路径、Clean 后包含 `..` 的路径，以及非 `.js` 文件都会被
// 视为无效缓存。无效缓存不会向上返回错误，调用方会自动回退扫描。
func (f *Feature) cachedCommonRelativePath(cachedRelativePath string) (string, bool) {
	if cachedRelativePath == "" || filepath.IsAbs(cachedRelativePath) {
		return "", false
	}

	cleanedPath := filepath.Clean(filepath.FromSlash(cachedRelativePath))
	if cleanedPath == "." || !strings.EqualFold(filepath.Ext(cleanedPath), ".js") || containsParentSegment(cleanedPath) {
		return "", false
	}

	cleanedBundleDir := filepath.Clean(filepath.FromSlash(f.rule.BundleDir))
	bundlePrefix := cleanedBundleDir + string(filepath.Separator)
	if strings.EqualFold(cleanedPath, cleanedBundleDir) {
		return "", false
	}
	if strings.HasPrefix(strings.ToLower(cleanedPath), strings.ToLower(bundlePrefix)) {
		cleanedPath = cleanedPath[len(bundlePrefix):]
	}
	if cleanedPath == "." || filepath.IsAbs(cleanedPath) || containsParentSegment(cleanedPath) {
		return "", false
	}

	return filepath.ToSlash(cleanedPath), true
}

// containsParentSegment 判断规整后的相对路径是否包含会逃逸 commonDir 的 `..` 段。
//
// Windows 路径和 slash 路径都会先转换为当前平台分隔符，再逐段检查，避免 `a\..\b.js`
// 之类的缓存路径在 Join 后指向非预期位置。
func containsParentSegment(path string) bool {
	for _, segment := range strings.Split(path, string(filepath.Separator)) {
		if segment == ".." {
			return true
		}
	}

	return false
}

// scoreFileCandidate 读取单个 JS 文件，计算 marker 分数并检查 strong marker 是否全部命中。
func (f *Feature) scoreFileCandidate(commonDir string, path string, info os.FileInfo) (BundleCandidate, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return BundleCandidate{}, false, err
	}

	score, strongMatched := f.scoreContent(string(content))
	if !strongMatched {
		return BundleCandidate{}, false, nil
	}

	relativePath, err := filepath.Rel(commonDir, path)
	if err != nil {
		return BundleCandidate{}, false, err
	}

	return BundleCandidate{
		Path:         path,
		RelativePath: filepath.ToSlash(relativePath),
		Score:        score,
		Size:         info.Size(),
	}, true, nil
}

// scoreContent 按规则 marker 计算候选分数，并返回 strong marker 是否全部命中。
func (f *Feature) scoreContent(content string) (int, bool) {
	score := 0
	for _, marker := range f.rule.StrongMarkers {
		if !strings.Contains(content, marker) {
			return score, false
		}
		score += 100
	}

	for _, marker := range f.rule.MediumMarkers {
		if strings.Contains(content, marker) {
			score += 10
		}
	}
	for _, marker := range f.rule.WeakMarkers {
		if strings.Contains(content, marker) {
			score++
		}
	}

	return score, true
}

// chooseBestCandidate 从多个 strong marker 命中的候选里挑出唯一可接受的目标文件。
//
// 如果没有候选，返回零值；如果只有一个候选，直接返回；如果存在多个候选，则按
// score、文件大小、相对路径排序，并要求“最高分唯一”才能接受。最高分并列说明
// 当前规则无法唯一定位 bundle，此时返回零值让上层安全回落到 unknown。
func chooseBestCandidate(candidates []BundleCandidate) BundleCandidate {
	if len(candidates) == 0 {
		return BundleCandidate{}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].Size != candidates[j].Size {
			return candidates[i].Size > candidates[j].Size
		}

		return candidates[i].RelativePath < candidates[j].RelativePath
	})

	best := candidates[0]
	if candidates[1].Score == best.Score {
		return BundleCandidate{}
	}

	return best
}

type jsFileInfo struct {
	path string
	info os.FileInfo
}

// collectJSFiles 收集 commonDir 下的直接子级 .js 文件，并按大小降序排序。
func collectJSFiles(commonDir string) ([]jsFileInfo, error) {
	entries, err := os.ReadDir(commonDir)
	if err != nil {
		return nil, err
	}

	files := make([]jsFileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".js") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		files = append(files, jsFileInfo{
			path: filepath.Join(commonDir, entry.Name()),
			info: info,
		})
	}

	sort.SliceStable(files, func(i, j int) bool {
		return files[i].info.Size() > files[j].info.Size()
	})

	return files, nil
}
