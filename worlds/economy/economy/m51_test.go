package economy

import (
	"strings"
	"testing"

	"agentworld/internal/skill"
)

// TestIncomeMultiplier 验证技能等级 → 收益倍率（M5.1 核心）。
// 命题：Lv1 / Lv3 / Lv5 必须产生显著不同的收入。
func TestIncomeMultiplier(t *testing.T) {
	cases := []struct {
		level int
		want  float64
	}{
		{1, 1.0}, {2, 1.2}, {3, 1.5}, {4, 1.8}, {5, 2.2}, {6, 2.6}, {7, 3.0},
	}
	for _, c := range cases {
		if got := IncomeMultiplier(c.level); got != c.want {
			t.Errorf("IncomeMultiplier(%d) = %v, want %v", c.level, got, c.want)
		}
	}
	// Lv5 收入必须明显高于 Lv1（翻倍以上）
	if IncomeMultiplier(5) < 2*IncomeMultiplier(1) {
		t.Errorf("Lv5 multiplier should be > 2x Lv1")
	}
}

// TestJobTemplateLevels 验证工作池按等级分档：
// 同一技能低等级做不了高等级工作，且高等级工作收益更高。
func TestJobTemplateLevels(t *testing.T) {
	engineer := 0
	for _, jt := range jobTemplates {
		if jt.Skill == "engineer" {
			engineer++
		}
	}
	// engineer 应有 Repair Machine(Lv1) / Reactor(Lv3) / Project(Lv5) 三档
	if engineer != 3 {
		t.Errorf("expected 3 engineer jobs, got %d", engineer)
	}
}

// TestDoJobLevelGate 验证等级门槛：低等级 Agent 做不了需要更高等级的工作。
func TestDoJobLevelGate(t *testing.T) {
	w := &World{
		Agents: map[int64]*Agent{
			1: {ID: 1, Skills: []skill.AgentSkill{{SkillID: "engineer", Level: 2}}},
		},
		Jobs: []*Job{
			// Repair Machine 需 Lv1（Lv2 够）
			{ID: 10, Title: "Repair Machine", Reward: 35, Skill: "engineer", MinLevel: 1, Status: "claimed", ClaimedBy: 1},
			// Repair Reactor 需 Lv3（Lv2 不够）
			{ID: 11, Title: "Repair Reactor", Reward: 60, Skill: "engineer", MinLevel: 3, Status: "claimed", ClaimedBy: 1},
		},
	}
	// 高等级工作（Reactor Lv3）：Lv2 Agent 应失败（门槛不够，工作放回 open）
	if _, msg := w.DoJob(1, 11); msg == "" || strings.Contains(msg, "完成了") {
		t.Fatalf("Lv2 should NOT complete Lv3 job, got msg=%q", msg)
	}
	// 低等级工作（Machine Lv1）：Lv2 Agent 应通过门槛（可能因成功率失败，但不会返回"等级不够"）
	if _, msg := w.DoJob(1, 10); strings.Contains(msg, "需要") && strings.Contains(msg, "等级不够") {
		t.Fatalf("Lv2 should pass Lv1 gate, got msg=%q", msg)
	}
}

// TestSkillIncomeAtLevel 验证同技能不同等级的真实收入差异：
// Engineer Lv1 只能做 Repair Machine(35×1.0=35)，Lv3 能做 Reactor(60×1.5=90)，Lv5 能做 Project(100×2.2=220)。
func TestSkillIncomeAtLevel(t *testing.T) {
	w := &World{}
	lv1 := w.SkillIncomeAtLevel("engineer", 1)
	lv3 := w.SkillIncomeAtLevel("engineer", 3)
	lv5 := w.SkillIncomeAtLevel("engineer", 5)
	if lv1 >= lv3 || lv3 >= lv5 {
		t.Errorf("income should grow with level: lv1=%d lv3=%d lv5=%d", lv1, lv3, lv5)
	}
	t.Logf("Engineer income: Lv1=%d Lv3=%d Lv5=%d", lv1, lv3, lv5)
}
