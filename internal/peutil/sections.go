// Package peutil 提供 PE 文件结构读取相关工具。
//
// 当前模块只暴露可执行节区的文件偏移范围，用于后续对 frame.dll 等 PE 文件进行安全、
// 定界的二进制扫描。
package peutil

import (
	"debug/pe"
	"sort"
)

const imageScnMemExecute = 0x20000000

// Range 表示文件中的半开字节范围 [Start, End)。
//
// Start 和 End 均为文件偏移，而不是虚拟地址；End 指向范围结束后的第一个字节。
type Range struct {
	Start int64
	End   int64
}

// ExecutableRanges 返回 PE 文件内所有带 IMAGE_SCN_MEM_EXECUTE 标志的 section 文件范围。
//
// path 指向待解析的 PE 文件。函数使用标准库 debug/pe 读取节区表，只根据 section 的
// Characteristics 判断是否可执行。返回范围采用 section.Offset 与 section.Size 计算，
// 并按 Start 升序排列。没有可执行节区时返回空 slice 和 nil 错误。
func ExecutableRanges(path string) ([]Range, error) {
	file, err := pe.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	ranges := make([]Range, 0)
	for _, section := range file.Sections {
		if section.Characteristics&imageScnMemExecute == 0 {
			continue
		}
		start := int64(section.Offset)
		end := start + int64(section.Size)
		ranges = append(ranges, Range{Start: start, End: end})
	}

	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start < ranges[j].Start
	})

	return ranges, nil
}
