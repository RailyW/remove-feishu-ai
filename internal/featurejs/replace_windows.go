//go:build windows
// +build windows

package featurejs

import (
	"errors"
	"syscall"
	"unsafe"
)

var (
	modKernel32     = syscall.NewLazyDLL("kernel32.dll")
	procMoveFileExW = modKernel32.NewProc("MoveFileExW")
)

const (
	// moveFileReplaceExisting 允许 MoveFileExW 在目标已存在时直接覆盖目标文件。
	moveFileReplaceExisting = 0x1

	// moveFileWriteThrough 要求系统在返回前尽量把文件系统元数据落盘。
	moveFileWriteThrough = 0x8
)

// replaceFileAtomically 使用 Windows MoveFileExW 原子覆盖目标文件。
//
// 只要系统返回替换失败，原目标文件就仍然保留在原地；上层 defer 会清理临时文件。
// 这样能够把失败影响限制在“本次写入未生效”，而不是“目标文件直接消失”。
func replaceFileAtomically(tmpPath string, targetPath string) error {
	tmpPtr, err := syscall.UTF16PtrFromString(tmpPath)
	if err != nil {
		return err
	}
	targetPtr, err := syscall.UTF16PtrFromString(targetPath)
	if err != nil {
		return err
	}

	result, _, callErr := procMoveFileExW.Call(
		uintptr(unsafe.Pointer(tmpPtr)),
		uintptr(unsafe.Pointer(targetPtr)),
		uintptr(moveFileReplaceExisting|moveFileWriteThrough),
	)
	if result != 0 {
		return nil
	}
	if callErr != syscall.Errno(0) {
		return callErr
	}

	return errors.New("MoveFileExW failed")
}
