package tx

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"remove-feishu-ai/internal/backup"
)

// TestNewCreatesManifestWithInProgressStatus 验证新事务会创建初始 manifest，状态
// 为 in_progress，且带有调用方提供的安装目录和操作类型。
func TestNewCreatesManifestWithInProgressStatus(t *testing.T) {
	t.Parallel()

	backupRoot := filepath.Join(t.TempDir(), "backup-root")
	installPath := `E:\Feishu`
	operation := "remove"

	tx, err := New(backupRoot, installPath, operation)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	manifest, err := backup.ReadManifest(tx.Root())
	if err != nil {
		t.Fatalf("backup.ReadManifest() error = %v", err)
	}

	if manifest.ToolVersion != toolVersion {
		t.Fatalf("ToolVersion = %q, want %q", manifest.ToolVersion, toolVersion)
	}
	if _, err := time.Parse(time.RFC3339, manifest.CreatedAt); err != nil {
		t.Fatalf("CreatedAt = %q, should be RFC3339: %v", manifest.CreatedAt, err)
	}
	if manifest.InstallPath != installPath {
		t.Fatalf("InstallPath = %q, want %q", manifest.InstallPath, installPath)
	}
	if manifest.Operation != operation {
		t.Fatalf("Operation = %q, want %q", manifest.Operation, operation)
	}
	if manifest.Status != statusInProgress {
		t.Fatalf("Status = %q, want %q", manifest.Status, statusInProgress)
	}
	if len(manifest.Files) != 0 {
		t.Fatalf("len(Files) = %d, want 0", len(manifest.Files))
	}
}

// TestBackupFileThenRollbackRestoresOriginalContent 验证事务备份后即使原文件被修改，
// Rollback 仍会按逆序恢复原内容，并把 manifest 状态更新为 rolled_back。
func TestBackupFileThenRollbackRestoresOriginalContent(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	backupRoot := filepath.Join(tempDir, "backup-root")
	sourcePath := filepath.Join(tempDir, "install", "app", "frame.dll")
	relativePath := `app\frame.dll`
	originalContent := []byte("original-content")

	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(sourcePath, originalContent, 0o644); err != nil {
		t.Fatalf("os.WriteFile(source) error = %v", err)
	}

	tx, err := New(backupRoot, filepath.Join(tempDir, "install"), "remove")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := tx.BackupFile(relativePath, sourcePath); err != nil {
		t.Fatalf("BackupFile() error = %v", err)
	}

	manifestBeforeRollback, err := backup.ReadManifest(tx.Root())
	if err != nil {
		t.Fatalf("backup.ReadManifest(before rollback) error = %v", err)
	}
	if manifestBeforeRollback.Status != statusInProgress {
		t.Fatalf("Status before rollback = %q, want %q", manifestBeforeRollback.Status, statusInProgress)
	}
	if len(manifestBeforeRollback.Files) < 1 {
		t.Fatalf("len(Files) before rollback = %d, want at least 1", len(manifestBeforeRollback.Files))
	}
	if manifestBeforeRollback.Files[0].RelativePath != relativePath {
		t.Fatalf("Files[0].RelativePath before rollback = %q, want %q", manifestBeforeRollback.Files[0].RelativePath, relativePath)
	}

	if err := os.WriteFile(sourcePath, []byte("patched-content"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(mutated source) error = %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	restoredData, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("os.ReadFile(restored source) error = %v", err)
	}
	if string(restoredData) != string(originalContent) {
		t.Fatalf("restored content = %q, want %q", string(restoredData), string(originalContent))
	}

	manifest, err := backup.ReadManifest(tx.Root())
	if err != nil {
		t.Fatalf("backup.ReadManifest() error = %v", err)
	}
	if manifest.Status != statusRolledBack {
		t.Fatalf("Status = %q, want %q", manifest.Status, statusRolledBack)
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(manifest.Files))
	}
	if manifest.Files[0].RelativePath != relativePath {
		t.Fatalf("Files[0].RelativePath = %q, want %q", manifest.Files[0].RelativePath, relativePath)
	}
}

