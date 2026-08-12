// economy.go —— 经济世界的"操作层"：工作/交易/消费/资金转移。
//
// 所有资金变动都走 Transfer（产出 Transaction 记录 + 事件），
// 这样 Observatory 能观察"谁赚了、谁花了、谁和谁交易"。
//
// 并发安全：Scheduler（Agent 行动）与世界生成器（RoundTick）并发调用，
// 所有公开方法内部加锁；嵌套调用走无锁的内部版，避免 sync.Mutex 不可重入死锁。
package economy

import (
	"math/rand"
	"time"

	"agentworld/worlds/goosegame/goose"
)

// ---- 资金转移（唯一入口，所有资金变动都过这里） ----

// Transfer 一笔资金转移。from/to 为 0 表示世界（发工资/收税等）。
func (w *World) Transfer(from, to, amount int64, kind, detail string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.transferLocked(from, to, amount, kind, detail)
}

// transferLocked 无锁版（调用方必须已持锁）。
func (w *World) transferLocked(from, to, amount int64, kind, detail string) bool {
	if amount <= 0 {
		return false
	}
	if from != 0 {
		a, ok := w.Agents[from]
		if !ok || a.Balance < amount {
			return false
		}
	}
	if from != 0 {
		w.Agents[from].Balance -= amount
		w.Agents[from].TotalSpent += amount
	}
	if to != 0 {
		if a, ok := w.Agents[to]; ok {
			a.Balance += amount
			a.TotalEarned += amount
		}
	}
	tx := Transaction{
		ID:   w.nextTxID,
		Time: time.Now(), From: from, To: to, Amount: amount,
		Kind: kind, Detail: detail,
	}
	w.nextTxID++
	w.TxLog = append(w.TxLog, tx)
	w.obs.Publish("tx", map[string]interface{}{
		"id": tx.ID, "from": from, "to": to, "amount": amount, "kind": kind, "detail": detail,
	})
	return true
}

// BalanceOf 返回某 Agent 余额。
func (w *World) BalanceOf(id int64) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if a, ok := w.Agents[id]; ok {
		return a.Balance
	}
	return 0
}

// ---- 工作 ----

// ClaimJob 领取一份工作（先到先得）。
func (w *World) ClaimJob(agentID, jobID int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, j := range w.Jobs {
		if j.ID == jobID && j.Status == "open" {
			j.Status = "claimed"
			j.ClaimedBy = agentID
			w.obs.Publish("job.claimed", map[string]interface{}{
				"job": j.ID, "title": j.Title, "agent": agentID, "reward": j.Reward,
			})
			return true
		}
	}
	return false
}

// DoJob 完成一份工作：按技能判定成功率，成功则发奖励（世界付款）。
func (w *World) DoJob(agentID, jobID int64) (int64, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, j := range w.Jobs {
		if j.ID == jobID && j.Status == "claimed" && j.ClaimedBy == agentID {
			skill := w.Agents[agentID].Skill[j.Skill]
			success := rand.Float64() < 0.3+0.5*float64(skill)/7.0
			if success {
				j.Status = "done"
				w.transferLocked(0, agentID, j.Reward, "job-reward", j.Title)
				w.obs.Publish("job.done", map[string]interface{}{
					"job": j.ID, "title": j.Title, "agent": agentID, "reward": j.Reward,
				})
				return j.Reward, "完成了" + j.Title + "，获得" + itoa(j.Reward) + " coins"
			}
			j.Status = "open"
			j.ClaimedBy = 0
			return 0, "工作" + j.Title + "失败了"
		}
	}
	return 0, "没有这份工作"
}

// AvailableJobs 返回 open 状态的工作（供 Perception 展示）。
func (w *World) AvailableJobs() []*Job {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := []*Job{}
	for _, j := range w.Jobs {
		if j.Status == "open" {
			out = append(out, j)
		}
	}
	return out
}

// ---- 市场 / 交易 ----

