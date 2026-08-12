package economy

import (
	"testing"

	"agentworld/internal/skill"
	"agentworld/worlds/goosegame/goose"
)

// setupLaborWorld 构建一个带雇工场景的世界：
//   - 雇主 bob 没有 engineer 技能
//   - worker alice 有 engineer Lv5
//   - 提供 repair_machine 服务（价格 20）
func setupLaborWorld(t *testing.T) *World {
	w := &World{
		obs: goose.NewObservatory(goose.ObservOpts{}),
		Agents: map[int64]*Agent{
			1: {ID: 1, Name: "Bob", Balance: 100, Skills: []skill.AgentSkill{{SkillID: "courier", Level: 3}}},
			2: {ID: 2, Name: "Alice", Balance: 50, Skills: []skill.AgentSkill{{SkillID: "engineer", Level: 5}}},
		},
		Services: map[string]*Service{
			"repair_machine": {ID: "repair_machine", Name: "Repair Machine", Skill: "engineer", MinLevel: 1, Price: 20},
		},
		Contracts: []*Contract{},
	}
	return w
}

// TestHireAgentEscrow 验证雇佣的核心：Escrow 扣款 + 合约创建。
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
	// 合约创建，Escrow = 20
	ct := w.Contracts[0]
	if ct.Escrow != 20 || ct.Status != "pending" {
		t.Errorf("contract escrow=%d status=%s", ct.Escrow, ct.Status)
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

// TestExecuteContractEscrow 验证合约执行后 Escrow 正确流向。
// 强制 worker 技能成功（Lv5 有 0.3+0.5*5/7≈0.66 成功率，此处可能失败）。
// 我们验证状态流转而非随机结果：exec 后合约必为 completed 或 failed，且资金不丢失。
func TestExecuteContractEscrow(t *testing.T) {
	w := setupLaborWorld(t)
	contractID, ok := w.HireAgent(1, 2, "repair_machine")
	if !ok {
		t.Fatal("hire should succeed")
	}
	_, _ = w.ExecuteContract(contractID)
	ct := w.Contracts[0]
	if ct.Status != "completed" && ct.Status != "failed" {
		t.Fatalf("contract should be completed or failed, got %s", ct.Status)
	}
	// 资金守恒：Bob 100 → Alice 50，世界总资金 150 不变
	// (Bob -20) + (Alice +20 或 0，refund 给 Bob) = 世界总额不变
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
