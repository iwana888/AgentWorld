// module.go —— Economy World 的 sdk.Module 实现。
//
// 关键：Planner 不写死"赚钱行为"，而是把经济状态作为输入，
// 基于"目标 + 性格 + 技能 + 余额 + 市场机会"综合判断，产出带"为什么"的决策。
package economy

import (
	"context"
	"fmt"
	"strings"

	"agentworld/internal/skill"
	"agentworld/sdk"
	"agentworld/worlds/economy/economy"
	"agentworld/worlds/goosegame/goose"
)

// Module 经济世界模块。
type Module struct {
	world   *economy.World
	planner *planner
	executor *executor
	rt      sdk.Runtime          // 运行时（OnBoot 注入），供 Planner 做技能隔离
	skills  *skill.Registry      // Skill Registry（技能定义）
}

// New 创建经济世界模块。
func New(agentIDs []int64, names []string, personalities []string, obs *goose.Observatory) *Module {
	// 注册技能定义（Skill → Tools 映射 + M5 固定价格）
	reg := skill.NewRegistry()
	registerEconomySkills(reg)
	// 技能市场注册表注入 World，供 Agent 感知可买技能 / 做技能投资
	w := economy.NewWorld(agentIDs, names, personalities, obs, reg)
	return &Module{
		world:    w,
		planner:  &planner{world: w, skills: reg},
		executor: &executor{world: w},
		skills:   reg,
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
func (m *Module) OnBoot(rt sdk.Runtime) error {
	m.rt = rt
	m.planner.rt = rt
	return nil
}

// registerEconomySkills 注册经济世界的技能定义（Skill → 可用 Tool）。
// 每个技能的 Tools 决定 Agent 能调用哪些 MCP Tool（技能隔离）。
//
// M5 Skill Economy MVP：固定技能价格（BasePrice），第一版不做价格波动。
// 价格依据"收益潜力"设计（能赚越多越贵）：
//   Engineer 100 / Doctor 120 / Miner 80 / Trader 60 / Chef 60 / Courier 40 / Researcher 200。
// 这样测的是"Agent 会不会做 Skill Investment"，而不是"Agent 会不会适应价格波动"。
func registerEconomySkills(r *skill.Registry) {
	r.Register(&skill.Skill{
		ID: "engineer", Name: "Engineer", Description: "维修/操作机器（高价值技能）",
		Tools: []string{"repair_machine", "query_machine"}, BasePrice: 100,
	})
	r.Register(&skill.Skill{
		ID: "farmer", Name: "Farmer", Description: "种植/收获",
		Tools: []string{"harvest_crops"}, BasePrice: 50,
	})
	r.Register(&skill.Skill{
		ID: "trader", Name: "Trader", Description: "买卖/议价（套利）",
		Tools: []string{"buy_item", "sell_item"}, BasePrice: 60,
	})
	r.Register(&skill.Skill{
		ID: "courier", Name: "Courier", Description: "运输/配送",
		Tools: []string{"deliver_package", "collect_data"}, BasePrice: 40,
	})
	r.Register(&skill.Skill{
		ID: "doctor", Name: "Doctor", Description: "医疗/治疗（高价值技能）",
		Tools: []string{"medical_treatment"}, BasePrice: 120,
	})
	r.Register(&skill.Skill{
		ID: "miner", Name: "Miner", Description: "采矿/开采",
		Tools: []string{"mine_ore"}, BasePrice: 80,
	})
	r.Register(&skill.Skill{
		ID: "chef", Name: "Chef", Description: "烹饪/制作",
		Tools: []string{"cook_meal"}, BasePrice: 60,
	})
}

// ---- Planner ----

type planner struct {
	world  *economy.World
	rt     sdk.Runtime
	skills *skill.Registry
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
//   - 技能投资（M5）：市场机会 → 经济评估 → 决定是否 buy_skill
//
// 决策优先级（尽量贴近真实决策顺序）：
//   1. 攒够"闲钱"且看到值得买的技能 → 优先做 Skill Investment（长期收益 > 眼前打工）
//   2. 有可做的工作 → 先赚眼前的钱（尤其余额低时）
//   3. 有钱 → 消费/交易
//   4. 等待
//
// 关键：技能投资要在"有闲钱"时优先于继续打工，否则 Agent 会永远接工作、
// 永远不会停下来评估投资 —— 那 Skill Economy 实验就测不到"投资决策"。
func (p *planner) decideEconomically(v *economy.Perception) *sdk.Decision {
	// 1) 技能投资：当余额达到"有闲钱"水平（>=60），优先评估是否值得投资。
	//    注意：即使有可做的工作，也会先停下来评估技能机会（长期规划 Agent 行为）。
	if v.Balance >= 60 {
		if d := p.evaluateSkill(v); d != nil {
			return d
		}
	}
	// 2) 有可接的工作时，评估是否值得做（先赚眼前的钱）
	if len(v.OpenJobs) > 0 {
		if d := p.evaluateJob(v); d != nil {
			return d
		}
	}
	// 3) 余额较低但攒到 40 时，也可偶发评估便宜技能（买得起低价技能）
	if v.Balance >= 40 {
		if d := p.evaluateSkill(v); d != nil {
			return d
		}
	}
	// 4) 有钱时，考虑消费或交易（商人倾向套利）
	if v.Balance >= 60 {
		if d := p.evaluateTradeOrConsume(v); d != nil {
			return d
		}
	}
	// 5) 默认：等待新机会
	return &sdk.Decision{Action: "idle", Reason: "暂时没有合适的机会，观察市场"}
}

// evaluateJob 评估是否接受某份工作：综合报酬、技能匹配、当前余额、性格风险偏好。
// 关键（M7 技能隔离）：Agent 只能选择它拥有技能对应的工作。
// 没有对应技能的岗位，即使报酬再高，这个 Agent 也不会选（不能调用它没有的工具）。
func (p *planner) evaluateJob(v *economy.Perception) *sdk.Decision {
	var best *economy.JobPublic
	bestScore := -1.0
	for i := range v.OpenJobs {
		j := &v.OpenJobs[i]
		// 技能隔离：Agent 没有该工作对应技能 → 直接跳过（看不见/用不了）
		// 性能优化：直接复用 Perception 里已构建的技能信息（无锁），
		// 避免循环内重复加锁查询 world.Agent / world.SkillOf。
		if !v.HasSkill(j.Skill) {
			continue
		}
		// 技能等级匹配度（0~1）：本职业/高等级的工作更可能成功
		skill := v.SkillLevel(j.Skill)
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

// skillIncomeRef 技能收益参考（该技能对应工作的平均报酬，用于评估"买了能赚多少"）。
// 与 world.SpawnJobs 的工作池保持一致（Repair Reactor 40 / Harvest 20 / ...）。
// Trader 没有固定工作，靠套利，收益记为浮动值 20。
var skillIncomeRef = map[string]int64{
	"engineer": 40, "farmer": 20, "courier": 13, "doctor": 50,
	"miner": 35, "chef": 14, "trader": 20,
}

// currentIncome 估算 Agent 当前（已拥有技能）的平均单次工作收益。
// 只统计它"看得见"（已拥有）的技能对应的收益参考，取平均。
func currentIncome(v *economy.Perception) int64 {
	if len(v.Skills) == 0 {
		return 0
	}
	var sum, n int64
	for _, s := range v.Skills {
		if inc, ok := skillIncomeRef[s.SkillID]; ok {
			sum += inc
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// SkillEval 一次技能投资评估的结构化结果（M5 核心：喂给 Planner 的结构）。
// 这与 evaluateJob / evaluateTrade 并列，让 Runtime 更像真实 Agent 决策系统，
// 而不是一堆 if。Planner 基于这个结构化结果决定 BUY / NOT BUY。
type SkillEval struct {
	SkillID       string `json:"skillID"`
	Name          string `json:"name"`
	Price         int64  `json:"price"`
	CurrentBalance int64 `json:"currentBalance"`
	CurrentIncome  int64 `json:"currentIncome"`  // 单次工作收益参考
	AvailableJobs  []economy.JobPublic `json:"availableJobs"` // 该技能对应的工作机会
	ExpectedIncome int64 `json:"expectedIncome"` // 买后预计单次收益
	Risk          string `json:"risk"`          // Low / Medium / High
	Affordable    bool   `json:"affordable"`    // 买得起吗
	Recommendation string `json:"recommendation"` // BUY / NOT_BUY
}

// evaluateSkill 统一技能投资评估：给 Planner 一个结构化结果，再由 Planner 决定买不买。
// 评估维度（模仿真实投资决策）：
//   - 买得起吗：余额 ≥ 价格
//   - 值不值：新技能收益潜力 vs 当前收入提升多少
//   - 风险：余额富余程度（买完还剩多少）、是否盲目投资
//   - 收益回收期：Price / ExpectedIncome（几单回本）
//
// 关键：不硬编码"买 Engineer"。Agent 根据自己的余额/当前收入/风险偏好，
// 对每个技能独立评估。所以不同 Agent 会得出不同结论 → 产生经济策略分化。
func (p *planner) evaluateSkill(v *economy.Perception) *sdk.Decision {
	var best *SkillEval
	// 只评估"未拥有"的技能（已拥有的没必要再买）
	for i := range v.Market {
		off := &v.Market[i]
		if off.Owned || off.Price <= 0 {
			continue
		}
		ev := &SkillEval{
			SkillID: off.SkillID, Name: off.Name, Price: off.Price,
			CurrentBalance: v.Balance, CurrentIncome: currentIncome(v),
			ExpectedIncome: skillIncomeRef[off.SkillID],
			Affordable:     v.Balance >= off.Price,
		}
		// 该技能对应的开放工作（能看到有哪些机会）
		for _, j := range v.OpenJobs {
			if j.Skill == off.SkillID {
				ev.AvailableJobs = append(ev.AvailableJobs, j)
			}
		}
		// 风险等级：买完还剩多少钱 / 该技能本身收益波动
		left := v.Balance - off.Price
		switch {
		case left < 0:
			ev.Risk = "High" // 买不起
		case left < 10:
			ev.Risk = "High" // 买完几乎清零，破产风险
		case left < off.Price/2:
			ev.Risk = "Medium"
		default:
			ev.Risk = "Low"
		}
		// 收益吸引力：新技能收入 vs 当前收入（提升越大越值得）
		incomeUp := ev.ExpectedIncome - ev.CurrentIncome
		// 回收期（几单回本）：越短越划算
		payback := int64(0)
		if ev.ExpectedIncome > 0 {
			payback = (off.Price + ev.ExpectedIncome - 1) / ev.ExpectedIncome
		}
		// 综合评分（0~1）：回收期 + 收入提升 + 风险
		score := 0.0
		switch {
		case payback <= 2:
			score += 0.5 // 2 单内回本：很值
		case payback <= 5:
			score += 0.35
		case payback <= 10:
			score += 0.2
		}
		if incomeUp > 0 {
			score += 0.3 // 能提升收入
		}
		switch ev.Risk {
		case "Low":
			score += 0.2
		case "Medium":
			score += 0.1
		}
		// 性格修正：冒险者更愿意冒风险投资；稳健者要求更低风险
		riskTolerance := 0.0
		if strings.Contains(v.Personality, "冒险") || strings.Contains(v.Personality, "大胆") || strings.Contains(v.Personality, "追求") || strings.Contains(v.Personality, "独立") {
			riskTolerance = 0.15 // 冒险者愿意接受中高风险
		} else if strings.Contains(v.Personality, "稳健") || strings.Contains(v.Personality, "谨慎") || strings.Contains(v.Personality, "理性") || strings.Contains(v.Personality, "冷静") {
			riskTolerance = -0.1 // 稳健者更保守
		}
		score += riskTolerance
		// 买不起直接跳过（不够格）
		if !ev.Affordable {
			ev.Recommendation = "NOT_BUY"
			if best == nil {
				best = ev // 记录最接近的一次，供"差多少钱"参考
			}
			continue
		}
		// 决策阈值：买得起 + 评分足够高
		ev.Recommendation = "NOT_BUY"
		if score >= 0.75 {
			ev.Recommendation = "BUY"
			// 挑评分最高、最值得的技能买
			if best == nil || !best.Affordable || score > p.evalScore(best) {
				best = ev
			}
		} else if best == nil || !best.Affordable {
			best = ev
		}
	}
	if best == nil || best.Recommendation != "BUY" {
		return nil
	}
	return &sdk.Decision{
		Action:  "buy_skill",
		Target:  0,
		Content: best.SkillID,
		Reason: fmt.Sprintf("买 %s(%d coins)，预计单次收入 %d，当前收入 %d，风险 %s，回收约 %d 单",
			best.Name, best.Price, best.ExpectedIncome, best.CurrentIncome, best.Risk,
			(best.Price+best.ExpectedIncome-1)/best.ExpectedIncome),
	}
}

// evalScore 重算一个 SkillEval 的评分（供多个候选技能比较用）。
func (p *planner) evalScore(ev *SkillEval) float64 {
	payback := int64(0)
	if ev.ExpectedIncome > 0 {
		payback = (ev.Price + ev.ExpectedIncome - 1) / ev.ExpectedIncome
	}
	score := 0.0
	switch {
	case payback <= 2:
		score += 0.5
	case payback <= 5:
		score += 0.35
	case payback <= 10:
		score += 0.2
	}
	if ev.ExpectedIncome > ev.CurrentIncome {
		score += 0.3
	}
	switch ev.Risk {
	case "Low":
		score += 0.2
	case "Medium":
		score += 0.1
	}
	return score
}

// visibleTools 返回该 Agent 通过其技能能调用的工具名集合（M7 技能隔离）。
// 从全局能力里按 Agent 拥有的 Skill 过滤，只保留它"看得见"的工具。
func (p *planner) visibleTools(agentID int64) map[string]bool {
	visible := map[string]bool{}
	ag := p.world.Agent(agentID)
	if ag == nil {
		return visible
	}
	for _, as := range ag.Skills {
		for _, t := range p.visibleToolNamesForSkill(as.SkillID) {
			visible[t] = true
		}
	}
	return visible
}

// visibleToolNamesForSkill 返回某技能对应的工具名（从 Skill Registry 查）。
func (p *planner) visibleToolNamesForSkill(skillID string) []string {
	if p.skills == nil {
		return nil
	}
	return p.skills.ToolsOf(skillID)
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
			// M7 真实 MCP 链路：DoJob 内部已发奖励，这里再通过工具"实际执行"，
			// 把工具调用结果写入 Outcome（验证 Skill → Tool → MCP 完整链路）。
			tool := economy.SkillToTool(e.world.JobSkill(dec.Target))
			if out, err := rt.CallTool("economy_machine", tool, map[string]interface{}{
				"target": dec.Target, "agent": a.ID,
			}); err == nil && out != "" {
				result = result + "（工具 " + tool + " → " + out + "）"
				// 发布技能使用事件（Timeline 展示：🔧 Alice 使用 Engineer 技能）
				e.world.PublishSkillUsed(a.ID, tool, out)
			}
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
	case "buy_skill":
		// M5 Skill Economy：购买新技能。成功后该技能对应新 Job 会自动出现在世界，
		// Agent 用它赚钱 → "购买 → 新 Job → 新收入 → 下一轮决策"。
		if e.world.BuySkill(a.ID, dec.Content) {
			result = "投资购买了技能 " + dec.Content
			// M7 真实 MCP 链路：买技能本质也是调用一个"技能市场"能力（返回购买确认）
			if out, err := rt.CallTool("economy_machine", "buy_skill", map[string]interface{}{
				"skill": dec.Content, "agent": a.ID,
			}); err == nil && out != "" {
				result = result + "（" + out + "）"
			}
		} else {
			result = "技能投资失败：余额不足或已拥有 " + dec.Content
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
	// 技能（M7）：展示 Agent 拥有的技能与等级 —— "为什么"能解释它选了哪个技能
	if len(v.Skills) > 0 {
		lines = append(lines, "技能："+describeSkills(v.Skills))
	}
	// 机会：只显示该 Agent 技能相关（技能隔离后"看得见"的工作）
	visible := make([]economy.JobPublic, 0, len(v.OpenJobs))
	for _, j := range v.OpenJobs {
		if v.HasSkill(j.Skill) {
			visible = append(visible, j)
		}
	}
	if len(visible) > 0 {
		lines = append(lines, "机会："+describeJobs(visible))
	}
	// M5：技能市场感知 —— 展示可买但还没买的技能及其价格（"为什么没买"可观察）
	if len(v.Market) > 0 {
		buyable := make([]string, 0, len(v.Market))
		for _, off := range v.Market {
			if !off.Owned && off.Price > 0 {
				buyable = append(buyable, fmt.Sprintf("%s(%d)", off.Name, off.Price))
			}
		}
		if len(buyable) > 0 {
			lines = append(lines, "技能市场："+strings.Join(buyable, "、"))
		}
	}
	lines = append(lines, "因此：我决定"+describeDecision(dec))
	return strings.Join(lines, "\n")
}

// describeSkills 展示技能集合：{engineer Lv5, trader Lv2}。
func describeSkills(skills []skill.AgentSkill) string {
	parts := make([]string, 0, len(skills))
	for _, s := range skills {
		if s.Level > 0 {
			parts = append(parts, fmt.Sprintf("%s Lv%d", s.SkillID, s.Level))
		}
	}
	return strings.Join(parts, "、")
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
		"buy_skill": "技能投资",
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
