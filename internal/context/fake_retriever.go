package context

import (
	"context"
	"fmt"
	"strings"
)

// FakeRetriever M8.6：不返回"假数据"，而是验证 Intent → Query → Retriever →
// Relevant Context 的机制。
//
// 它的价值在于证明：同一个 Agent，面对不同 Intent，Retriever 返回明显不同的
// ContextBlock 集合。这正是 Retrieval-on-demand 的核心——
//   WORK         → 与"工作/ farming 经验/历史成功"相关
//   HIRE_AGENT   → 与"雇佣/目标 Agent 历史/合同"相关
// 而不是把 Agent 的全部记忆倒进 Context。
//
// 真实 MemoryRetriever（M8.7）上线后，FakeRetriever 仍可作为测试桩与降级实现保留。
type FakeRetriever struct {
	est Estimator
}

// NewFakeRetriever 构造假检索器。
func NewFakeRetriever(est Estimator) *FakeRetriever {
	if est == nil {
		est = roughEstimate
	}
	return &FakeRetriever{est: est}
}

// Retrieve 依据 Intent 返回语义相关的"记忆类"块（验证机制，非随机）。
func (r *FakeRetriever) Retrieve(ctx context.Context, req *RetrieveRequest) ([]ContextBlock, error) {
	if req == nil {
		return nil, fmt.Errorf("context: nil retrieve request")
	}
	var blocks []ContextBlock

	// 通用：自身近期经验（所有 Intent 都带一点，但 WORK 不强调）。
	mk := func(id, src, content string, prio int) ContextBlock {
		return ContextBlock{
			ID: id, Type: TypeRetrieved, Source: src, Content: content,
			Priority: prio, Stable: false, Tokens: r.est(content),
		}
	}

	switch req.Intent.Type {
	case "WORK":
		// 工作相关经验：历史工作、技能经验、成功记录。
		for i, c := range []string{
			"past_work_farming: 上次 farming 工作完成度 90%，收入 +45",
			"work_success_history: 近 5 次工作 4 次成功",
			"farming_skill_experience: 擅长 farming，熟练度 Lv3",
		} {
			blocks = append(blocks, mk(fmt.Sprintf("retrieved:work:%d", i), "retrieved.work", c, 65))
		}
	case "HIRE_AGENT":
		// 雇佣相关：过去雇佣、目标 Agent 历史、合同、worker 经验。
		rel := ""
		if len(req.RelatedAgentIDs) > 0 {
			rel = " (" + strings.Join(req.RelatedAgentIDs, ",") + ")"
		}
		skill := ""
		if len(req.SkillIDs) > 0 {
			skill = " skill=" + strings.Join(req.SkillIDs, ",")
		}
		for i, c := range []string{
			"past_hiring: 曾雇佣过 3 名 worker，2 次成功",
			"Bob_history" + rel + ": 目标 Agent 历史声誉 5，成功率 90%",
			"engineering_worker_experience" + skill + ": 工程类雇佣平均回报 +120",
			"contract_history: 上次合同托管 50 coins 顺利结算",
		} {
			blocks = append(blocks, mk(fmt.Sprintf("retrieved:hire:%d", i), "retrieved.hire", c, 65))
		}
	case "BUY_SKILL":
		for i, c := range []string{
			"skill_exp: 已掌握基础技能，投资回收期约 5 单",
			"purchase_history: 上次买技能后收入提升 30%",
		} {
			blocks = append(blocks, mk(fmt.Sprintf("retrieved:buy:%d", i), "retrieved.purchase", c, 65))
		}
	default:
		// 未知 Intent：只给一条兜底自身记忆。
		blocks = append(blocks, mk("retrieved:default", "retrieved.self", "近期自身活动记录", 60))
	}

	// 受 Budget / Limit 约束（与真检索器行为一致，便于统一下游）。
	used := 0
	var out []ContextBlock
	for _, b := range blocks {
		if req.Limit > 0 && len(out) >= req.Limit {
			break
		}
		if req.BudgetTokens > 0 && used+b.Tokens > req.BudgetTokens {
			break
		}
		out = append(out, b)
		used += b.Tokens
	}
	return out, nil
}
