package economy

import (
	"strings"
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

// withSkillMarket 给世界挂一个技能市场（含价格）。
func (w *World) withSkillMarket() *World {
	reg := skill.NewRegistry()
	reg.Register(&skill.Skill{ID: "engineer", Name: "Engineer", Description: "工程", BasePrice: 100})
	reg.Register(&skill.Skill{ID: "courier", Name: "Courier", Description: "物流", BasePrice: 40})
	reg.Register(&skill.Skill{ID: "farmer", Name: "Farmer", Description: "农业", BasePrice: 50})
	w.skills = reg
	return w
}

// TestHumanBuySkillWithMarket 验证 M7：Human 用 100 初始余额买技能（带市场）。
// 修复：区分"已拥有/余额不足/无此技能"。
func TestHumanBuySkillWithMarket(t *testing.T) {
	w := newHumanWorld().withSkillMarket()
	id, _, ok := w.RegisterHuman("Ouyang", "secret")
	if !ok {
		t.Fatal("register fail")
	}
	// 买 courier（已拥有 → 应提示已拥有，不扣钱）
	ok, msg := w.HumanBuySkill(id, "courier")
	if ok {
		t.Error("should not buy already-owned courier")
	}
	if !stringsContains(msg, "已经拥有") {
		t.Errorf("expected already-owned msg, got: %s", msg)
	}
	// 买 engineer（100，初始 100 够）
	ok, msg = w.HumanBuySkill(id, "engineer")
	if !ok {
		t.Errorf("should buy engineer with 100 balance, got: %s", msg)
	}
	if w.Agents[id].Balance != 0 {
		t.Errorf("balance should be 0 after buying engineer(100), got %d", w.Agents[id].Balance)
	}
	if w.Agents[id].SkillLevel("engineer") != 1 {
		t.Errorf("should have engineer Lv1, got %d", w.Agents[id].SkillLevel("engineer"))
	}
	// 买 farmer（余额 0 → 应提示余额不足）
	ok, msg = w.HumanBuySkill(id, "farmer")
	if ok {
		t.Error("should not buy farmer with 0 balance")
	}
	if !stringsContains(msg, "余额不足") {
		t.Errorf("expected insufficient-balance msg, got: %s", msg)
	}
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && indexOfStr(s, sub) >= 0
}
func indexOfStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
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
	// 场景 A：开放工作（open，无人认领）—— 用户真实场景，Human 应能 claim+do
	w.Jobs = []*Job{{ID: 10, Title: "Deliver Package", Reward: 10, Skill: "courier", MinLevel: 1, Status: "open", ClaimedBy: 0}}
	reward, msg, success := w.HumanDoJob(id, 10)
	if !success {
		// 可能是成功率失败，但不应该返回"没有这份工作"
		if strings.Contains(msg, "没有这份工作") {
			t.Errorf("open job should be claim+do, not 'no job': %s", msg)
		}
	} else {
		if w.Agents[id].Balance != 100+reward {
			t.Errorf("human balance should increase by reward, got %d", w.Agents[id].Balance)
		}
	}
	// 场景 B：job 已 done（不存在）→ 返回"没有这份工作"
	w.Jobs = []*Job{{ID: 11, Title: "X", Reward: 5, Skill: "courier", MinLevel: 1, Status: "done", ClaimedBy: 0}}
	_, msg2, success2 := w.HumanDoJob(id, 11)
	if success2 {
		t.Errorf("done job should not be doable")
	}
	_ = msg2
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