// TestBackupFileIsIdempotentForSameRelativePath 验证同一 relativePath 重复备份时，
// 第二次调用不会覆盖第一次原始备份，也不会重复追加 manifest 和 restore。
func TestBackupFileIsIdempotentForSameRelativePath(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	backupRoot := filepath.Join(tempDir, "backup-root")
	sourcePath := filepath.Join(tempDir, "install", "app", "frame.dll")
	relativePath := `app\frame.dll`
	firstContent := []byte("first-original")
	secondContent := []byte("second-mutated")

	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(sourcePath, firstContent, 0o644); err != nil {
		t.Fatalf("os.WriteFile(first source) error = %v", err)
	}

	tx, err := New(backupRoot, filepath.Join(tempDir, "install"), "remove")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := tx.BackupFile(relativePath, sourcePath); err != nil {
		t.Fatalf("BackupFile(first) error = %v", err)
	}

	firstManifest := tx.Manifest()
	if len(firstManifest.Files) != 1 {
		t.Fatalf("len(Files after first backup) = %d, want 1", len(firstManifest.Files))
	}
	backupPath := filepath.Join(tx.Root(), firstManifest.Files[0].BackupPath)

	if err := os.WriteFile(sourcePath, secondContent, 0o644); err != nil {
		t.Fatalf("os.WriteFile(second source) error = %v", err)
	}

	if err := tx.BackupFile(relativePath, sourcePath); err != nil {
		t.Fatalf("BackupFile(second) error = %v", err)
	}

	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("os.ReadFile(backupPath) error = %v", err)
	}
	if string(backupData) != string(firstContent) {
		t.Fatalf("backup content after duplicate backup = %q, want %q", string(backupData), string(firstContent))
	}

	manifest := tx.Manifest()
	if len(manifest.Files) != 1 {
		t.Fatalf("len(Files after duplicate backup) = %d, want 1", len(manifest.Files))
	}

	diskManifest, err := backup.ReadManifest(tx.Root())
	if err != nil {
		t.Fatalf("backup.ReadManifest() error = %v", err)
	}
	if len(diskManifest.Files) != 1 {
		t.Fatalf("len(disk manifest Files) = %d, want 1", len(diskManifest.Files))
	}
}

// TestRollbackRestoresFilesInReverseOrderStatefully 验证事务记录多个文件后，
// manifest 中文件顺序与备份顺序一致，Rollback 至少能在该顺序基础上完成双文件恢复，
// 并把事务状态标记为 rolled_back。由于当前实现没有可注入替身，测试只能通过
// manifest 顺序与最终恢复结果间接证明逆序恢复路径被覆盖。
func TestRollbackRestoresFilesInReverseOrderStatefully(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	installRoot := filepath.Join(tempDir, "install")
	backupRoot := filepath.Join(tempDir, "backup-root")
	firstPath := filepath.Join(installRoot, "app", "frame.dll")
	secondPath := filepath.Join(installRoot, "app", "webcontent", "messenger-vc", "common", "bundle.js")
	firstRelativePath := `app\frame.dll`
	secondRelativePath := `app\webcontent\messenger-vc\common\bundle.js`
	firstOriginal := []byte("frame-original")
	secondOriginal := []byte("bundle-original")

	for _, file := range []struct {
		path    string
		content []byte
	}{
		{path: firstPath, content: firstOriginal},
		{path: secondPath, content: secondOriginal},
	} {
		if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(file.path), err)
		}
		if err := os.WriteFile(file.path, file.content, 0o644); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", file.path, err)
		}
	}

	tx, err := New(backupRoot, installRoot, "remove")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := tx.BackupFile(firstRelativePath, firstPath); err != nil {
		t.Fatalf("BackupFile(first) error = %v", err)
	}
	if err := tx.BackupFile(secondRelativePath, secondPath); err != nil {
		t.Fatalf("BackupFile(second) error = %v", err)
	}

	manifestSnapshot := tx.Manifest()
	if len(manifestSnapshot.Files) != 2 {
		t.Fatalf("len(tx.Manifest().Files) = %d, want 2", len(manifestSnapshot.Files))
	}
	if manifestSnapshot.Files[0].RelativePath != firstRelativePath {
		t.Fatalf("Files[0].RelativePath = %q, want %q", manifestSnapshot.Files[0].RelativePath, firstRelativePath)
	}
	if manifestSnapshot.Files[1].RelativePath != secondRelativePath {
		t.Fatalf("Files[1].RelativePath = %q, want %q", manifestSnapshot.Files[1].RelativePath, secondRelativePath)
	}

	diskManifestPath := filepath.Join(tx.Root(), "manifest.json")
	diskManifestData, err := os.ReadFile(diskManifestPath)
	if err != nil {
		t.Fatalf("os.ReadFile(manifest.json) error = %v", err)
	}

	var diskManifest struct {
		Files []backup.FileRecord `json:"files"`
	}
	if err := json.Unmarshal(diskManifestData, &diskManifest); err != nil {
		t.Fatalf("json.Unmarshal(manifest.json) error = %v", err)
	}
	if len(diskManifest.Files) != 2 {
		t.Fatalf("len(disk manifest Files) = %d, want 2", len(diskManifest.Files))
	}
	if diskManifest.Files[0].RelativePath != firstRelativePath || diskManifest.Files[1].RelativePath != secondRelativePath {
		t.Fatalf("disk manifest file order = [%q %q], want [%q %q]",
			diskManifest.Files[0].RelativePath,
			diskManifest.Files[1].RelativePath,
			firstRelativePath,
			secondRelativePath,
		)
	}

	if err := os.WriteFile(firstPath, []byte("frame-mutated"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(first mutated) error = %v", err)
	}
	if err := os.WriteFile(secondPath, []byte("bundle-mutated"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(second mutated) error = %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	firstRestored, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatalf("os.ReadFile(first restored) error = %v", err)
	}
	if string(firstRestored) != string(firstOriginal) {
		t.Fatalf("first restored content = %q, want %q", string(firstRestored), string(firstOriginal))
	}

	secondRestored, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatalf("os.ReadFile(second restored) error = %v", err)
	}
	if string(secondRestored) != string(secondOriginal) {
		t.Fatalf("second restored content = %q, want %q", string(secondRestored), string(secondOriginal))
	}

	rolledBackManifest, err := backup.ReadManifest(tx.Root())
	if err != nil {
		t.Fatalf("backup.ReadManifest(after rollback) error = %v", err)
	}
	if rolledBackManifest.Status != statusRolledBack {
		t.Fatalf("Status after rollback = %q, want %q", rolledBackManifest.Status, statusRolledBack)
	}
}

