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
	"sort"
	"time"

	"agentworld/internal/skill"
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
	w.touchVersionLocked() // 资金变动使快照失效
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
	// 性能：交易记录只保留最近 maxTxLog 条，防止无限增长
	if len(w.TxLog) > maxTxLog {
		drop := len(w.TxLog) - maxTxLog
		w.doneTx += int64(drop)
		w.TxLog = w.TxLog[drop:]
	}
	w.obs.Publish("tx", map[string]interface{}{
		"id": tx.ID, "from": from, "to": to, "amount": amount, "kind": kind, "detail": detail,
	})
	return true
}

// maxTxLog 交易记录保留上限（资金流动总量仍由 doneTx 偏移累计，观测不受影响）。
const maxTxLog = 5000

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

// DoJob 完成一份工作：先检查技能等级门槛，再按技能判定成功率，成功则发奖励（世界付款）。
// M5.1 关键改动：
//   - 等级门槛：Agent 技能等级 < j.MinLevel → 直接失败（不能做更高等级的工作）
//   - 收益倍率：奖励 = baseReward * incomeMultiplier(level)，等级越高赚得越多
// 这让"升级技能本身就是投资"成立（Lv1/Lv3/Lv5 的收入真正不同）。
func (w *World) DoJob(agentID, jobID int64) (int64, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, j := range w.Jobs {
		if j.ID == jobID && j.Status == "claimed" && j.ClaimedBy == agentID {
			skill := w.Agents[agentID].SkillLevel(j.Skill)
			// M5.1 等级门槛：等级不够直接做不了（技能隔离的"等级维度"）
			if skill < j.MinLevel {
				j.Status = "open"
				j.ClaimedBy = 0
				return 0, j.Title + "需要" + j.Skill + " Lv" + itoa(int64(j.MinLevel)) + "，我等级不够"
			}
			// M6.2.1 成功率随技能等级（与服务一致）
			success := rand.Float64() < SkillSuccessRate(skill)
			if success {
				j.Status = "done"
				// M5.1 收益倍率：等级越高，同一份工作收入越高
				reward := int64(float64(j.Reward) * IncomeMultiplier(skill))
				w.transferLocked(0, agentID, reward, "job-reward", j.Title)
				// M5：累加技能投资收益 + 技能熟练度升级（做得多 → 越熟练 → 成功率/收益越高）。
				// 这让"购买新技能后需要做该类工作逐渐熟练"成为真实过程：
				// 买了技能 → 做该类工作 → 升级 → 收入增长（投资回报显现）。
				if a, ok := w.Agents[agentID]; ok {
					a.SkillEarned += reward
					a.UpgradeSkill(j.Skill)
				}
				w.obs.Publish("job.done", map[string]interface{}{
					"job": j.ID, "title": j.Title, "agent": agentID, "reward": reward, "skill": j.Skill, "level": skill,
				})
				// 性能：已完成工作归档清理，防止 Jobs 无限增长
				w.trimJobsLocked()
				// M6.2.1 行动冷却：做完工作后忙碌一段，不能无限连续干活
				if a, ok := w.Agents[agentID]; ok {
					cd := jobCooldown(j.Skill)
					if t := time.Now().Add(cd); t.After(a.BusyUntil) {
						a.BusyUntil = t
					}
				}
				return reward, "完成了" + j.Title + "，获得" + itoa(reward) + " coins"
			}
			j.Status = "open"
			j.ClaimedBy = 0
			return 0, "工作" + j.Title + "失败了"
		}
	}
	return 0, "没有这份工作"
}

// JobTemplates 返回世界工作模板的只读副本（供 Planner 估算技能收益潜力）。
func JobTemplates() []JobTemplate {
	out := make([]JobTemplate, len(jobTemplates))
	copy(out, jobTemplates)
	return out
}

