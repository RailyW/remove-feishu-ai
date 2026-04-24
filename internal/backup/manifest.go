package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// FileRecord 描述事务 manifest 中单个文件的备份记录。
//
// Feature 与 Kind 供后续补丁模块标记来源功能和文件类型；当前备份层只维护
// 文件路径、备份路径、原始大小和可选状态字段。RelativePath 是目标文件相对
// 安装目录的路径，BackupPath 是备份文件相对事务根目录的路径。
type FileRecord struct {
	Feature      string `json:"feature,omitempty"`
	Kind         string `json:"kind,omitempty"`
	RelativePath string `json:"relative_path"`
	BackupPath   string `json:"backup_path"`
	SizeBefore   int64  `json:"size_before"`
	StateBefore  string `json:"state_before,omitempty"`
	StateAfter   string `json:"state_after,omitempty"`
}

// Manifest 描述一次补丁事务的持久化审计信息。
//
// ToolVersion 记录生成 manifest 的工具版本；CreatedAt 使用 RFC3339 字符串；
// InstallPath 和 Operation 来自调用方；Status 表示事务生命周期状态；Files
// 保存本次事务已备份的文件列表。
type Manifest struct {
	ToolVersion string       `json:"tool_version"`
	CreatedAt   string       `json:"created_at"`
	InstallPath string       `json:"install_path"`
	Operation   string       `json:"operation"`
	Status      string       `json:"status"`
	Files       []FileRecord `json:"files"`
}

// WriteManifest 将 manifest 以格式化 JSON 写入事务根目录下的 manifest.json。
//
// 该函数会确保事务根目录存在，并在 JSON 末尾追加换行，便于人工查看和后续
// 测试使用稳定文本格式读取。
func WriteManifest(root string, manifest Manifest) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o644)
}

// ReadManifest 从事务根目录读取 manifest.json。
//
// 该函数主要供测试和后续状态检查使用，不会对状态机做额外解释，只负责 JSON
// 到 Manifest 结构的反序列化。
func ReadManifest(root string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}

	return manifest, nil
}
