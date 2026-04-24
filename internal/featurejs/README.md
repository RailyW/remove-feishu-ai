# `internal/featurejs`

## 模块职责

`internal/featurejs` 模块负责处理飞书前端哈希命名 JS bundle 的“文件级定位 + 状态检测 + 受保护写入”。

本模块提供以下能力：

1. 在 `app\webcontent\messenger-vc\common` 目录下，根据 marker 从多个哈希文件名 JS 中定位目标 bundle。
2. 统计原始 pattern 与补丁 pattern 的命中次数，判断目标功能当前处于 `original`、`patched`、`mixed` 或 `unknown`。
3. 仅在状态明确时执行 `Remove` / `Restore`，并在写入前通过事务备份目标 bundle。
4. 通过写入同目录临时文件再以原子替换方式覆盖目标文件，让 JS 内容在 original 与 patched pattern 之间成对切换。

## 文件说明

### [`rule.go`](./rule.go)

定义本模块的规则与功能对象：

- `Rule`：描述功能 ID、显示名称、bundle 目录、marker 集合、原始/补丁 pattern 与可选 pattern 变体。
- `PatternVariant`：描述同一功能在不同飞书版本或不同压缩布局下的一组可恢复 exact pattern。
- `Feature`：持有单条 `Rule`，并实现 `feature.Feature` 所需的基础接口。
- `DefaultKnowledgeSidebarRule()`：返回“知识库 AI”当前已知的稳定规则。
- `DefaultGroupSummaryRule()`：返回“群聊 AI 消息速览/群聊总结”规则，通过 `my-ai-summarize-button`、`summarizeButtonEnable` 和 `onSummarizeButtonClick` 等 marker 定位新消息提示里的 My AI 总结入口。

### [`patch.go`](./patch.go)

实现 JS bundle 的受保护写入：

- `ErrJSActionNotAllowed`：当前状态不是动作允许状态时返回，例如 `mixed`、`unknown`、未找到 bundle，或对 `patched` 执行 `Remove`。
- `ErrJSVerifyFailed`：写后重检未达到目标状态，或规则中的原始/补丁 pattern 无法成对匹配。
- `Remove()`：仅在 bundle 被检测为 `original` 时，把所有 original pattern 替换为 patched pattern。
- `Restore()`：仅在 bundle 被检测为 `patched` 时，把所有 patched pattern 替换回 original pattern。
- `replaceFileWithTemp()`：先写入 `bundle.js.tmp`，再调用平台相关的原子替换实现覆盖目标 bundle；失败时保留原文件并清理临时文件。

### [`replace_windows.go`](./replace_windows.go)

定义 Windows 平台专用的原子替换实现：

- `replaceFileAtomically()`：通过 `MoveFileExW(REPLACE_EXISTING)` 覆盖目标文件，避免“先删原文件再改名”带来的破坏性失败窗口。

### [`replace_other.go`](./replace_other.go)

定义非 Windows 平台的编译兼容回退实现：

- `replaceFileAtomically()`：使用 `os.Rename` 维持包在非 Windows 平台上的基本可编译性。

### [`locate.go`](./locate.go)

实现哈希文件名 JS bundle 的候选定位：

- `BundleCandidate`：描述定位到的候选文件路径、相对路径、marker 分数和文件大小。
- `locateBundle()`：优先验证缓存相对路径；缓存失效时扫描 `common` 目录下所有 `.js` 文件。
- `cachedCommonRelativePath()`：收紧缓存路径边界，并兼容“相对 commonDir”和“相对安装根目录且带 BundleDir 前缀”两种语义。
- `scoreContent()`：按照 strong/medium/weak marker 权重计算候选分数。
- `collectJSFiles()`：按文件大小降序返回候选文件，优先处理更可能是真正 bundle 的大文件。
- `chooseBestCandidate()`：从多个 strong marker 命中的候选中选择唯一最高分文件；若最高分并列，则视为当前规则无法唯一定位并返回零值候选。

### [`detect.go`](./detect.go)

实现只读状态检测：

- `Detect()` 与 `DetectWithCache()`：对外提供按安装根目录执行的检测入口。
- `detect()`：串联定位与 pattern 计数，构造 `DetectMeta`。
- `detectPatternVariant()`：按优先级检测多个 `PatternVariant`，用于兼容不同飞书版本中压缩变量名或渲染结构的变化。
- `classify()`：根据每个 exact pattern 的单独命中结果给出最终状态。
- `patternHits()`：按字符串 pattern 分别统计命中次数，避免“单个 pattern 重复两次但另一个缺失”被误判成明确状态。

### [`js_test.go`](./js_test.go)

覆盖本模块的核心行为：

- strong marker 完整命中的哈希 JS 文件定位。
- 缓存路径命中、缓存不存在回退扫描、安装根相对缓存路径命中，以及非法缓存路径回退扫描。
- 知识库 AI 的 `original` / `patched` / `mixed` 判定，以及单个 original pattern 重复多次时必须返回 `unknown`。
- 知识库 AI `Remove` / `Restore` 的成功路径、备份失败不写入，以及 `mixed` / `unknown` 拒绝写入。
- 单个 original pattern 重复多次且另一个缺失时，`Remove` 返回 `ErrJSActionNotAllowed` 且不会调用备份。
- 群聊总结规则能识别原始、已补丁和混合状态，并能在主渲染门控缺失时使用备用状态门控变体。
- 群聊总结 `Remove` / `Restore` 使用检测时命中的同一变体执行成对替换，保证备用规则也可恢复。
- 未找到候选 bundle 时返回 `unknown`，而不是误报或写入。

