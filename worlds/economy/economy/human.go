// human.go —— M7 Human Entrance：真实人类接入 Economy World。
//
// 架构原则：Human 也是 Agent（Kind=human），与 AI Agent 共享同一套
// Identity / Money / Skill / Economy / Contract / Reputation。
// 区别只有决策来源：AI 由 Runtime 自动 Think，Human 由真人通过 UI/API 决策。
// 因此 Human 的每个 Action（do_job / buy_skill / hire_agent）都直接复用
// World 现有的经济规则方法，绝不因为"是人"而绕过 Economy。
package economy

import (
	"crypto/rand"
	"encoding/hex"

	"agentworld/internal/skill"
)

// RegisterHuman M7：注册一个 Human Agent。
//   - Kind=human，不进入 Scheduler 自动唤醒（scheduler 已排除 human）
//   - 初始：Balance=100，基础技能 Courier Lv1（不让 Human 初始拥有高级技能）
//   - 返回 (agentID, token, 是否成功)
func (w *World) RegisterHuman(name, password string) (int64, string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if name == "" || password == "" {
		return 0, "", false
	}
	// 名字唯一
	for _, a := range w.Agents {
		if a.Kind == "human" && a.Name == name {
			return 0, "", false // 已存在同名 human
		}
	}
	id := w.nextAgent
	w.nextAgent++
	a := &Agent{
		ID:            id,
		Name:          name,
		Kind:          "human",
		Password:      password,
		Profession:    "Courier", // 初始基础职业
		Personality:   "人类",
		Goal:          "在 AgentWorld 中生活和赚钱",
		Balance:       100,
		Inventory:     map[string]int{},
		Skills:        []skill.AgentSkill{{SkillID: "courier", Level: 1}},
		Relationships: map[int64]float64{},
	}
	w.Agents[id] = a
	w.touchVersionLocked()
	w.obs.Publish("human.join", map[string]interface{}{"agent": id, "name": name})
	token := newToken()
	w.tokens[token] = id
	return id, token, true
}

// LoginHuman M7：Human 登录，验证密码并签发 token。
func (w *World) LoginHuman(name, password string) (int64, string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, a := range w.Agents {
		if a.Kind == "human" && a.Name == name && a.Password == password {
			token := newToken()
			w.tokens[token] = a.ID
			return a.ID, token, true
		}
	}
	return 0, "", false
}

// AuthHuman M7：校验 token，返回对应 human agentID。
func (w *World) AuthHuman(token string) (int64, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	id, ok := w.tokens[token]
	return id, ok
}

// HumanDoJob M7：Human 执行一份工作（完全复用 AI 的 DoJob，含技能检查/成功率/奖励/冷却）。
// 返回 (收入, 结果描述, 是否成功)。
func (w *World) HumanDoJob(agentID, jobID int64) (int64, string, bool) {
	reward, msg := w.DoJob(agentID, jobID)
	if reward > 0 {
		return reward, msg, true
	}
	return 0, msg, false
}

// HumanBuySkill M7：Human 购买技能（完全复用 AI 的 BuySkill，含余额检查/扣款/事件）。
func (w *World) HumanBuySkill(agentID int64, skillID string) (bool, string) {
	ok := w.BuySkill(agentID, skillID)
	if ok {
		return true, "购买了 " + skillID
	}
	return false, "购买失败（余额不足 / 已拥有 / 无此技能）"
}

// HumanHireAgent M7：Human 雇佣 AI（完全复用 HireAgent，含 Escrow/冷却/结算）。
// 返回 (合约ID, 是否成功, 说明)。
func (w *World) HumanHireAgent(agentID, worker int64, serviceID string) (int64, bool, string) {
	contractID, ok := w.HireAgent(agentID, worker, serviceID)
	if !ok {
		return 0, false, "雇佣失败（余额不足 / worker 忙 / 无此服务）"
	}
	return contractID, true, "已雇佣，托管 " + itoaInt64(w.ContractPrice(contractID)) + " coins"
}

// AgentOf 返回 Agent 指针的带锁只读副本（供 API 检查身份）。
func (w *World) AgentOf(agentID int64) *Agent {
	w.mu.Lock()
	defer w.mu.Unlock()
	if a, ok := w.Agents[agentID]; ok {
		cp := *a
		cp.Skills = append([]skill.AgentSkill{}, a.Skills...)
		return &cp
	}
	return nil
}

// newToken 生成随机 token（鉴权用）。
func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// itoaInt64 简易 int64 转字符串。
func itoaInt64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
