// Package economy —— Economy World：一个 Agent 自主生产、交易、赚钱、消费的虚拟经济世界。
//
// 核心问题（第一版想验证的）：当 Agent 有资源约束时，它会不会为了自己的利益自主改变行为？
//
// 设计原则：
//   - 不写死"赚钱行为"（不要 if balance<20 then goal="赚钱"）。
//   - 经济状态是 Perception 的一部分（Balance/Prices/AvailableJobs/Opportunities），
//     让 Planner 基于"世界 + 目标 + 性格 + 记忆 + 关系 + 经济状态"自主判断。
//   - 复用 goosegame 的 Observatory 事件总线（M5/M8 机制），同样产出"为什么/决策记录"。
package economy

import (
	"fmt"
	"log"
	"sync"
	"time"

	"agentworld/internal/skill"
	"agentworld/worlds/goosegame/goose"
)

// ---- 经济世界状态 ----

// Agent 一个参与经济的 Agent（Economy World 视角）。
// 经济状态是 Agent 的一部分（不是数据库字段，是内存里的真实世界）。
type Agent struct {
	ID          int64             // 对应 AgentWorld AgentID
	Name        string            // 名字
	Profession  string            // 职业（工程师/农民/商人/信使/医生…）
	Personality string            // 性格
	Goal        string            // 当前经济目标（可由 Planner 更新）
	Balance     int64             // 钱包余额
	Inventory   map[string]int    // 库存（商品 -> 数量）
	// M7 Skill System：技能集合 + 等级共存。
	//   SkillID 决定"能不能用"（技能隔离：只看到该技能对应的 Tool）。
	//   Level 决定"做得好不好"（成功率/收益预期）。
	Skills []skill.AgentSkill // 例如 [{engineer,5},{trader,2}]
	Relationships map[int64]float64 // 对其他 Agent 的信任度（-1~+1，M：关系影响交易）
	LastDecision string            // 最近决策
	LastAction   string            // 最近动作
	LastWhy      string            // 最近决策依据（"为什么"）
	TotalEarned  int64             // 累计赚取（统计）
	TotalSpent   int64             // 累计花费（统计）
	LastTradeAt  time.Time         // 上次交易时间（避免连续刷）
	// M5 Skill Economy 实验指标：技能投资回报
	SkillInvested int64 // 技能投资累计花费（买技能花的钱）
	SkillEarned   int64 // 通过技能工作累计赚的钱（投资收益）
}

// SkillLevel 返回 Agent 对某技能的熟练度（0=未拥有）。
func (a *Agent) SkillLevel(skillID string) int {
	return skill.LevelOf(a.Skills, skillID)
}

// HasSkill 返回 Agent 是否拥有某技能。
func (a *Agent) HasSkill(skillID string) bool {
	return a.SkillLevel(skillID) > 0
}

// UpgradeSkill 成功完成某技能对应工作后升级该技能（上限 7，M5 技能熟练度演化）。
// 调用方需持锁（通常在 DoJob 内调用）。
func (a *Agent) UpgradeSkill(skillID string) {
	for i := range a.Skills {
		if a.Skills[i].SkillID == skillID && a.Skills[i].Level < 7 {
			a.Skills[i].Level++
			return
		}
	}
}

// ---- 市场 / 商品 ----

// Goods 商品定义。
type Goods struct {
	Name   string // 商品名
	Base   int64  // 基础价
	Supply int64  // 供给
	Demand int64  // 需求
}

// Job 一份工作（世界不断产生的需求）。
type Job struct {
	ID        int64
	Title     string   // 工作名
	Reward    int64    // 基础奖励（coins，再按技能等级乘收益倍率）
	Skill     string   // 所需技能
	MinLevel  int      // M5.1：最低技能等级门槛（Lv<MinLevel 的 Agent 不能接/做）
	Location  string   // 地点
	PostedAt  time.Time
	ClaimedBy int64    // 被谁接单（0=未接）
	Status    string   // open / claimed / done
}

// Transaction 一笔资金流动记录。
type Transaction struct {
	ID       int64
	Time     time.Time
	From     int64    // 付款方（0=世界）
	To       int64    // 收款方（0=世界）
	Amount   int64
	Kind     string   // job-reward / purchase / sale / transfer / consume / contract-pay
	Detail   string
}

