package featureframe

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"remove-feishu-ai/internal/config"
	"remove-feishu-ai/internal/feature"
)

func TestDetectReportsOriginalForExpectedOldPatternCount(t *testing.T) {
	path, oldOffsets, _ := writeFrameSample(t, samplePlacement{oldCount: 2})

	state, meta, err := New().detect(path, config.OffsetCache{})
	if err != nil {
		t.Fatalf("detect returned error: %v", err)
	}

	assertState(t, state, feature.StateOriginal)
	assertOffsets(t, meta.OldOffsets, oldOffsets)
	assertOffsets(t, meta.NewOffsets, nil)
	if meta.SearchMode != "full_file" {
		t.Fatalf("SearchMode = %q, want %q", meta.SearchMode, "full_file")
	}
}

func TestDetectReportsPatchedForExpectedNewPatternCount(t *testing.T) {
	path, _, newOffsets := writeFrameSample(t, samplePlacement{newCount: 2})

	state, meta, err := New().detect(path, config.OffsetCache{})
	if err != nil {
		t.Fatalf("detect returned error: %v", err)
	}

	assertState(t, state, feature.StatePatched)
	assertOffsets(t, meta.OldOffsets, nil)
	assertOffsets(t, meta.NewOffsets, newOffsets)
	if meta.SearchMode != "full_file" {
		t.Fatalf("SearchMode = %q, want %q", meta.SearchMode, "full_file")
	}
}

func TestDetectReportsMixedWhenOldAndNewPatternsCoexist(t *testing.T) {
	path, oldOffsets, newOffsets := writeFrameSample(t, samplePlacement{oldCount: 1, newCount: 1})

	state, meta, err := New().detect(path, config.OffsetCache{})
	if err != nil {
		t.Fatalf("detect returned error: %v", err)
	}

	assertState(t, state, feature.StateMixed)
	assertOffsets(t, meta.OldOffsets, oldOffsets)
	assertOffsets(t, meta.NewOffsets, newOffsets)
	if meta.SearchMode != "full_file" {
		t.Fatalf("SearchMode = %q, want %q", meta.SearchMode, "full_file")
	}
}

func TestDetectReportsUnknownWhenPatternCountUnexpected(t *testing.T) {
	tests := []struct {
		name     string
		oldCount int
	}{
		{name: "one old pattern", oldCount: 1},
		{name: "three old patterns", oldCount: 3},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path, oldOffsets, _ := writeFrameSample(t, samplePlacement{oldCount: test.oldCount})

			state, meta, err := New().detect(path, config.OffsetCache{})
			if err != nil {
				t.Fatalf("detect returned error: %v", err)
			}

			assertState(t, state, feature.StateUnknown)
			assertOffsets(t, meta.OldOffsets, oldOffsets)
			assertOffsets(t, meta.NewOffsets, nil)
			if meta.SearchMode != "full_file" {
				t.Fatalf("SearchMode = %q, want %q", meta.SearchMode, "full_file")
			}
		})
	}
}

func TestDetectUsesCacheExactWhenCachedOffsetsStillMatch(t *testing.T) {
	path, oldOffsets, _ := writeFrameSample(t, samplePlacement{oldCount: 2})
	cache := cacheForFile(t, path)
	cache.OldPatternOffsets = oldOffsets

	state, meta, err := New().detect(path, cache)
	if err != nil {
		t.Fatalf("detect returned error: %v", err)
	}

	assertState(t, state, feature.StateOriginal)
	assertOffsets(t, meta.OldOffsets, oldOffsets)
	assertOffsets(t, meta.NewOffsets, nil)
	if meta.SearchMode != "cache_exact" {
		t.Fatalf("SearchMode = %q, want %q", meta.SearchMode, "cache_exact")
	}
}

