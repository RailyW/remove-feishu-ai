package featurejs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"remove-feishu-ai/internal/feature"
)

func TestLocateBundleSelectsFileWithAllStrongMarkers(t *testing.T) {
	commonDir := makeCommonDir(t)
	writeJSFile(t, commonDir, "small-without-markers.js", repeatedText("console.log(1);", 10))
	writeJSFile(t, commonDir, "larger-partial.js", repeatedText("settingKey:\"lark_knowledge_ai_client_setting\";", 20))
	targetPath := writeJSFile(t, commonDir, "a1.js", knowledgeSidebarJS(originalPatterns()))

	candidate, err := NewKnowledgeSidebarFeature().locateBundle(commonDir, "")
	if err != nil {
		t.Fatalf("locateBundle returned error: %v", err)
	}

	if candidate.Path != targetPath {
		t.Fatalf("candidate.Path = %q, want %q", candidate.Path, targetPath)
	}
	if candidate.RelativePath != "a1.js" {
		t.Fatalf("candidate.RelativePath = %q, want %q", candidate.RelativePath, "a1.js")
	}
	if candidate.Score != 300 {
		t.Fatalf("candidate.Score = %d, want %d", candidate.Score, 300)
	}
}

func TestLocateBundleReturnsNoCandidateWhenMultipleStrongMatchesTie(t *testing.T) {
	commonDir := makeCommonDir(t)
	writeJSFile(t, commonDir, "a1.js", knowledgeSidebarJS(originalPatterns()))
	writeJSFile(t, commonDir, "b2.js", knowledgeSidebarJS(originalPatterns()))

	candidate, err := NewKnowledgeSidebarFeature().locateBundle(commonDir, "")
	if err != nil {
		t.Fatalf("locateBundle returned error: %v", err)
	}

	if candidate.Path != "" || candidate.RelativePath != "" || candidate.Score != 0 {
		t.Fatalf("candidate = %#v, want zero-value candidate for ambiguous strong matches", candidate)
	}
}

func TestDetectWithCacheUsesCachedPathWhenMarkersStillMatch(t *testing.T) {
	installRoot, commonDir := makeInstalledCommonDir(t)
	writeJSFile(t, commonDir, "noise.js", "console.log('noise');")
	writeJSFile(t, commonDir, "cached.js", knowledgeSidebarJS(originalPatterns()))

	state, meta, err := NewKnowledgeSidebarFeature().DetectWithCache(
		context.Background(),
		fakeEnv{installPath: installRoot},
		"cached.js",
	)
	if err != nil {
		t.Fatalf("DetectWithCache returned error: %v", err)
	}

	assertJSState(t, state, feature.StateOriginal)
	if meta.LocateMode != "cache_path" {
		t.Fatalf("LocateMode = %q, want %q", meta.LocateMode, "cache_path")
	}
	if meta.RelativePath != "cached.js" {
		t.Fatalf("RelativePath = %q, want %q", meta.RelativePath, "cached.js")
	}
}

func TestDetectWithCacheReturnsUnknownWhenCachedCandidateBecomesAmbiguous(t *testing.T) {
	installRoot, commonDir := makeInstalledCommonDir(t)
	writeJSFile(t, commonDir, "a1.js", knowledgeSidebarJS(originalPatterns()))
	writeJSFile(t, commonDir, "b2.js", knowledgeSidebarJS(originalPatterns()))

	state, meta, err := NewKnowledgeSidebarFeature().DetectWithCache(
		context.Background(),
		fakeEnv{installPath: installRoot},
		"a1.js",
	)
	if err != nil {
		t.Fatalf("DetectWithCache returned error: %v", err)
	}

	assertJSState(t, state, feature.StateUnknown)
	if meta.BundlePath != "" || meta.RelativePath != "" || meta.Score != 0 || meta.OriginalCount != 0 || meta.PatchedCount != 0 {
		t.Fatalf("meta = %#v, want zero-value fields for ambiguous cached candidate", meta)
	}
}

