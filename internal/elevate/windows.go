//go:build windows
// +build windows

// Package elevate 提供 Windows 平台下的管理员权限检测与 UAC 提权重启能力。
//
// 本文件只处理当前进程是否已提升权限、如何将参数安全传递给提权后的
// 新进程，以及在需要提权时如何通过 ShellExecuteW 触发 UAC。该包不负
// 责真实业务流程，只为 internal/app 提供权限保障入口。
package elevate

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const shellExecuteSuccessThreshold = 32

var (
	modShell32       = syscall.NewLazyDLL("shell32.dll")
	procShellExecute = modShell32.NewProc("ShellExecuteW")
)

// adminDeps 描述 EnsureAdmin 所依赖的最小权限判断与提权能力集合。
//
// 该结构只在包内使用，用于把“启动即提权”的产品行为与可替换的实现细节
// 解耦。导出的 EnsureAdmin 固定传入默认依赖，而测试直接调用 ensureAdmin
// 并注入桩函数，从而避免修改包级全局变量。
type adminDeps struct {
	isAdmin  func() (bool, error)
	relaunch func([]string) error
}

// tokenElevation 对应 Win32 TOKEN_ELEVATION 结构。
//
// TokenIsElevated 非零表示当前访问令牌已经完成 UAC 提升。
type tokenElevation struct {
	TokenIsElevated uint32
}

// shellExecuteError 描述 ShellExecuteW 返回值在 32 及以下时的失败状态。
//
// 该错误保留原始返回码，便于调用方定位“文件未找到”“访问被拒绝”等
// ShellExecuteW 级别失败。
type shellExecuteError struct {
	code       uintptr
	underlying error
}

// Error 返回适合日志与上层错误传播的错误文本。
func (e shellExecuteError) Error() string {
	if e.underlying != nil {
		return fmt.Sprintf("ShellExecuteW failed with code %d: %v", e.code, e.underlying)
	}

	return fmt.Sprintf("ShellExecuteW failed with code %d", e.code)
}

// Unwrap 返回 ShellExecuteW 调用附带的底层错误。
func (e shellExecuteError) Unwrap() error {
	return e.underlying
}

// buildElevatedArgs 返回参数切片的独立副本。
//
// 该函数保持参数顺序和值不变，并保证返回切片与输入切片底层数组分离，
// 避免后续提权流程或测试中的修改反向污染原始参数。
func buildElevatedArgs(args []string) []string {
	return append([]string(nil), args...)
}

// quoteArgs 按 Windows 命令行规则转义并拼接参数。
//
// ShellExecuteW 的 lpParameters 接收单个命令行字符串，因此需要将多个独
// 立参数重新编码为 Windows 进程创建 API 可逆解析的形式。这里直接复用
// 标准库 syscall.EscapeArg，避免自行维护引号与反斜杠规则。
func quoteArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}

	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, syscall.EscapeArg(arg))
	}

	return strings.Join(quoted, " ")
}

// IsAdmin 判断当前进程是否已经以管理员权限运行。
//
// 函数通过查询当前进程访问令牌的 TokenElevation 信息判断是否已完成 UAC
// 提升。返回 false 且 error 为 nil 表示当前进程未提升，并非调用失败。
func IsAdmin() (bool, error) {
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return false, err
	}
	defer token.Close()

	var elevation tokenElevation
	var returnedLen uint32
	err = syscall.GetTokenInformation(
		token,
		syscall.TokenElevation,
		(*byte)(unsafe.Pointer(&elevation)),
		uint32(unsafe.Sizeof(elevation)),
		&returnedLen,
	)
	if err != nil {
		return false, err
	}

	return elevation.TokenIsElevated != 0, nil
}

// RelaunchAsAdmin 通过 ShellExecuteW 的 runas verb 重新启动当前可执行文件。
//
// args 会先按 Windows 规则转义为单个命令行字符串，再交给 ShellExecuteW
// 作为提升后新进程的参数。该函数只负责发起重启请求，不等待子进程退出。
func RelaunchAsAdmin(args []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	verbPtr, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	exePtr, err := syscall.UTF16PtrFromString(exePath)
	if err != nil {
		return err
	}

	parameters := quoteArgs(args)
	var parametersPtr *uint16
	if parameters != "" {
		parametersPtr, err = syscall.UTF16PtrFromString(parameters)
		if err != nil {
			return err
		}
	}

	result, _, callErr := procShellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(exePtr)),
		uintptr(unsafe.Pointer(parametersPtr)),
		0,
		uintptr(1),
	)
	if result <= shellExecuteSuccessThreshold {
		var underlying error
		if callErr != syscall.Errno(0) {
			underlying = callErr
		}

		return shellExecuteError{
			code:       result,
			underlying: underlying,
		}
	}

	return nil
}

// EnsureAdmin 确保当前流程在管理员权限下继续执行。
//
// 当当前进程已提升时，函数返回 (false, nil)，调用方应继续原有流程。
// 当当前进程未提升时，函数会发起一次 UAC 提权重启；如果重启请求成功，
// 返回 (true, nil)，调用方应立即结束当前原进程。
func EnsureAdmin(args []string) (relaunched bool, err error) {
	return ensureAdmin(args, adminDeps{
		isAdmin:  IsAdmin,
		relaunch: RelaunchAsAdmin,
	})
}

// ensureAdmin 在固定产品行为下执行管理员检测与按需提权。
//
// 该私有函数集中封装决策逻辑：只要检测到当前进程不是管理员，就立即使用
// 注入的 relaunch 实现重新启动自身。因此产品层面仍然是“启动即检测并自
// 动 UAC 提权”，只是把 Win32 细节替换成了可注入依赖，便于单元测试。
func ensureAdmin(args []string, deps adminDeps) (relaunched bool, err error) {
	admin, err := deps.isAdmin()
	if err != nil {
		return false, err
	}
	if admin {
		return false, nil
	}

	if err := deps.relaunch(buildElevatedArgs(args)); err != nil {
		return false, err
	}

	return true, nil
}
