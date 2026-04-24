// Package featureframe 提供 frame.dll 中知识库 AI 功能字节规则的检测能力。
//
// 本模块在 Task 6 只负责判断 frame.dll 当前处于原始、已补丁、混合或未知状态，
// 不执行任何写入、备份、替换或恢复操作。后续 Task 7 会在本模块规则基础上实现 patch。
package featureframe

// Rule 描述 frame.dll 内一条固定长度二进制替换规则。
//
// FileRelativePath 是相对于飞书安装目录的目标文件路径。
// OldPattern 是未补丁版本中应出现的原始字节序列。
// NewPattern 是完成补丁后应出现的目标字节序列。
// ExpectedCount 是在一个可识别文件中期望命中的 pattern 次数。
// SearchRadius 是缓存偏移失效时，在缓存点附近执行窗口扫描的半径，单位为字节。
type Rule struct {
	FileRelativePath string
	OldPattern       []byte
	NewPattern       []byte
	ExpectedCount    int
	SearchRadius     int64
}

// Feature 持有 frame.dll 检测所需的规则配置。
//
// 当前结构只封装单条 Rule，便于 Detect、后续 Remove/Restore 以及测试共享同一规则来源。
type Feature struct {
	rule Rule
}

// DefaultRule 返回当前已知 frame.dll 知识库 AI 开关对应的默认二进制规则。
//
// OldPattern 与 NewPattern 仅最后两个功能标识字节不同：原始字节以 "ai" 结尾，
// 补丁字节以 "xx" 结尾。ExpectedCount 为 2，表示同一 frame.dll 中应同时命中两处。
func DefaultRule() Rule {
	return Rule{
		FileRelativePath: `app\frame.dll`,
		OldPattern: []byte{
			0x48, 0xb9, 0x6b, 0x6e, 0x6f, 0x77, 0x6c, 0x65, 0x64, 0x67,
			0x48, 0x89, 0x08, 0xc7, 0x40, 0x07, 0x67, 0x65, 0x61, 0x69,
		},
		NewPattern: []byte{
			0x48, 0xb9, 0x6b, 0x6e, 0x6f, 0x77, 0x6c, 0x65, 0x64, 0x67,
			0x48, 0x89, 0x08, 0xc7, 0x40, 0x07, 0x67, 0x65, 0x78, 0x78,
		},
		ExpectedCount: 2,
		SearchRadius:  0x200000,
	}
}

// New 创建使用默认 frame.dll 规则的 Feature。
func New() *Feature {
	return &Feature{rule: DefaultRule()}
}