## 关键约束

### 不依赖固定文件名

本模块不假设目标 bundle 文件名稳定。无论文件名是 `a1.js`、`b2.js` 还是更长的哈希名，都会通过 marker 命中来定位文件。

### 候选必须命中全部 strong marker

只有当一个 `.js` 文件同时包含当前规则定义的全部 strong marker 时，才会被视为目标 bundle。

这意味着：

- 文件名变化不会影响定位结果。
- 仅命中部分 marker 的相似文件不会被误判。
- 当规则失效或版本变化时，会自然回落到 `unknown`。
- 当多个文件都完整命中 strong marker 且最高分并列时，也会安全回落到 `unknown`，而不是按扫描顺序盲选第一个文件。

### 缓存只做加速，不做强行判定

如果上层传入缓存的相对路径，本模块会先规范化并验证该文件是否仍然存在且仍满足 strong marker。

缓存路径边界：

- 拒绝绝对路径。
- `Clean` 后包含 `..` 的路径会被忽略。
- 只允许 `.js` 文件。
- 非法缓存、缓存不存在、缓存文件不满足规则时，不返回错误，而是自动回退目录扫描。
- 缓存路径文件读取或校验报错时，同样继续扫描其他 `.js`，不直接把缓存异常当成最终结果。
- 即使缓存路径本身仍满足 strong marker，也不会直接强行判定为最终目标；它只会作为一个候选参与最终比较。如果目录中同时出现同分强匹配竞争者，仍然会回退到 `unknown`。

缓存路径语义：

- `a1.js` 表示相对 `common` 目录的 bundle。
- `app\webcontent\messenger-vc\common\a1.js` 表示相对安装根目录且带 `BundleDir` 前缀的 bundle，内部会规整为 `a1.js` 再验证。

- 满足时，直接使用缓存路径并返回 `LocateMode = "cache_path"`。
- 不满足时，自动回退目录扫描并返回 `LocateMode = "scan"`。

### 写入门槛

`Remove` 与 `Restore` 不会仅凭 marker 命中写入。写入前必须经过 `detect()` 分类，且每个 exact pattern 的单独命中数必须让状态明确为动作允许状态：

- `original`：每个 original pattern 恰好命中 1 次，且所有 patched pattern 命中 0 次。
- `patched`：每个 patched pattern 恰好命中 1 次，且所有 original pattern 命中 0 次。
- `mixed`：original 总命中数大于 0 且 patched 总命中数大于 0。
- `Remove` 只允许 `original -> patched`。
- `Restore` 只允许 `patched -> original`。
- `mixed`、`unknown`、未找到 bundle、部分 original/patched pattern 命中都返回 `ErrJSActionNotAllowed`，且不会调用 `tx.BackupFile`。
- 写入前调用 `tx.BackupFile(relativePathFromInstallRoot, targetPath)`，其中 `relativePathFromInstallRoot` 形如 `app\webcontent\messenger-vc\common\a1.js`。
- 写后重新检测，状态没有切换到目标状态时返回 `ErrJSVerifyFailed`。

### 群聊总结的多版本兼容策略

“群聊 AI 消息速览/群聊总结”目前定位到飞书新消息提示中的 My AI 总结按钮。规则不依赖固定哈希文件名，而是要求候选 bundle 同时命中：

- `my-ai-summarize-button`
- `summarizeButtonEnable`
- `onSummarizeButtonClick`

写入时优先使用按钮渲染门控变体，把创建 `my-ai-summarize-button` 组件前的条件改为恒 false；如果该片段因版本变化不存在，会回退到 `summarizeButtonEnable` 状态门控变体，把初始状态改为 `Boolean(!1)`。

多变体检测遵循保守优先级：只要高优先级变体出现任何原始或补丁片段，即使数量不足导致状态为 `unknown`，也不会继续尝试低优先级变体，以免在半修改文件上错误继续写入。只有当前变体完全没有命中时，才会尝试备用变体。

### Windows 替换安全性

本模块不会采用“先删除原文件，再把 `.tmp` 改名过去”的方式替换 bundle。

当前实现的顺序是：

1. 先把新内容写入同目录 `.tmp`。
2. 在 Windows 上使用 `MoveFileExW(REPLACE_EXISTING)` 直接覆盖目标文件。
3. 若替换失败，保留原目标文件，并清理 `.tmp`。

这样即使遭遇文件占用、杀软或索引器句柄竞争，也只会表现为“本次写入失败”，不会把飞书安装目录留在“目标 JS 文件直接消失”的破坏状态。

### 测试不触碰真实飞书安装目录

所有测试都在 `t.TempDir()` 下构造临时安装根和 `common` 目录，只写入最小化的模拟 JS 内容，不访问真实飞书安装目录。
