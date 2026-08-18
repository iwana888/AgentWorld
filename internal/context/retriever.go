package context

import (
	"context"
	"fmt"
	"time"
)

// Retriever M8.6/M8.7：从 Memory（或其它源）按 Intent 按需检索历史/关系/知识。
//
// 核心原则（你定的）：
//   - Retriever 不返回 Memory 本身作为 Runtime 最终产物，而是返回 []ContextBlock。
//     这样 Runtime 职责清晰：Memory → Retriever → ContextBlock → Compiler → CompiledContext。
//   - Intent 驱动"该找什么"，而不是把 Agent 的 Memory 全部倒进 Context。
//   - M8.7 第一版只做结构化检索（MemoryType / AgentID / RelatedAgentID / Importance / 时间），
//     不碰 keyword / embedding / hybrid / reranking——否则会膨胀成 RAG 项目。
//   - Retrieval 是"少加载"，应永远优先于 Compaction（"已加载太多怎么办"）。
//
// 接口刻意通用：以后加 Memory / Relation / Event / Knowledge / Economy History /
// Hotel History / Code History，只需新增 Retriever 实现或扩展 RetrieveRequest，不改接口。
type Retriever interface {
	Retrieve(ctx context.Context, req *RetrieveRequest) ([]ContextBlock, error)
}

// RetrieveRequest 检索请求（World-agnostic）。
// 字段可随源类型扩展，不会破坏 Retriever 接口。
type RetrieveRequest struct {
	AgentID         string         // 检索主体 Agent
	Intent          DecisionIntent  // 当前决策意图（驱动检索维度）
	RelatedAgentIDs []string       // 涉及的其他 Agent（如 hire 对象）—— 用于 about_agent 记忆
	SkillIDs        []string       // 涉及的技能 —— 用于按技能过滤记忆
	BudgetTokens    int            // 该次检索的 Token 预算（软约束，超出即截断）
	Limit           int            // 最多返回多少条（硬约束）
}

// intentMemoryTypes 把 Intent.Type 映射到应优先检索的 MemoryType 集合。
// 这是"Intent 驱动检索"的核心映射。World 不感知 MemoryType，Retriever 负责翻译。
// 以后新增 Intent 类型只需在此登记，不改动 Memory 层。
var intentMemoryTypes = map[string][]string{
	"WORK":         {"work", "self", "skill_exp"},
	"HIRE_AGENT":   {"hire", "about_agent", "contract", "skill_exp"},
	"BUY_SKILL":    {"skill_exp", "self", "purchase"},
	"BOOK":         {"booking", "self", "service"},
	"IMPLEMENT_FEATURE": {"code", "self", "bug", "review"},
}

// MemoryRetriever M8.7：基于结构化 Memory 的检索实现。
// 依赖一个最小存储接口（而非直接 import gorm），便于测试与替换数据源。
//
// 检索策略（结构化，非向量）：
//   1. 由 Intent 解析候选 MemoryType（可扩展：RelatedAgentID→about_agent，SkillIDs→skill_exp）
//   2. 按 (AgentID [+ RelatedAgentID] + Type) 过滤
//   3. 排序：Importance 降序，CreatedAt 降序（越重要+越近优先）
//   4. 截断：先 Limit，再 BudgetTokens（粗糙估算）
//   不把全部 Memory 塞进去——只取与当前 Intent 相关的。
type MemoryRetriever struct {
	store MemoryStore
	est   Estimator
}

// MemoryStore M8.7 最小存储接口：与 db 层解耦（db.Memory 适配即可）。
type MemoryStore interface {
	// QueryMemories 返回某 Agent 在指定 Type 下、按 importance desc, created_at desc 的记忆。
	// aboutAgentID 非空时额外包含"关于该 Agent"的 about_agent 记忆。
	QueryMemories(ctx context.Context, agentID string, types []string, aboutAgentID string, limit int) ([]MemoryRow, error)
}

// MemoryRow 存储层返回的一行记忆（与 models.Memory 对应，但隔离 gorm 依赖）。
type MemoryRow struct {
	ID         int64
	AgentID    string
	Type       string
	Content    string
	Importance int
	CreatedAt  time.Time
}

// NewMemoryRetriever 构造结构化记忆检索器。
func NewMemoryRetriever(store MemoryStore, est Estimator) *MemoryRetriever {
	if est == nil {
		est = roughEstimate
	}
	return &MemoryRetriever{store: store, est: est}
}

// Retrieve M8.7 结构化检索：Intent → MemoryType → 过滤 → 排序 → 截断 → []ContextBlock。
func (r *MemoryRetriever) Retrieve(ctx context.Context, req *RetrieveRequest) ([]ContextBlock, error) {
	if req == nil {
		return nil, fmt.Errorf("context: nil retrieve request")
	}
	// 1) 由 Intent 推导候选 MemoryType。
	types := append([]string{}, intentMemoryTypes[req.Intent.Type]...)
	if len(types) == 0 {
		types = []string{"self"} // 兜底：未知 Intent 只取自身记忆
	}
	// SkillIDs 命中 skill_exp 维度。
	if len(req.SkillIDs) > 0 && !contains(types, "skill_exp") {
		types = append(types, "skill_exp")
	}
	// 2) 取 RelatedAgentID（用于 about_agent 过滤）。
	var aboutID string
	if len(req.RelatedAgentIDs) > 0 {
		aboutID = req.RelatedAgentIDs[0]
	}
	// 3) 查询（limit 先放大，后续按预算截断）。
	pullLimit := req.Limit
	if pullLimit <= 0 {
		pullLimit = 20
	}
	if pullLimit > 100 {
		pullLimit = 100
	}
	rows, err := r.store.QueryMemories(ctx, req.AgentID, types, aboutID, pullLimit*3)
	if err != nil {
		return nil, err
	}
	// 4) 组装 ContextBlock + 按 Budget 截断（Important 已经由存储层排序保证在前）。
	var blocks []ContextBlock
	used := 0
	for _, m := range rows {
		if req.Limit > 0 && len(blocks) >= req.Limit {
			break
		}
		tok := r.est(m.Content)
		if req.BudgetTokens > 0 && used+tok > req.BudgetTokens {
			break
		}
		blocks = append(blocks, ContextBlock{
			ID:      fmt.Sprintf("retrieved:%d", m.ID),
			Type:    TypeRetrieved,
			Source:  "retrieved." + m.Type,
			Content: m.Content,
			Priority: 60 + m.Importance, // 越重要越优先（仍低于 State/Stable）
			Stable:  false,
			Tokens:  tok,
		})
		used += tok
	}
	return blocks, nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
