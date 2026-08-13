package economy

import (
	"testing"
	"time"

	"agentworld/internal/skill"
	"agentworld/worlds/goosegame/goose"
)

// setupLaborWorld 构建一个带雇工场景的世界：
//   - 雇主 bob 没有 engineer 技能
//   - worker alice 有 engineer Lv5
//   - 提供 repair_machine 服务（价格 20，M6.2.1 有执行耗时）
func setupLaborWorld(t *testing.T) *World {
	w := &World{
		obs: goose.NewObservatory(goose.ObservOpts{}),
		Agents: map[int64]*Agent{
			1: {ID: 1, Name: "Bob", Balance: 100, Skills: []skill.AgentSkill{{SkillID: "courier", Level: 3}}},
			2: {ID: 2, Name: "Alice", Balance: 50, Skills: []skill.AgentSkill{{SkillID: "engineer", Level: 5}}},
		},
		Services: map[string]*Service{
			"repair_machine": {ID: "repair_machine", Name: "Repair Machine", Skill: "engineer", MinLevel: 1, Price: 20, Duration: 30 * time.Second},
		},
		Contracts: []*Contract{},
	}
	return w
}

// TestHireAgentEscrow 验证雇佣的核心：Escrow 扣款 + 合约创建 + 进入 working（M6.2.1）。
// M6.4：扣款 = worker 独立报价（动态），非固定 20。
func TestHireAgentEscrow(t *testing.T) {
	w := setupLaborWorld(t)
	contractID, ok := w.HireAgent(1, 2, "repair_machine")
	if !ok {
		t.Fatal("hire should succeed")
	}
	// M6.4：扣款 = Alice 的独立报价（动态）
	quote := w.workerQuoteLocked(w.Services["repair_machine"], w.Agents[2])
	if w.Agents[1].Balance != 100-quote {
		t.Errorf("Bob balance should be %d, got %d", 100-quote, w.Agents[1].Balance)
	}
	// M6.2.1：合约进入 working（不再立即完成），Escrow = 报价
	ct := w.Contracts[0]
	if ct.Escrow != quote || ct.Status != "working" {
		t.Errorf("contract escrow=%d status=%s (want working, escrow=%d)", ct.Escrow, ct.Status, quote)
	}
	// worker Alice 忙碌到 ReadyAt（不能立刻接新单）
	if !time.Now().Before(w.Agents[2].BusyUntil) {
		t.Error("worker Alice should be busy after hire")
	}
	// 反例：余额不足不能雇
	w.Agents[1].Balance = 5
	if _, ok := w.HireAgent(1, 2, "repair_machine"); ok {
		t.Error("should not hire when employer cannot afford")
	}
	_ = contractID
}

// TestHireRejectsNoSkill 验证 worker 没有对应技能时无法被雇。
func TestHireRejectsNoSkill(t *testing.T) {
	w := setupLaborWorld(t)
	// Alice 没有 miner 技能，雇佣 mine_ore 应失败
	if _, ok := w.HireAgent(1, 2, "mine_ore"); ok {
		t.Error("should not hire worker without the skill")
	}
}

// TestSettleContractsEscrow 验证合约到期结算后 Escrow 正确流向 + 资金守恒。
// M6.2.1：到期前不结算（未到 ReadyAt），到期后 SettleContracts 结算为 completed/failed。
func TestSettleContractsEscrow(t *testing.T) {
	w := setupLaborWorld(t)
	contractID, ok := w.HireAgent(1, 2, "repair_machine")
	if !ok {
		t.Fatal("hire should succeed")
	}
	ct := w.Contracts[0]
	quote := ct.Escrow // M6.4 动态报价
	// 到期前：不结算（仍 working）
	w.SettleContracts(ct.CreatedAt) // now = 创建时刻 < ReadyAt
	if ct.Status != "working" {
		t.Fatalf("contract should still be working before ready, got %s", ct.Status)
	}
	// 到期后：结算
	w.SettleContracts(ct.ReadyAt.Add(time.Millisecond))
	if ct.Status != "completed" && ct.Status != "failed" {
		t.Fatalf("contract should be completed or failed after settle, got %s", ct.Status)
	}
	// 资金守恒：Bob 100 → Alice 50，世界总资金 150 不变
	total := w.Agents[1].Balance + w.Agents[2].Balance
	if total != 150 {
		t.Errorf("total wealth should stay 150, got %d", total)
	}
	if ct.Status == "completed" && w.Agents[2].Balance != 50+quote {
		t.Errorf("Alice should get %d on success, got %d", quote, w.Agents[2].Balance)
	}
	if ct.Status == "failed" && w.Agents[1].Balance != 100 {
		t.Errorf("Bob should get full refund on failure, got %d", w.Agents[1].Balance)
	}
	_ = contractID
}

// TestWorkerBusyCooldown 验证 M6.2.1 行动冷却：worker 忙时不能接新单，忙完可以。
func TestWorkerBusyCooldown(t *testing.T) {
	w := setupLaborWorld(t)
	_, ok := w.HireAgent(1, 2, "repair_machine")
	if !ok {
		t.Fatal("hire should succeed")
	}
	// worker Alice 忙，雇她做第二个单子应失败（busy）
	if _, ok := w.HireAgent(1, 2, "repair_machine"); ok {
		t.Error("should not hire busy worker")
	}
	// 手动让 Alice 空闲，再雇应成功
	w.Agents[2].BusyUntil = time.Now().Add(-time.Second)
	if _, ok := w.HireAgent(1, 2, "repair_machine"); !ok {
		t.Error("should hire worker after idle")
	}
}

