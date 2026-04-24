// Package cli 提供控制台交互所需的菜单展示与输入解析能力。
//
// 本文件专注“主菜单”这一类交互：如何把应用层已经准备好的状态、动作列表
// 以稳定、克制的文本形式渲染到终端，并把用户输入的菜单编号解析为具体动作。
// 包内不理解底层补丁细节，也不直接访问飞书目录或事务文件。
package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Action 表示控制台主菜单中的一个用户动作。
//
// 该类型只承载“用户想做什么”，不承载任何底层补丁实现；应用层会根据 Action
// 决定执行批量移除、单项恢复、重选路径或查看备份等具体流程。
type Action string

const (
	// ActionRemoveAll 表示禁用当前可识别且可执行移除动作的全部功能。
	ActionRemoveAll Action = "remove_all"

	// ActionRestoreAll 表示恢复当前已经处于禁用状态的全部功能。
	ActionRestoreAll Action = "restore_all"

	// ActionRemoveKnowledgeSidebar 表示只禁用“侧边栏知识问答”。
	ActionRemoveKnowledgeSidebar Action = "remove_knowledge_sidebar"

	// ActionRestoreKnowledgeSidebar 表示只恢复“侧边栏知识问答”。
	ActionRestoreKnowledgeSidebar Action = "restore_knowledge_sidebar"

	// ActionRemoveGroupSummary 表示只禁用“群聊 AI 消息速览/群聊总结”。
	ActionRemoveGroupSummary Action = "remove_group_summary"

	// ActionRestoreGroupSummary 表示只恢复“群聊 AI 消息速览/群聊总结”。
	ActionRestoreGroupSummary Action = "restore_group_summary"

	// ActionReselectInstallPath 表示重新选择飞书安装路径。
	ActionReselectInstallPath Action = "reselect_install_path"

	// ActionShowRecentBackups 表示查看程序目录 backup 下最近的事务备份。
	ActionShowRecentBackups Action = "show_recent_backups"

	// ActionExit 表示退出程序。
	ActionExit Action = "exit"
)

// MenuItem 描述主菜单中的一个可显示动作。
//
// Label 是展示给用户的文案；Enabled 表示该动作当前是否允许被选择。即使
// Enabled 为 false，本项仍会显示在菜单中，便于用户直观看到“为什么当前不可用”。
type MenuItem struct {
	Action  Action
	Label   string
	Enabled bool
}

// StatusLine 描述主界面中一个用户可见功能的状态行。
//
// DisplayName 是功能名称，例如“侧边栏知识问答”；StateText 是状态机转换后的
// 用户可读状态，例如“未禁用”“已禁用”或“未识别，可能是飞书版本变化”。
type StatusLine struct {
	DisplayName string
	StateText   string
}

// MenuScreen 描述一次主菜单渲染所需的完整界面数据。
//
// InstallPath 与 StrictMode 组成顶部概览；Statuses 展示两个目标功能的当前状态；
// Items 则是可以被选择的操作列表。
type MenuScreen struct {
	InstallPath string
	StrictMode  bool
	Statuses    []StatusLine
	Items       []MenuItem
}

// Console 封装一次控制台会话需要的输入输出流。
//
// reader 通过 bufio.Reader 逐行消费用户输入，避免直接把整个终端流读入内存；
// out 则承载所有正常提示输出。错误信息是否进入 stderr 由上层 logger 决定。
type Console struct {
	reader *bufio.Reader
	out    io.Writer
}

// NewConsole 构造一份可用于控制台交互的 Console。
//
// 传入 nil 时会自动回退为安全的空输入/空输出，保证测试和上层依赖注入时不会
// 因空指针崩溃。
func NewConsole(in io.Reader, out io.Writer) *Console {
	if in == nil {
		in = bytes.NewReader(nil)
	}
	if out == nil {
		out = io.Discard
	}

	return &Console{
		reader: bufio.NewReader(in),
		out:    out,
	}
}

// SelectAction 渲染主菜单并循环读取一个合法动作。
//
// 该方法会持续提示用户输入编号，直到读到一个存在且已启用的菜单项；如果用户
// 选择了当前不可用的动作，会明确提示“当前不可用”并继续等待新的输入。
func (c *Console) SelectAction(screen MenuScreen) (Action, error) {
	if err := c.renderMenu(screen); err != nil {
		return "", err
	}

	for {
		if _, err := fmt.Fprint(c.out, "请输入编号: "); err != nil {
			return "", err
		}

		line, err := c.readLine()
		if err != nil {
			return "", err
		}

		index, parseErr := strconv.Atoi(strings.TrimSpace(line))
		if parseErr != nil || index < 1 || index > len(screen.Items) {
			if _, err := fmt.Fprintln(c.out, "输入无效，请输入菜单前的数字编号。"); err != nil {
				return "", err
			}
			continue
		}

		item := screen.Items[index-1]
		if !item.Enabled {
			if _, err := fmt.Fprintln(c.out, "该操作当前不可用，请重新选择。"); err != nil {
				return "", err
			}
			continue
		}

		return item.Action, nil
	}
}

// renderMenu 将主界面渲染为稳定、可读的文本块。
//
// 输出风格保持克制：只展示安装路径、严格模式、功能状态和菜单项，不打印任何
// 长文本内容，也不把底层扫描细节直接暴露到终端。
func (c *Console) renderMenu(screen MenuScreen) error {
	strictModeText := "关闭"
	if screen.StrictMode {
		strictModeText = "开启（批量操作失败会自动回滚）"
	}

	if _, err := fmt.Fprintln(c.out, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(c.out, "Feishu AI Patcher"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.out, "安装路径：%s\n", screen.InstallPath); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.out, "严格模式：%s\n", strictModeText); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(c.out, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(c.out, "功能状态："); err != nil {
		return err
	}

	for _, status := range screen.Statuses {
		if _, err := fmt.Fprintf(c.out, "- %s：%s\n", status.DisplayName, status.StateText); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(c.out, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(c.out, "可选操作："); err != nil {
		return err
	}
	for index, item := range screen.Items {
		suffix := ""
		if !item.Enabled {
			suffix = "（当前不可用）"
		}
		if _, err := fmt.Fprintf(c.out, "%d. %s%s\n", index+1, item.Label, suffix); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(c.out, ""); err != nil {
		return err
	}

	return nil
}
