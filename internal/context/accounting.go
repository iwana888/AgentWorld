package context

// accounting.go —— M8.10 Token Accounting（收口）。
//
// 设计原则（你定的，比前几个阶段更克制）：
//   - 只做 Accounting，不做 Economy 化。Token 只是观测数据，不流入决策、不变成资源。
//   - 零副作用：Accounting 只"读" CompiledContext / LLM 响应并汇总，绝不修改
//     Candidate / Planner / Decision / Economy / ContextBlock / Budget / Retriever / Compactor。
//   - ContextTokens（Runtime 编译成本）与 InputTokens（真正发给 Provider 的成本）
//     是两个独立维度，刻意不合并——未来两者可能不同。
//
// 字段（与 TokenUsage 一一对应，但按 Agent/Think 维度聚合快照）：
//   [Runtime]  Stable / State / Retrieved / Event / Decision / Compacted / ContextTokens
//   [Provider] Input / Output / Total
type TokenAccount struct {
	AgentID string `json:"agent_id"`
	ThinkID string `json:"think_id"` // 一次决策的标识（由调用方提供，如 "alice#42"）

	// [A] Runtime 编译成本（来自 CompiledContext.TokenUsage，纯拷贝，不修改原对象）
	StableTokens    int `json:"stable_tokens"`
	StateTokens     int `json:"state_tokens"`
	RetrievedTokens int `json:"retrieved_tokens"`
	EventTokens     int `json:"event_tokens"`
	DecisionTokens  int `json:"decision_tokens"`
	CompactedTokens int `json:"compacted_tokens"`
	ContextTokens   int `json:"context_tokens"`

	// [B] Provider 实际成本（由 RecordLLM 回填，未调用前为 0）
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`

	OverBudget bool `json:"over_budget"`
}

// AccountCompile 从一次 Compile 结果生成零副作用的 Token 快照。
// 仅读取 cc.TokenUsage，不修改 cc 的任何字段。
func AccountCompile(cc *CompiledContext, agentID, thinkID string) *TokenAccount {
	if cc == nil {
		return &TokenAccount{AgentID: agentID, ThinkID: thinkID}
	}
	u := cc.TokenUsage
	return &TokenAccount{
		AgentID:         agentID,
		ThinkID:         thinkID,
		StableTokens:    u.StableTokens,
		StateTokens:     u.StateTokens,
		RetrievedTokens: u.RetrievedTokens,
		EventTokens:     u.EventTokens,
		DecisionTokens:  u.DecisionTokens,
		CompactedTokens: u.CompactedTokens,
		ContextTokens:   u.ContextTokens,
		OverBudget:      u.OverBudget,
	}
}

// RecordLLM 回填 Provider 实际成本。零副作用：只写本 TokenAccount，不触及任何 Runtime 对象。
// input / output 由调用方在拿到 LLM 响应后传入（M8.10 不调 LLM、不解析响应体）。
func (a *TokenAccount) RecordLLM(input, output int) {
	if a == nil {
		return
	}
	a.InputTokens = input
	a.OutputTokens = output
	a.TotalTokens = input + output
}

// ThinkCost 一个 Agent 一次 Think 的总资源占用（Runtime Context + Provider Output）。
// 这对应你描述的 "Think Cost: 1800 tokens" = ContextTokens + OutputTokens。
// 仅作派生读数，不写入任何状态。
func (a *TokenAccount) ThinkCost() int {
	if a == nil {
		return 0
	}
	return a.ContextTokens + a.OutputTokens
}

// TokenLedger 一次实验（如 Economy World 跑 1000 Think）的聚合账本。
// 提供基础统计（平均/P50/P90/P99），供 M8 完后的架构复盘用真实数据回答
// "Context Runtime 有没有让 Agent 少看东西、但看得更对"。
// Ledger 同样零副作用：只累加 Append 进来的 TokenAccount。
type TokenLedger struct {
	entries []TokenAccount
}

// NewTokenLedger 构造空账本。
func NewTokenLedger() *TokenLedger { return &TokenLedger{} }

// Append 追加一条 Token 快照（拷贝值，不持有原对象引用，避免外部修改串账）。
func (l *TokenLedger) Append(a *TokenAccount) {
	if a == nil {
		return
	}
	l.entries = append(l.entries, *a)
}

// Len 记录条数。
func (l *TokenLedger) Len() int { return len(l.entries) }

// Stats 计算单维度（由 picker 选定字段）的分布统计。
func (l *TokenLedger) Stats(picker func(TokenAccount) int) LedgerStats {
	vals := make([]int, len(l.entries))
	for i, e := range l.entries {
		vals[i] = picker(e)
	}
	return computeStats(vals)
}

// LedgerStats 单维度分布。
type LedgerStats struct {
	Count int     `json:"count"`
	Avg   float64 `json:"avg"`
	P50   int     `json:"p50"`
	P90   int     `json:"p90"`
	P99   int     `json:"p99"`
}

// computeStats 计算平均值与 P50/P90/P99 分位（升序取位，零依赖）。
func computeStats(vals []int) LedgerStats {
	n := len(vals)
	if n == 0 {
		return LedgerStats{}
	}
	// 简单选择排序（账本条目不会特别大，足够；如需可换 sort）。
	for i := 0; i < n; i++ {
		min := i
		for j := i + 1; j < n; j++ {
			if vals[j] < vals[min] {
				min = j
			}
		}
		vals[i], vals[min] = vals[min], vals[i]
	}
	sum := 0
	for _, v := range vals {
		sum += v
	}
	pct := func(p float64) int {
		idx := int(p * float64(n-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		return vals[idx]
	}
	return LedgerStats{
		Count: n,
		Avg:   float64(sum) / float64(n),
		P50:   pct(0.50),
		P90:   pct(0.90),
		P99:   pct(0.99),
	}
}
