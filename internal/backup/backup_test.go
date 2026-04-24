package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCreateTransactionLayoutCreatesTimestampedDirectories 验证事务目录布局
// 固定为 `<backupRoot>\YYYYMMDD-HHMMSS\files`，方便后续 manifest 和备份文件
// 在稳定位置落盘。
func TestCreateTransactionLayoutCreatesTimestampedDirectories(t *testing.T) {
	t.Parallel()

	backupRoot := filepath.Join(t.TempDir(), "backup-root")
	now := time.Date(2026, 4, 23, 18, 30, 45, 0, time.UTC)

	root, filesDir, err := CreateTransactionLayout(backupRoot, now)
	if err != nil {
		t.Fatalf("CreateTransactionLayout() error = %v", err)
	}

	wantRoot := filepath.Join(backupRoot, "20260423-183045")
	if root != wantRoot {
		t.Fatalf("root = %q, want %q", root, wantRoot)
	}

	wantFilesDir := filepath.Join(wantRoot, "files")
	if filesDir != wantFilesDir {
		t.Fatalf("filesDir = %q, want %q", filesDir, wantFilesDir)
	}

	info, err := os.Stat(filesDir)
	if err != nil {
		t.Fatalf("os.Stat(%q) error = %v", filesDir, err)
	}
	if !info.IsDir() {
		t.Fatalf("%q should be a directory", filesDir)
	}
}

// TestCreateTransactionLayoutAddsSuffixWhenTimestampDirectoryExists 验证同一秒目录
// 已存在时，会自动落到带 `-NN` 后缀的新事务目录，避免覆盖已有事务。
func TestCreateTransactionLayoutAddsSuffixWhenTimestampDirectoryExists(t *testing.T) {
	t.Parallel()

	backupRoot := filepath.Join(t.TempDir(), "backup-root")
	now := time.Date(2026, 4, 23, 18, 30, 45, 0, time.UTC)
	existingRoot := filepath.Join(backupRoot, "20260423-183045")
	if err := os.MkdirAll(filepath.Join(existingRoot, "files"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(existingRoot) error = %v", err)
	}

	root, filesDir, err := CreateTransactionLayout(backupRoot, now)
	if err != nil {
		t.Fatalf("CreateTransactionLayout() error = %v", err)
	}

	wantRoot := filepath.Join(backupRoot, "20260423-183045-01")
	if root != wantRoot {
		t.Fatalf("root = %q, want %q", root, wantRoot)
	}
	if filesDir != filepath.Join(wantRoot, "files") {
		t.Fatalf("filesDir = %q, want %q", filesDir, filepath.Join(wantRoot, "files"))
	}
}

// TestSanitizeRelativePathUsesWindowsSafeFilename 验证相对路径会被压平成稳定且
// 适合 Windows 文件系统使用的备份文件名。
func TestSanitizeRelativePathUsesWindowsSafeFilename(t *testing.T) {
	t.Parallel()

	if got, want := SanitizeRelativePath(`app\frame.dll`), "app_frame.dll.bak"; got != want {
		t.Fatalf("SanitizeRelativePath(frame) = %q, want %q", got, want)
	}

	if got, want := SanitizeRelativePath(`app/webcontent/messenger-vc/common/a.js`), "app_webcontent_messenger-vc_common_a.js.bak"; got != want {
		t.Fatalf("SanitizeRelativePath(bundle) = %q, want %q", got, want)
	}
}

// TestCopyIntoTransactionAndRestore 验证文件复制到事务目录后会记录 manifest 所需
// 的元信息，并且 RestoreItem 可以把目标文件恢复为备份内容。
func TestCopyIntoTransactionAndRestore(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	txRoot := filepath.Join(tempDir, "backup-root", "20260423-183045")
	filesDir := filepath.Join(txRoot, "files")
	sourcePath := filepath.Join(tempDir, "install", "app", "frame.dll")
	relativePath := `app\frame.dll`
	originalContent := []byte("original-frame-content")

	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(filesDir) error = %v", err)
	}
	if err := os.WriteFile(sourcePath, originalContent, 0o644); err != nil {
		t.Fatalf("os.WriteFile(source) error = %v", err)
	}

	record, restore, err := CopyIntoTransaction(txRoot, filesDir, relativePath, sourcePath)
	if err != nil {
		t.Fatalf("CopyIntoTransaction() error = %v", err)
	}

	if record.RelativePath != relativePath {
		t.Fatalf("record.RelativePath = %q, want %q", record.RelativePath, relativePath)
	}
	if record.BackupPath != filepath.Join("files", "app_frame.dll.bak") {
		t.Fatalf("record.BackupPath = %q, want %q", record.BackupPath, filepath.Join("files", "app_frame.dll.bak"))
	}
	if record.SizeBefore != int64(len(originalContent)) {
		t.Fatalf("record.SizeBefore = %d, want %d", record.SizeBefore, len(originalContent))
	}

	backupPath := filepath.Join(txRoot, record.BackupPath)
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("os.ReadFile(backupPath) error = %v", err)
	}
	if string(backupData) != string(originalContent) {
		t.Fatalf("backup content = %q, want %q", string(backupData), string(originalContent))
	}

	if restore.TargetPath != sourcePath {
		t.Fatalf("restore.TargetPath = %q, want %q", restore.TargetPath, sourcePath)
	}
	if restore.BackupPath != backupPath {
		t.Fatalf("restore.BackupPath = %q, want %q", restore.BackupPath, backupPath)
	}

	if err := os.WriteFile(sourcePath, []byte("mutated-content"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(mutated source) error = %v", err)
	}

	if err := restore.Restore(); err != nil {
		t.Fatalf("restore.Restore() error = %v", err)
	}

	restoredData, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("os.ReadFile(restored source) error = %v", err)
	}
	if string(restoredData) != string(originalContent) {
		t.Fatalf("restored content = %q, want %q", string(restoredData), string(originalContent))
	}
}