// PriceOf 返回某商品当前价格。
func (w *World) PriceOf(goods string) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if p, ok := w.Prices[goods]; ok {
		return p
	}
	return 0
}

// Buy 购买商品（消耗余额 + 获得库存）。
func (w *World) Buy(agentID int64, goods string, qty int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	price := w.PriceOfLocked(goods)
	if price <= 0 || qty <= 0 {
		return false
	}
	cost := price * int64(qty)
	if !w.transferLocked(agentID, 0, cost, "purchase", goods+" x"+itoa(int64(qty))) {
		return false
	}
	a := w.Agents[agentID]
	a.Inventory[goods] += qty
	w.Prices[goods] += int64(qty) / 2
	w.obs.Publish("trade.buy", map[string]interface{}{
		"agent": agentID, "goods": goods, "qty": qty, "price": price,
	})
	return true
}

// PriceOfLocked 无锁价格查询（调用方已持锁）。
func (w *World) PriceOfLocked(goods string) int64 {
	if p, ok := w.Prices[goods]; ok {
		return p
	}
	return 0
}

// Sell 卖出商品（消耗库存 + 获得余额）。
func (w *World) Sell(agentID int64, goods string, qty int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	a, ok := w.Agents[agentID]
	if !ok || a.Inventory[goods] < qty || qty <= 0 {
		return false
	}
	price := w.Prices[goods]
	revenue := price * int64(qty)
	w.transferLocked(0, agentID, revenue, "sale", goods+" x"+itoa(int64(qty)))
	a.Inventory[goods] -= qty
	if w.Prices[goods] > 3 {
		w.Prices[goods] -= int64(qty) / 2
	}
	w.obs.Publish("trade.sell", map[string]interface{}{
		"agent": agentID, "goods": goods, "qty": qty, "price": price,
	})
	return true
}

// Consume 消费（用库存满足需求，或直接花钱）。
func (w *World) Consume(agentID int64, goods string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	a, ok := w.Agents[agentID]
	if !ok {
		return false
	}
	if a.Inventory[goods] > 0 {
		a.Inventory[goods]--
		w.obs.Publish("consume", map[string]interface{}{"agent": agentID, "goods": goods})
		return true
	}
	price := w.Prices[goods]
	if price > 0 && w.transferLocked(agentID, 0, price, "consume", goods) {
		w.obs.Publish("consume", map[string]interface{}{"agent": agentID, "goods": goods, "paid": price})
		return true
	}
	return false
}

// ---- 世界需求生成 ----

// SpawnJobs 世界不断产生新需求。
func (w *World) SpawnJobs() {
	w.mu.Lock()
	defer w.mu.Unlock()
	pool := []struct {
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
	n := 1 + rand.Intn(3)
	for i := 0; i < n; i++ {
		j := pool[rand.Intn(len(pool))]
		w.Jobs = append(w.Jobs, &Job{
			ID:    w.nextJobID,
			Title: j.title, Reward: j.reward, Skill: j.skill,
			PostedAt: time.Now(), Status: "open",
		})
		w.nextJobID++
	}
	w.obs.Publish("market.update", map[string]interface{}{
		"openJobs": len(w.openJobsLocked()),
	})
}

// openJobsLocked 无锁计算开放工作数。
func (w *World) openJobsLocked() []*Job {
	out := []*Job{}
	for _, j := range w.Jobs {
		if j.Status == "open" {
			out = append(out, j)
		}
	}
	return out
}

// RoundTick 每轮推进：生成新需求 + 价格波动（世界自己动）。
func (w *World) RoundTick() {
	w.mu.Lock()
	w.round++
	w.mu.Unlock()
	w.SpawnJobs()
	// 随机价格波动
	w.mu.Lock()
	if rand.Float64() < 0.5 {
		for name := range w.Goods {
			w.Prices[name] += 1 + rand.Int63n(3)
			break
		}
	}
	w.mu.Unlock()
}

// ---- 快照 / Inspector ----

// PublicSnapshot 经济世界公开快照。
type PublicSnapshot struct {
	Round       int              `json:"round"`
	Agents      []AgentPublic    `json:"agents"`
	Prices      map[string]int64 `json:"prices"`
	OpenJobs    []JobPublic      `json:"openJobs"`
	RecentTx    []Transaction    `json:"recentTx"`
	TotalWealth int64            `json:"totalWealth"`
}

// AgentPublic Agent 公开经济信息。
type AgentPublic struct {
	ID         int64            `json:"id"`
	Name       string           `json:"name"`
	Profession string           `json:"profession"`
	Balance    int64            `json:"balance"`
	Inventory  map[string]int   `json:"inventory"`
}

// JobPublic 公开工作信息。
type JobPublic struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Reward int64  `json:"reward"`
	Skill  string `json:"skill"`
}