// SkillIncomeAtLevel 返回某技能在指定等级下"可做的最高档工作"的实际到手收益（基础收益×倍率）。
// M5.1：等级越高能解锁更高收益工作（等级门槛）+ 同一工作赚更多（倍率）。
// 返回 0 表示该技能没有匹配的工作（如 Trader 靠套利，无固定工作）。
func (w *World) SkillIncomeAtLevel(skillID string, level int) int64 {
	if level <= 0 {
		return 0
	}
	// 找到该技能等级 ≤ level 的最高档工作
	best := int64(0)
	for _, t := range jobTemplates {
		if t.Skill == skillID && t.MinLevel <= level {
			inc := int64(float64(t.Reward) * IncomeMultiplier(level))
			if inc > best {
				best = inc
			}
		}
	}
	return best
}

// SkillMaxIncome 返回某技能可达到的最高收益潜力（Lv7 满级 × 最高档工作）。
// 用于"买了这个技能，长期能赚多少"的评估参考。
func (w *World) SkillMaxIncome(skillID string) int64 {
	best := int64(0)
	for _, t := range jobTemplates {
		if t.Skill == skillID {
			inc := int64(float64(t.Reward) * IncomeMultiplier(7))
			if inc > best {
				best = inc
			}
		}
	}
	return best
}

// IncomeMultiplier 技能等级 → 收益倍率（M5.1）。
// 阶梯式增长，让"升级技能"的收益回报显著：
//   Lv1:1.0  Lv2:1.2  Lv3:1.5  Lv4:1.8  Lv5:2.2  Lv6:2.6  Lv7:3.0
// 配合等级门槛，Lv1 只能做低收益工作、Lv5 能做高收益工作且收入翻倍以上。
// 导出供 Planner（module.go）计算实际到手收益用。
func IncomeMultiplier(level int) float64 {
	switch {
	case level >= 7:
		return 3.0
	case level >= 6:
		return 2.6
	case level >= 5:
		return 2.2
	case level >= 4:
		return 1.8
	case level >= 3:
		return 1.5
	case level >= 2:
		return 1.2
	default:
		return 1.0
	}
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

// BuySkill 购买一门技能（M5 Skill Economy MVP）。
//   - 余额不足 / 已拥有 / 技能不存在 → 返回 false
//   - 成功：扣款（transferLocked 到世界）+ 获得该技能（Level 1 起步）+ 记录 SkillBuy + 发布事件
// 购买后该技能对应的新 Job 会自动出现在世界工作池里（SpawnJobs 会生成），
// Agent 即可用它赚钱 → "购买 → 新 Job → 新收入 → 下一轮决策"。
func (w *World) BuySkill(agentID int64, skillID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	a, ok := w.Agents[agentID]
	if !ok || a.SkillLevel(skillID) > 0 {
		return false // 已拥有，不重复买
	}
	if w.skills == nil {
		return false
	}
	s := w.skills.Get(skillID)
	if s == nil || s.BasePrice <= 0 {
		return false
	}
	if !w.transferLocked(agentID, 0, s.BasePrice, "skill-buy", "Skill: "+s.Name) {
		return false // 余额不足
	}
	a.Skills = append(a.Skills, skill.AgentSkill{SkillID: skillID, Level: 1})
	a.SkillInvested += s.BasePrice
	w.SkillBuys = append(w.SkillBuys, SkillBuy{
		AgentID: agentID, Name: a.Name, SkillID: skillID, Price: s.BasePrice,
		BalanceAt: a.Balance, Round: w.round, Time: time.Now(),
	})
	w.obs.Publish("skill.buy", map[string]interface{}{
		"agent": agentID, "name": a.Name, "skill": skillID, "price": s.BasePrice,
		"balance": a.Balance, "round": w.round,
	})
	return true
}

// SkillMarket 返回技能市场的公开快照（供前端 Marketplace 面板展示）。
func (w *World) SkillMarket() []SkillOffer {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.skillMarketLocked()
}

// skillMarketLocked 无锁版技能市场快照（调用方需持锁）。
func (w *World) skillMarketLocked() []SkillOffer {
	if w.skills == nil {
		return nil
	}
	// M5.1 稀缺性：统计各技能拥有者数 + 需求（开放工作的总报酬）
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
	out := make([]SkillOffer, 0, len(w.skills.List()))
	for _, s := range w.skills.List() {
		owners := ownerCount[s.ID]
		dem := float64(demand[s.ID])
		scarcity := 0.0
		if owners > 0 {
			scarcity = dem / float64(owners)
		}
		out = append(out, SkillOffer{
			SkillID: s.ID, Name: s.Name, Description: s.Description, Price: s.BasePrice,
			Owners: owners, Demand: dem, Scarcity: scarcity,
		})
	}
	return out
}

// ServiceOffer 劳动力市场上可雇佣的一个服务（含该技能可用的 worker 视图）。
// WorkerOffer M6.3：劳动力市场上一个可雇的 worker（含信誉/成功率/等级）。
// 让 Planner 能在多个 worker 间比较（价格 × 成功率 × 声誉），形成真实的劳动市场竞争。
type WorkerOffer struct {
	AgentID     int64   `json:"agentID"`
	Name        string  `json:"name"`
	SkillLevel  int     `json:"skillLevel"`
	SuccessRate float64 `json:"successRate"` // 0~1
	Reputation  int64   `json:"reputation"`  // 0~100 职业信用
	Price       int64   `json:"price"`       // 该服务固定价
}

type ServiceOffer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Skill    string `json:"skill"`
	MinLevel int    `json:"minLevel"`
	Price    int64  `json:"price"` // 固定服务价格
	// 可用 worker 统计（谁有能力提供服务）
	AvailableWorkers int           `json:"availableWorkers"` // 拥有该技能的 Agent 数
	Workers          []WorkerOffer `json:"workers"`          // M6.3 可雇 worker 列表（含信誉，供选择）
}