func TestDetectDoesNotTrustStaleCacheOriginalWhenFileIsMixed(t *testing.T) {
	path, oldOffsets, newOffsets := writeFrameSample(t, samplePlacement{oldCount: 2, newCount: 1})
	cache := config.OffsetCache{
		OldPatternOffsets: oldOffsets,
		FileSize:          1,
		MTime:             "2000-01-01T00:00:00Z",
	}

	state, meta, err := New().detect(path, cache)
	if err != nil {
		t.Fatalf("detect returned error: %v", err)
	}

	assertState(t, state, feature.StateMixed)
	assertOffsets(t, meta.OldOffsets, oldOffsets)
	assertOffsets(t, meta.NewOffsets, newOffsets)
	if meta.SearchMode != "cache_window" {
		t.Fatalf("SearchMode = %q, want %q", meta.SearchMode, "cache_window")
	}
}

func TestDetectUsesCacheWindowWhenNearbyOffsetsMatch(t *testing.T) {
	path, oldOffsets, _ := writeFrameSample(t, samplePlacement{oldCount: 2})
	cache := cacheForFile(t, path)
	cache.OldPatternOffsets = []int64{oldOffsets[0] - 3, oldOffsets[1] + 5}

	state, meta, err := New().detect(path, cache)
	if err != nil {
		t.Fatalf("detect returned error: %v", err)
	}

	assertState(t, state, feature.StateOriginal)
	assertOffsets(t, meta.OldOffsets, oldOffsets)
	assertOffsets(t, meta.NewOffsets, nil)
	if meta.SearchMode != "cache_window" {
		t.Fatalf("SearchMode = %q, want %q", meta.SearchMode, "cache_window")
	}
}

func TestDetectFallsBackToFullFileForNonPESample(t *testing.T) {
	path, oldOffsets, _ := writeFrameSample(t, samplePlacement{oldCount: 2})

	state, meta, err := New().detect(path, config.OffsetCache{})
	if err != nil {
		t.Fatalf("detect returned error: %v", err)
	}

	assertState(t, state, feature.StateOriginal)
	assertOffsets(t, meta.OldOffsets, oldOffsets)
	if meta.SearchMode != "full_file" {
		t.Fatalf("SearchMode = %q, want %q", meta.SearchMode, "full_file")
	}
}

func TestDetectReturnsErrorForMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-frame.dll")

	_, _, err := New().detect(path, config.OffsetCache{})
	if err == nil {
		t.Fatalf("detect expected error for missing file")
	}
}

func TestRemovePatchesAllOriginalOffsetsAfterBackup(t *testing.T) {
	installRoot, targetPath, oldOffsets, _ := writeInstalledFrameSample(t, samplePlacement{oldCount: 2})
	env := fakeEnv{installPath: installRoot}
	tx := &fakeTx{
		t:            t,
		targetPath:   targetPath,
		requireExist: DefaultRule().OldPattern,
	}

	err := New().Remove(context.Background(), env, tx)
	if err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}

	if len(tx.backupCalls) != 1 {
		t.Fatalf("backup call count = %d, want 1", len(tx.backupCalls))
	}
	call := tx.backupCalls[0]
	if call.relativePath != DefaultRule().FileRelativePath {
		t.Fatalf("backup relativePath = %q, want %q", call.relativePath, DefaultRule().FileRelativePath)
	}
	if call.sourcePath != targetPath {
		t.Fatalf("backup sourcePath = %q, want %q", call.sourcePath, targetPath)
	}

	assertPatternAtOffsets(t, targetPath, DefaultRule().NewPattern, oldOffsets)
	assertFeatureStateAtPath(t, targetPath, feature.StatePatched)
}

