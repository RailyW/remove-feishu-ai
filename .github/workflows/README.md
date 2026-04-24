# `.github/workflows`

## 模块职责

本目录保存项目的 GitHub Actions 工作流。当前只包含 Windows Release 构建发布流程，目标是在 GitHub 托管 runner 中完成测试、编译、打包和 Release 上传。

## 文件说明

### [`release.yml`](./release.yml)

`release.yml` 是面向发布的自动化脚本，包含以下阶段：

1. 在 `windows-latest` runner 上检出仓库。
2. 根据 `go.mod` 安装 Go 工具链并启用模块缓存。
3. 执行 `go test ./...`，确保 Windows 平台下所有包可以通过测试。
4. 构建 `windows/amd64` 与 `windows/arm64` 两个可执行文件。
5. 将每个架构的 exe 和根目录 `readme.md` 打包为独立 zip。
6. 上传 workflow artifact，方便在 Release 发布失败时保留构建产物。
7. 使用 GitHub CLI 创建或更新同名 Release，并上传 zip 资产。

## 触发方式

- 推送 `v*` 标签会自动发布，例如 `v1.0.0`。
- 手动运行 workflow 时必须填写 `tag` 输入项，例如 `v1.0.0`。

## 设计约束

- 工作流固定运行在 Windows runner，避免 Linux 交叉编译遗漏 Windows 专属代码问题。
- Release 权限只申请 `contents: write`，用于创建 Release 和上传资产。
- 产物只包含用户运行需要的 exe 与 README，不包含源码、中间目录或测试缓存。