// workerQuote M6.4 Dynamic Pricing：计算某 worker 提供某服务的独立报价。
// 报价 = 基础价 × (1 + 等级溢价 + 声誉溢价) × 供需系数
//   - 等级溢价：Lv1~Lv7 → 0 ~ +0.5（高等级能提供更可靠服务，要价更高）
//   - 声誉溢价：0~100 → 0 ~ +0.3（高信誉值得溢价）
//   - 供需系数：该技能 worker 越少、需求越高 → 溢价；竞争多 → 打折
// 这形成"Lv7 贵 + 可靠" vs "Lv1 便宜 + 易失败"的市场分层。
func (w *World) workerQuote(svc *Service, worker *Agent, demand int64, workerCount int) int64 {
	levelPrem := 0.5 * float64(worker.SkillLevel(svc.Skill)) / 7.0
	repPrem := 0.3 * float64(worker.Reputation) / 100.0
	// 供需：需求/worker 比（>1 供不应求涨价，<1 竞争激烈降价）
	supplyFactor := 1.0
	if workerCount > 0 {
		ratio := float64(demand) / (float64(workerCount) * 10.0)
		if ratio > 1.5 {
			supplyFactor = 1.25 // 供不应求，涨 25%
		} else if ratio < 0.5 {
			supplyFactor = 0.85 // 竞争激烈，降 15%
		}
	}
	price := float64(svc.Price) * (1 + levelPrem + repPrem) * supplyFactor
	if price < float64(svc.Price)*0.5 {
		price = float64(svc.Price) * 0.5 // 保底半价
	}
	return int64(price + 0.5)
}

// LaborMarket 返回劳动力市场的公开快照（供 Planner 感知 + 前端）。
func (w *World) LaborMarket() []ServiceOffer {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.laborMarketLocked()
}

