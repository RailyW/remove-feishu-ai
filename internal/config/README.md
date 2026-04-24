# `internal/config`

## 模块职责

`internal/config` 模块负责补丁工具本地配置文件的结构定义、默认值生成、向前兼容读取和稳定格式写回。

本模块只处理配置数据本身，不校验飞书安装目录是否有效，也不执行任何补丁动作；安装目录解析由 `internal/install` 负责，具体功能检测与写入由 `internal/featureframe`、`internal/featurejs` 和 `internal/app` 负责。

## 文件说明

### [`config.go`](./config.go)

定义配置模型和读写逻辑：

- `Config`：保存上次安装路径、备份根目录、严格模式默认值、frame.dll 偏移缓存、JS bundle 路径缓存和规则命中文件元信息。
- `OffsetCache`：记录 `frame.dll` 中 old/new pattern 的历史命中偏移、文件大小和修改时间。
- `RuleHitCache`：记录某条规则上次命中文件的大小和修改时间。
- `Default()`：生成可直接使用的默认配置，并初始化所有 map 字段。
- `LoadOrCreate()`：读取 JSON 配置；文件不存在时创建默认配置；旧配置缺字段时执行兼容补齐。
- `Save()`：补齐配置后，以缩进 JSON 写回文件。
- `applyDefaults()`：集中维护向前兼容默认值，保证旧版本配置升级后仍可直接使用。

### [`config_test.go`](./config_test.go)

覆盖配置模块的关键行为：

- 配置文件缺失时会创建默认配置。
- 旧版本 JSON 缺少新字段时会补齐默认值和非 nil map。
- `Save()` 写出的 JSON 可以被 `LoadOrCreate()` 原样读回。
- `last_bundle_relative_paths` 会随配置保存和加载保留，用于按 JS 规则区分 bundle 缓存。

## 关键字段

### `last_bundle_relative_path`

旧版本单值 JS bundle 缓存字段，表示上一次成功命中的 bundle 相对路径。

该字段仍保留用于读取旧配置。应用层会把它作为兜底加速路径，但不会只凭该字段决定最终命中结果；`featurejs` 仍会重新校验 marker 和 exact pattern。

### `last_bundle_relative_paths`

新版本按规则 ID 记录 JS bundle 缓存路径，例如：

- `knowledge_sidebar`：知识问答侧边栏 JS 规则上次命中的 bundle。
- `group_summary`：群聊 AI 消息速览/群聊总结 JS 规则上次命中的 bundle。

该 map 避免不同 JS 规则复用同一个缓存路径导致重复无效验证，尤其适用于不同飞书版本中多个 AI 相关入口分散到不同 bundle 的情况。

## 兼容原则

- 新增字段必须通过 `Default()` 和 `applyDefaults()` 同时补齐。
- map 字段必须保证加载后非 nil，调用方可以直接写入。
- 旧字段只做兼容读取，不应作为新的功能边界。
- 配置缓存只用于加速，不能替代功能模块自己的安全检测。
