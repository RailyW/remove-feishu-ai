//go:build !windows
// +build !windows

package featurejs

import "os"

// replaceFileAtomically 在非 Windows 平台上使用 os.Rename 完成原子替换。
//
// 该仓库当前产品目标是 Windows，但提供这个回退实现可以保持包在其他平台上仍具备
// 基本可编译性，避免把 Windows API 直接泄漏到通用文件中。
func replaceFileAtomically(tmpPath string, targetPath string) error {
	return os.Rename(tmpPath, targetPath)
}
