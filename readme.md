# 飞书 AI 功能补丁工具

`remove-feishu-ai` 是一个面向 Windows 的 Go 控制台工具，用于在本地飞书安装目录中检测、禁用或恢复部分 AI 相关功能。当前 Go 版本入口位于 `cmd/feishu-ai-patcher`，程序启动后会通过菜单引导用户确认飞书安装路径、查看功能状态，并按需执行补丁或恢复。

> 本工具会修改本机飞书安装目录下的文件。请只在你有权限维护的设备上使用，并在执行前关闭飞书客户端。

## 当前能力

- **侧边栏知识问答**：同时处理 `frame.dll` 与对应前端 JS bundle 中的知识问答入口。
- **群聊 AI 消息速览/群聊总结**：处理飞书前端 JS bundle 中的群聊 AI 总结入口。
- **自动备份**：每次写入前都会把被修改文件备份到程序目录下的 `backup` 目录。
- **事务回滚**：默认严格模式下，批量操作任一目标失败会回滚本次已经完成的修改。
- **配置缓存**：程序目录下的 `config.json` 会记录上次安装路径、备份目录和扫描缓存。

## 目录结构

- `cmd/feishu-ai-patcher/`：Go 可执行程序入口，只负责启动应用层并返回退出码。
- `internal/app/`：控制台应用编排层，串联权限、配置、扫描、菜单和事务执行。
- `internal/featureframe/`：负责 `frame.dll` 规则检测、禁用和恢复。
- `internal/featurejs/`：负责飞书前端 JS bundle 规则定位、替换和恢复。
- `internal/backup/` 与 `internal/tx/`：负责备份清单、文件备份、事务提交和回滚。
- `.github/workflows/`：GitHub Actions 自动测试、构建和发布脚本。
- `toggle-knowledgeai.ps1`：早期 PowerShell 脚本，当前仍保留在仓库中作为独立辅助脚本。

## 本地构建

本项目使用 Go 1.22。Windows 环境下可以直接运行以下命令：

```powershell
go test ./...
go build -trimpath -ldflags "-s -w" -o dist\feishu-ai-patcher.exe .\cmd\feishu-ai-patcher
```

构建完成后，`dist\feishu-ai-patcher.exe` 即为可运行版本。首次运行时程序会检查管理员权限；如果当前进程未提升，Windows 会弹出 UAC 授权窗口并重新启动程序。

## 使用方式

1. 关闭正在运行的飞书客户端。
2. 从 Release 下载对应架构的压缩包，常见 Intel/AMD 电脑选择 `windows-amd64`。
3. 解压后运行 `feishu-ai-patcher.exe`。
4. 按提示确认或输入飞书安装目录，默认路径通常为 `C:\Program Files\Feishu`。
5. 根据菜单选择“禁用全部可识别功能”“恢复全部已禁用功能”或单项操作。

程序会在可执行文件同目录创建：

- `config.json`：保存上次安装路径、备份目录、严格模式和扫描缓存。
- `backup\`：保存每次补丁操作前的备份文件与事务清单。

## GitHub Actions 发布

仓库已包含 `.github/workflows/release.yml`，用于直接生成 Windows 可运行版本并发布到 GitHub Release。

### 自动发布

推送形如 `v1.0.0` 的标签后，工作流会自动执行：

```powershell
git tag v1.0.0
git push origin v1.0.0
```

工作流会在 Windows runner 上执行 `go test ./...`，然后分别构建：

- `feishu-ai-patcher-windows-amd64.zip`
- `feishu-ai-patcher-windows-arm64.zip`

构建产物会被上传到同名 GitHub Release。

### 手动发布

也可以在 GitHub 页面进入 **Actions -> Build Windows Release -> Run workflow**，填写要发布的标签名，例如 `v1.0.0`。如果该标签尚不存在，Release 创建时会指向当前运行工作流的提交。

## 安全与恢复建议

- 执行补丁前请关闭飞书，避免目标文件被占用。
- 如果菜单状态显示“未识别”，通常表示飞书版本结构发生变化，不建议强行操作。
- 如果补丁后需要恢复，优先使用本工具菜单中的恢复功能。
- 如果自动恢复失败，可以查看 `backup\` 中最近事务目录的清单和备份文件。

## 开发说明

- 代码中的模块 README 会描述该模块职责和文件分工；修改模块代码时请同步更新对应 README。
- Go 代码需要保留清晰的中文注释，尤其是导出类型、接口契约和关键流程。
- 发布流水线只负责生成 Windows 可执行包，不会提交代码或修改仓库分支。