// TestRollbackRestoresSameTargetInReverseOrder 直接构造 restores 列表，验证
// Rollback 对同一 TargetPath 的多个恢复项按逆序执行。若实现误写成正序，
// 最终文件内容会停留在第二个备份 `second-newer`，本测试会失败。
func TestRollbackRestoresSameTargetInReverseOrder(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "install", "app", "frame.dll")
	backupAPath := filepath.Join(tempDir, "backup", "files", "a.bak")
	backupBPath := filepath.Join(tempDir, "backup", "files", "b.bak")
	firstOriginal := []byte("first-original")
	secondNewer := []byte("second-newer")

	for _, dir := range []string{
		filepath.Dir(targetPath),
		filepath.Dir(backupAPath),
		filepath.Dir(backupBPath),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", dir, err)
		}
	}

	if err := os.WriteFile(targetPath, []byte("mutated"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(target mutated) error = %v", err)
	}
	if err := os.WriteFile(backupAPath, firstOriginal, 0o644); err != nil {
		t.Fatalf("os.WriteFile(backupA) error = %v", err)
	}
	if err := os.WriteFile(backupBPath, secondNewer, 0o644); err != nil {
		t.Fatalf("os.WriteFile(backupB) error = %v", err)
	}

	tx := &Transaction{
		root: tempDir,
		manifest: backup.Manifest{
			ToolVersion: toolVersion,
			CreatedAt:   time.Now().Format(time.RFC3339),
			InstallPath: filepath.Join(tempDir, "install"),
			Operation:   "remove",
			Status:      statusInProgress,
			Files: []backup.FileRecord{
				{
					RelativePath: `app\frame.dll`,
					BackupPath:   filepath.Join("files", "a.bak"),
					SizeBefore:   int64(len(firstOriginal)),
				},
				{
					RelativePath: `app\frame.dll`,
					BackupPath:   filepath.Join("files", "b.bak"),
					SizeBefore:   int64(len(secondNewer)),
				},
			},
		},
		restores: []backup.RestoreItem{
			{TargetPath: targetPath, BackupPath: backupAPath},
			{TargetPath: targetPath, BackupPath: backupBPath},
		},
	}

	if err := backup.WriteManifest(tx.root, tx.manifest); err != nil {
		t.Fatalf("backup.WriteManifest() error = %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	finalData, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("os.ReadFile(target) error = %v", err)
	}
	if string(finalData) != string(firstOriginal) {
		t.Fatalf("final target content = %q, want %q; forward-order restore would leave %q", string(finalData), string(firstOriginal), string(secondNewer))
	}

	manifest, err := backup.ReadManifest(tx.root)
	if err != nil {
		t.Fatalf("backup.ReadManifest() error = %v", err)
	}
	if manifest.Status != statusRolledBack {
		t.Fatalf("manifest.Status = %q, want %q", manifest.Status, statusRolledBack)
	}
}

// TestCommitMarksManifestCompletedAndPreventsRollback 验证事务提交后状态会持久化为
// completed，且不允许再执行回滚。
func TestCommitMarksManifestCompletedAndPreventsRollback(t *testing.T) {
	t.Parallel()

	backupRoot := filepath.Join(t.TempDir(), "backup-root")

	tx, err := New(backupRoot, `E:\Feishu`, "remove")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	manifest, err := backup.ReadManifest(tx.Root())
	if err != nil {
		t.Fatalf("backup.ReadManifest() error = %v", err)
	}
	if manifest.Status != statusCompleted {
		t.Fatalf("Status = %q, want %q", manifest.Status, statusCompleted)
	}

	err = tx.Rollback()
	if !errors.Is(err, ErrCommittedTransaction) {
		t.Fatalf("Rollback() error = %v, want ErrCommittedTransaction", err)
	}
}

// TestBackupFileReturnsErrorAfterCommit 验证事务一旦完成提交，就不允许继续追加备份。
func TestBackupFileReturnsErrorAfterCommit(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "install", "app", "frame.dll")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("content"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	tx, err := New(filepath.Join(tempDir, "backup-root"), filepath.Join(tempDir, "install"), "remove")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	err = tx.BackupFile(`app\frame.dll`, sourcePath)
	if !errors.Is(err, ErrTransactionNotInProgress) {
		t.Fatalf("BackupFile() error = %v, want ErrTransactionNotInProgress", err)
	}
}

// TestBackupFileReturnsErrorAfterRollback 验证事务回滚后不允许再追加备份。
func TestBackupFileReturnsErrorAfterRollback(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "install", "app", "frame.dll")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("content"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	tx, err := New(filepath.Join(tempDir, "backup-root"), filepath.Join(tempDir, "install"), "remove")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	err = tx.BackupFile(`app\frame.dll`, sourcePath)
	if !errors.Is(err, ErrTransactionNotInProgress) {
		t.Fatalf("BackupFile() error = %v, want ErrTransactionNotInProgress", err)
	}
}

// TestCommitReturnsErrorAfterRollback 验证已经回滚的事务不允许再次提交。
func TestCommitReturnsErrorAfterRollback(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	tx, err := New(filepath.Join(tempDir, "backup-root"), filepath.Join(tempDir, "install"), "remove")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	err = tx.Commit()
	if !errors.Is(err, ErrTransactionNotInProgress) {
		t.Fatalf("Commit() error = %v, want ErrTransactionNotInProgress", err)
	}
}

// TestBackupFileRollsBackInMemoryStateWhenManifestWriteFails 验证写 manifest 失败时，
// BackupFile 不会在内存中留下已追加但未持久化的脏记录。
func TestBackupFileRollsBackInMemoryStateWhenManifestWriteFails(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "install", "app", "frame.dll")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("content"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	tx, err := New(filepath.Join(tempDir, "backup-root"), filepath.Join(tempDir, "install"), "remove")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	originalWriteManifest := tx.writeManifest
	tx.writeManifest = func(string, backup.Manifest) error {
		return errors.New("forced manifest write failure")
	}
	t.Cleanup(func() {
		tx.writeManifest = originalWriteManifest
	})

	err = tx.BackupFile(`app\frame.dll`, sourcePath)
	if err == nil {
		t.Fatal("BackupFile() error = nil, want manifest write failure")
	}

	manifest := tx.Manifest()
	if len(manifest.Files) != 0 {
		t.Fatalf("len(tx.Manifest().Files) after failure = %d, want 0", len(manifest.Files))
	}
	if len(tx.restores) != 0 {
		t.Fatalf("len(tx.restores) after failure = %d, want 0", len(tx.restores))
	}
}

// TestRollbackMarksRollbackFailedAndContinuesRestoring 验证回滚中途遇到恢复错误时，
// 仍会继续恢复剩余项，并把事务状态标记为 rollback_failed。
func TestRollbackMarksRollbackFailedAndContinuesRestoring(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "install", "app", "frame.dll")
	goodBackupPath := filepath.Join(tempDir, "backup", "files", "good.bak")
	missingBackupPath := filepath.Join(tempDir, "backup", "files", "missing.bak")
	originalContent := []byte("original")

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(target dir) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(goodBackupPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(backup dir) error = %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("mutated"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(target) error = %v", err)
	}
	if err := os.WriteFile(goodBackupPath, originalContent, 0o644); err != nil {
		t.Fatalf("os.WriteFile(good backup) error = %v", err)
	}

	tx := &Transaction{
		root: filepath.Join(tempDir, "backup"),
		manifest: backup.Manifest{
			Status: statusInProgress,
			Files:  []backup.FileRecord{},
		},
		restores: []backup.RestoreItem{
			{TargetPath: targetPath, BackupPath: goodBackupPath},
			{TargetPath: targetPath, BackupPath: missingBackupPath},
		},
	}
	tx.writeManifest = backup.WriteManifest

	if err := backup.WriteManifest(tx.root, tx.manifest); err != nil {
		t.Fatalf("backup.WriteManifest() error = %v", err)
	}

	err := tx.Rollback()
	if err == nil {
		t.Fatal("Rollback() error = nil, want joined restore error")
	}

	finalData, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatalf("os.ReadFile(target) error = %v", readErr)
	}
	if string(finalData) != string(originalContent) {
		t.Fatalf("final target content = %q, want %q", string(finalData), string(originalContent))
	}

	manifest, readManifestErr := backup.ReadManifest(tx.root)
	if readManifestErr != nil {
		t.Fatalf("backup.ReadManifest() error = %v", readManifestErr)
	}
	if manifest.Status != statusRollbackFailed {
		t.Fatalf("manifest.Status = %q, want %q", manifest.Status, statusRollbackFailed)
	}
}