// laborMarketLocked 无锁构建劳动力市场快照（调用方需持锁）。
// M6.3：每个服务列出可雇 worker 的信誉/成功率/等级（排名），供 Planner 比较选择。
// M6.4：每个 worker 有独立报价（workerQuote，随等级/声誉/供需动态）。
func (w *World) laborMarketLocked() []ServiceOffer {
	out := make([]ServiceOffer, 0, len(w.Services))
	// 先统计每个技能的需求（开放工作总报酬）和 worker 数（供需）
	demand := map[string]int64{}
	workerCount := map[string]int{}
	for _, j := range w.Jobs {
		if j.Status == "open" {
			demand[j.Skill] += j.Reward
		}
	}
	for _, ag := range w.Agents {
		for _, as := range ag.Skills {
			if as.Level > 0 {
				workerCount[as.SkillID]++
			}
		}
	}
	for _, s := range w.Services {
		offer := ServiceOffer{
			ID: s.ID, Name: s.Name, Skill: s.Skill, MinLevel: s.MinLevel, Price: s.Price,
		}
		// 列出拥有该技能的所有 worker（含信誉/成功率/等级 + M6.4 独立报价）
		for _, ag := range w.Agents {
			lv := ag.SkillLevel(s.Skill)
			if lv <= 0 {
				continue
			}
			price := w.workerQuote(s, ag, demand[s.Skill], workerCount[s.Skill])
			offer.Workers = append(offer.Workers, WorkerOffer{
				AgentID: ag.ID, Name: ag.Name, SkillLevel: lv,
				SuccessRate: ag.SuccessRate(), Reputation: ag.Reputation, Price: price,
			})
		}
		offer.AvailableWorkers = len(offer.Workers)
		// 按（成功率降序、声誉降序、等级降序）排序：更可靠的 worker 排前面
		sort.Slice(offer.Workers, func(i, j int) bool {
			ri, rj := offer.Workers[i], offer.Workers[j]
			if ri.SuccessRate != rj.SuccessRate {
				return ri.SuccessRate > rj.SuccessRate
			}
			if ri.Reputation != rj.Reputation {
				return ri.Reputation > rj.Reputation
			}
			return ri.SkillLevel > rj.SkillLevel
		})
		out = append(out, offer)
	}
	return out
}

// HireAgent M6.1 + M6.2.1：雇主雇佣 worker 完成一个服务（Labor Market 交易）。
//   - 校验：雇主余额足够、worker 存在且拥有对应技能（等级够）
//   - Escrow：创建 Contract，把服务费从雇主余额锁进合约
//   - M6.2.1：合约进入 working，按服务耗时设定 ReadyAt；worker 进入忙碌（期间不能接新活），
//     雇主也短暂忙碌（下单协调）。完成/失败由 RoundTick 的 SettleContracts 到期结算，
//     不再创建后瞬间完成 —— 阻断"高速刷合约"。
//   - 返回 (contractID, 是否创建成功)
func (w *World) HireAgent(employer, worker int64, serviceID string) (int64, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	svc, ok := w.Services[serviceID]
	if !ok {
		return 0, false
	}
	if employer == worker {
		return 0, false // 不能雇自己
	}
	emp, ok := w.Agents[employer]
	if !ok {
		return 0, false
	}
	wrk, ok := w.Agents[worker]
	if !ok {
		return 0, false
	}
	// worker 忙（正在做别的服务）→ 不能接新单
	if time.Now().Before(wrk.BusyUntil) {
		return 0, false
	}
	lv := wrk.SkillLevel(svc.Skill)
	if lv < svc.MinLevel {
		return 0, false // worker 没有该技能或等级不够
	}
	// M6.4：按 worker 独立报价扣款（随等级/声誉/供需动态）
	price := w.workerQuoteLocked(svc, wrk)
	if emp.Balance < price {
		return 0, false // 雇主余额不足（付不起该 worker 的报价）
	}
	// Escrow：从雇主扣款锁进合约
	emp.Balance -= price
	emp.TotalSpent += price
	now := time.Now()
	dur := svc.Duration
	if dur <= 0 {
		dur = 5 * time.Second // 兜底
	}
	ct := &Contract{
		ID: w.nextContractID, Employer: employer, Worker: worker,
		Service: svc.Name, Price: price, Status: "working",
		CreatedAt: now, StartedAt: now, ReadyAt: now.Add(dur), Escrow: price,
	}
	w.nextContractID++
	w.Contracts = append(w.Contracts, ct)
	// 冷却：worker 忙到 ReadyAt（服务执行中），雇主忙一小段（下单协调，不能无限下单）
	wrk.BusyUntil = ct.ReadyAt
	if emp.BusyUntil.Before(ct.ReadyAt) {
		emp.BusyUntil = ct.ReadyAt
	}
	w.touchVersionLocked()
	w.obs.Publish("contract.created", map[string]interface{}{
		"id": ct.ID, "employer": employer, "worker": worker, "service": svc.Name,
		"price": price, "readyIn": int(dur.Seconds()),
	})
	return ct.ID, true
}