// TestCopyIntoTransactionAvoidsBackupNameCollision 验证不同 relativePath 在清洗后
// 产生相同基础文件名时，后续备份会生成带短 hash 的稳定新文件名，避免覆盖。
func TestCopyIntoTransactionAvoidsBackupNameCollision(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	txRoot := filepath.Join(tempDir, "backup-root", "20260423-183045")
	filesDir := filepath.Join(txRoot, "files")
	firstSourcePath := filepath.Join(tempDir, "install", "first.js")
	secondSourcePath := filepath.Join(tempDir, "install", "second.js")
	firstContent := []byte("first-content")
	secondContent := []byte("second-content")

	if err := os.MkdirAll(filepath.Dir(firstSourcePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(source dir) error = %v", err)
	}
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(filesDir) error = %v", err)
	}
	if err := os.WriteFile(firstSourcePath, firstContent, 0o644); err != nil {
		t.Fatalf("os.WriteFile(first source) error = %v", err)
	}
	if err := os.WriteFile(secondSourcePath, secondContent, 0o644); err != nil {
		t.Fatalf("os.WriteFile(second source) error = %v", err)
	}

	firstRecord, _, err := CopyIntoTransaction(txRoot, filesDir, "app/a_b.js", firstSourcePath)
	if err != nil {
		t.Fatalf("CopyIntoTransaction(first) error = %v", err)
	}
	secondRecord, _, err := CopyIntoTransaction(txRoot, filesDir, "app/a/b.js", secondSourcePath)
	if err != nil {
		t.Fatalf("CopyIntoTransaction(second) error = %v", err)
	}

	if firstRecord.BackupPath == secondRecord.BackupPath {
		t.Fatalf("BackupPath collision: both records use %q", firstRecord.BackupPath)
	}
	if firstRecord.BackupPath != filepath.Join("files", "app_a_b.js.bak") {
		t.Fatalf("first BackupPath = %q, want %q", firstRecord.BackupPath, filepath.Join("files", "app_a_b.js.bak"))
	}

	firstBackupData, err := os.ReadFile(filepath.Join(txRoot, firstRecord.BackupPath))
	if err != nil {
		t.Fatalf("os.ReadFile(first backup) error = %v", err)
	}
	if string(firstBackupData) != string(firstContent) {
		t.Fatalf("first backup content = %q, want %q", string(firstBackupData), string(firstContent))
	}

	secondBackupData, err := os.ReadFile(filepath.Join(txRoot, secondRecord.BackupPath))
	if err != nil {
		t.Fatalf("os.ReadFile(second backup) error = %v", err)
	}
	if string(secondBackupData) != string(secondContent) {
		t.Fatalf("second backup content = %q, want %q", string(secondBackupData), string(secondContent))
	}
}

