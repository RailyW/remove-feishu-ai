package cli

import (
	"strings"
	"testing"
)

func TestPromptInstallPathUsesFallbackOnEmptyInput(t *testing.T) {
	console := NewConsole(strings.NewReader("\n"), &strings.Builder{})

	got, err := console.PromptInstallPath(`C:\Program Files\Feishu`)
	if err != nil {
		t.Fatalf("PromptInstallPath() error = %v", err)
	}

	if got != `C:\Program Files\Feishu` {
		t.Fatalf("PromptInstallPath() = %q, want fallback path", got)
	}
}
