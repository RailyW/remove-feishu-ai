package app

import (
	"path/filepath"
	"strings"

	"remove-feishu-ai/internal/config"
	"remove-feishu-ai/internal/logx"
)

// Env 汇总应用运行时需要共享给各功能模块的环境信息。
//
// ConfigPath 记录当前配置文件路径，便于后续功能在需要持久化配置时定位文件。
// ExeDir 记录当前可执行文件所在目录，便于解析相对资源路径。
// InstallDir 记录已解析或待使用的飞书安装目录。
// BackupRoot 记录事务备份根目录，供后续 app 组装 tx.Transaction 使用。
// StrictMode 记录当前运行是否采用严格事务模式；当前版本中，批量动作会遵守该
// 开关，而单项动作始终只处理对应功能。
// Config 保存当前加载后的配置快照。
// Logger 提供统一日志输出能力。
type Env struct {
	ConfigPath string
	ExeDir     string
	InstallDir string
	BackupRoot string
	StrictMode bool
	Config     config.Config
	Logger     *logx.Logger
}

// InstallPath 返回功能模块访问飞书安装目录时使用的路径。
//
// 该方法保持 Task 1 已建立的 feature.Env 接口契约，避免调用方依赖 Env
// 的字段布局；后续如果安装路径来源调整，只需要维护此方法的返回语义。
func (e Env) InstallPath() string {
	return e.InstallDir
}

// ResolveBackupRoot 将配置中的备份根目录解析为当前运行时实际使用的路径。
//
// 当配置值为空时会回退到 `backup`；当配置值是相对路径时，会固定解析到 exe 所在
// 目录下，满足“备份目录位于程序目录下 backup 文件夹”的产品约束。
func ResolveBackupRoot(exeDir string, configured string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		configured = "backup"
	}

	if filepath.IsAbs(configured) {
		return filepath.Clean(configured)
	}

	return filepath.Join(exeDir, configured)
}
