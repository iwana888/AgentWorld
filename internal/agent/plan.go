// plan.go — M8 多步计划：从"单动作"升级到"目标→计划→逐步执行"。
//
// 计划由规则模板从 Agent 的 Goal/Need 推导（零 LLM），所有 Agent 都能用。
// 框架只负责计划的存取与推进；具体计划模板由世界（Module）决定。
package agent

import (
	"strings"

	"agentworld/internal/db"
	"agentworld/internal/models"
	"agentworld/sdk"
)

// planTemplate 世界 → 计划模板函数。返回该 Agent 的步骤序列（动作名）。
type planTemplate func(a models.Agent) []string

// 各世界的计划模板（按 Goal / Need 生成步骤）。
var planTemplates = map[string]planTemplate{
	"social": socialPlan,
	"hotel":  hotelPlan,
}

// ensurePlan 确保 Agent 有一个活跃计划；没有则按世界模板生成（若可生成）。
// 返回当前活跃计划。
func (m *SocialModule) ensurePlan(a models.Agent) *models.AgentPlan {
	return ensurePlanBy(m.rt, a, planTemplates["social"])
}

func (m *HotelModule) ensurePlan(a models.Agent) *models.AgentPlan {
	return ensurePlanBy(m.rt, a, planTemplates["hotel"])
}

// ensurePlanBy 按指定模板确保有活跃计划。
func ensurePlanBy(rt sdk.Runtime, a models.Agent, tmpl planTemplate) *models.AgentPlan {
	plan, err := db.GetActivePlan(rt.DB(), a.ID)
	if err != nil {
		return nil
	}
	if plan != nil {
		return plan
	}
	steps := tmpl(a)
	if len(steps) == 0 {
		return nil // 该 Agent 无需计划（如生活类，随机行动）
	}
	np := &models.AgentPlan{
		AgentID: a.ID, Goal: a.Goal,
		Steps: db.EncodeSteps(steps), StepIndex: 0, Status: "active",
	}
	_ = db.SavePlan(rt.DB(), np)
	return np
}

// currentStep 返回计划当前步骤动作；无计划/已完成返回空。
func currentStep(rt sdk.Runtime, plan *models.AgentPlan) string {
	if plan == nil {
		return ""
	}
	steps := db.DecodeSteps(plan.Steps)
	if plan.StepIndex >= len(steps) {
		return ""
	}
	return steps[plan.StepIndex]
}

// advancePlan 推进计划到下一步；若已到最后一步则标记完成并返回空。
func advancePlan(rt sdk.Runtime, plan *models.AgentPlan) {
	if plan == nil {
		return
	}
	steps := db.DecodeSteps(plan.Steps)
	if plan.StepIndex+1 >= len(steps) {
		_ = db.MarkPlanDone(rt.DB(), plan.ID)
		return
	}
	plan.StepIndex++
	_ = db.SavePlan(rt.DB(), plan)
}

// socialPlan 社交计划模板：按 Goal 倾向生成多步动作序列。
func socialPlan(a models.Agent) []string {
	g := a.Goal
	switch {
	case containsAny(g, "意见", "技术", "输出", "影响"):
		return []string{"post", "comment", "follow", "post"}
	case containsAny(g, "结识", "合作", "同好", "关注"):
		return []string{"follow", "comment", "like", "follow"}
	case containsAny(g, "潜水", "观察", "看准", "少发言"):
		return []string{"like", "nothing", "like", "nothing"}
	default:
		return nil // 无明确目标 → 随机行动
	}
}

// hotelPlan 酒店计划模板：按角色生成多步动作序列。
func hotelPlan(a models.Agent) []string {
	in := a.Interests
	switch {
	case strings.Contains(in, "前台"):
		return []string{"checkin", "checkout", "checkin", "review"}
	case strings.Contains(in, "客房"):
		return []string{"clean", "clean", "review"}
	case strings.Contains(in, "工程"):
		return []string{"maintain", "clean", "maintain"}
	case strings.Contains(in, "营收"):
		return []string{"review", "review"}
	default:
		return nil
	}
}