// TestWriteManifestWritesExpectedJSONKeys 验证 manifest.json 的顶层字段名和 files
// 内部字段名符合约定，而不是仅做结构体 round-trip。
func TestWriteManifestWritesExpectedJSONKeys(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifest := Manifest{
		ToolVersion: "0.1.0-dev",
		CreatedAt:   "2026-04-23T18:30:45Z",
		InstallPath: `E:\Feishu`,
		Operation:   "remove",
		Status:      "in_progress",
		Files: []FileRecord{
			{
				Feature:      "frame-patch",
				Kind:         "binary",
				RelativePath: `app\frame.dll`,
				BackupPath:   filepath.Join("files", "app_frame.dll.bak"),
				SizeBefore:   128,
				StateBefore:  "original",
				StateAfter:   "patched",
			},
		},
	}

	if err := WriteManifest(root, manifest); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(manifest.json) error = %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal(top-level) error = %v", err)
	}

	for _, key := range []string{"tool_version", "created_at", "install_path", "operation", "status", "files"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("top-level key %q missing in manifest.json", key)
		}
	}

	var files []map[string]json.RawMessage
	if err := json.Unmarshal(raw["files"], &files); err != nil {
		t.Fatalf("json.Unmarshal(files) error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(files))
	}

	for _, key := range []string{"feature", "kind", "relative_path", "backup_path", "size_before", "state_before", "state_after"} {
		if _, ok := files[0][key]; !ok {
			t.Fatalf("files[0] key %q missing in manifest.json", key)
		}
	}

	got, err := ReadManifest(root)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if got.ToolVersion != manifest.ToolVersion || got.CreatedAt != manifest.CreatedAt || got.InstallPath != manifest.InstallPath || got.Operation != manifest.Operation || got.Status != manifest.Status {
		t.Fatalf("ReadManifest() = %#v, want core fields %#v", got, manifest)
	}
	if len(got.Files) != 1 || got.Files[0].BackupPath != manifest.Files[0].BackupPath {
		t.Fatalf("ReadManifest() files = %#v, want %#v", got.Files, manifest.Files)
	}
}

// TestWriteManifestOmitsEmptyOptionalFields 验证 FileRecord 的可选字段在为空时不会写入
// JSON，确保 omitempty 标签实际生效。
func TestWriteManifestOmitsEmptyOptionalFields(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifest := Manifest{
		ToolVersion: "0.1.0-dev",
		CreatedAt:   "2026-04-23T18:30:45Z",
		InstallPath: `E:\Feishu`,
		Operation:   "restore",
		Status:      "completed",
		Files: []FileRecord{
			{
				RelativePath: `app\frame.dll`,
				BackupPath:   filepath.Join("files", "app_frame.dll.bak"),
				SizeBefore:   256,
			},
		},
	}

	if err := WriteManifest(root, manifest); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(manifest.json) error = %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal(top-level) error = %v", err)
	}

	var files []map[string]json.RawMessage
	if err := json.Unmarshal(raw["files"], &files); err != nil {
		t.Fatalf("json.Unmarshal(files) error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(files))
	}

	for _, key := range []string{"feature", "kind", "state_before", "state_after"} {
		if _, ok := files[0][key]; ok {
			t.Fatalf("files[0] key %q should be omitted when empty", key)
		}
	}
}
