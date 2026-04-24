package logx

import (
	"bytes"
	"strings"
	"testing"
)

func TestNilLoggerMethodsDoNotPanic(t *testing.T) {
	var logger *Logger

	assertNotPanic(t, func() { logger.Info("info") })
	assertNotPanic(t, func() { logger.Success("success") })
	assertNotPanic(t, func() { logger.Warn("warn") })
	assertNotPanic(t, func() { logger.Fail("fail") })
	assertNotPanic(t, func() { logger.Debug("debug") })
}

func TestDebugIsHiddenUnlessVerbose(t *testing.T) {
	var out bytes.Buffer
	var err bytes.Buffer

	logger := NewWithWriters(false, &out, &err)
	logger.Debug("debug %s", "message")

	if got := out.String(); got != "" {
		t.Fatalf("non-verbose Debug wrote stdout %q, want empty", got)
	}

	if got := err.String(); got != "" {
		t.Fatalf("non-verbose Debug wrote stderr %q, want empty", got)
	}
}

func TestDebugWritesWhenVerbose(t *testing.T) {
	var out bytes.Buffer
	var err bytes.Buffer

	logger := NewWithWriters(true, &out, &err)
	logger.Debug("debug %s", "message")

	if got := out.String(); !strings.Contains(got, "[DEBUG] debug message") {
		t.Fatalf("verbose Debug stdout = %q, want debug message", got)
	}

	if got := err.String(); got != "" {
		t.Fatalf("verbose Debug stderr = %q, want empty", got)
	}
}

func assertNotPanic(t *testing.T, fn func()) {
	t.Helper()

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("unexpected panic: %v", recovered)
		}
	}()

	fn()
}