func TestDetectWithCacheFallsBackToScanWhenCachedPathDoesNotExist(t *testing.T) {
	installRoot, commonDir := makeInstalledCommonDir(t)
	writeJSFile(t, commonDir, "target.js", knowledgeSidebarJS(originalPatterns()))

	state, meta, err := NewKnowledgeSidebarFeature().DetectWithCache(
		context.Background(),
		fakeEnv{installPath: installRoot},
		"missing.js",
	)
	if err != nil {
		t.Fatalf("DetectWithCache returned error: %v", err)
	}

	assertJSState(t, state, feature.StateOriginal)
	if meta.LocateMode != "scan" {
		t.Fatalf("LocateMode = %q, want %q", meta.LocateMode, "scan")
	}
	if meta.RelativePath != "target.js" {
		t.Fatalf("RelativePath = %q, want %q", meta.RelativePath, "target.js")
	}
}

func TestDetectWithCacheAcceptsInstallRootRelativeCachedPath(t *testing.T) {
	installRoot, commonDir := makeInstalledCommonDir(t)
	writeJSFile(t, commonDir, "cached.js", knowledgeSidebarJS(originalPatterns()))

	state, meta, err := NewKnowledgeSidebarFeature().DetectWithCache(
		context.Background(),
		fakeEnv{installPath: installRoot},
		filepath.Join(DefaultKnowledgeSidebarRule().BundleDir, "cached.js"),
	)
	if err != nil {
		t.Fatalf("DetectWithCache returned error: %v", err)
	}

	assertJSState(t, state, feature.StateOriginal)
	if meta.LocateMode != "cache_path" {
		t.Fatalf("LocateMode = %q, want %q", meta.LocateMode, "cache_path")
	}
	if meta.RelativePath != "cached.js" {
		t.Fatalf("RelativePath = %q, want %q", meta.RelativePath, "cached.js")
	}
}

func TestDetectWithCacheIgnoresUnsafeCachedPathAndFallsBackToScan(t *testing.T) {
	installRoot, commonDir := makeInstalledCommonDir(t)
	writeJSFile(t, commonDir, "target.js", knowledgeSidebarJS(originalPatterns()))

	state, meta, err := NewKnowledgeSidebarFeature().DetectWithCache(
		context.Background(),
		fakeEnv{installPath: installRoot},
		`..\evil.js`,
	)
	if err != nil {
		t.Fatalf("DetectWithCache returned error: %v", err)
	}

	assertJSState(t, state, feature.StateOriginal)
	if meta.LocateMode != "scan" {
		t.Fatalf("LocateMode = %q, want %q", meta.LocateMode, "scan")
	}
	if meta.RelativePath != "target.js" {
		t.Fatalf("RelativePath = %q, want %q", meta.RelativePath, "target.js")
	}
}

func TestKnowledgeSidebarDetectClassifiesOriginalPatchedAndMixed(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		want     feature.InternalState
	}{
		{name: "original", patterns: originalPatterns(), want: feature.StateOriginal},
		{name: "patched", patterns: patchedPatterns(), want: feature.StatePatched},
		{name: "mixed", patterns: append(originalPatterns()[:1], patchedPatterns()[:1]...), want: feature.StateMixed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			commonDir := makeCommonDir(t)
			writeJSFile(t, commonDir, "b2.js", knowledgeSidebarJS(test.patterns))

			state, meta, err := NewKnowledgeSidebarFeature().detect(commonDir, "")
			if err != nil {
				t.Fatalf("detect returned error: %v", err)
			}

			assertJSState(t, state, test.want)
			if meta.LocateMode != "scan" {
				t.Fatalf("LocateMode = %q, want %q", meta.LocateMode, "scan")
			}
		})
	}
}

