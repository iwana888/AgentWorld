// perception.go —— 把经济状态注入 Agent 的感知（关键设计）。
//
// 原则：不写死"赚钱行为"。经济状态（Balance/Prices/AvailableJobs/Inventory）
// 是 Perception 的一部分，Planner 基于"世界 + 目标 + 性格 + 经济状态"自主判断：
//   钱够不够？要不要赚钱？哪个工作值得做？要不要花钱？要不要找别人合作？
package economy

import (
	"sort"
	"time"

	"agentworld/internal/skill"
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
	// M7 技能系统：该 Agent 拥有的技能集合（决定它"看得见"哪些工具）
	Skills      []skill.AgentSkill
	// M5 Skill Economy：Skill Marketplace 感知 —— 市场上可买的技能及其价格。
	// Agent 基于"当前能力 + 市场机会"评估是否做技能投资。
	Market      []SkillOffer
	// M6.1 Labor Market：可雇佣的服务 + 各技能可用的 worker。
	Services    []ServiceOffer    // 服务市场（价格/所需技能/可用 worker 数）
	WorkersBySkill map[string][]int64 // 技能 → 拥有该技能的 Agent ID 列表（供 hire）
	Names       map[int64]string  // Agent ID → 名字（供 Why 展示 hire 的 worker）
	// 市场机会
	OpenJobs    []JobPublic
	Prices      map[string]int64
	// 经济状态摘要（供 Planner 判断）
	WealthRank  int      // 财富排名（1=最富）
	AgentCount  int
	TotalWealth int64
	// M6.2.1 行动冷却：true = 正在忙（做工作/提供服务中），本轮不该接新活
	IsBusy bool
	// BusyRemain M6.2.1 忙碌剩余时间（秒，供 Why 展示"还要忙多久"）
	BusyRemain int64
}

// SkillOffer 市场上的一门技能（可购买）。
// M5.1 稀缺性：加入 Owners（拥有者数）/ Demand（需求）/ Scarcity（稀缺度），
// 让 Agent 的投资决策多一个维度（不只比价格/收益，还看稀缺程度）。
type SkillOffer struct {
	SkillID     string `json:"skillID"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       int64  `json:"price"`    // 固定购买价
	Owned       bool   `json:"owned"`    // 该 Agent 是否已拥有
	OwnLevel    int    `json:"ownLevel"` // 已有等级（0=未拥有）
	// M5.1 稀缺性指标
	Owners   int     `json:"owners"`   // 拥有该技能的 Agent 数（越少越稀缺）
	Demand   float64 `json:"demand"`   // 需求强度（0~1+，来自该技能开放工作的总报酬）
	Scarcity float64 `json:"scarcity"` // 稀缺度 = Demand / Owners（0=无需求；>0 越大越稀缺）
}

// SkillLevel 返回该 Agent 对某技能的熟练度（0=未拥有）。
func (p *Perception) SkillLevel(skillID string) int {
	return skill.LevelOf(p.Skills, skillID)
}

// HasSkill 返回该 Agent 是否拥有某技能。
func (p *Perception) HasSkill(skillID string) bool {
	return p.SkillLevel(skillID) > 0
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
		Skills:      a.Skills,
	}
	for g, q := range a.Inventory {
		p.Inventory[g] = q
	}
	// 可用工作（BuildPerception 已持锁，用无锁版避免死锁）
	for _, j := range w.openJobsLocked() {
		p.OpenJobs = append(p.OpenJobs, JobPublic{ID: j.ID, Title: j.Title, Reward: j.Reward, Skill: j.Skill, MinLevel: j.MinLevel})
	}
	// 价格
	for name, price := range w.Prices {
		p.Prices[name] = price
	}
	// M5 Skill Marketplace：注入可购买技能及其价格（固定），标记是否已拥有。
	// "当前能力"（已拥有技能）与"市场机会"（可买技能）一起喂给 Planner。
	if w.skills != nil {
		// M5.1 稀缺性：先统计各技能拥有者数 + 需求（开放工作的总报酬）
		ownerCount := map[string]int{}
		demand := map[string]int64{}
		for _, ag := range w.Agents {
			for _, as := range ag.Skills {
				if as.Level > 0 {
					ownerCount[as.SkillID]++
				}
			}
		}
		for _, j := range w.Jobs {
			if j.Status == "open" {
				demand[j.Skill] += j.Reward
			}
		}
		for _, s := range w.skills.List() {
			lv := a.SkillLevel(s.ID)
			owners := ownerCount[s.ID]
			dem := float64(demand[s.ID])
			scarcity := 0.0
			if owners > 0 {
				scarcity = dem / float64(owners)
			}
			p.Market = append(p.Market, SkillOffer{
				SkillID: s.ID, Name: s.Name, Description: s.Description,
				Price: s.BasePrice, Owned: lv > 0, OwnLevel: lv,
				Owners: owners, Demand: dem, Scarcity: scarcity,
			})
		}
	}
	// M6.1 Labor Market：注入可雇佣的服务 + 各技能可用的 worker（供 hire_agent 决策）。
	p.Services = w.laborMarketLocked()
	p.WorkersBySkill = map[string][]int64{}
	p.Names = map[int64]string{}
	for _, ag := range w.Agents {
		p.Names[ag.ID] = ag.Name
		for _, as := range ag.Skills {
			if as.Level > 0 {
				p.WorkersBySkill[as.SkillID] = append(p.WorkersBySkill[as.SkillID], ag.ID)
			}
		}
	}
	// M6.2.1 行动冷却：当前 Agent 是否忙碌（正在做工作/提供服务）
	now := time.Now()
	if now.Before(a.BusyUntil) {
		p.IsBusy = true
		p.BusyRemain = int64(now.Sub(a.BusyUntil).Seconds())
		if p.BusyRemain < 0 {
			p.BusyRemain = 0
		}
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
