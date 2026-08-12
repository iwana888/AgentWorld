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
	// M6.1 架桥：若开放工作里有"想做但缺技能"的机会，进入 Buy Skill vs Hire Agent 的投资决策。
	// 这是 M5（投资自己）→ M6（使用别人）的分叉点，必须让 Planner 综合权衡，不能硬编码。
	if missing := p.missingSkillOpportunity(v); missing != "" {
		// 先评估雇人（短期、成本低、解决当下）
		if d := p.evaluateHire(v, missing); d != nil {
			return d
		}
		// 雇人不合适 → 评估买技能（长期投资）
		if d := p.evaluateSkill(v); d != nil {
			return d
		}
		// 都不行 → 等待（没钱/不值得）
		return &sdk.Decision{Action: "idle", Reason: "想做 " + missing + " 的工作但雇不起也买不起，先等待"}
	}
	// 1) 有可接的工作时，评估是否值得做（先赚眼前的钱）
	if len(v.OpenJobs) > 0 {
		if d := p.evaluateJob(v); d != nil {
			return d
		}
	}
	// 2) 技能投资：余额充足时，主动评估要不要学新技能（长期规划）
	if v.Balance >= 60 {
		if d := p.evaluateSkill(v); d != nil {
			return d
		}
	}
	// 3) 有钱时，考虑消费或交易（商人倾向套利）
	if v.Balance >= 60 {
		if d := p.evaluateTradeOrConsume(v); d != nil {
			return d
		}
	}
	// 4) 默认：等待新机会
	return &sdk.Decision{Action: "idle", Reason: "暂时没有合适的机会，观察市场"}
}

// missingSkillOpportunity 返回一个"该 Agent 想做但缺技能"的工作技能（空串=没有）。
// 即：有开放工作，其技能 Agent 未拥有或等级不够。
func (p *planner) missingSkillOpportunity(v *economy.Perception) string {
	for i := range v.OpenJobs {
		j := &v.OpenJobs[i]
		lv := v.SkillLevel(j.Skill)
		if lv <= 0 || lv < j.MinLevel {
			return j.Skill
		}
	}
	return ""
}

// evaluateHire M6.1：评估是否雇佣别人完成自己缺技能的工作（Labor Market）。
// 决策因素：
//   - 该技能有可雇的服务（v.Services）
//   - 有可用 worker（v.WorkersBySkill）
//   - 服务价格明显低于买技能价格（否则自己买更值）
//   - 余额够付服务费，且服务费占比不过高（避免雇完破产）
// 返回 hire_agent 决策（选择该技能的某个可用 worker）；不合适返回 nil。
func (p *planner) evaluateHire(v *economy.Perception, skillID string) *sdk.Decision {
	// 找到该技能对应的服务
	var svc *economy.ServiceOffer
	for i := range v.Services {
		if v.Services[i].Skill == skillID {
			svc = &v.Services[i]
			break
		}
	}
	if svc == nil || svc.AvailableWorkers <= 0 {
		return nil // 没人提供服务
	}
	// 该技能的购买价（用于对比：雇人是否显著便宜）
	skillPrice := p.skills.PriceOf(skillID)
	if skillPrice <= 0 {
		return nil
	}
	// 服务费应明显低于买技能价（雇人 = 一次性便宜方案）
	// 只有当余额够付服务费 且 服务费 <= 余额的 50%（避免雇完没钱） 才考虑雇
	if v.Balance < svc.Price || svc.Price > int64(float64(v.Balance)*0.5) {
		return nil // 雇不起
	}
	// 服务费不便宜 → 不如自己买技能（交给 evaluateSkill）
	if svc.Price > skillPrice/2 {
		return nil
	}
	// Buy VS Hire 的真正权衡：
	//   当余额能舒适买下该技能（余额 ≥ 1.2 × 技能价），且服务费相对技能价便宜到
	//   值得"买断"时 → 返回 nil，把决策交给 evaluateSkill（可能 BUY，长期投资更划算）。
	//   否则（余额不足以舒适买技能）→ 雇人解决当下（短期便宜方案）。
	// 这制造行为分化：有钱 Agent 倾向投资技能，拮据 Agent 倾向雇人。
	if v.Balance >= int64(float64(skillPrice)*1.2) {
		// 资金充裕：买技能更值（长期），把机会让给 evaluateSkill
		return nil
	}
	// 选择该技能的可用 worker（取列表第一个；可进一步按等级/价格选优）
	workers := v.WorkersBySkill[skillID]
	if len(workers) == 0 {
		return nil
	}
	worker := workers[0]
	return &sdk.Decision{
		Action:  "hire_agent",
		Target:  worker,
		Content: svc.ID,
		Reason:  "雇 " + workerName(v, worker) + " 做 " + svc.Name + "(" + itoaInt64(svc.Price) + " coins)，比买 " + skillID + "(" + itoaInt64(skillPrice) + ") 便宜",
	}
}

