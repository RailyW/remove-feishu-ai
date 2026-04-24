package install

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAcceptsValidInstallDirectory(t *testing.T) {
	t.Parallel()

	root := createValidInstallDir(t, t.TempDir())

	resolved, err := Resolve(root, "")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := filepath.Clean(root)
	if resolved != want {
		t.Fatalf("Resolve() = %q, want %q", resolved, want)
	}
}

func TestResolveUsesFallbackWhenInputEmpty(t *testing.T) {
	t.Parallel()

	fallback := createValidInstallDir(t, t.TempDir())

	resolved, err := Resolve("   ", fallback)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := filepath.Clean(fallback)
	if resolved != want {
		t.Fatalf("Resolve() = %q, want %q", resolved, want)
	}
}

func TestResolveReturnsErrInstallPathInvalidWhenFrameMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	commonDir := filepath.Join(root, "app", "webcontent", "messenger-vc", "common")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	_, err := Resolve(root, "")
	if !errors.Is(err, ErrInstallPathInvalid) {
		t.Fatalf("Resolve() error = %v, want ErrInstallPathInvalid", err)
	}
}

func TestResolveReturnsErrInstallPathInvalidWhenCommonDirMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	framePath := filepath.Join(root, "app", "frame.dll")
	if err := os.MkdirAll(filepath.Dir(framePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(framePath, []byte("frame"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	_, err := Resolve(root, "")
	if !errors.Is(err, ErrInstallPathInvalid) {
		t.Fatalf("Resolve() error = %v, want ErrInstallPathInvalid", err)
	}
}

func createValidInstallDir(t *testing.T, root string) string {
	t.Helper()

	framePath := filepath.Join(root, "app", "frame.dll")
	commonDir := filepath.Join(root, "app", "webcontent", "messenger-vc", "common")

	if err := os.MkdirAll(filepath.Dir(framePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(frame dir) error = %v", err)
	}
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(common dir) error = %v", err)
	}
	if err := os.WriteFile(framePath, []byte("frame"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(frame) error = %v", err)
	}

	return root
}
