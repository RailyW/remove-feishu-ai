package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateCreatesDefaultConfigWhenFileMissing(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "nested", "config.json")

	cfg, err := LoadOrCreate(configPath)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}

	want := Default()
	assertConfigEquals(t, cfg, want)

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	var saved Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	assertConfigEquals(t, saved, want)
}

func TestLoadOrCreateFillsMissingFieldsAndNilMaps(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	initialJSON := `{
  "last_install_path": "D:\\Apps\\Feishu",
  "strict_mode_default": false
}`
	if err := os.WriteFile(configPath, []byte(initialJSON), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	cfg, err := LoadOrCreate(configPath)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}

	if cfg.LastInstallPath != `D:\Apps\Feishu` {
		t.Fatalf("LastInstallPath = %q, want %q", cfg.LastInstallPath, `D:\Apps\Feishu`)
	}
	if cfg.BackupRoot != Default().BackupRoot {
		t.Fatalf("BackupRoot = %q, want %q", cfg.BackupRoot, Default().BackupRoot)
	}
	if cfg.StrictModeDefault {
		t.Fatalf("StrictModeDefault = %v, want false", cfg.StrictModeDefault)
	}
	if cfg.LastSuccessOffsets == nil {
		t.Fatal("LastSuccessOffsets should be initialized")
	}
	if cfg.LastBundleRelativePaths == nil {
		t.Fatal("LastBundleRelativePaths should be initialized")
	}
	if cfg.LastRuleHits == nil {
		t.Fatal("LastRuleHits should be initialized")
	}
}

func TestSaveWritesJSONReadableByLoadOrCreate(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config", "settings.json")
	cfg := Default()
	cfg.LastInstallPath = `E:\Portable\Feishu`
	cfg.BackupRoot = "custom-backup"
	cfg.StrictModeDefault = false
	cfg.LastBundleRelativePath = filepath.Join("app", "webcontent", "messenger-vc", "common", "bundle.js")
	cfg.LastBundleRelativePaths["knowledge_sidebar"] = "knowledge.js"
	cfg.LastBundleRelativePaths["group_summary"] = "group.js"
	cfg.LastSuccessOffsets["frame.dll"] = OffsetCache{
		OldPatternOffsets: []int64{10, 20},
		NewPatternOffsets: []int64{30},
		FileSize:          1234,
		MTime:             "2026-04-23T17:30:00Z",
	}
	cfg.LastRuleHits["rules.json"] = RuleHitCache{
		FileSize: 5678,
		MTime:    "2026-04-23T17:31:00Z",
	}

	if err := Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := LoadOrCreate(configPath)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}

	assertConfigEquals(t, loaded, cfg)
}

func assertConfigEquals(t *testing.T, got Config, want Config) {
	t.Helper()

	if got.LastInstallPath != want.LastInstallPath {
		t.Fatalf("LastInstallPath = %q, want %q", got.LastInstallPath, want.LastInstallPath)
	}
	if got.BackupRoot != want.BackupRoot {
		t.Fatalf("BackupRoot = %q, want %q", got.BackupRoot, want.BackupRoot)
	}
	if got.StrictModeDefault != want.StrictModeDefault {
		t.Fatalf("StrictModeDefault = %v, want %v", got.StrictModeDefault, want.StrictModeDefault)
	}
	if got.LastBundleRelativePath != want.LastBundleRelativePath {
		t.Fatalf("LastBundleRelativePath = %q, want %q", got.LastBundleRelativePath, want.LastBundleRelativePath)
	}
	if !equalStringMap(got.LastBundleRelativePaths, want.LastBundleRelativePaths) {
		t.Fatalf("LastBundleRelativePaths = %#v, want %#v", got.LastBundleRelativePaths, want.LastBundleRelativePaths)
	}
	if !equalOffsetCacheMap(got.LastSuccessOffsets, want.LastSuccessOffsets) {
		t.Fatalf("LastSuccessOffsets = %#v, want %#v", got.LastSuccessOffsets, want.LastSuccessOffsets)
	}
	if !equalRuleHitCacheMap(got.LastRuleHits, want.LastRuleHits) {
		t.Fatalf("LastRuleHits = %#v, want %#v", got.LastRuleHits, want.LastRuleHits)
	}
}

func equalStringMap(a map[string]string, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for key, av := range a {
		bv, ok := b[key]
		if !ok || av != bv {
			return false
		}
	}

	return true
}

func equalOffsetCacheMap(a map[string]OffsetCache, b map[string]OffsetCache) bool {
	if len(a) != len(b) {
		return false
	}

	for key, av := range a {
		bv, ok := b[key]
		if !ok {
			return false
		}
		if av.FileSize != bv.FileSize || av.MTime != bv.MTime {
			return false
		}
		if !equalInt64Slice(av.OldPatternOffsets, bv.OldPatternOffsets) {
			return false
		}
		if !equalInt64Slice(av.NewPatternOffsets, bv.NewPatternOffsets) {
			return false
		}
	}

	return true
}

func equalRuleHitCacheMap(a map[string]RuleHitCache, b map[string]RuleHitCache) bool {
	if len(a) != len(b) {
		return false
	}

	for key, av := range a {
		bv, ok := b[key]
		if !ok {
			return false
		}
		if av != bv {
			return false
		}
	}

	return true
}

func equalInt64Slice(a []int64, b []int64) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
