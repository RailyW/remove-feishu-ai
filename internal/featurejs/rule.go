// Package featurejs 提供飞书哈希命名 JS bundle 的候选定位、状态检测与受保护写入能力。
//
// 本模块负责三件事：
// 1. 在 `app\webcontent\messenger-vc\common` 目录下，根据 marker 为目标功能定位候选 JS bundle。
// 2. 基于原始/补丁 pattern 命中数，判断 bundle 当前属于 original、patched、mixed 或 unknown。
// 3. 在状态明确且完成事务备份后，通过临时文件替换实现 original 与 patched 内容切换。
package featurejs

// Rule 描述一个前端功能对应的 JS bundle 识别与状态检测规则。
//
// ID 与 DisplayName 用于上层配置、日志与界面展示。
// BundleDir 是目标 bundle 所在目录，相对于飞书安装根目录。
// StrongMarkers / MediumMarkers / WeakMarkers 用于在哈希文件名场景下给候选 JS 打分。
// OriginalPatterns / PatchedPatterns 用于状态检测。
// ExpectedOriginal / ExpectedPatched 用于约束可识别 bundle 中应命中的 pattern 数量。
type Rule struct {
	ID               string
	DisplayName      string
	BundleDir        string
	StrongMarkers    []string
	MediumMarkers    []string
	WeakMarkers      []string
	OriginalPatterns []string
	PatchedPatterns  []string
	ExpectedOriginal int
	ExpectedPatched  int
}

// Feature 封装单个前端功能的 JS 规则。
//
// 当前 Feature 仅持有一条 Rule，便于 Detect、Remove/Restore 以及测试共享相同规则来源。
type Feature struct {
	rule Rule
}

// DefaultKnowledgeSidebarRule 返回“知识库 AI 侧边栏”当前已知的默认规则。
func DefaultKnowledgeSidebarRule() Rule {
	return Rule{
		ID:          "knowledge_sidebar",
		DisplayName: "知识库 AI",
		BundleDir:   `app\webcontent\messenger-vc\common`,
		StrongMarkers: []string{
			`settingKey:"lark_knowledge_ai_client_setting"`,
			`pluginType:a.Vx.EDITOR_EXTENSION`,
			`lark__editor--extension-knowledge-qa`,
		},
		OriginalPatterns: []string{
			`return s.P4.setEnable(u),u},h=e=>`,
			`getShowExtension:()=>t.scene===a.pC.main?dt.P4.enable.main:dt.P4.enable.thread`,
		},
		PatchedPatterns: []string{
			`return s.P4.setEnable({main:!1,thread:!1}),{main:!1,thread:!1}},h=e=>`,
			`getShowExtension:()=>!1`,
		},
		ExpectedOriginal: 2,
		ExpectedPatched:  2,
	}
}

// DefaultGroupSummaryRule 返回“群聊总结”功能的占位规则。
//
// 由于当前没有稳定 marker / pattern，本规则故意使用明显不会命中的占位 marker，
// 让检测流程安全回落到 unknown，而不是制造误判。
func DefaultGroupSummaryRule() Rule {
	return Rule{
		ID:               "group_summary",
		DisplayName:      "群聊总结",
		BundleDir:        `app\webcontent\messenger-vc\common`,
		StrongMarkers:    []string{`__featurejs_placeholder_group_summary_marker__do_not_match__`},
		OriginalPatterns: []string{`__featurejs_placeholder_group_summary_original__`},
		PatchedPatterns:  []string{`__featurejs_placeholder_group_summary_patched__`},
		ExpectedOriginal: 1,
		ExpectedPatched:  1,
	}
}

// NewKnowledgeSidebarFeature 创建使用默认“知识库 AI 侧边栏”规则的 Feature。
func NewKnowledgeSidebarFeature() *Feature {
	return &Feature{rule: DefaultKnowledgeSidebarRule()}
}

// NewGroupSummaryFeature 创建使用默认“群聊总结”占位规则的 Feature。
func NewGroupSummaryFeature() *Feature {
	return &Feature{rule: DefaultGroupSummaryRule()}
}

// ID 返回功能的稳定标识。
func (f *Feature) ID() string {
	return f.rule.ID
}

// DisplayName 返回面向用户展示的功能名称。
func (f *Feature) DisplayName() string {
	return f.rule.DisplayName
}
