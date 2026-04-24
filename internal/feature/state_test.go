package feature

import "testing"

func TestStateCapabilities(t *testing.T) {
	tests := []struct {
		name       string
		state      State
		canRemove  bool
		canRestore bool
	}{
		{
			name:       "original allows remove only",
			state:      State{Internal: StateOriginal},
			canRemove:  true,
			canRestore: false,
		},
		{
			name:       "patched allows restore only",
			state:      State{Internal: StatePatched},
			canRemove:  false,
			canRestore: true,
		},
		{
			name:       "mixed allows neither",
			state:      State{Internal: StateMixed},
			canRemove:  false,
			canRestore: false,
		},
		{
			name:       "unknown allows neither",
			state:      State{Internal: StateUnknown},
			canRemove:  false,
			canRestore: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.CanRemove(); got != tt.canRemove {
				t.Fatalf("CanRemove() = %v, want %v", got, tt.canRemove)
			}

			if got := tt.state.CanRestore(); got != tt.canRestore {
				t.Fatalf("CanRestore() = %v, want %v", got, tt.canRestore)
			}
		})
	}
}

func TestStateDisplayStringForMixed(t *testing.T) {
	state := State{Internal: StateMixed}

	if got, want := state.DisplayString(), "部分修改，已停止"; got != want {
		t.Fatalf("DisplayString() = %q, want %q", got, want)
	}
}

func TestStateDisplayStringUsesUserFacingCopy(t *testing.T) {
	tests := []struct {
		name  string
		state State
		want  string
	}{
		{
			name:  "original",
			state: State{Internal: StateOriginal},
			want:  "未禁用",
		},
		{
			name:  "patched",
			state: State{Internal: StatePatched},
			want:  "已禁用",
		},
		{
			name:  "unknown",
			state: State{Internal: StateUnknown},
			want:  "未识别，可能是飞书版本变化",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.DisplayString(); got != tt.want {
				t.Fatalf("DisplayString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestZeroStateIsTreatedAsUnknown(t *testing.T) {
	var state State

	if state.CanRemove() {
		t.Fatal("zero State CanRemove() = true, want false")
	}

	if state.CanRestore() {
		t.Fatal("zero State CanRestore() = true, want false")
	}

	if got, want := state.DisplayString(), "未识别，可能是飞书版本变化"; got != want {
		t.Fatalf("zero State DisplayString() = %q, want %q", got, want)
	}
}
