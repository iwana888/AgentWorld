// module.go —— Economy World 的 sdk.Module 实现。
//
// 关键：Planner 不写死"赚钱行为"，而是把经济状态作为输入，
// 基于"目标 + 性格 + 技能 + 余额 + 市场机会"综合判断，产出带"为什么"的决策。
package economy

import (
	"context"
	"fmt"
	"strings"

	"agentworld/sdk"
	"agentworld/worlds/economy/economy"
	"agentworld/worlds/goosegame/goose"
)

// Module 经济世界模块。
type Module struct {
	world *economy.World
	planner *planner
	executor *executor
}

// New 创建经济世界模块。
func New(agentIDs []int64, names []string, personalities []string, obs *goose.Observatory) *Module {
	w := economy.NewWorld(agentIDs, names, personalities, obs)
	return &Module{
		world: w,
		planner: &planner{world: w},
		executor: &executor{world: w},
	}
}

// Game 返回世界（供 server 访问）。
func (m *Module) Game() *economy.World { return m.world }

func (m *Module) Name() string { return "economy" }

func (m *Module) Perceive(ctx context.Context, a sdk.Agent) (sdk.Perception, error) {
	return m.world.BuildPerception(a.ID), nil
}

func (m *Module) Planner() sdk.Planner { return m.planner }
func (m *Module) Executor() sdk.Executor { return m.executor }
func (m *Module) WakePolicy() sdk.WakePolicy { return AllWakePolicy{} }
func (m *Module) OnBoot(rt sdk.Runtime) error { return nil }

// ---- Planner ----

type planner struct {
	world *economy.World
}

// Decide 自主决策：基于经济状态 + 职业 + 性格 + 目标，判断"现在该做什么最有利"。
// 这里不是写死的 if-else 规则，而是把多种因素权衡后选一个最优动作。
func (p *planner) Decide(ctx context.Context, a sdk.Agent, perc sdk.Perception) (*sdk.Decision, error) {
	v, ok := perc.(*economy.Perception)
	if !ok || v == nil {
		return &sdk.Decision{Action: "idle", Reason: "世界还没准备好"}, nil
	}
	dec := p.decideEconomically(v)
	// 记录"为什么"（复用 M8 DecisionRecord 概念）
	p.world.RecordDecision(v.AgentID, buildWhy(v, dec))
	return dec, nil
}

// decideEconomically 核心决策逻辑：多因素权衡。
//   - 目标：当前经济目标（如"赚到 100"）驱动是否需要更多收入
//   - 性格：稳健 vs 冒险 影响风险偏好（接高风险高收益 vs 稳定工作）
//   - 技能：接自己能做好的工作（成功率）
//   - 余额：缺钱倾向赚钱，有钱倾向消费/交易
func (p *planner) decideEconomically(v *economy.Perception) *sdk.Decision {
	// 1) 有可接的工作时，评估是否值得做
	if len(v.OpenJobs) > 0 {
		if d := p.evaluateJob(v); d != nil {
			return d
		}
	}
	// 2) 有钱时，考虑消费或交易（商人倾向套利）
	if v.Balance >= 60 {
		if d := p.evaluateTradeOrConsume(v); d != nil {
			return d
		}
	}
	// 3) 默认：等待新机会
	return &sdk.Decision{Action: "idle", Reason: "暂时没有合适的机会，观察市场"}
}

// evaluateJob 评估是否接受某份工作：综合报酬、技能匹配、当前余额、性格风险偏好。
func (p *planner) evaluateJob(v *economy.Perception) *sdk.Decision {
	var best *economy.JobPublic
	bestScore := -1.0
	for i := range v.OpenJobs {
		j := &v.OpenJobs[i]
		// 技能匹配度（0~1）：本职业/高技能的工作更可能成功
		skill := p.world.SkillOf(v.AgentID, j.Skill)
		skillMatch := 0.3 + 0.7*float64(skill)/7.0
		// 报酬吸引力：报酬越高越想要
		rewardAttract := float64(j.Reward) / 60.0
		if rewardAttract > 1 {
			rewardAttract = 1
		}
		// 缺钱时更看重报酬（经济压力 → 行为改变，正是要观察的）
		neediness := 1.0
		if v.Balance < 40 {
			neediness = 1.3
		}
		// 性格：稳健性格偏好技能匹配高（稳定收益），冒险性格偏好报酬高
		riskBias := 1.0
		if strings.Contains(v.Personality, "冒险") || strings.Contains(v.Personality, "大胆") || strings.Contains(v.Personality, "追求") {
			riskBias = 1.2 // 冒险者更看重报酬
		} else if strings.Contains(v.Personality, "稳健") || strings.Contains(v.Personality, "踏实") || strings.Contains(v.Personality, "专注") {
			riskBias = 0.8 // 稳健者更看重技能匹配
		}
		score := skillMatch*(2-riskBias) + rewardAttract*riskBias*neediness
		if score > bestScore {
			bestScore = score
			best = j
		}
	}
	if best == nil {
		return nil
	}
	return &sdk.Decision{
		Action:     "claim",
		Target:     best.ID,
		Content:    best.Title,
		Reason:     best.Title + "报酬" + fmt.Sprintf("%d", best.Reward) + "，与我技能匹配",
	}
}

