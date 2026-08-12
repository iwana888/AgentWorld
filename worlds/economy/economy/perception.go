// perception.go —— 把经济状态注入 Agent 的感知（关键设计）。
//
// 原则：不写死"赚钱行为"。经济状态（Balance/Prices/AvailableJobs/Inventory）
// 是 Perception 的一部分，Planner 基于"世界 + 目标 + 性格 + 经济状态"自主判断：
//   钱够不够？要不要赚钱？哪个工作值得做？要不要花钱？要不要找别人合作？
package economy

import (
	"sort"
)

// Perception 一个 Agent 在经济世界中的完整感知视图。
type Perception struct {
	AgentID    int64
	Name       string
	Profession string
	Personality string
	Goal       string
	Balance    int64
	Inventory  map[string]int
	// 市场机会
	OpenJobs    []JobPublic
	Prices      map[string]int64
	// 经济状态摘要（供 Planner 判断）
	WealthRank  int      // 财富排名（1=最富）
	AgentCount  int
	TotalWealth int64
}

// BuildPerception 构建某 Agent 的经济感知（基于世界当前状态）。
func (w *World) BuildPerception(agentID int64) *Perception {
	w.mu.Lock()
	defer w.mu.Unlock()
	a, ok := w.Agents[agentID]
	if !ok {
		return nil
	}
	p := &Perception{
		AgentID:     agentID,
		Name:        a.Name,
		Profession:  a.Profession,
		Personality: a.Personality,
		Goal:        a.Goal,
		Balance:     a.Balance,
		Inventory:   map[string]int{},
		Prices:      map[string]int64{},
	}
	for g, q := range a.Inventory {
		p.Inventory[g] = q
	}
	// 可用工作（BuildPerception 已持锁，用无锁版避免死锁）
	for _, j := range w.openJobsLocked() {
		p.OpenJobs = append(p.OpenJobs, JobPublic{ID: j.ID, Title: j.Title, Reward: j.Reward, Skill: j.Skill})
	}
	// 价格
	for name, price := range w.Prices {
		p.Prices[name] = price
	}
	// 财富排名
	p.AgentCount = len(w.Agents)
	p.TotalWealth = w.totalWealth()
	bal := make([]int64, 0, len(w.Agents))
	for _, ag := range w.Agents {
		bal = append(bal, ag.Balance)
	}
	sort.Slice(bal, func(i, j int) bool { return bal[i] > bal[j] })
	for i, b := range bal {
		if b == a.Balance {
			p.WealthRank = i + 1
			break
		}
	}
	return p
}
