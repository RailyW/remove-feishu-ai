package search

import (
	"bytes"
	"io"
)

// FindAllInReader 在 reader 的 [0, size) 文件范围内查找 pattern 的全部出现位置。
//
// size 是可搜索数据长度，pattern 不能为空，chunkSize 必须为正数。实现会委托给
// FindAllInWindow，并保持结果以文件偏移升序返回。
func FindAllInReader(r io.ReaderAt, size int64, pattern []byte, chunkSize int) ([]int64, error) {
	return FindAllInWindow(r, 0, size, pattern, chunkSize)
}

// VerifyAt 验证 reader 在指定文件偏移 offset 处是否完整匹配 pattern。
//
// 如果 offset 后剩余数据不足以读取完整 pattern，函数返回 false 和 ReaderAt 返回的错误；
// 调用方可以用 errors.Is(err, io.EOF) 判断尾部短读。pattern 为空时返回 ErrEmptyPattern。
func VerifyAt(r io.ReaderAt, offset int64, pattern []byte) (bool, error) {
	if len(pattern) == 0 {
		return false, ErrEmptyPattern
	}
	if offset < 0 {
		return false, nil
	}

	buffer := make([]byte, len(pattern))
	read, err := r.ReadAt(buffer, offset)
	if read != len(pattern) {
		return false, err
	}
	if err != nil && err != io.EOF {
		return false, err
	}

	return bytes.Equal(buffer, pattern), nil
}
