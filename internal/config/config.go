// Package config 提供补丁工具的本地配置读写能力。
//
// 本文件包含配置文件的结构定义、默认值生成、JSON 读取补齐以及
// 格式化写回逻辑。该包不解析安装目录有效性，安装目录校验由
// internal/install 包负责，从而保持配置持久化与环境探测职责分离。
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	// defaultLastInstallPath 是 Windows 上飞书默认安装目录的配置默认值。
	//
	// 这里仅作为配置文件初始值使用，不会主动访问或探测该真实目录。
	defaultLastInstallPath = `C:\Program Files\Feishu`

	// defaultBackupRoot 是备份文件根目录的相对路径默认值。
	defaultBackupRoot = "backup"
)

// Config 描述补丁工具需要跨运行持久化的用户配置与缓存信息。
//
// LastInstallPath 保存用户上一次选择或确认的飞书安装目录。
// BackupRoot 保存备份数据写入位置，可以是相对路径或绝对路径。
// StrictModeDefault 保存严格模式的默认开关，供命令行或界面初始化使用。
// LastSuccessOffsets 缓存二进制替换在文件中的历史命中偏移，后续 frame.dll
// 处理可基于文件大小和修改时间判断缓存是否仍可复用。
// LastBundleRelativePath 保存上一次成功处理的 JS bundle 相对路径。
// LastRuleHits 缓存规则命中文件的元信息，供后续快速判断文件是否变化。
type Config struct {
	LastInstallPath        string                  `json:"last_install_path"`
	BackupRoot             string                  `json:"backup_root"`
	StrictModeDefault      bool                    `json:"strict_mode_default"`
	LastSuccessOffsets     map[string]OffsetCache  `json:"last_success_offsets,omitempty"`
	LastBundleRelativePath string                  `json:"last_bundle_relative_path,omitempty"`
	LastRuleHits           map[string]RuleHitCache `json:"last_rule_hits,omitempty"`
}

// OffsetCache 描述一个目标文件中已成功匹配的字节偏移缓存。
//
// OldPatternOffsets 记录旧模式命中的偏移列表。
// NewPatternOffsets 记录新模式命中的偏移列表。
// FileSize 与 MTime 共同描述缓存生成时的文件状态，用于后续判断缓存是否过期。
type OffsetCache struct {
	OldPatternOffsets []int64 `json:"old_pattern_offsets"`
	NewPatternOffsets []int64 `json:"new_pattern_offsets"`
	FileSize          int64   `json:"file_size"`
	MTime             string  `json:"mtime"`
}

// RuleHitCache 描述规则命中文件的轻量元信息缓存。
//
// FileSize 与 MTime 用于后续判断对应规则命中文件是否发生变化。
type RuleHitCache struct {
	FileSize int64  `json:"file_size"`
	MTime    string `json:"mtime"`
}

// Default 返回一份完整可用的默认配置。
//
// 返回值中的 map 字段始终初始化为非 nil，调用方可以直接写入缓存项，
// 不需要额外判断 nil。
func Default() Config {
	return Config{
		LastInstallPath:    defaultLastInstallPath,
		BackupRoot:         defaultBackupRoot,
		StrictModeDefault:  true,
		LastSuccessOffsets: make(map[string]OffsetCache),
		LastRuleHits:       make(map[string]RuleHitCache),
	}
}

// LoadOrCreate 从 path 指向的 JSON 文件加载配置。
//
// 当文件不存在时，该函数会创建父目录、写入默认配置并返回默认配置。
// 当文件存在时，该函数读取 JSON，并对旧版本配置中缺失的字段补齐默认值；
// map 字段会被补齐为非 nil，便于后续调用方直接写入缓存。
func LoadOrCreate(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := Default()
			if saveErr := Save(path, cfg); saveErr != nil {
				return Config{}, saveErr
			}
			return cfg, nil
		}

		return Config{}, err
	}

	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	applyDefaults(&cfg)

	return cfg, nil
}

// Save 将 cfg 以稳定、易读的格式化 JSON 写入 path。
//
// 该函数会先创建配置文件父目录，再以 0644 权限写入文件。写入前会补齐
// 必要默认值，避免调用方传入零值 Config 时生成不可直接使用的配置文件。
func Save(path string, cfg Config) error {
	applyDefaults(&cfg)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return os.WriteFile(path, data, 0o644)
}

// applyDefaults 对已有配置执行向前兼容补齐。
//
// 字符串字段使用空字符串作为“缺失”判断；布尔字段无法区分旧配置缺失与
// 用户显式设置 false，因此保留 JSON 读取后的值，只由 Default 提供 true
// 默认值。map 字段始终补齐为非 nil。
func applyDefaults(cfg *Config) {
	if cfg.LastInstallPath == "" {
		cfg.LastInstallPath = defaultLastInstallPath
	}
	if cfg.BackupRoot == "" {
		cfg.BackupRoot = defaultBackupRoot
	}
	if cfg.LastSuccessOffsets == nil {
		cfg.LastSuccessOffsets = make(map[string]OffsetCache)
	}
	if cfg.LastRuleHits == nil {
		cfg.LastRuleHits = make(map[string]RuleHitCache)
	}
}