func TestKnowledgeSidebarDetectReturnsUnknownWhenSingleOriginalPatternRepeats(t *testing.T) {
	commonDir := makeCommonDir(t)
	writeJSFile(t, commonDir, "dup.js", knowledgeSidebarJS([]string{
		originalPatterns()[0],
		originalPatterns()[0],
	}))

	state, _, err := NewKnowledgeSidebarFeature().detect(commonDir, "")
	if err != nil {
		t.Fatalf("detect returned error: %v", err)
	}

	assertJSState(t, state, feature.StateUnknown)
}

func TestKnowledgeSidebarRemovePatchesOriginalBundleAfterBackup(t *testing.T) {
	installRoot, commonDir := makeInstalledCommonDir(t)
	targetPath := writeJSFile(t, commonDir, "a1.js", knowledgeSidebarJS(originalPatterns()))
	tx := &fakeTx{t: t, wantRelativePath: filepath.Join(DefaultKnowledgeSidebarRule().BundleDir, "a1.js"), wantSourcePath: targetPath}

	if err := NewKnowledgeSidebarFeature().Remove(context.Background(), fakeEnv{installPath: installRoot}, tx); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}

	tx.assertBackupCalledOnce(t)
	assertBundleContentState(t, targetPath, feature.StatePatched)
}

func TestKnowledgeSidebarRestoreRevertsPatchedBundleAfterBackup(t *testing.T) {
	installRoot, commonDir := makeInstalledCommonDir(t)
	targetPath := writeJSFile(t, commonDir, "a1.js", knowledgeSidebarJS(patchedPatterns()))
	tx := &fakeTx{t: t, wantRelativePath: filepath.Join(DefaultKnowledgeSidebarRule().BundleDir, "a1.js"), wantSourcePath: targetPath}

	if err := NewKnowledgeSidebarFeature().Restore(context.Background(), fakeEnv{installPath: installRoot}, tx); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}

	tx.assertBackupCalledOnce(t)
	assertBundleContentState(t, targetPath, feature.StateOriginal)
}

func TestKnowledgeSidebarRemoveDoesNotWriteWhenBackupFails(t *testing.T) {
	installRoot, commonDir := makeInstalledCommonDir(t)
	targetPath := writeJSFile(t, commonDir, "a1.js", knowledgeSidebarJS(originalPatterns()))
	originalContent := readTextFile(t, targetPath)
	backupErr := errors.New("backup failed")
	tx := &fakeTx{
		t:                t,
		wantRelativePath: filepath.Join(DefaultKnowledgeSidebarRule().BundleDir, "a1.js"),
		wantSourcePath:   targetPath,
		err:              backupErr,
	}

	err := NewKnowledgeSidebarFeature().Remove(context.Background(), fakeEnv{installPath: installRoot}, tx)
	if !errors.Is(err, backupErr) {
		t.Fatalf("Remove error = %v, want backup failure", err)
	}

	tx.assertBackupCalledOnce(t)
	if got := readTextFile(t, targetPath); got != originalContent {
		t.Fatalf("bundle content changed after backup failure")
	}
}

func TestReplaceFileWithTempKeepsOriginalWhenAtomicReplaceFails(t *testing.T) {
	root := t.TempDir()
	targetPath := writeJSFile(t, root, "bundle.js", "original-content")

	err := replaceFileWithTempUsing(targetPath, []byte("patched-content"), func(tmpPath string, dstPath string) error {
		if tmpPath != targetPath+".tmp" {
			t.Fatalf("tmpPath = %q, want %q", tmpPath, targetPath+".tmp")
		}
		if dstPath != targetPath {
			t.Fatalf("dstPath = %q, want %q", dstPath, targetPath)
		}
		if got := readTextFile(t, tmpPath); got != "patched-content" {
			t.Fatalf("tmp content = %q, want %q", got, "patched-content")
		}
		return errors.New("replace failed")
	})
	if err == nil {
		t.Fatal("replaceFileWithTempUsing() error = nil, want failure")
	}

	if got := readTextFile(t, targetPath); got != "original-content" {
		t.Fatalf("target content = %q, want original content preserved", got)
	}
	if _, statErr := os.Stat(targetPath + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatalf("tmp file still exists, stat error = %v", statErr)
	}
}