// workerQuoteLocked 计算某 worker 的独立报价（M6.4，调用方需持锁）。
// 复用 workerQuote，但统计供需时需要遍历世界状态。
func (w *World) workerQuoteLocked(svc *Service, worker *Agent) int64 {
	demand := int64(0)
	for _, j := range w.Jobs {
		if j.Status == "open" && j.Skill == svc.Skill {
			demand += j.Reward
		}
	}
	count := 0
	for _, ag := range w.Agents {
		if ag.SkillLevel(svc.Skill) > 0 {
			count++
		}
	}
	return w.workerQuote(svc, worker, demand, count)
}

// SettleContracts M6.2.1：结算所有到期的 working 合约（由 RoundTick 每 tick 调用）。
//   - 按 worker 技能成功率判定（成功率随技能等级提高）
//   - 成功：Escrow 释放给 worker（Transfer 记录）+ 技能升级
//   - 失败：Escrow 退回雇主 + Contract failed
func (w *World) SettleContracts(now time.Time) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	settled := 0
	for _, ct := range w.Contracts {
		if ct.Status != "working" || now.Before(ct.ReadyAt) {
			continue // 未到期
		}
		settled++
		wrk := w.Agents[ct.Worker]
		if wrk == nil {
			ct.Status = "failed"
			w.obs.Publish("contract.failed", map[string]interface{}{"id": ct.ID, "reason": "worker gone"})
			continue
		}
		// 该合约对应的服务技能
		svcSkill := w.contractSkillLocked(ct.Service)
		lv := wrk.SkillLevel(svcSkill)
		// M6.2.1 成功率随技能等级（Lv1~60% / Lv3~73% / Lv5~85% / Lv7~97%）
		success := rand.Float64() < SkillSuccessRate(lv)
		if success {
			ct.Status = "completed"
			// Escrow 释放给 worker
			w.transferLocked(0, ct.Worker, ct.Escrow, "contract-pay", ct.Service)
			if a, ok := w.Agents[ct.Worker]; ok {
				a.SkillEarned += ct.Escrow
				a.UpgradeSkill(svcSkill) // worker 通过服务升级技能
				a.OnContractSettled(true) // M6.3：成功 → 声誉 +1
			}
			w.obs.Publish("contract.completed", map[string]interface{}{
				"id": ct.ID, "employer": ct.Employer, "worker": ct.Worker, "service": ct.Service,
				"price": ct.Escrow, "reputation": w.Agents[ct.Worker].Reputation,
			})
			w.obs.Publish("reputation.change", map[string]interface{}{
				"agent": ct.Worker, "name": w.Agents[ct.Worker].Name,
				"reputation": w.Agents[ct.Worker].Reputation, "delta": 1,
			})
		} else {
			ct.Status = "failed"
			// 失败：Escrow 退回雇主
			w.transferLocked(0, ct.Employer, ct.Escrow, "contract-refund", ct.Service+"退款")
			if a, ok := w.Agents[ct.Worker]; ok {
				a.OnContractSettled(false) // M6.3：失败 → 声誉 -2
			}
			w.obs.Publish("contract.failed", map[string]interface{}{
				"id": ct.ID, "employer": ct.Employer, "worker": ct.Worker, "service": ct.Service,
				"refund": ct.Escrow, "reputation": w.Agents[ct.Worker].Reputation,
			})
			w.obs.Publish("reputation.change", map[string]interface{}{
				"agent": ct.Worker, "name": w.Agents[ct.Worker].Name,
				"reputation": w.Agents[ct.Worker].Reputation, "delta": -2,
			})
		}
	}
	return settled
}