// Snapshot 返回带锁的经济世界快照。
func (w *World) Snapshot() *PublicSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	snap := &PublicSnapshot{
		Round:       w.round,
		Prices:      map[string]int64{},
		RecentTx:    []Transaction{},
		TotalWealth: w.totalWealthLocked(),
	}
	for name, p := range w.Prices {
		snap.Prices[name] = p
	}
	for _, a := range w.Agents {
		inv := map[string]int{}
		for g, q := range a.Inventory {
			inv[g] = q
		}
		snap.Agents = append(snap.Agents, AgentPublic{
			ID: a.ID, Name: a.Name, Profession: a.Profession, Balance: a.Balance, Inventory: inv,
		})
	}
	for _, j := range w.Jobs {
		if j.Status == "open" {
			snap.OpenJobs = append(snap.OpenJobs, JobPublic{ID: j.ID, Title: j.Title, Reward: j.Reward, Skill: j.Skill})
		}
	}
	if len(w.TxLog) > 15 {
		snap.RecentTx = w.TxLog[len(w.TxLog)-15:]
	} else {
		snap.RecentTx = w.TxLog
	}
	return snap
}

// totalWealthLocked 无锁统计总资产。
func (w *World) totalWealthLocked() int64 {
	var sum int64
	for _, a := range w.Agents {
		sum += a.Balance
	}
	return sum
}

// Inspector 返回某个 Agent 的深度经济状态。
func (w *World) Inspector(id int64) map[string]interface{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	a, ok := w.Agents[id]
	if !ok {
		return nil
	}
	return map[string]interface{}{
		"id": a.ID, "name": a.Name, "profession": a.Profession, "personality": a.Personality,
		"balance": a.Balance, "inventory": a.Inventory, "goal": a.Goal,
		"totalEarned": a.TotalEarned, "totalSpent": a.TotalSpent,
		"lastDecision": a.LastDecision, "lastAction": a.LastAction, "lastWhy": a.LastWhy,
		"skill": a.Skill,
	}
}

// Observatory 返回事件总线。
func (w *World) Observatory() *goose.Observatory { return w.obs }

// Agent 返回某 Agent（调用方注意：返回指针，读字段安全，外部勿并发写）。
func (w *World) Agent(id int64) *Agent {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Agents[id]
}

// SkillOf 查询 Agent 某技能水平。
func (w *World) SkillOf(agentID int64, skill string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if a, ok := w.Agents[agentID]; ok {
		return a.Skill[skill]
	}
	return 0
}

// RecordDecision 记录一次决策依据（"为什么"）。
func (w *World) RecordDecision(id int64, why string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if a, ok := w.Agents[id]; ok {
		a.LastWhy = why
	}
}

// SetOutcome 回填最近决策的动作与结果。
func (w *World) SetOutcome(id int64, action, outcome string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if a, ok := w.Agents[id]; ok {
		a.LastDecision = action
		a.LastAction = outcome
	}
}

// itoa 简易 int64 转字符串。
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