// workerName 返回 worker ID 对应的名字（用于 Why 展示）。
func workerName(v *economy.Perception, id int64) string {
	// Perception 不含名字映射，简化返回 ID 数字
	return fmt.Sprintf("Agent#%d", id)
}

// itoaInt64 简易 int64 转字符串（module 包本地）。
func itoaInt64(v int64) string { return fmt.Sprintf("%d", v) }

// evaluateJob 评估是否接受某份工作：综合报酬、技能匹配、当前余额、性格风险偏好。
// 关键（M7 技能隔离）：Agent 只能选择它拥有技能对应的工作。
// 没有对应技能的岗位，即使报酬再高，这个 Agent 也不会选（不能调用它没有的工具）。
// M5.1：等级门槛 —— 技能等级 < 工作 MinLevel 的岗位同样"看不见/做不了"。
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
		// M5.1 等级门槛：等级不够做不了（即使拥有该技能）
		if skill < j.MinLevel {
			continue
		}
		skillMatch := 0.3 + 0.7*float64(skill)/7.0
		// 报酬吸引力：用"等级倍率后的实际到手收益"计算，等级越高越倾向高收益工作
		effectiveReward := float64(j.Reward) * economy.IncomeMultiplier(skill)
		rewardAttract := effectiveReward / 80.0
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

// skillIncomeRef 技能"长期收益潜力"参考（满级 Lv7 × 该技能最高档工作的收益）。
// 用于 evaluateSkill 评估"买了这个技能，长期能赚多少"。
// M5.1：收益随等级增长（倍率），所以用最高档工作 × 满级倍率代表潜力。
var skillIncomeRef = map[string]int64{}

func init() {
	// 从工作池模板计算各技能的收益潜力（与 world.jobTemplates 一致）
	skillIncomeRef = map[string]int64{
		"engineer": incomePotential("engineer"), "farmer": incomePotential("farmer"),
		"courier": incomePotential("courier"), "doctor": incomePotential("doctor"),
		"miner": incomePotential("miner"), "chef": incomePotential("chef"),
		"trader": 20, // Trader 无固定工作，靠套利，记浮动收益
	}
}

// incomePotential 计算某技能的最高档基础收益（Lv7 满级倍率），作为收益潜力参考。
func incomePotential(skillID string) int64 {
	best := int64(0)
	for _, t := range economy.JobTemplates() {
		if t.Skill == skillID {
			inc := int64(float64(t.Reward) * economy.IncomeMultiplier(7))
			if inc > best {
				best = inc
			}
		}
	}
	return best
}

// currentIncome 估算 Agent 当前实际能拿到的最高单次收益。
// M5.1：从开放工作里筛出"该 Agent 当前技能等级够得着"的工作，取最高实际到手收益
// （基础收益 × 等级倍率）。等级越低 → 能做的工作收益越低。
func currentIncome(v *economy.Perception) int64 {
	best := int64(0)
	for i := range v.OpenJobs {
		j := &v.OpenJobs[i]
		lv := v.SkillLevel(j.Skill)
		if lv <= 0 || lv < j.MinLevel {
			continue // 没技能 或 等级不够
		}
		inc := int64(float64(j.Reward) * economy.IncomeMultiplier(lv))
		if inc > best {
			best = inc
		}
	}
	if best > 0 {
		return best
	}
	// 没有可见工作时的兜底：用技能收益潜力估算
	if len(v.Skills) > 0 {
		var sum, n int64
		for _, s := range v.Skills {
			if inc, ok := skillIncomeRef[s.SkillID]; ok {
				sum += inc
				n++
			}
		}
		if n > 0 {
			return sum / n
		}
	}
	return 0
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
		// M5.1 稀缺性：拥有者越少、需求越高（Scarcity 越大）越值得投 —— 稀缺技能价值更高
		if off.Scarcity > 0 {
			switch {
			case off.Owners <= 1:
				score += 0.1 // 几乎没人会 → 稀缺
			case off.Owners <= 3:
				score += 0.05
			}
		}
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
	case "hire_agent":
		// M6.1 Labor Market：雇佣 worker（dec.Target）完成一个服务（dec.Content）。
		// 简化：创建合约后本 tick 内立即由 worker 执行（领取即做，与 DoJob 一致）。
		contractID, ok := e.world.HireAgent(a.ID, dec.Target, dec.Content)
		if !ok {
			result = "雇佣失败（余额不足 / worker 不可用 / 无此服务）"
			break
		}
		_, msg := e.world.ExecuteContract(contractID)
		result = "雇佣了 Agent#" + fmt.Sprintf("%d", dec.Target) + "： " + msg
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
		"buy_skill": "技能投资", "hire_agent": "雇佣",
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