func TestKnowledgeSidebarRemoveRejectsMixedAndUnknownWithoutBackup(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "mixed", content: knowledgeSidebarJS(append(originalPatterns()[:1], patchedPatterns()[:1]...))},
		{name: "unknown", content: knowledgeSidebarJS(originalPatterns()[:1])},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			installRoot, commonDir := makeInstalledCommonDir(t)
			writeJSFile(t, commonDir, "a1.js", test.content)
			tx := &fakeTx{t: t}

			err := NewKnowledgeSidebarFeature().Remove(context.Background(), fakeEnv{installPath: installRoot}, tx)
			if !errors.Is(err, ErrJSActionNotAllowed) {
				t.Fatalf("Remove error = %v, want ErrJSActionNotAllowed", err)
			}
			tx.assertNoBackup(t)
		})
	}
}

func TestKnowledgeSidebarRemoveRejectsRepeatedSingleOriginalPatternWithoutBackup(t *testing.T) {
	installRoot, commonDir := makeInstalledCommonDir(t)
	writeJSFile(t, commonDir, "a1.js", knowledgeSidebarJS([]string{
		originalPatterns()[0],
		originalPatterns()[0],
	}))
	tx := &fakeTx{t: t}

	err := NewKnowledgeSidebarFeature().Remove(context.Background(), fakeEnv{installPath: installRoot}, tx)
	if !errors.Is(err, ErrJSActionNotAllowed) {
		t.Fatalf("Remove error = %v, want ErrJSActionNotAllowed", err)
	}
	tx.assertNoBackup(t)
}

func TestGroupSummaryRemoveRestoreRejectPlaceholderRuleWithoutBackup(t *testing.T) {
	installRoot, commonDir := makeInstalledCommonDir(t)
	writeJSFile(t, commonDir, "a1.js", knowledgeSidebarJS(originalPatterns()))

	featureUnderTest := NewGroupSummaryFeature()
	for _, test := range []struct {
		name   string
		action func(context.Context, feature.Env, feature.Tx) error
	}{
		{name: "remove", action: featureUnderTest.Remove},
		{name: "restore", action: featureUnderTest.Restore},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeTx{t: t}
			err := test.action(context.Background(), fakeEnv{installPath: installRoot}, tx)
			if !errors.Is(err, ErrJSActionNotAllowed) {
				t.Fatalf("%s error = %v, want ErrJSActionNotAllowed", test.name, err)
			}
			tx.assertNoBackup(t)
		})
	}
}

func TestGroupSummaryDetectReturnsUnknownWithPlaceholderRule(t *testing.T) {
	commonDir := makeCommonDir(t)
	writeJSFile(t, commonDir, "c3.js", knowledgeSidebarJS(originalPatterns()))

	state, _, err := NewGroupSummaryFeature().detect(commonDir, "")
	if err != nil {
		t.Fatalf("detect returned error: %v", err)
	}

	assertJSState(t, state, feature.StateUnknown)
}

func TestDetectReturnsUnknownWhenNoCandidateBundleFound(t *testing.T) {
	commonDir := makeCommonDir(t)
	writeJSFile(t, commonDir, "d4.js", "console.log('not target');")

	state, meta, err := NewKnowledgeSidebarFeature().detect(commonDir, "")
	if err != nil {
		t.Fatalf("detect returned error: %v", err)
	}

	assertJSState(t, state, feature.StateUnknown)
	if meta.BundlePath != "" || meta.RelativePath != "" || meta.Score != 0 || meta.OriginalCount != 0 || meta.PatchedCount != 0 || meta.LocateMode != "" || len(meta.OriginalPatternHits) != 0 || len(meta.PatchedPatternHits) != 0 {
		t.Fatalf("meta = %#v, want zero-value fields when no candidate is found", meta)
	}
}