// evaluateTradeOrConsume 有钱时的决策：商人套利 / 普通消费。
func (p *planner) evaluateTradeOrConsume(v *economy.Perception) *sdk.Decision {
	// 商人/精明性格 → 倾向低价买入、高价卖出（套利）
	if strings.Contains(v.Profession, "Trader") || strings.Contains(v.Personality, "精明") {
		// 找一个相对低价的商品买入
		for name, price := range v.Prices {
			if price <= 15 {
				return &sdk.Decision{
					Action: "buy", Target: 0, Content: name,
					Reason: "价格" + fmt.Sprintf("%d", price) + "偏低，买入待涨",
				}
			}
		}
	}
	// 有库存 → 卖掉换取现金
	for name, qty := range v.Inventory {
		if qty > 0 && v.Prices[name] > 10 {
			return &sdk.Decision{
				Action: "sell", Target: 0, Content: name,
				Reason: "库存" + name + "当前价" + fmt.Sprintf("%d", v.Prices[name]) + "，卖出获利",
			}
		}
	}
	// 普通消费：有钱且缺基本物资 → 买 Food
	if v.Balance > 20 {
		return &sdk.Decision{
			Action: "buy", Target: 0, Content: "Food",
			Reason: "留些钱购买基本物资，保证生活",
		}
	}
	return nil
}

// ---- Executor ----

type executor struct {
	world *economy.World
}

// Execute 把决策落到经济世界（领工作/做工作/买/卖/消费）。
func (e *executor) Execute(ctx context.Context, rt sdk.Runtime, a sdk.Agent, perc sdk.Perception, dec *sdk.Decision) (string, error) {
	v, _ := perc.(*economy.Perception)
	if v == nil {
		return "世界还没准备好", nil
	}
	var result string
	switch dec.Action {
	case "claim":
		// 领取工作后，本 tick 内尝试完成（简化：领取即做）
		if e.world.ClaimJob(a.ID, dec.Target) {
			reward, msg := e.world.DoJob(a.ID, dec.Target)
			_ = reward
			result = msg
		} else {
			result = "工作已被抢走"
		}
	case "buy":
		if e.world.Buy(a.ID, dec.Content, 1) {
			result = "购买了 " + dec.Content
		} else {
			result = "余额不足，买不起 " + dec.Content
		}
	case "sell":
		if e.world.Sell(a.ID, dec.Content, 1) {
			result = "卖出了 " + dec.Content
		} else {
			result = "没有可卖的 " + dec.Content
		}
	case "consume":
		if e.world.Consume(a.ID, dec.Content) {
			result = "消费了 " + dec.Content
		} else {
			result = "消费失败"
		}
	default:
		result = "观望中"
	}
	// 回填 Action / 结果
	e.world.SetOutcome(a.ID, dec.Action, result)
	return result, nil
}

// ---- WakePolicy ----

// AllWakePolicy 每轮唤醒所有 Agent（经济世界所有 Agent 都有机会行动）。
type AllWakePolicy struct{}

func (AllWakePolicy) Select(ctx context.Context, rt sdk.Runtime, triggered, all []sdk.Agent) []sdk.Agent {
	return all
}

// ---- "为什么" ----

// buildWhy 构造决策依据（复用 M8：目标/经济状态/性格 → 因此决定）。
func buildWhy(v *economy.Perception, dec *sdk.Decision) string {
	lines := []string{}
	if v.Goal != "" {
		lines = append(lines, "目标："+v.Goal)
	}
	lines = append(lines, fmt.Sprintf("经济：余额 %d coins（财富第 %d/%d 名）", v.Balance, v.WealthRank, v.AgentCount))
	if v.Personality != "" {
		lines = append(lines, "性格："+v.Personality)
	}
	if len(v.OpenJobs) > 0 {
		lines = append(lines, "机会："+describeJobs(v.OpenJobs))
	}
	lines = append(lines, "因此：我决定"+describeDecision(dec))
	return strings.Join(lines, "\n")
}

func describeJobs(jobs []economy.JobPublic) string {
	parts := make([]string, 0, len(jobs))
	for _, j := range jobs {
		parts = append(parts, fmt.Sprintf("%s(%d)", j.Title, j.Reward))
	}
	if len(parts) > 4 {
		parts = parts[:4]
	}
	return strings.Join(parts, "、")
}

func describeDecision(d *sdk.Decision) string {
	if d == nil {
		return "观望"
	}
	base := map[string]string{
		"claim": "接受工作", "buy": "买入", "sell": "卖出", "consume": "消费", "idle": "观望",
	}[d.Action]
	if base == "" {
		base = d.Action
	}
	if d.Content != "" {
		base += " " + d.Content
	}
	if d.Reason != "" {
		base += "（" + d.Reason + "）"
	}
	return base
}
