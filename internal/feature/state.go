package feature

type InternalState string

const (
	StateUnknown  InternalState = "unknown"
	StateOriginal InternalState = "original"
	StatePatched  InternalState = "patched"
	StateMixed    InternalState = "mixed"
)

type State struct {
	Internal InternalState
}

func (s State) CanRemove() bool {
	return s.Normalized().Internal == StateOriginal
}

func (s State) CanRestore() bool {
	return s.Normalized().Internal == StatePatched
}

func (s State) Normalized() State {
	if s.Internal == "" {
		s.Internal = StateUnknown
	}

	return s
}

func (s State) DisplayString() string {
	switch s.Normalized().Internal {
	case StateOriginal:
		return "未禁用"
	case StatePatched:
		return "已禁用"
	case StateMixed:
		return "部分修改，已停止"
	default:
		return "未识别，可能是飞书版本变化"
	}
}