type fakeEnv struct {
	installPath string
}

func (e fakeEnv) InstallPath() string {
	return e.installPath
}

type fakeTx struct {
	t                *testing.T
	wantRelativePath string
	wantSourcePath   string
	err              error
	backupCalls      int
}

func (tx *fakeTx) BackupFile(relativePath, sourcePath string) error {
	tx.t.Helper()

	tx.backupCalls++
	if tx.wantRelativePath != "" && filepath.Clean(relativePath) != filepath.Clean(tx.wantRelativePath) {
		tx.t.Fatalf("BackupFile relativePath = %q, want %q", relativePath, tx.wantRelativePath)
	}
	if tx.wantSourcePath != "" && sourcePath != tx.wantSourcePath {
		tx.t.Fatalf("BackupFile sourcePath = %q, want %q", sourcePath, tx.wantSourcePath)
	}

	return tx.err
}

func (tx *fakeTx) assertBackupCalledOnce(t *testing.T) {
	t.Helper()

	if tx.backupCalls != 1 {
		t.Fatalf("BackupFile called %d times, want 1", tx.backupCalls)
	}
}

func (tx *fakeTx) assertNoBackup(t *testing.T) {
	t.Helper()

	if tx.backupCalls != 0 {
		t.Fatalf("BackupFile called %d times, want 0", tx.backupCalls)
	}
}

func makeInstalledCommonDir(t *testing.T) (string, string) {
	t.Helper()

	installRoot := t.TempDir()
	commonDir := filepath.Join(installRoot, DefaultKnowledgeSidebarRule().BundleDir)
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatalf("create common dir: %v", err)
	}

	return installRoot, commonDir
}

func makeCommonDir(t *testing.T) string {
	t.Helper()

	_, commonDir := makeInstalledCommonDir(t)
	return commonDir
}

func writeJSFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write JS file %q: %v", path, err)
	}

	return path
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %q: %v", path, err)
	}

	return string(content)
}

func knowledgeSidebarJS(patterns []string) string {
	content := "settingKey:\"lark_knowledge_ai_client_setting\";\n"
	content += "pluginType:a.Vx.EDITOR_EXTENSION;\n"
	content += "lark__editor--extension-knowledge-qa;\n"
	for _, pattern := range patterns {
		content += pattern + ";\n"
	}

	return content
}

func originalPatterns() []string {
	return []string{
		"return s.P4.setEnable(u),u},h=e=>",
		"getShowExtension:()=>t.scene===a.pC.main?dt.P4.enable.main:dt.P4.enable.thread",
	}
}

func patchedPatterns() []string {
	return []string{
		"return s.P4.setEnable({main:!1,thread:!1}),{main:!1,thread:!1}},h=e=>",
		"getShowExtension:()=>!1",
	}
}

func repeatedText(text string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += text
	}

	return result
}

func assertJSState(t *testing.T, state feature.State, want feature.InternalState) {
	t.Helper()

	if state.Normalized().Internal != want {
		t.Fatalf("state = %q, want %q", state.Normalized().Internal, want)
	}
}

func assertBundleContentState(t *testing.T, path string, want feature.InternalState) {
	t.Helper()

	content := readTextFile(t, path)
	meta := DetectMeta{
		OriginalPatternHits: patternHits(content, originalPatterns()),
		PatchedPatternHits:  patternHits(content, patchedPatterns()),
		OriginalCount:       countPatternHits(patternHits(content, originalPatterns())),
		PatchedCount:        countPatternHits(patternHits(content, patchedPatterns())),
	}
	state := NewKnowledgeSidebarFeature().classify(meta)
	assertJSState(t, state, want)
}
