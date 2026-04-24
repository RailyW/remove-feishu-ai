// Package install 提供飞书安装目录解析与结构校验能力。
//
// 本文件只校验调用方明确传入的路径或回退路径，不会主动扫描或访问真实
// Windows 默认安装目录，避免测试和普通运行过程意外触碰用户机器上的飞书。
package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrInstallPathInvalid 表示传入路径不是当前工具可处理的飞书安装目录。
var ErrInstallPathInvalid = errors.New("install path invalid")

// Resolve 解析并校验飞书安装目录。
//
// input 会先去除首尾空白；如果 input 为空，则使用 fallback。最终路径会
// 经过 filepath.Clean 规范化。校验条件包括 app/frame.dll 文件存在，以及
// app/webcontent/messenger-vc/common 目录存在。校验成功时返回规范化后的路径；
// 校验失败时返回可通过 errors.Is 判断的 ErrInstallPathInvalid。
func Resolve(input string, fallback string) (string, error) {
	candidate := strings.TrimSpace(input)
	if candidate == "" {
		candidate = strings.TrimSpace(fallback)
	}
	if candidate == "" {
		return "", fmt.Errorf("%w: empty path", ErrInstallPathInvalid)
	}

	cleaned := filepath.Clean(candidate)
	if err := requireFile(filepath.Join(cleaned, "app", "frame.dll")); err != nil {
		return "", err
	}
	if err := requireDir(filepath.Join(cleaned, "app", "webcontent", "messenger-vc", "common")); err != nil {
		return "", err
	}

	return cleaned, nil
}

// requireFile 校验 path 必须存在且是普通文件或文件类节点。
func requireFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInstallPathInvalid, path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%w: %s is directory", ErrInstallPathInvalid, path)
	}

	return nil
}

// requireDir 校验 path 必须存在且是目录。
func requireDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInstallPathInvalid, path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not directory", ErrInstallPathInvalid, path)
	}

	return nil
}
