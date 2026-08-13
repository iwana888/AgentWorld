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
func TestHireAgentEscrow(t *testing.T) {
	w := setupLaborWorld(t)
	contractID, ok := w.HireAgent(1, 2, "repair_machine")
	if !ok {
		t.Fatal("hire should succeed")
	}
	// 雇主 Bob：100 - 20 = 80
	if w.Agents[1].Balance != 80 {
		t.Errorf("Bob balance should be 80, got %d", w.Agents[1].Balance)
	}
	// M6.2.1：合约进入 working（不再立即完成），Escrow = 20
	ct := w.Contracts[0]
	if ct.Escrow != 20 || ct.Status != "working" {
		t.Errorf("contract escrow=%d status=%s (want working)", ct.Escrow, ct.Status)
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
	if ct.Status == "completed" && w.Agents[2].Balance != 70 {
		t.Errorf("Alice should get 20 on success, got %d", w.Agents[2].Balance)
	}
	if ct.Status == "failed" && w.Agents[1].Balance != 100 {
		t.Errorf("Bob should get refund on failure, got %d", w.Agents[1].Balance)
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

// TestSkillSuccessRate 验证成功率随技能等级提高（M6.2.1）。
func TestSkillSuccessRate(t *testing.T) {
	if skillSuccessRate(1) >= skillSuccessRate(7) {
		t.Error("Lv7 should be more reliable than Lv1")
	}
	// Lv7 接近 97%
	if skillSuccessRate(7) < 0.9 {
		t.Errorf("Lv7 success rate should be high, got %v", skillSuccessRate(7))
	}
}