// TestLaborMarketView 验证劳动力市场快照包含服务 + 可用 worker 数。
func TestLaborMarketView(t *testing.T) {
	w := setupLaborWorld(t)
	market := w.LaborMarket()
	found := false
	for _, s := range market {
		if s.ID == "repair_machine" {
			found = true
			if s.Price != 20 || s.AvailableWorkers != 1 {
				t.Errorf("repair_machine offer wrong: %+v", s)
			}
		}
	}
	if !found {
		t.Error("repair_machine should be in labor market")
	}
}

// TestReputation 验证 M6.3 声誉由合同结果驱动（成功+1/失败-2），且独立于技能等级。
func TestReputation(t *testing.T) {
	a := &Agent{Skills: []skill.AgentSkill{{SkillID: "engineer", Level: 7}}}
	// Lv7 新手：声誉 0（技能等级 ≠ 声誉）
	if a.Reputation != 0 {
		t.Errorf("new Lv7 should start with reputation 0, got %d", a.Reputation)
	}
	// 多次成功 → 声誉上升
	for i := 0; i < 10; i++ {
		a.OnContractSettled(true)
	}
	if a.Reputation != 10 || a.CompletedContracts != 10 {
		t.Errorf("after 10 success: rep=%d completed=%d", a.Reputation, a.CompletedContracts)
	}
	// 失败 → 声誉下降
	a.OnContractSettled(false)
	if a.Reputation != 8 || a.FailedContracts != 1 {
		t.Errorf("after 1 fail: rep=%d failed=%d", a.Reputation, a.FailedContracts)
	}
	// 成功率
	if a.SuccessRate() != 10.0/11.0 {
		t.Errorf("success rate should be 10/11, got %v", a.SuccessRate())
	}
	// 声誉钳制 0~100
	a.Reputation = 99
	a.OnContractSettled(true)
	if a.Reputation > 100 {
		t.Errorf("reputation should cap at 100, got %d", a.Reputation)
	}
}

// TestWorkerCompetition 验证 M6.3 劳动力市场 worker 按成功率/声誉排序（可靠性优先）。
func TestWorkerCompetition(t *testing.T) {
	w := &World{
		Services: map[string]*Service{
			"repair_machine": {ID: "repair_machine", Name: "Repair Machine", Skill: "engineer", MinLevel: 1, Price: 20},
		},
		Agents: map[int64]*Agent{
			1: {ID: 1, Name: "BobLv1", Skills: []skill.AgentSkill{{SkillID: "engineer", Level: 1}}, Reputation: 30, FailedContracts: 10, CompletedContracts: 5},
			2: {ID: 2, Name: "AliceLv7", Skills: []skill.AgentSkill{{SkillID: "engineer", Level: 7}}, Reputation: 90, CompletedContracts: 40, FailedContracts: 1},
			3: {ID: 3, Name: "CharlieLv5", Skills: []skill.AgentSkill{{SkillID: "engineer", Level: 5}}, Reputation: 70, CompletedContracts: 20, FailedContracts: 3},
		},
	}
	// laborMarketLocked 返回的服务里，workers 应按成功率降序排列
	market := w.LaborMarket()
	for _, svc := range market {
		if svc.ID != "repair_machine" {
			continue
		}
		if len(svc.Workers) != 3 {
			t.Fatalf("expected 3 workers, got %d", len(svc.Workers))
		}
		// Alice Lv7 成功率最高，应排第一
		if svc.Workers[0].Name != "AliceLv7" {
			t.Errorf("most reliable worker (Alice) should rank first, got %s", svc.Workers[0].Name)
		}
		// Bob Lv1 成功率最低，应排最后
		if svc.Workers[len(svc.Workers)-1].Name != "BobLv1" {
			t.Errorf("least reliable worker (Bob) should rank last, got %s", svc.Workers[len(svc.Workers)-1].Name)
		}
	}
}

// TestDynamicPricing 验证 M6.4：worker 独立报价随等级/声誉分化（高技能高声誉报价更高）。
func TestDynamicPricing(t *testing.T) {
	svc := &Service{ID: "repair_machine", Name: "Repair Machine", Skill: "engineer", MinLevel: 1, Price: 20}
	w := &World{
		Services: map[string]*Service{svc.ID: svc},
	}
	// 同一服务，不同 worker：低等级低声誉 vs 高等级高声誉
	lowWorker := &Agent{ID: 1, Skills: []skill.AgentSkill{{SkillID: "engineer", Level: 1}}, Reputation: 20}
	highWorker := &Agent{ID: 2, Skills: []skill.AgentSkill{{SkillID: "engineer", Level: 7}}, Reputation: 95}
	// 需求高、worker 少（供不应求）
	low := w.workerQuote(svc, lowWorker, 500, 2)
	high := w.workerQuote(svc, highWorker, 500, 2)
	if low >= high {
		t.Errorf("high-level/reputable worker should cost more: low=%d high=%d", low, high)
	}
	// 高声誉高等级 worker 报价应明显高于基础价
	if high <= 20 {
		t.Errorf("high-level worker quote should exceed base price, got %d", high)
	}
	// 供过于求时报价下降（worker 多、需求低）
	crowded := w.workerQuote(svc, highWorker, 5, 20)
	if crowded >= high {
		t.Errorf("oversupply should lower quote: crowded=%d high=%d", crowded, high)
	}
}

// TestSkillSuccessRate 验证成功率随技能等级提高（M6.2.1）。
func TestSkillSuccessRate(t *testing.T) {
	if SkillSuccessRate(1) >= SkillSuccessRate(7) {
		t.Error("Lv7 should be more reliable than Lv1")
	}
	// Lv7 接近 97%
	if SkillSuccessRate(7) < 0.9 {
		t.Errorf("Lv7 success rate should be high, got %v", SkillSuccessRate(7))
	}
}
