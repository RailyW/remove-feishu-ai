package backup

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RestoreItem 描述一个可以从事务备份恢复回原目标路径的文件项。
//
// TargetPath 是待恢复的真实目标文件路径；BackupPath 是事务 files 目录中已经
// 复制好的备份文件路径。当前任务只需要无条件覆盖恢复，后续可在此结构基础
// 上扩展变更检测。
type RestoreItem struct {
	TargetPath string
	BackupPath string
}

// Restore 将备份文件内容覆盖写回目标文件。
//
// 该函数会确保目标文件父目录存在，然后直接复制 BackupPath 到 TargetPath。
func (r RestoreItem) Restore() error {
	if err := os.MkdirAll(filepath.Dir(r.TargetPath), 0o755); err != nil {
		return err
	}

	return copyFile(r.BackupPath, r.TargetPath)
}

// CreateTransactionLayout 创建一次事务使用的备份目录结构。
//
// 返回的 root 为 `<backupRoot>\YYYYMMDD-HHMMSS`，filesDir 为 root 下的 files
// 子目录。调用方传入 now，使测试能够固定目录名并避免依赖真实时间。root 目录
// 使用 os.Mkdir 原子创建，避免同一秒并发事务错误复用同一个事务根目录。
func CreateTransactionLayout(backupRoot string, now time.Time) (root string, filesDir string, err error) {
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		return "", "", err
	}

	baseName := now.Format("20060102-150405")
	for i := 0; i < 100; i++ {
		name := baseName
		if i > 0 {
			name = fmt.Sprintf("%s-%02d", baseName, i)
		}

		root = filepath.Join(backupRoot, name)
		filesDir = filepath.Join(root, "files")

		if err := os.Mkdir(root, 0o755); err != nil {
			if os.IsExist(err) {
				continue
			}

			return "", "", err
		}

		if err := os.Mkdir(filesDir, 0o755); err != nil {
			if removeErr := os.Remove(root); removeErr != nil {
				return "", "", fmt.Errorf("create files dir: %w; cleanup root: %v", err, removeErr)
			}

			return "", "", err
		}

		return root, filesDir, nil
	}

	return "", "", fmt.Errorf("transaction layout collision after 100 attempts: %s", filepath.Join(backupRoot, baseName))
}

// SanitizeRelativePath 将安装目录内的相对路径转换为安全的备份文件名。
//
// 当前实现只追求 Windows 文件名稳定安全，不追求可逆：路径分隔符会转换为
// 下划线，Windows 文件名非法字符也会转换为下划线，最后追加 .bak 后缀。
func SanitizeRelativePath(relativePath string) string {
	replacer := strings.NewReplacer(
		`\`, "_",
		"/", "_",
		":", "_",
		"*", "_",
		"?", "_",
		`"`, "_",
		"<", "_",
		">", "_",
		"|", "_",
	)

	name := replacer.Replace(relativePath)
	name = strings.Trim(name, " ._")
	if name == "" {
		name = "file"
	}

	return name + ".bak"
}

// uniqueBackupPath 根据 relativePath 生成 filesDir 下不会覆盖已有文件的备份路径。
//
// 首次备份时直接使用 SanitizeRelativePath 结果；如果文件已存在，则在 `.bak`
// 前追加源 relativePath 的短 hash，保证冲突场景下文件名稳定且彼此区分。
func uniqueBackupPath(filesDir string, relativePath string) (string, error) {
	baseName := SanitizeRelativePath(relativePath)
	candidatePath := filepath.Join(filesDir, baseName)
	if _, err := os.Stat(candidatePath); err == nil {
		return filepath.Join(filesDir, appendHashBeforeBak(baseName, relativePath)), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	return candidatePath, nil
}

// appendHashBeforeBak 在基础备份文件名 `.bak` 前附加 relativePath 的短 hash。
//
// 这样可以在不破坏简单示例文件名的前提下，为碰撞场景生成稳定不同的备份名。
func appendHashBeforeBak(baseName string, relativePath string) string {
	sum := sha1.Sum([]byte(relativePath))
	shortHash := hex.EncodeToString(sum[:4])

	if strings.HasSuffix(baseName, ".bak") {
		return strings.TrimSuffix(baseName, ".bak") + "__" + shortHash + ".bak"
	}

	return baseName + "__" + shortHash
}

// CopyIntoTransaction 将 sourcePath 指向的目标文件复制到事务 files 目录。
//
// txRoot 用于计算 manifest 中相对事务根目录的 BackupPath；filesDir 是实际
// 备份文件落盘目录。函数会返回 manifest 文件记录和后续回滚所需 RestoreItem。
// 复制完成后会比较源文件与备份文件大小，发现不一致时返回错误。
func CopyIntoTransaction(txRoot, filesDir, relativePath, sourcePath string) (record FileRecord, restore RestoreItem, err error) {
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return FileRecord{}, RestoreItem{}, err
	}
	if sourceInfo.IsDir() {
		return FileRecord{}, RestoreItem{}, fmt.Errorf("backup source is directory: %s", sourcePath)
	}

	backupPath, err := uniqueBackupPath(filesDir, relativePath)
	if err != nil {
		return FileRecord{}, RestoreItem{}, err
	}

	if err := copyFile(sourcePath, backupPath); err != nil {
		return FileRecord{}, RestoreItem{}, err
	}

	backupInfo, err := os.Stat(backupPath)
	if err != nil {
		return FileRecord{}, RestoreItem{}, err
	}
	if backupInfo.Size() != sourceInfo.Size() {
		return FileRecord{}, RestoreItem{}, fmt.Errorf("backup size mismatch: source=%d backup=%d", sourceInfo.Size(), backupInfo.Size())
	}

	backupRelativePath, err := filepath.Rel(txRoot, backupPath)
	if err != nil {
		return FileRecord{}, RestoreItem{}, err
	}

	record = FileRecord{
		RelativePath: relativePath,
		BackupPath:   backupRelativePath,
		SizeBefore:   sourceInfo.Size(),
	}
	restore = RestoreItem{
		TargetPath: sourcePath,
		BackupPath: backupPath,
	}

	return record, restore, nil
}

// copyFile 将 src 文件内容复制到 dst，并确保目标父目录已存在。
//
// 该 helper 被备份和恢复共同使用，避免两处文件复制逻辑产生行为差异。
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		closeErr := out.Close()
		if closeErr != nil {
			return fmt.Errorf("%w; close destination: %v", err, closeErr)
		}
		return err
	}

	return out.Close()
}
