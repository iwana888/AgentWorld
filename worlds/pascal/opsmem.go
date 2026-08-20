package pascal

// 本文件只定义“经验如何被表示”，不触碰 Retriever / Compiler / LLM / Agent 决策。
//
// 设计约束（单一变量实验）：
//   - A 组 No Memory        : 不写任何经验。
//   - B 组 Raw Memory       : 写自由文本经验（现状）。
//   - C 组 Operational Memory: 写结构化经验 OperationalMemory。
//
// 关键：三组共用同一个 Retriever（按文本相似度检索）。C 组之所以可能更有效，
// 仅仅是因为“写入的内容形状”不同——它把一条经验渲染成
//   PROBLEM / ACTION / FAILURE / CAUSE / RESOLUTION
// 的叙述，使 Agent 检索到后能直接推导出“下一次该做什么不同的动作”。
// Retriever 代码本身一字未改。

// OperationalMemory 是 C 组的结构化经验。
// 它不是告诉 Agent：“我以前遇到过这个。”
// 而是告诉它：“我遇到过什么 → 我做了什么 → 为什么失败 → 原因是什么 → 最后怎么解决。”
type OperationalMemory struct {
	Problem     string `json:"problem"`
	Action      string `json:"action"`
	Failure     string `json:"failure"`
	Cause       string `json:"cause"`
	Resolution  string `json:"resolution"`
	IssueID     string `json:"issue_id,omitempty"`
}

// Format 把结构化经验渲染成一段叙述文本，供现有 Retriever 检索。
// 这是 C 组与 B 组的唯一差异来源（内容形态）。
func (o OperationalMemory) Format() string {
	return "OPERATIONAL EXPERIENCE\n" +
		"PROBLEM: " + o.Problem + "\n" +
		"ACTION: " + o.Action + "\n" +
		"FAILURE: " + o.Failure + "\n" +
		"CAUSE: " + o.Cause + "\n" +
		"RESOLUTION: " + o.Resolution
}

// MemMode 是经验表示模式的枚举。
type MemMode string

const (
	// MemRaw 写自由文本经验（B 组）。
	MemRaw MemMode = "raw"
	// MemOperational 写结构化经验（C 组）。
	MemOperational MemMode = "operational"
	// MemNone 不写经验（A 组）。由 runner 在每 Issue 前清空 Memory 实现。
	MemNone MemMode = "none"
)

// BuildFailureExperience 从一次失败事件构造结构化经验。
// action 是 Agent 当时采取的动作（如 write_file(src/Broken.pas)）。
// failure 是编译器/测试器给出的错误摘要；cause 是推断的根因；
// resolution 是“下次应如何处理”的可执行建议。
func BuildFailureExperience(it Issue, action, failure, cause, resolution string) OperationalMemory {
	return OperationalMemory{
		Problem:     "Issue " + it.ID + ": " + it.Title,
		Action:      action,
		Failure:     failure,
		Cause:       cause,
		Resolution:  resolution,
		IssueID:     it.ID,
	}
}

// BuildSuccessExperience 从一次成功修复构造结构化经验。
// 它固化“哪类问题 → 用哪种修复动作 → 成功了”，供后续相似 Issue 直接复用。
func BuildSuccessExperience(it Issue, action, resolution string) OperationalMemory {
	return OperationalMemory{
		Problem:     "Issue " + it.ID + ": " + it.Title,
		Action:      action,
		Failure:     "(none — resolved)",
		Cause:       "root cause addressed by the applied fix",
		Resolution:  resolution,
		IssueID:     it.ID,
	}
}
