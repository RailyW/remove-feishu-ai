// Package search 提供面向大文件的二进制模式搜索工具。
//
// 本包只依赖 io.ReaderAt 分段读取数据，不会为了搜索把整个文件一次性读入内存。
package search

import "errors"

// ErrEmptyPattern 表示调用方传入了空模式串。
//
// 空模式串在“查找所有位置”语义下会匹配无限多或所有偏移，容易掩盖调用错误，
// 因此所有公开搜索 API 都会显式拒绝该输入。
var ErrEmptyPattern = errors.New("search pattern must not be empty")

// ErrInvalidChunkSize 表示调用方传入的分块大小不是正数。
//
// 搜索器依赖正数分块大小推进 ReaderAt 读取窗口，零值或负值无法形成有效进度。
var ErrInvalidChunkSize = errors.New("search chunk size must be greater than zero")

// buildBMHSkipTable 为 Boyer-Moore-Horspool 搜索算法构造跳转表。
//
// 表中每个字节默认跳过整个 pattern 长度；pattern 中除最后一个字节外的字符会覆盖
// 自己的跳转距离。调用方必须保证 pattern 非空。
func buildBMHSkipTable(pattern []byte) [256]int {
	var table [256]int
	patternLength := len(pattern)

	for i := range table {
		table[i] = patternLength
	}
	for i := 0; i < patternLength-1; i++ {
		table[pattern[i]] = patternLength - 1 - i
	}

	return table
}

// findAllInBlock 在内存块内查找 pattern 的所有偏移。
//
// 返回值是相对于 block 起点的升序偏移。公开 API 负责把这些偏移换算为文件绝对偏移。
func findAllInBlock(block []byte, pattern []byte, table [256]int) []int {
	if len(pattern) > len(block) {
		return nil
	}

	var matches []int
	patternLength := len(pattern)
	for cursor := 0; cursor <= len(block)-patternLength; {
		compareIndex := patternLength - 1
		for compareIndex >= 0 && block[cursor+compareIndex] == pattern[compareIndex] {
			compareIndex--
		}
		if compareIndex < 0 {
			matches = append(matches, cursor)
			cursor++
			continue
		}
		cursor += table[block[cursor+patternLength-1]]
	}

	return matches
}