// ContractDuration 返回某合约的服务执行耗时（供 executor 展示"多久后结算"）。
func (w *World) ContractDuration(contractID int64) time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, ct := range w.Contracts {
		if ct.ID == contractID {
			return ct.ReadyAt.Sub(ct.StartedAt)
		}
	}
	return 0
}

// ContractPrice 返回某合约的价格（供 executor 展示托管金额）。
func (w *World) ContractPrice(contractID int64) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, ct := range w.Contracts {
		if ct.ID == contractID {
			return ct.Price
		}
	}
	return 0
}

// contractSkillLocked 返回合约对应服务的技能（无锁，调用方需持锁）。
func (w *World) contractSkillLocked(serviceName string) string {
	for _, s := range w.Services {
		if s.Name == serviceName {
			return s.Skill
		}
	}
	return ""
}

// SkillSuccessRate M6.2.1：技能等级 → 服务/工作成功率（更陡峭，高等级明显更可靠）。
//   Lv1≈60%  Lv3≈73%  Lv5≈85%  Lv7≈97%
// 相比旧的 0.3+0.5*Lv/7（Lv1≈37%），高等级 worker 的可靠性优势更明显，
// 让"便宜低技能 worker"与"贵高技能 worker"形成真实取舍。
// 导出供 Planner（module.go）用技能等级估算成功率。
func SkillSuccessRate(level int) float64 {
	if level <= 0 {
		return 0.3
	}
	if level >= 7 {
		return 0.97
	}
	return 0.55 + 0.06*float64(level)
}