// TestRollbackRestoresStatusWhenFinalManifestWriteFails 验证成功恢复文件后，如果终态
// manifest 落盘失败，内存状态会恢复到旧状态，并允许调用方再次 Rollback。
func TestRollbackRestoresStatusWhenFinalManifestWriteFails(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "install", "app", "frame.dll")
	backupPath := filepath.Join(tempDir, "backup", "files", "frame.bak")
	originalContent := []byte("original")
	persistErr := errors.New("forced final manifest write failure")

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(target dir) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(backup dir) error = %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("mutated"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(target) error = %v", err)
	}
	if err := os.WriteFile(backupPath, originalContent, 0o644); err != nil {
		t.Fatalf("os.WriteFile(backup) error = %v", err)
	}

	tx := &Transaction{
		root: filepath.Join(tempDir, "backup"),
		manifest: backup.Manifest{
			Status: statusInProgress,
			Files:  []backup.FileRecord{},
		},
		restores: []backup.RestoreItem{
			{TargetPath: targetPath, BackupPath: backupPath},
		},
		writeManifest: func(string, backup.Manifest) error {
			return persistErr
		},
	}

	err := tx.Rollback()
	if !errors.Is(err, persistErr) {
		t.Fatalf("Rollback() error = %v, want persistErr", err)
	}
	if tx.manifest.Status != statusInProgress {
		t.Fatalf("tx.manifest.Status after failed persist = %q, want %q", tx.manifest.Status, statusInProgress)
	}

	tx.writeManifest = backup.WriteManifest
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() retry error = %v", err)
	}
	if tx.manifest.Status != statusRolledBack {
		t.Fatalf("tx.manifest.Status after retry = %q, want %q", tx.manifest.Status, statusRolledBack)
	}
}