func TestRestoreRevertsAllPatchedOffsetsAfterBackup(t *testing.T) {
	installRoot, targetPath, _, newOffsets := writeInstalledFrameSample(t, samplePlacement{newCount: 2})
	env := fakeEnv{installPath: installRoot}
	tx := &fakeTx{
		t:            t,
		targetPath:   targetPath,
		requireExist: DefaultRule().NewPattern,
	}

	err := New().Restore(context.Background(), env, tx)
	if err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}

	if len(tx.backupCalls) != 1 {
		t.Fatalf("backup call count = %d, want 1", len(tx.backupCalls))
	}

	assertPatternAtOffsets(t, targetPath, DefaultRule().OldPattern, newOffsets)
	assertFeatureStateAtPath(t, targetPath, feature.StateOriginal)
}

func TestRemoveDoesNotWriteWhenBackupFails(t *testing.T) {
	installRoot, targetPath, _, _ := writeInstalledFrameSample(t, samplePlacement{oldCount: 2})
	env := fakeEnv{installPath: installRoot}
	originalContent := mustReadFile(t, targetPath)
	tx := &fakeTx{
		t:            t,
		targetPath:   targetPath,
		requireExist: DefaultRule().OldPattern,
		backupErr:    errors.New("backup failed"),
	}

	err := New().Remove(context.Background(), env, tx)
	if err == nil {
		t.Fatalf("Remove expected backup error")
	}
	if !errors.Is(err, tx.backupErr) {
		t.Fatalf("Remove error = %v, want backup error %v", err, tx.backupErr)
	}
	if len(tx.backupCalls) != 1 {
		t.Fatalf("backup call count = %d, want 1", len(tx.backupCalls))
	}

	currentContent := mustReadFile(t, targetPath)
	if !bytes.Equal(currentContent, originalContent) {
		t.Fatalf("file content changed after backup failure")
	}
	assertFeatureStateAtPath(t, targetPath, feature.StateOriginal)
}

func TestRemoveRejectsStateThatIsNotOriginal(t *testing.T) {
	tests := []struct {
		name      string
		placement samplePlacement
	}{
		{name: "mixed", placement: samplePlacement{oldCount: 1, newCount: 1}},
		{name: "unknown", placement: samplePlacement{oldCount: 1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			installRoot, targetPath, _, _ := writeInstalledFrameSample(t, test.placement)
			env := fakeEnv{installPath: installRoot}
			tx := &fakeTx{
				t:          t,
				targetPath: targetPath,
			}

			err := New().Remove(context.Background(), env, tx)
			if err == nil {
				t.Fatalf("Remove expected state rejection error")
			}
			if !errors.Is(err, ErrFrameActionNotAllowed) {
				t.Fatalf("Remove error = %v, want ErrFrameActionNotAllowed", err)
			}
			if len(tx.backupCalls) != 0 {
				t.Fatalf("backup should not be called, got %d call(s)", len(tx.backupCalls))
			}
		})
	}
}

