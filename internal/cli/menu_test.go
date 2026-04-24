package cli

import (
	"strings"
	"testing"
)

func TestConsoleSelectActionSkipsDisabledChoice(t *testing.T) {
	input := strings.NewReader("2\n3\n")
	output := &strings.Builder{}
	console := NewConsole(input, output)

	action, err := console.SelectAction(MenuScreen{
		InstallPath: `C:\Program Files\Feishu`,
		StrictMode:  true,
		Statuses: []StatusLine{
			{DisplayName: "侧边栏知识问答", StateText: "未禁用"},
		},
		Items: []MenuItem{
			{Action: ActionRemoveAll, Label: "禁用全部可识别功能", Enabled: true},
			{Action: ActionRestoreAll, Label: "恢复全部已禁用功能", Enabled: false},
			{Action: ActionExit, Label: "退出", Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("SelectAction() error = %v", err)
	}

	if action != ActionExit {
		t.Fatalf("SelectAction() = %q, want %q", action, ActionExit)
	}

	if !strings.Contains(output.String(), "当前不可用") {
		t.Fatalf("output = %q, want disabled hint", output.String())
	}
}