// jobCooldown M6.2.1：不同技能的工作做完后的冷却时长（越复杂的工作耗时越久）。
// 让经济节奏更真实：不能像机器一样每轮都高速完成工作赚钱。
func jobCooldown(skill string) time.Duration {
	switch skill {
	case "doctor":
		return 40 * time.Second
	case "engineer":
		return 30 * time.Second
	case "miner":
		return 20 * time.Second
	case "farmer":
		return 15 * time.Second
	case "chef":
		return 12 * time.Second
	case "courier":
		return 10 * time.Second
	default:
		return 8 * time.Second
	}
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
	w.touchVersionLocked() // 工作池变化使快照失效
	n := 1 + rand.Intn(3)
	for i := 0; i < n; i++ {
		j := jobTemplates[rand.Intn(len(jobTemplates))]
		w.Jobs = append(w.Jobs, &Job{
			ID:       w.nextJobID,
			Title:    j.Title, Reward: j.Reward, Skill: j.Skill, MinLevel: j.MinLevel,
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

// maxOpenJobs / maxDoneJobs 控制工作池规模，防止 Jobs 无限增长导致遍历越来越慢。
//   - 保留最近 maxDoneJobs 个已完成的 Job（前端观察、快照不受影响）
//   - 保留 maxOpenJobs 个开放 Job（世界活跃需求）
const (
	maxOpenJobs = 200
	maxDoneJobs = 500
)

// trimJobsLocked 归档清理已完成的工作：超过上限的从 Jobs 头部移除。
// 调用方需持锁（通常在 DoJob 成功后调用）。
func (w *World) trimJobsLocked() {
	w.doneJobs++
	// 统计当前开放工作数 + 已完成工作数（头部多为已完成/已认领的旧工作）
	var done, open int
	trim := 0
	for _, j := range w.Jobs {
		switch j.Status {
		case "open":
			open++
		default: // claimed / done：都算"非开放"，可从头部清理
			done++
		}
	}
	// 开放工作超限：也清理最旧的（优先清已认领/已完成的头部）
	if open > maxOpenJobs {
		trim = open - maxOpenJobs
	}
	if done-trim > maxDoneJobs {
		if need := done - maxDoneJobs - trim; need > 0 {
			trim += need
		}
	}
	if trim <= 0 {
		return
	}
	if trim >= len(w.Jobs) {
		w.Jobs = w.Jobs[:0]
		return
	}
	w.Jobs = w.Jobs[trim:]
}

// RoundTick 每轮推进：生成新需求 + 价格波动（世界自己动）。
func (w *World) RoundTick() {
	w.mu.Lock()
	w.round++
	w.mu.Unlock()
	w.SpawnJobs()
	// M6.2.1 合约结算：到期的 working 合约在本 tick 完成/失败
	w.SettleContracts(time.Now())
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
	// M5 Skill Economy：技能市场 + 技能购买记录（Observatory 回答实验问题）
	SkillMarket []SkillOffer `json:"skillMarket"`
	SkillBuys   []SkillBuy   `json:"skillBuys"`
	// M6.1 Labor Market：服务市场 + 合约统计
	Services      []ServiceOffer `json:"services"`
	Contracts     []ContractView `json:"contracts"`     // 最近 40 条（用于展示）
	ContractStats ContractStats  `json:"contractStats"` // 累计统计（口径清晰，供实验观测）
}

// ContractStats 合约累计统计（M6.2 统一口径：累计值，非当前快照）。
type ContractStats struct {
	Total        int64 `json:"total"`        // 累计创建的合约数
	Completed    int64 `json:"completed"`    // 累计成功数
	Failed       int64 `json:"failed"`       // 累计失败数
	Pending      int64 `json:"pending"`      // 当前进行中
	TotalVolume  int64 `json:"totalVolume"`  // 累计成交额（completed 的合约金额）
	MoneyMoved   int64 `json:"moneyMoved"`   // 实际转移金额（worker 实收）
}

// ContractView 合约的公开视图（Observatory 展示雇佣活动）。
// Status: working(执行中) / completed / failed
type ContractView struct {
	ID        int64  `json:"id"`
	Employer  int64  `json:"employer"`
	Worker    int64  `json:"worker"`
	Service   string `json:"service"`
	Price     int64  `json:"price"`
	Status    string `json:"status"`
	Duration  int64  `json:"duration"`  // M6.2.1 服务执行耗时（秒）
	CreatedAt int64  `json:"createdAt"`
}

// AgentPublic Agent 公开经济信息。
type AgentPublic struct {
	ID         int64            `json:"id"`
	Name       string           `json:"name"`
	Profession string           `json:"profession"`
	Balance    int64            `json:"balance"`
	Inventory  map[string]int   `json:"inventory"`
	Skills     []skill.AgentSkill `json:"skills"`
	// M6.3 职业信誉
	Reputation         int64   `json:"reputation"`
	CompletedContracts int64   `json:"completedContracts"`
	FailedContracts    int64   `json:"failedContracts"`
	SuccessRate        float64 `json:"successRate"`
}

// JobPublic 公开工作信息。
type JobPublic struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Reward   int64  `json:"reward"`
	Skill    string `json:"skill"`
	MinLevel int    `json:"minLevel"` // M5.1：所需技能最低等级
}

// Snapshot 返回带锁的经济世界快照。
// 性能优化：round 未变化时直接返回缓存，避免前端高频轮询时每次都全量重建。
func (w *World) Snapshot() *PublicSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.snapshotLocked()
}

// snapshotLocked 无锁构建快照（带缓存：状态版本号未变则复用）。
func (w *World) snapshotLocked() *PublicSnapshot {
	if w.snapCache != nil && w.snapVer == w.version {
		return w.snapCache
	}
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
			Skills: a.Skills,
			Reputation: a.Reputation, CompletedContracts: a.CompletedContracts,
			FailedContracts: a.FailedContracts, SuccessRate: a.SuccessRate(),
		})
	}
	for _, j := range w.Jobs {
		if j.Status == "open" {
			snap.OpenJobs = append(snap.OpenJobs, JobPublic{ID: j.ID, Title: j.Title, Reward: j.Reward, Skill: j.Skill, MinLevel: j.MinLevel})
		}
	}
	if len(w.TxLog) > 15 {
		snap.RecentTx = w.TxLog[len(w.TxLog)-15:]
	} else {
		snap.RecentTx = w.TxLog
	}
	snap.SkillMarket = w.skillMarketLocked()
	if len(w.SkillBuys) > 40 {
		snap.SkillBuys = w.SkillBuys[len(w.SkillBuys)-40:]
	} else {
		snap.SkillBuys = w.SkillBuys
	}
	// M6.1 Labor Market：服务市场 + 合约（最近 40 条）+ 累计统计（清晰口径）
	snap.Services = w.laborMarketLocked()
	stat := ContractStats{}
	start := 0
	if len(w.Contracts) > 40 {
		start = len(w.Contracts) - 40
	}
	for _, ct := range w.Contracts[start:] {
		snap.Contracts = append(snap.Contracts, ContractView{
			ID: ct.ID, Employer: ct.Employer, Worker: ct.Worker, Service: ct.Service,
			Price: ct.Price, Status: ct.Status,
			Duration: int64(ct.ReadyAt.Sub(ct.StartedAt).Seconds()),
			CreatedAt: ct.CreatedAt.UnixMilli(),
		})
	}
	// 累计统计基于全部合约（口径：累计值）
	for _, ct := range w.Contracts {
		stat.Total++
		switch ct.Status {
		case "completed":
			stat.Completed++
			stat.TotalVolume += ct.Price
			stat.MoneyMoved += ct.Price
		case "failed":
			stat.Failed++
		case "pending":
			stat.Pending++
		}
	}
	snap.ContractStats = stat
	// 缓存本次快照：版本号未变时的重复请求直接复用
	w.snapCache = snap
	w.snapVer = w.version
	return snap
}

