package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// PromptInstallPath 询问用户本次要使用的飞书安装路径。
//
// currentPath 会作为默认值显示在提示中；用户直接回车时返回 currentPath，便于
// 每次启动都“确认一次路径”，同时又不需要重复完整输入。
func (c *Console) PromptInstallPath(currentPath string) (string, error) {
	if strings.TrimSpace(currentPath) == "" {
		currentPath = `C:\Program Files\Feishu`
	}

	if _, err := fmt.Fprintf(c.out, "请输入飞书安装路径，直接回车使用 [%s]: ", currentPath); err != nil {
		return "", err
	}

	line, err := c.readLine()
	if err != nil {
		return "", err
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return currentPath, nil
	}

	return line, nil
}

// readLine 从控制台输入流中读取一行用户输入。
//
// 该方法会兼容最后一行没有换行符的 EOF 场景：只要已经读到内容，就把它当成有效
// 输入返回，而不是直接视为错误。
func (c *Console) readLine() (string, error) {
	if c == nil || c.reader == nil {
		return "", io.EOF
	}

	line, err := c.reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && line != "" {
			return strings.TrimRight(line, "\r\n"), nil
		}
		return "", err
	}

	return strings.TrimRight(line, "\r\n"), nil
}
