package search

import "io"

// FindAllInWindow 在 reader 的半开区间 [start, end) 内查找 pattern 的全部出现位置。
//
// 搜索只返回完全落在窗口内部的命中，即命中起点 >= start 且命中终点 <= end。读取时按
// chunkSize 分块，并在相邻块之间保留 len(pattern)-1 字节重叠区，确保跨块命中不会漏掉。
func FindAllInWindow(r io.ReaderAt, start, end int64, pattern []byte, chunkSize int) ([]int64, error) {
	if len(pattern) == 0 {
		return nil, ErrEmptyPattern
	}
	if chunkSize <= 0 {
		return nil, ErrInvalidChunkSize
	}
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}

	patternLength := int64(len(pattern))
	if end-start < patternLength {
		return []int64{}, nil
	}

	table := buildBMHSkipTable(pattern)
	offsets := make([]int64, 0)
	overlapLength := len(pattern) - 1
	previousTail := make([]byte, 0, overlapLength)
	readOffset := start

	for readOffset < end {
		bytesRemaining := end - readOffset
		bytesToRead := int64(chunkSize)
		if bytesToRead > bytesRemaining {
			bytesToRead = bytesRemaining
		}

		chunk := make([]byte, bytesToRead)
		bytesRead, err := r.ReadAt(chunk, readOffset)
		if err != nil && err != io.EOF {
			return nil, err
		}
		if bytesRead == 0 {
			break
		}
		chunk = chunk[:bytesRead]

		blockStart := readOffset - int64(len(previousTail))
		block := make([]byte, 0, len(previousTail)+len(chunk))
		block = append(block, previousTail...)
		block = append(block, chunk...)

		for _, relativeOffset := range findAllInBlock(block, pattern, table) {
			absoluteOffset := blockStart + int64(relativeOffset)
			if absoluteOffset < start {
				continue
			}
			if absoluteOffset+patternLength > end {
				continue
			}
			if len(offsets) > 0 && offsets[len(offsets)-1] == absoluteOffset {
				continue
			}
			offsets = append(offsets, absoluteOffset)
		}

		if overlapLength > 0 {
			if len(block) <= overlapLength {
				previousTail = append(previousTail[:0], block...)
			} else {
				previousTail = append(previousTail[:0], block[len(block)-overlapLength:]...)
			}
		}

		readOffset += int64(bytesRead)
		if err == io.EOF {
			break
		}
	}

	return offsets, nil
}
