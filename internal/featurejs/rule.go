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
// PatternVariants 用于描述同一功能在不同飞书版本或不同压缩布局下的多个可选
// 补丁点；检测会按声明顺序尝试这些变体，优先使用更稳定、更小影响面的变体。
type Rule struct {
	ID               string
	DisplayName      string
	BundleDir        string
	StrongMarkers    []string
	MediumMarkers    []string
	WeakMarkers      []string
	OriginalPatterns []string
	PatchedPatterns  []string
	PatternVariants  []PatternVariant
}

// PatternVariant 描述同一前端功能的一组可互相恢复的 exact pattern。
//
// Name 只用于日志、测试和调试定位；OriginalPatterns 与 PatchedPatterns 必须一一
// 对应。对于同一 Rule，靠前的变体优先级更高：如果高优先级变体已经明确命中
// original、patched 或 mixed，检测不会继续尝试低优先级变体，避免在部分修改状态下
// 跳到备用规则继续写入。
type PatternVariant struct {
	Name             string
	OriginalPatterns []string
	PatchedPatterns  []string
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
	}
}

// DefaultGroupSummaryRule 返回“群聊 AI 消息速览/群聊总结”的默认 JS 规则。
//
// 当前已知飞书版本会在新消息提示条上渲染 My AI 总结按钮，bundle 中同时包含
// `my-ai-summarize-button`、`summarizeButtonEnable` 与 `onSummarizeButtonClick`
// 等较稳定语义标识。补丁点选择按钮组件创建前的布尔门控表达式，只把门控改为
// 恒 false，不改动组件实现、埋点逻辑或消息列表其它功能，从而在版本差异较小时
// 保持可恢复、可检测；若未来压缩变量名或结构变化导致 exact pattern 不再命中，
// classify 会回落到 unknown 并拒绝写入，避免误改其它 bundle。
func DefaultGroupSummaryRule() Rule {
	return Rule{
		ID:          "group_summary",
		DisplayName: "群聊总结",
		BundleDir:   `app\webcontent\messenger-vc\common`,
		StrongMarkers: []string{
			`my-ai-summarize-button`,
			`summarizeButtonEnable`,
			`onSummarizeButtonClick`,
		},
		MediumMarkers: []string{
			`click summarize have no quick action`,
			`getNewMessageCount()>=10`,
			`messageTip__countTip`,
		},
		WeakMarkers: []string{
			`renderUpNewMessagesTip`,
			`renderDownNewMessageTip`,
		},
		PatternVariants: []PatternVariant{
			{
				Name: "render_gate",
				OriginalPatterns: []string{
					`u&&h&&p&&c().createElement(GZt,{className:"my-ai-summarize-button",direction:o,onSummarizeButtonClick:h,channelType:p})`,
				},
				PatchedPatterns: []string{
					`!1&&h&&p&&c().createElement(GZt,{className:"my-ai-summarize-button",direction:o,onSummarizeButtonClick:h,channelType:p})`,
				},
			},
			{
				Name: "state_gate",
				OriginalPatterns: []string{
					`summarizeButtonEnable:Boolean(a&&(0,Pu.Wx)(a)&&!(0,Vg.Y)()&&a.getNewMessageCount&&a.getNewMessageCount()>=10)`,
				},
				PatchedPatterns: []string{
					`summarizeButtonEnable:Boolean(!1)`,
				},
			},
		},
	}
}

// NewKnowledgeSidebarFeature 创建使用默认“知识库 AI 侧边栏”规则的 Feature。
func NewKnowledgeSidebarFeature() *Feature {
	return &Feature{rule: DefaultKnowledgeSidebarRule()}
}

// NewGroupSummaryFeature 创建使用默认“群聊 AI 消息速览/群聊总结”规则的 Feature。
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

// patternVariants 返回当前规则实际用于检测和写入的 pattern 变体列表。
//
// 早期规则只填写 OriginalPatterns / PatchedPatterns；为了保持知识问答等既有规则
// 不需要立即迁移，未声明 PatternVariants 时会自动包装成一个 default 变体。
func (f *Feature) patternVariants() []PatternVariant {
	if len(f.rule.PatternVariants) > 0 {
		return f.rule.PatternVariants
	}

	return []PatternVariant{
		{
			Name:             "default",
			OriginalPatterns: f.rule.OriginalPatterns,
			PatchedPatterns:  f.rule.PatchedPatterns,
		},
	}
}