// touchVersionLocked 递增状态版本号，使快照缓存失效（调用方需持锁）。
// 所有会改变快照可见状态的写操作都应调用它。
func (w *World) touchVersionLocked() {
	w.version++
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
		"skills": a.Skills,
		// M5 技能投资指标
		"skillInvested": a.SkillInvested, "skillEarned": a.SkillEarned,
		"skillReturn": a.SkillEarned - a.SkillInvested,
		// M6.3 职业信誉
		"reputation": a.Reputation, "completedContracts": a.CompletedContracts,
		"failedContracts": a.FailedContracts, "successRate": a.SuccessRate(),
	}
}

// Observatory 返回事件总线。
func (w *World) Observatory() *goose.Observatory { return w.obs }

// JobSkill 返回某工作所需的技能（Executor 映射工具用）。
func (w *World) JobSkill(jobID int64) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, j := range w.Jobs {
		if j.ID == jobID {
			return j.Skill
		}
	}
	return ""
}

// PublishSkillUsed 发布技能使用事件（M7：Timeline 显示 Agent 用了哪个技能）。
func (w *World) PublishSkillUsed(agentID int64, tool, result string) {
	name := ""
	if a := w.Agent(agentID); a != nil {
		name = a.Name
	}
	w.obs.Publish("agent.skill.used", map[string]interface{}{
		"agent": agentID, "name": name, "tool": tool, "result": result,
	})
}

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
		return a.SkillLevel(skill)
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
