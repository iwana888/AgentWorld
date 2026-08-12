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
	"log"
	"sync"
	"time"

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
	Skill       map[string]int    // 各工种技能水平（决定工作成功率/收益）
	Relationships map[int64]float64 // 对其他 Agent 的信任度（-1~+1，M：关系影响交易）
	LastDecision string            // 最近决策
	LastAction   string            // 最近动作
	LastWhy      string            // 最近决策依据（"为什么"）
	TotalEarned  int64             // 累计赚取（统计）
	TotalSpent   int64             // 累计花费（统计）
	LastTradeAt  time.Time         // 上次交易时间（避免连续刷）
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
	Reward    int64    // 奖励（coins）
	Skill     string   // 所需技能
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
	Kind     string   // job-reward / purchase / sale / transfer / consume
	Detail   string
}

// World 经济世界状态（单例，带锁）。
type World struct {
	mu         sync.Mutex
	obs        *goose.Observatory
	Agents     map[int64]*Agent
	Goods      map[string]*Goods
	Jobs       []*Job
	TxLog      []Transaction // 所有资金流动
	Prices     map[string]int64 // 当前商品价格
	nextJobID  int64
	nextTxID   int64
	round      int
	startedAt  time.Time
}

// ---- 初始 20 个 Agent（职业 + 初始资产 + 性格）----
var InitialProfiles = []struct {
	Name        string
	Profession  string
	Personality string
	StartCoins  int64
}{
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

// NewWorld 创建经济世界，注入 20 个 Agent 的初始状态。
func NewWorld(agentIDs []int64, names []string, personalities []string, obs *goose.Observatory) *World {
	w := &World{
		obs:      obs,
		Agents:   map[int64]*Agent{},
		Goods:    map[string]*Goods{},
		Prices:   map[string]int64{},
		startedAt: time.Now(),
	}
	// 初始 20 个 Agent
	for i := 0; i < len(agentIDs) && i < len(InitialProfiles); i++ {
		p := InitialProfiles[i]
		pers := p.Personality
		if i < len(personalities) && personalities[i] != "" {
			pers = personalities[i]
		}
		balance := p.StartCoins
		// 允许通过环境变量/外部覆盖初始资产（第一阶段固定也可）
		w.Agents[agentIDs[i]] = &Agent{
			ID:            agentIDs[i],
			Name:          names[i],
			Profession:    p.Profession,
			Personality:   pers,
			Goal:          "赚到更多钱，改善生活",
			Balance:       balance,
			Inventory:     map[string]int{},
			Skill:         defaultSkills(p.Profession),
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

// defaultSkills 根据职业初始化各工种技能。
func defaultSkills(profession string) map[string]int {
	base := map[string]int{
		"engineer": 3, "farmer": 3, "trader": 3, "courier": 3, "doctor": 3,
		"miner": 3, "chef": 3,
	}
	// 本职业高技能
	switch profession {
	case "Engineer":
		base["engineer"] = 7
	case "Farmer":
		base["farmer"] = 7
	case "Trader":
		base["trader"] = 7
	case "Courier":
		base["courier"] = 7
	case "Doctor":
		base["doctor"] = 7
	case "Miner":
		base["miner"] = 7
	case "Chef":
		base["chef"] = 7
	}
	return base
}

// spawnInitialJobs 开局生成一批工作需求。
func (w *World) spawnInitialJobs() {
	seed := []struct {
		title  string
		reward int64
		skill  string
	}{
		{"Repair Reactor", 40, "engineer"},
		{"Harvest Crops", 20, "farmer"},
		{"Collect Data", 15, "courier"},
		{"Medical Treatment", 50, "doctor"},
		{"Deliver Package", 10, "courier"},
		{"Mine Ore", 35, "miner"},
		{"Cook Meal", 14, "chef"},
	}
	for _, j := range seed {
		w.Jobs = append(w.Jobs, &Job{
			ID: w.nextJobID,
			Title: j.title, Reward: j.reward, Skill: j.skill,
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