// TestRollbackRestoresStatusWhenRollbackFailedManifestWriteFails 验证存在 restore 错误时，
// 如果 rollback_failed 状态落盘失败，返回错误包含恢复错误和持久化错误，内存状态
// 仍恢复为旧状态，避免事务被错误锁死。
func TestRollbackRestoresStatusWhenRollbackFailedManifestWriteFails(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "install", "app", "frame.dll")
	missingBackupPath := filepath.Join(tempDir, "backup", "files", "missing.bak")
	persistErr := errors.New("forced rollback_failed manifest write failure")

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(target dir) error = %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("mutated"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(target) error = %v", err)
	}

	tx := &Transaction{
		root: filepath.Join(tempDir, "backup"),
		manifest: backup.Manifest{
			Status: statusInProgress,
			Files:  []backup.FileRecord{},
		},
		restores: []backup.RestoreItem{
			{TargetPath: targetPath, BackupPath: missingBackupPath},
		},
		writeManifest: func(string, backup.Manifest) error {
			return persistErr
		},
	}

	err := tx.Rollback()
	if err == nil {
		t.Fatal("Rollback() error = nil, want restore and persist errors")
	}
	if !errors.Is(err, persistErr) {
		t.Fatalf("Rollback() error = %v, want joined persistErr", err)
	}
	if tx.manifest.Status != statusInProgress {
		t.Fatalf("tx.manifest.Status after failed rollback_failed persist = %q, want %q", tx.manifest.Status, statusInProgress)
	}

	tx.writeManifest = backup.WriteManifest
	retryErr := tx.Rollback()
	if retryErr == nil {
		t.Fatal("Rollback() retry error = nil, want restore error")
	}
	if tx.manifest.Status != statusRollbackFailed {
		t.Fatalf("tx.manifest.Status after retry = %q, want %q", tx.manifest.Status, statusRollbackFailed)
	}
}