func TestRemoveReturnsContextErrorBeforeWriting(t *testing.T) {
	installRoot, targetPath, _, _ := writeInstalledFrameSample(t, samplePlacement{oldCount: 2})
	env := fakeEnv{installPath: installRoot}
	tx := &fakeTx{
		t:          t,
		targetPath: targetPath,
	}
	originalContent := mustReadFile(t, targetPath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := New().Remove(ctx, env, tx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Remove error = %v, want context.Canceled", err)
	}
	if len(tx.backupCalls) != 0 {
		t.Fatalf("backup should not be called when context already canceled")
	}
	if !bytes.Equal(mustReadFile(t, targetPath), originalContent) {
		t.Fatalf("file content changed after context cancellation")
	}
}

type samplePlacement struct {
	oldCount int
	newCount int
}

type fakeEnv struct {
	installPath string
}

func (e fakeEnv) InstallPath() string {
	return e.installPath
}

type fakeBackupCall struct {
	relativePath string
	sourcePath   string
}

type fakeTx struct {
	t            *testing.T
	targetPath   string
	requireExist []byte
	backupErr    error
	backupCalls  []fakeBackupCall
}

func (tx *fakeTx) BackupFile(relativePath, sourcePath string) error {
	tx.t.Helper()

	tx.backupCalls = append(tx.backupCalls, fakeBackupCall{
		relativePath: relativePath,
		sourcePath:   sourcePath,
	})

	if tx.targetPath != "" && sourcePath != tx.targetPath {
		tx.t.Fatalf("BackupFile sourcePath = %q, want %q", sourcePath, tx.targetPath)
	}
	if len(tx.requireExist) > 0 {
		content := mustReadFile(tx.t, sourcePath)
		if !bytes.Contains(content, tx.requireExist) {
			tx.t.Fatalf("BackupFile observed unexpected file content before write")
		}
	}

	return tx.backupErr
}

func writeFrameSample(t *testing.T, placement samplePlacement) (string, []int64, []int64) {
	t.Helper()

	rule := DefaultRule()
	var data bytes.Buffer
	oldOffsets := make([]int64, 0, placement.oldCount)
	newOffsets := make([]int64, 0, placement.newCount)

	writeNoise(&data, 37)
	for i := 0; i < placement.oldCount; i++ {
		oldOffsets = append(oldOffsets, int64(data.Len()))
		data.Write(rule.OldPattern)
		writeNoise(&data, 29+i)
	}
	for i := 0; i < placement.newCount; i++ {
		newOffsets = append(newOffsets, int64(data.Len()))
		data.Write(rule.NewPattern)
		writeNoise(&data, 31+i)
	}

	path := filepath.Join(t.TempDir(), "frame.dll")
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	return path, oldOffsets, newOffsets
}

func writeInstalledFrameSample(t *testing.T, placement samplePlacement) (string, string, []int64, []int64) {
	t.Helper()

	installRoot := t.TempDir()
	targetPath := filepath.Join(installRoot, DefaultRule().FileRelativePath)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("create install dir: %v", err)
	}

	sourcePath, oldOffsets, newOffsets := writeFrameSample(t, placement)
	content := mustReadFile(t, sourcePath)
	if err := os.WriteFile(targetPath, content, 0o600); err != nil {
		t.Fatalf("write installed sample: %v", err)
	}

	return installRoot, targetPath, oldOffsets, newOffsets
}

func cacheForFile(t *testing.T, path string) config.OffsetCache {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat sample: %v", err)
	}

	return config.OffsetCache{
		FileSize: info.Size(),
		MTime:    info.ModTime().UTC().Format(time.RFC3339Nano),
	}
}

func writeNoise(buffer *bytes.Buffer, count int) {
	for i := 0; i < count; i++ {
		buffer.WriteByte(byte('A' + i%26))
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %q: %v", path, err)
	}

	return content
}

func assertPatternAtOffsets(t *testing.T, path string, pattern []byte, offsets []int64) {
	t.Helper()

	content := mustReadFile(t, path)
	for _, offset := range offsets {
		start := int(offset)
		end := start + len(pattern)
		if start < 0 || end > len(content) {
			t.Fatalf("offset %d out of range for file length %d", offset, len(content))
		}
		if !bytes.Equal(content[start:end], pattern) {
			t.Fatalf("pattern mismatch at offset %d", offset)
		}
	}
}

func assertFeatureStateAtPath(t *testing.T, path string, want feature.InternalState) {
	t.Helper()

	state, _, err := New().detect(path, config.OffsetCache{})
	if err != nil {
		t.Fatalf("detect state at path %q: %v", path, err)
	}

	assertState(t, state, want)
}

func assertState(t *testing.T, state feature.State, want feature.InternalState) {
	t.Helper()

	if state.Normalized().Internal != want {
		t.Fatalf("state = %q, want %q", state.Normalized().Internal, want)
	}
}

func assertOffsets(t *testing.T, got []int64, want []int64) {
	t.Helper()

	if len(want) == 0 {
		want = []int64{}
	}
	if len(got) == 0 {
		got = []int64{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("offsets = %v, want %v", got, want)
	}
}
