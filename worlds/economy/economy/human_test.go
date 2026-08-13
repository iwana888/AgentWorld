package economy

import (
	"testing"
	"time"

	"agentworld/internal/skill"
	"agentworld/worlds/goosegame/goose"
)

// newHumanWorld 构建一个含 1 个 AI 和人类基础设施的世界。
func newHumanWorld() *World {
	w := &World{
		obs:       goose.NewObservatory(goose.ObservOpts{}),
		Agents:    map[int64]*Agent{},
		tokens:    map[string]int64{},
		nextAgent: 1000000,
	}
	// 一个 AI 工程师（可被雇 / 可雇人）
	w.Agents[1] = &Agent{
		ID: 1, Name: "Alice", Kind: "ai", Profession: "Engineer",
		Balance: 200, Skills: []skill.AgentSkill{{SkillID: "engineer", Level: 5}},
	}
	return w
}

// TestHumanRegisterLogin 验证 M7：注册 / 登录 / 鉴权。
func TestHumanRegisterLogin(t *testing.T) {
	w := newHumanWorld()
	id, token, ok := w.RegisterHuman("Ouyang", "secret")
	if !ok {
		t.Fatal("register should succeed")
	}
	// Human Agent 属性
	a := w.Agents[id]
	if a.Kind != "human" || a.Balance != 100 {
		t.Errorf("human should be kind=human balance=100, got kind=%s bal=%d", a.Kind, a.Balance)
	}
	// 初始只有 Courier Lv1（不让人类初始有高级技能）
	if a.SkillLevel("courier") != 1 {
		t.Errorf("human should start with Courier Lv1")
	}
	if a.SkillLevel("engineer") != 0 {
		t.Errorf("human should NOT have engineer initially")
	}
	// 同名重复注册失败
	if _, _, ok := w.RegisterHuman("Ouyang", "x"); ok {
		t.Error("duplicate name should fail")
	}
	// token 鉴权
	if aid, ok := w.AuthHuman(token); !ok || aid != id {
		t.Errorf("token auth failed: id=%d ok=%v", aid, ok)
	}
	if _, ok := w.AuthHuman("invalid"); ok {
		t.Error("invalid token should fail")
	}
	// 登录
	if _, _, ok := w.LoginHuman("Ouyang", "wrong"); ok {
		t.Error("wrong password should fail")
	}
	if _, t2, ok := w.LoginHuman("Ouyang", "secret"); !ok || t2 == "" {
		t.Error("correct password should login")
	}
}

// TestHumanDoJob 验证 M7：Human 工作（复用 AI 的 DoJob 经济规则）。
func TestHumanDoJob(t *testing.T) {
	w := newHumanWorld()
	id, _, ok := w.RegisterHuman("Ouyang", "secret")
	if !ok {
		t.Fatal("register fail")
	}
	// 一份 courier 工作（Deliver Package Lv1）
	w.Jobs = []*Job{{ID: 10, Title: "Deliver Package", Reward: 10, Skill: "courier", MinLevel: 1, Status: "claimed", ClaimedBy: id}}
	// Human 执行工作（可能因成功率失败，但应走经济规则）
	reward, msg, success := w.HumanDoJob(id, 10)
	if success {
		if w.Agents[id].Balance != 100+reward {
			t.Errorf("human balance should increase by reward, got %d", w.Agents[id].Balance)
		}
	} else {
		_ = msg
	}
}

// TestHumanBuySkill 验证 M7：Human 买技能（复用 AI 的 BuySkill 含余额/扣款）。
func TestHumanBuySkill(t *testing.T) {
	w := newHumanWorld()
	id, _, ok := w.RegisterHuman("Ouyang", "secret")
	if !ok {
		t.Fatal("register fail")
	}
	// 无技能市场注册表 → 买技能失败（Human 不能绕过经济）
	if w.skills != nil {
		t.Fatal("setup should have nil skills for this test")
	}
	if ok, _ := w.HumanBuySkill(id, "engineer"); ok {
		t.Error("buy without market should fail")
	}
}

// TestAIHireHuman 验证 M7 最重要验收：AI 能雇 Human，Human 获得收入。
func TestAIHireHuman(t *testing.T) {
	w := newHumanWorld()
	humanID, _, ok := w.RegisterHuman("Ouyang", "secret")
	if !ok {
		t.Fatal("register fail")
	}
	// 服务市场：Courier 服务（Human 有 courier 技能）
	w.Services = map[string]*Service{
		"collect_data": {ID: "collect_data", Name: "Collect Data", Skill: "courier", MinLevel: 1, Price: 8, Duration: time.Second},
	}
	// AI(1) 雇 Human(humanID) 做 Collect Data
	contractID, hireOK := w.HireAgent(1, humanID, "collect_data")
	if !hireOK {
		t.Fatal("AI should be able to hire human")
	}
	ct := w.Contracts[0]
	// 到期结算 → Human 获得收入
	w.SettleContracts(ct.ReadyAt.Add(time.Millisecond))
	if ct.Status != "completed" && ct.Status != "failed" {
		t.Fatalf("contract should settle, got %s", ct.Status)
	}
	if ct.Status == "completed" {
		if w.Agents[humanID].Balance != 100+ct.Escrow {
			t.Errorf("human should earn on contract completion, got %d", w.Agents[humanID].Balance)
		}
		// Human 声誉增长
		if w.Agents[humanID].CompletedContracts != 1 {
			t.Errorf("human completed contracts should be 1, got %d", w.Agents[humanID].CompletedContracts)
		}
	}
	_ = contractID
}
