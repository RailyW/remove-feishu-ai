package tx

import (
	"errors"
	"fmt"
	"time"

	"remove-feishu-ai/internal/backup"
)

const (
	// toolVersion 是当前开发阶段写入 manifest 的固定工具版本。
	toolVersion = "0.1.0-dev"

	// statusInProgress 表示事务已经创建但尚未提交或回滚。
	statusInProgress = "in_progress"

	// statusCompleted 表示事务已经成功提交，不允许再执行回滚。
	statusCompleted = "completed"

	// statusRolledBack 表示事务已执行回滚并写回原始文件。
	statusRolledBack = "rolled_back"

	// statusRollbackFailed 表示事务尝试回滚但至少有一个文件恢复失败。
	statusRollbackFailed = "rollback_failed"
)

// ErrCommittedTransaction 表示调用方尝试回滚一个已经提交完成的事务。
var ErrCommittedTransaction = errors.New("cannot rollback committed transaction")

// ErrTransactionNotInProgress 表示调用方尝试在非 in_progress 状态继续修改事务。
var ErrTransactionNotInProgress = errors.New("transaction is not in progress")

// ErrTransactionAlreadyRolledBack 表示调用方尝试重复回滚已经结束的事务。
var ErrTransactionAlreadyRolledBack = errors.New("transaction already rolled back")

// Transaction 管理一次补丁操作的备份目录、manifest 和回滚列表。
//
// root 是本次事务根目录；filesDir 是备份文件目录；manifest 保存持久化状态；
// restores 按备份发生顺序保存恢复项，Rollback 会逆序执行；writeManifest 是
// 私有持久化依赖点，便于测试 manifest 写入失败时的内存状态收敛。
type Transaction struct {
	root          string
	filesDir      string
	manifest      backup.Manifest
	restores      []backup.RestoreItem
	writeManifest func(string, backup.Manifest) error
}

// New 创建新的事务目录并写入初始 manifest。
//
// backupRoot 是所有事务备份的根目录；installPath 和 operation 会原样记录到
// manifest 中，供后续审计和恢复入口识别事务来源。
func New(backupRoot, installPath, operation string) (*Transaction, error) {
	root, filesDir, err := backup.CreateTransactionLayout(backupRoot, time.Now())
	if err != nil {
		return nil, err
	}

	tx := &Transaction{
		root:     root,
		filesDir: filesDir,
		manifest: backup.Manifest{
			ToolVersion: toolVersion,
			CreatedAt:   time.Now().Format(time.RFC3339),
			InstallPath: installPath,
			Operation:   operation,
			Status:      statusInProgress,
			Files:       []backup.FileRecord{},
		},
		restores:      []backup.RestoreItem{},
		writeManifest: backup.WriteManifest,
	}

	if err := tx.persistManifest(); err != nil {
		return nil, err
	}

	return tx, nil
}

// Root 返回事务根目录路径，供测试和后续业务读取 manifest 或备份文件。
func (tx *Transaction) Root() string {
	return tx.root
}

// Manifest 返回当前内存中的 manifest 快照。
//
// Files 会复制到新切片中，避免调用方直接修改事务内部状态。
func (tx *Transaction) Manifest() backup.Manifest {
	manifest := tx.manifest
	manifest.Files = append([]backup.FileRecord(nil), tx.manifest.Files...)

	return manifest
}

// BackupFile 将目标文件复制到事务备份目录，并立即更新 manifest。
//
// relativePath 是目标文件相对安装目录路径；sourcePath 是当前机器上的真实文件
// 路径。该方法满足 feature.Tx 接口，供后续 feature 实现调用。
func (tx *Transaction) BackupFile(relativePath, sourcePath string) error {
	if tx.manifest.Status != statusInProgress {
		return fmt.Errorf("%w: current status %q", ErrTransactionNotInProgress, tx.manifest.Status)
	}
	if tx.hasBackup(relativePath) {
		return nil
	}

	record, restore, err := backup.CopyIntoTransaction(tx.root, tx.filesDir, relativePath, sourcePath)
	if err != nil {
		return err
	}

	oldFiles := tx.manifest.Files
	oldRestores := tx.restores
	tx.manifest.Files = append(tx.manifest.Files, record)
	tx.restores = append(tx.restores, restore)

	if err := tx.persistManifest(); err != nil {
		tx.manifest.Files = oldFiles
		tx.restores = oldRestores
		return err
	}

	return nil
}

// Commit 将事务标记为 completed 并写回 manifest。
//
// Commit 成功后状态会变为 completed，Rollback 会拒绝回滚该事务。
func (tx *Transaction) Commit() error {
	if tx.manifest.Status != statusInProgress {
		return fmt.Errorf("%w: current status %q", ErrTransactionNotInProgress, tx.manifest.Status)
	}

	tx.manifest.Status = statusCompleted
	if err := tx.persistManifest(); err != nil {
		tx.manifest.Status = statusInProgress
		return err
	}

	return nil
}

// Rollback 按备份顺序的逆序恢复文件，并将 manifest 标记为 rolled_back。
//
// 已 Commit 的事务不允许回滚；未提交事务即使没有备份文件，也会写入
// rolled_back 状态，表示该事务已经被显式终止。
func (tx *Transaction) Rollback() error {
	switch tx.manifest.Status {
	case statusInProgress:
	case statusCompleted:
		return ErrCommittedTransaction
	case statusRolledBack, statusRollbackFailed:
		return fmt.Errorf("%w: current status %q", ErrTransactionAlreadyRolledBack, tx.manifest.Status)
	default:
		return fmt.Errorf("%w: current status %q", ErrTransactionNotInProgress, tx.manifest.Status)
	}

	var restoreErrs []error
	for i := len(tx.restores) - 1; i >= 0; i-- {
		if err := tx.restores[i].Restore(); err != nil {
			restoreErrs = append(restoreErrs, err)
		}
	}

	oldStatus := tx.manifest.Status
	if len(restoreErrs) > 0 {
		tx.manifest.Status = statusRollbackFailed
		if err := tx.persistManifest(); err != nil {
			tx.manifest.Status = oldStatus
			restoreErrs = append(restoreErrs, fmt.Errorf("persist rollback_failed status: %w", err))
		}
		return errors.Join(restoreErrs...)
	}

	tx.manifest.Status = statusRolledBack
	if err := tx.persistManifest(); err != nil {
		tx.manifest.Status = oldStatus
		return fmt.Errorf("persist rolled_back status: %w", err)
	}

	return nil
}

// hasBackup 判断当前事务是否已经为相同 relativePath 创建过原始备份。
//
// BackupFile 使用该检查实现幂等语义，避免同一文件第二次备份覆盖第一次原始
// 内容，也避免 manifest 中出现重复记录。
func (tx *Transaction) hasBackup(relativePath string) bool {
	for _, file := range tx.manifest.Files {
		if file.RelativePath == relativePath {
			return true
		}
	}

	return false
}

// persistManifest 将当前内存 manifest 写入事务根目录。
//
// 对于测试中手工构造的 Transaction，如果未显式注入 writeManifest，会回退到
// backup.WriteManifest，避免调用方必须了解内部依赖字段。
func (tx *Transaction) persistManifest() error {
	writeManifest := tx.writeManifest
	if writeManifest == nil {
		writeManifest = backup.WriteManifest
	}

	return writeManifest(tx.root, tx.manifest)
}
