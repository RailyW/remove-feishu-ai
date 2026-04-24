# `internal/featureframe`

## 模块职责

`internal/featureframe` 模块负责处理飞书 `app/frame.dll` 中“知识库 AI”功能对应的二进制特征规则。

当前模块包含两类能力：

1. 只读检测：识别目标文件当前是 `original`、`patched`、`mixed` 还是 `unknown`。
2. 定点补丁与恢复：仅在状态满足前置条件时，对命中的固定偏移执行 `WriteAt` 写入，并在写后重新检测确认结果。

本模块只面向调用方传入的安装根目录工作。测试中始终使用临时目录样本文件，不访问真实飞书安装目录。

## 文件说明

### [`rule.go`](./rule.go)

定义 `Rule` 与 `Feature` 的基础结构：

- `Rule` 描述目标相对路径、原始字节序列、补丁字节序列、命中次数和缓存邻域扫描半径。
- `DefaultRule()` 提供当前任务已知的 `frame.dll` 默认规则。
- `New()` 返回使用默认规则构造的 `Feature`。

### [`detect.go`](./detect.go)

实现只读检测逻辑：

- `Detect` 与 `DetectWithCache` 对外暴露状态检测能力。
- `detect` 负责串联缓存直验、缓存邻域扫描、PE 可执行节区扫描和全文件扫描。
- `DetectMeta` 返回 old/new pattern 的命中偏移，供日志、缓存以及补丁流程复用。

### [`patch.go`](./patch.go)

实现 Task 7 引入的写入能力：

- `Remove` 仅允许在 `original` 状态下执行，将所有 `OldPattern` 命中偏移定点写成 `NewPattern`。
- `Restore` 仅允许在 `patched` 状态下执行，将所有 `NewPattern` 命中偏移定点恢复为 `OldPattern`。
- 写入前必须调用 `tx.BackupFile(...)`；备份失败时不会写入目标文件。
- 写入使用 `os.OpenFile(..., os.O_RDWR, 0)` 与 `WriteAt`，不整文件重写。
- 写前会再次使用 `search.VerifyAt` 校验命中偏移仍为预期旧字节，避免检测后文件已变化。
- 写后会再次执行检测，只有状态切换到目标状态才算成功。

### [`frame_test.go`](./frame_test.go)

覆盖检测与补丁恢复测试：

- 原始、已补丁、混合、未知等状态识别。
- 缓存命中、缓存漂移与全文件回退。
- `Remove` / `Restore` 的成功路径。
- 备份失败不写入、状态不允许不备份不写入、上下文取消提前返回等保护逻辑。

## 关键约束

### 先备份再写入

补丁流程固定顺序如下：

1. 先检测当前状态。
2. 确认状态允许当前动作。
3. 调用 `tx.BackupFile` 备份原文件。
4. 重新打开目标文件为读写模式。
5. 对每个偏移执行写前校验。
6. 使用 `WriteAt` 定点写入。
7. 写后重新检测结果状态。

### 只允许安全状态写入

模块不会在以下状态执行写入：

- `mixed`
- `unknown`
- 文件缺失
- 检测报错
- `context.Context` 在开始写入前已取消

### 不触碰真实安装目录

模块本身根据 `feature.Env.InstallPath()` 解析目标路径；是否传入真实安装目录由上层决定。
Task 7 的测试全部使用 `t.TempDir()` 构造临时安装根目录，保证不会访问 `C:\Program Files\Feishu`。