// Service 一种可被雇佣的技能服务（M6.1 Labor Market）。
// MVP 固定价格，不做动态定价。
type Service struct {
	ID       string // 服务 ID（如 "repair_machine"）
	Name     string // 显示名
	Skill    string // 所需技能
	MinLevel int    // 最低技能等级
	Price    int64  // 固定服务价格（雇主付给 worker）
}

// Contract 一份雇佣合约（M6.1）。
// 交易不能直接转账，走合约 + Escrow 托管，保证"付钱后 worker 才执行"。
type Contract struct {
	ID        int64
	Employer  int64    // 雇主（付款方）
	Worker    int64    // 服务提供方（收款方）
	Service   string   // 服务名
	Price     int64    // 合约价（= 服务固定价）
	Status    string   // pending / completed / failed
	CreatedAt time.Time
	Escrow    int64    // 托管金额（雇主导出，完成后给 worker / 失败退回）
}

// World 经济世界状态（单例，带锁）。
type World struct {
	mu         sync.Mutex
	obs        *goose.Observatory
	Agents     map[int64]*Agent
	Goods      map[string]*Goods
	Jobs       []*Job
	TxLog      []Transaction // 资金流动（保留最近 maxTx，防止无限增长）
	Prices     map[string]int64 // 当前商品价格
	skills     *skill.Registry   // 技能市场（M5：技能定义 + 固定价格）
	// M5 实验观测：技能购买记录（谁买/花多少/买的什么）+ 投资收益统计
	SkillBuys  []SkillBuy
	// M6.1 Labor Market：可雇佣的服务 + 合约记录
	Services  map[string]*Service // 服务市场（id → 服务定义，固定价格）
	Contracts []*Contract         // 合约记录（谁雇佣谁/状态）
	nextContractID int64
	nextJobID  int64
	nextTxID   int64
	round      int
	startedAt  time.Time
	// 性能优化：清理/缓存
	doneJobs   int64         // 已完成并归档的工作数（用于控制 Jobs 长度）
	doneTx     int64         // 已归档的交易数（TxLog 的起始偏移）
	// 快照缓存：状态版本号变化才重建，避免前端高频轮询时全量重建。
	// 所有写操作（Transfer/DoJob/SpawnJobs/BuySkill…）都递增 version 使缓存失效，
	// 因此缓存内容始终与当前状态一致，不会过时。
	version    int64         // 状态版本号（每次写操作 +1）
	snapCache  *PublicSnapshot
	snapVer    int64         // 缓存对应的版本号
}

// SkillBuy 一次技能购买记录（Observatory 回答"谁买了技能"）。
type SkillBuy struct {
	AgentID   int64     `json:"agentID"`
	Name      string    `json:"name"`
	SkillID   string    `json:"skillID"`
	Price     int64     `json:"price"`
	BalanceAt int64     `json:"balanceAt"` // 购买时余额
	Round     int       `json:"round"`
	Time      time.Time `json:"time"`
}

// AgentProfile 一个 Agent 的人设（名字/职业/性格/初始资产）。
type AgentProfile struct {
	Name        string
	Profession  string
	Personality string
	StartCoins  int64
}

// ---- 初始 20 个 Agent（职业 + 初始资产 + 性格）----
var InitialProfiles = []AgentProfile{
	{"Alice", "Engineer", "稳健，喜欢稳定收益", 120},
	{"Bob", "Farmer", "勤劳，埋头苦干", 80},
	{"Charlie", "Trader", "精明，追求利润", 340},
	{"David", "Courier", "敏捷，接单快", 35},
	{"Emma", "Doctor", "谨慎，助人为乐", 210},
	{"Frank", "Miner", "粗犷，敢冒险", 60},
	{"Grace", "Chef", "温和，注重品质", 95},
	{"Henry", "Trader", "机敏，信息灵通", 150},
	{"Ivy", "Engineer", "专注，喜欢钻研", 110},
	{"Jack", "Farmer", "朴实，性价比优先", 70},
	{"Kate", "Courier", "利落，效率至上", 40},
	{"Leo", "Doctor", "理性，重视声誉", 180},
	{"Mia", "Miner", "大胆，追求高回报", 50},
	{"Nina", "Chef", "热情，乐于合作", 100},
	{"Oscar", "Engineer", "沉稳，长期规划", 130},
	{"Paul", "Farmer", "踏实，看天吃饭", 85},
	{"Quinn", "Courier", "灵活，见机行事", 45},
	{"Rose", "Trader", "敏锐，善于议价", 260},
	{"Sam", "Doctor", "冷静，重视信任", 165},
	{"Tina", "Miner", "独立，少依赖他人", 55},
}

