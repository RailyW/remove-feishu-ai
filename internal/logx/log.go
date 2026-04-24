package logx

import (
	"fmt"
	"io"
	"os"
	"sync"
)

type Logger struct {
	verbose bool
	out     io.Writer
	err     io.Writer
	mu      sync.Mutex
}

func New(verbose bool) *Logger {
	return NewWithWriters(verbose, os.Stdout, os.Stderr)
}

func NewWithWriters(verbose bool, out io.Writer, err io.Writer) *Logger {
	if out == nil {
		out = io.Discard
	}

	if err == nil {
		err = io.Discard
	}

	return &Logger{
		verbose: verbose,
		out:     out,
		err:     err,
	}
}

func (l *Logger) Info(format string, args ...any) {
	l.print("", format, args...)
}

func (l *Logger) Success(format string, args ...any) {
	l.print("[OK] ", format, args...)
}

func (l *Logger) Warn(format string, args ...any) {
	l.print("[WARN] ", format, args...)
}

func (l *Logger) Fail(format string, args ...any) {
	if l == nil {
		return
	}

	l.printTo(l.err, "[FAIL] ", format, args...)
}

func (l *Logger) Debug(format string, args ...any) {
	if l == nil || !l.verbose {
		return
	}

	l.printTo(l.out, "[DEBUG] ", format, args...)
}

func (l *Logger) print(prefix, format string, args ...any) {
	if l == nil {
		return
	}

	l.printTo(l.out, prefix, format, args...)
}

func (l *Logger) printTo(w io.Writer, prefix, format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}

	if !l.verbose && len(msg) > 240 {
		msg = msg[:240] + "..."
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	fmt.Fprintln(w, prefix+msg)
}
