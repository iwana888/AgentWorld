package context

// DecisionIntent 决策意图。
//
// 这是整个架构的灵魂：Compile 必须接收 Intent，而不是只接收 Agent。
// Retriever / Planner 靠 Intent 知道"该找什么"——
//   例：intent = HIRE_AGENT, skill = engineering
//     → Retriever 知道要找：工程师相关经验 / 该 Agent 历史 / 相关合同 / reputation
//   而不是：把这个 Agent 的 Memory 全部倒进 Context。
//
// DecisionIntent 是 World-agnostic 的通用结构（Economy/Hotel/Pascal 共用）。
type DecisionIntent struct {
	Type          string   `json:"type"`           // WORK / HIRE_AGENT / BUY_SKILL / BOOK / IMPLEMENT_FEATURE ...
	TargetAgentID string   `json:"target_agent_id,omitempty"` // 涉及的另一 Agent（如 hire 对象）
	SkillID       string   `json:"skill_id,omitempty"`         // 涉及的技能（如 engineering）
	CandidateIDs  []string `json:"candidate_ids,omitempty"`    // 候选 ID（job id / worker id / room id ...）
	Complexity    int      `json:"complexity,omitempty"`       // 复杂度（0~5，影响 Decision Context 深度）
}

// relatedIDs 把单值 TargetAgentID 适配成 RetrieveRequest.RelatedAgentIDs。
func (d *DecisionIntent) relatedIDs() []string {
	if d == nil || d.TargetAgentID == "" {
		return nil
	}
	return []string{d.TargetAgentID}
}

// skillIDList 把单值 SkillID 适配成 RetrieveRequest.SkillIDs。
func (d *DecisionIntent) skillIDList() []string {
	if d == nil || d.SkillID == "" {
		return nil
	}
	return []string{d.SkillID}
}

// CandidateAction 一个候选行动（来自 Planner）。
//
// 设计：把 Decision Context 从 Perception 里拿出来。
// 现在链路是：Perception → Planner → CandidateActions → DecisionIntent
//             → ContextRuntime → LLM。
// World 负责产生事实，Planner 负责产生候选，Context Runtime 负责决定
// LLM 应该看到哪些事实，LLM 负责选择。
type CandidateAction struct {
	ID      string  `json:"id"`      // 候选 id（如 "buy_skill:engineer" / "hire:42"）
	Action  string  `json:"action"`  // buy_skill / hire_agent / claim / wait ...
	Label   string  `json:"label"`   // 人类可读标签（供 Decision Context 展示）
	Cost    int64   `json:"cost,omitempty"`    // 立即成本
	Reward  int64   `json:"reward,omitempty"`  // 立即收益
	Score   float64 `json:"score,omitempty"`   // 统一评分（越高越优）
	Detail  string  `json:"detail,omitempty"`  // 额外说明（如 worker 声誉/成功率）
}