// NewWorld 创建经济世界，注入 N 个 Agent 的初始状态（N = len(agentIDs)）。
// 前 20 个用 InitialProfiles 的固定人设；超出部分用 generatedProfile 按职业/性格池生成，
// 这样既保留 20 个精雕细琢的基线，又能一键扩到 100/200 Agent 跑大规模分化实验。
// skills 是技能市场注册表（M5：含技能定义与固定价格），供 Agent 做技能投资。
func NewWorld(agentIDs []int64, names []string, personalities []string, obs *goose.Observatory, skills *skill.Registry) *World {
	w := &World{
		obs:       obs,
		Agents:    map[int64]*Agent{},
		Goods:     map[string]*Goods{},
		Prices:    map[string]int64{},
		skills:    skills,
		SkillBuys: []SkillBuy{},
		Services:  map[string]*Service{},
		startedAt: time.Now(),
	}
	w.initServices()
	for i := 0; i < len(agentIDs); i++ {
		p := InitialProfiles[i%len(InitialProfiles)]
		if i < len(InitialProfiles) {
			p = InitialProfiles[i]
		} else {
			p = GeneratedProfile(i)
		}
		pers := p.Personality
		if i < len(personalities) && personalities[i] != "" {
			pers = personalities[i]
		}
		balance := p.StartCoins
		w.Agents[agentIDs[i]] = &Agent{
			ID:            agentIDs[i],
			Name:          names[i],
			Profession:    p.Profession,
			Personality:   pers,
			Goal:          "赚到更多钱，改善生活",
			Balance:       balance,
			Inventory:     map[string]int{},
			Skills:        defaultSkills(p.Profession),
			Relationships: map[int64]float64{},
			TotalEarned:   0,
			TotalSpent:    0,
		}
	}
	// 初始化商品市场
	initMarket(w)
	// 开局发放一批工作
	w.spawnInitialJobs()
	log.Printf("[economy] 经济世界启动：%d 个 Agent，初始总资产 %d coins",
		len(w.Agents), w.totalWealth())
	return w
}

// GeneratedProfile 为超出 InitialProfiles 的索引生成人设（职业循环 + 性格池 + 初始资产）。
// 支持大规模实验（100/200 Agent）时，每个人依然有职业/性格/资金差异。
// 索引 i 从 0 开始（对应第 i+1 个 Agent）。
func GeneratedProfile(i int) AgentProfile {
	professions := []string{"Engineer", "Farmer", "Trader", "Courier", "Doctor", "Miner", "Chef"}
	personalities := []string{
		"稳健，喜欢稳定收益", "冒险，追求高回报", "谨慎，助人为乐", "精明，追求利润",
		"踏实，看天吃饭", "大胆，敢冒险", "理性，重视声誉", "独立，少依赖他人",
	}
	baseCoins := []int64{60, 90, 120, 150, 40, 70, 100, 200}
	prof := professions[i%len(professions)]
	pers := personalities[i%len(personalities)]
	coins := baseCoins[i%len(baseCoins)] + int64((i*7)%25) // 微调，制造资金差异
	return AgentProfile{
		Name: fmt.Sprintf("Agent%d", i+1), Profession: prof,
		Personality: pers, StartCoins: coins,
	}
}

// initServices 初始化 Labor Market 的服务市场（M6.1）。
// 服务价格 < 对应技能价格（否则 Agent 没理由雇人而不买技能）。
// 这制造 M5→M6 的架桥决策：买技能（100，长期）vs 雇人（20，一次性）。
func (w *World) initServices() {
	for _, s := range []*Service{
		{ID: "repair_machine", Name: "Repair Machine", Skill: "engineer", MinLevel: 1, Price: 20},
		{ID: "harvest_crops", Name: "Harvest Crops", Skill: "farmer", MinLevel: 1, Price: 10},
		{ID: "collect_data", Name: "Collect Data", Skill: "courier", MinLevel: 1, Price: 8},
		{ID: "medical_treatment", Name: "Medical Treatment", Skill: "doctor", MinLevel: 1, Price: 25},
		{ID: "mine_ore", Name: "Mine Ore", Skill: "miner", MinLevel: 1, Price: 15},
		{ID: "cook_meal", Name: "Cook Meal", Skill: "chef", MinLevel: 1, Price: 10},
	} {
		w.Services[s.ID] = s
	}
}

