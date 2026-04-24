//go:build windows
// +build windows

package elevate

import (
	"errors"
	"reflect"
	"testing"
)

func TestBuildElevatedArgsReturnsIndependentCopy(t *testing.T) {
	t.Parallel()

	original := []string{"--verbose", "--path", `C:\Program Files\Feishu`}

	got := buildElevatedArgs(original)
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("buildElevatedArgs() = %#v, want %#v", got, original)
	}

	got[0] = "--changed"
	if original[0] != "--verbose" {
		t.Fatalf("buildElevatedArgs() modified original slice, original = %#v", original)
	}
}

func TestQuoteArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "escapes spaces and quotes",
			args: []string{"--verbose", "--path", `C:\Program Files\Feishu`, `say "hello"`},
			want: `--verbose --path "C:\Program Files\Feishu" "say \"hello\""`,
		},
		{
			name: "keeps empty argument",
			args: []string{"--name", ""},
			want: `--name ""`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := quoteArgs(tt.args)
			if got != tt.want {
				t.Fatalf("quoteArgs() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsureAdminReturnsWithoutRelaunchWhenAlreadyAdmin(t *testing.T) {
	relaunched, err := ensureAdmin([]string{"--verbose"}, adminDeps{
		isAdmin: func() (bool, error) {
			return true, nil
		},
		relaunch: func(args []string) error {
			t.Fatalf("relaunch() should not be called, args = %#v", args)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ensureAdmin() error = %v", err)
	}
	if relaunched {
		t.Fatal("ensureAdmin() relaunched = true, want false")
	}
}

func TestEnsureAdminRelaunchesWithCopiedArgsWhenNotAdmin(t *testing.T) {
	input := []string{"--path", `C:\Program Files\Feishu`}
	var captured []string

	relaunched, err := ensureAdmin(input, adminDeps{
		isAdmin: func() (bool, error) {
			return false, nil
		},
		relaunch: func(args []string) error {
			captured = append([]string(nil), args...)
			if len(args) > 0 {
				args[0] = "--mutated"
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ensureAdmin() error = %v", err)
	}
	if !relaunched {
		t.Fatal("ensureAdmin() relaunched = false, want true")
	}

	want := []string{"--path", `C:\Program Files\Feishu`}
	if !reflect.DeepEqual(captured, want) {
		t.Fatalf("ensureAdmin() relaunch args = %#v, want %#v", captured, want)
	}
	if !reflect.DeepEqual(input, want) {
		t.Fatalf("ensureAdmin() modified input args, got %#v, want %#v", input, want)
	}
}

func TestEnsureAdminReturnsIsAdminError(t *testing.T) {
	wantErr := errors.New("check admin failed")
	relaunched, err := ensureAdmin([]string{"--verbose"}, adminDeps{
		isAdmin: func() (bool, error) {
			return false, wantErr
		},
		relaunch: func(args []string) error {
			t.Fatalf("relaunch() should not be called, args = %#v", args)
			return nil
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ensureAdmin() error = %v, want %v", err, wantErr)
	}
	if relaunched {
		t.Fatal("ensureAdmin() relaunched = true, want false")
	}
}

func TestEnsureAdminReturnsRelaunchError(t *testing.T) {
	wantErr := errors.New("relaunch failed")
	relaunched, err := ensureAdmin([]string{"--verbose"}, adminDeps{
		isAdmin: func() (bool, error) {
			return false, nil
		},
		relaunch: func(args []string) error {
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ensureAdmin() error = %v, want %v", err, wantErr)
	}
	if relaunched {
		t.Fatal("ensureAdmin() relaunched = true, want false")
	}
}

func TestShellExecuteErrorRetainsResultCodeAndUnderlyingError(t *testing.T) {
	t.Parallel()

	underlying := errors.New("access denied")
	err := shellExecuteError{
		code:       5,
		underlying: underlying,
	}

	if got := err.Error(); got != "ShellExecuteW failed with code 5: access denied" {
		t.Fatalf("shellExecuteError.Error() = %q", got)
	}
	if !errors.Is(err, underlying) {
		t.Fatal("shellExecuteError should unwrap underlying error")
	}
}

func TestShellExecuteErrorWithoutUnderlyingStillReportsCode(t *testing.T) {
	t.Parallel()

	err := shellExecuteError{code: 31}
	if got := err.Error(); got != "ShellExecuteW failed with code 31" {
		t.Fatalf("shellExecuteError.Error() = %q", got)
	}
}