// initMarket 初始化商品市场（10 种商品 + 初始价格）。
func initMarket(w *World) {
	for _, g := range []Goods{
		{Name: "Food", Base: 8},
		{Name: "Tools", Base: 15},
		{Name: "Medicine", Base: 25},
		{Name: "Data", Base: 12},
		{Name: "RepairKit", Base: 30},
		{Name: "Cloth", Base: 10},
		{Name: "Fuel", Base: 18},
		{Name: "Luxury", Base: 60},
		{Name: "Metal", Base: 20},
		{Name: "Seeds", Base: 6},
	} {
		gd := g
		w.Goods[g.Name] = &gd
		w.Prices[g.Name] = g.Base
	}
}

// defaultSkills 初始化 Agent 的技能集合（M5 Skill Economy MVP）。
// 关键改动（M5）：不再让 Agent 初始拥有全部技能，而是**只拥有本职业技能 Lv3**，
// 其他职业技能一律不拥有（❌）。
//
// 这样制造"投资能力"的需求：
//   Courier 只会送快递（courier 技能），想修机器（engineer）就得去 Skill Marketplace 花钱买。
//   买不买、买哪个、何时买 —— 由 Agent 自主决定（evaluate_skill）。
func defaultSkills(profession string) []skill.AgentSkill {
	own := skillIDToProfessionID(profession)
	if own == "" {
		return nil
	}
	return []skill.AgentSkill{{SkillID: own, Level: 3}}
}

// skillIDToProfessionID 把职业名映射回技能 ID（用于判断本职业技能）。
func skillIDToProfessionID(profession string) string {
	switch profession {
	case "Engineer":
		return "engineer"
	case "Farmer":
		return "farmer"
	case "Trader":
		return "trader"
	case "Courier":
		return "courier"
	case "Doctor":
		return "doctor"
	case "Miner":
		return "miner"
	case "Chef":
		return "chef"
	}
	return ""
}

// JobTemplate 一个工作模板（世界工作池，含 M5.1 技能等级门槛）。
type JobTemplate struct {
	Title    string
	Reward   int64  // 基础奖励
	Skill    string // 所需技能
	MinLevel int    // 最低技能等级
}

// jobTemplates 世界全部工作模板。
// M5.1 关键：同一技能按等级分档，等级越高能接的工作收益越高。
// 这验证"Engineer Lv1 / Lv3 / Lv5 产生不同的可做工作 + 收入"。
var jobTemplates = []JobTemplate{
	// Engineer：Lv1 基础维修 → Lv3 反应堆 → Lv5 大型工程
	{"Repair Machine", 35, "engineer", 1},
	{"Repair Reactor", 60, "engineer", 3},
	{"Engineering Project", 100, "engineer", 5},
	// Farmer
	{"Harvest Crops", 20, "farmer", 1},
	{"Irrigation", 40, "farmer", 3},
	// Courier
	{"Deliver Package", 10, "courier", 1},
	{"Collect Data", 15, "courier", 2},
	{"Fleet Transport", 30, "courier", 4},
	// Doctor
	{"First Aid", 30, "doctor", 1},
	{"Medical Treatment", 55, "doctor", 3},
	{"Surgery", 90, "doctor", 5},
	// Miner
	{"Mine Ore", 35, "miner", 1},
	{"Deep Mining", 60, "miner", 3},
	// Chef
	{"Cook Meal", 14, "chef", 1},
	{"Banquet", 35, "chef", 3},
}

// spawnInitialJobs 开局生成一批工作需求（覆盖所有技能/等级档位，保证世界活跃）。
func (w *World) spawnInitialJobs() {
	for _, t := range jobTemplates {
		w.Jobs = append(w.Jobs, &Job{
			ID: w.nextJobID,
			Title: t.Title, Reward: t.Reward, Skill: t.Skill, MinLevel: t.MinLevel,
			PostedAt: time.Now(), Status: "open",
		})
		w.nextJobID++
	}
}

// totalWealth 统计所有 Agent 总资产。
func (w *World) totalWealth() int64 {
	var sum int64
	for _, a := range w.Agents {
		sum += a.Balance
	}
	return sum
}
