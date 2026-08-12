// capabilities.go —— Economy World 的工具能力（M7 Skill System）。
//
// 第一版工具用本地模拟后端（mockBackend），返回 {success, reward}。
// 但"调用链路"是真实的：Skill → Tool Filter → Planner → rt.CallTool → Backend → 结果。
// 这样一次验证完整链路；以后把 mockBackend 换成真实 MCP/HTTP 后端即可，接口不变。
package economy

import (
	"fmt"

	"agentworld/internal/capability"
)

// mockBackend 实现 capability.Backend 接口：返回固定结果（本地模拟工具）。
// 按工具名区分奖励（repair 30 / deliver 15 / research 25），返回可解析的 JSON。
type mockBackend struct {
	toolName string
}

// Execute 模拟工具执行：返回 {success:true, reward:N}。
func (m *mockBackend) Execute(args map[string]interface{}) (string, error) {
	reward := int64(0)
	switch m.toolName {
	case "repair_machine":
		reward = 30
	case "deliver_package":
		reward = 15
	case "collect_data", "research_data":
		reward = 25
	case "harvest_crops":
		reward = 20
	case "medical_treatment":
		reward = 50
	case "mine_ore":
		reward = 35
	case "cook_meal":
		reward = 14
	case "buy_skill":
		// M5：技能投资动作返回确认（实际技能获取在 World.BuySkill 完成）
		if sk, _ := args["skill"].(string); sk != "" {
			return fmt.Sprintf(`{"success":true,"action":"buy_skill","skill":"%s"}`, sk), nil
		}
		return `{"success":true,"action":"buy_skill"}`, nil
	}
	return fmt.Sprintf(`{"success":true,"reward":%d}`, reward), nil
}

// economyTools 定义经济世界所有可用的工具（Skill → Tools 映射的目标）。
var economyTools = []struct {
	name    string
	desc    string
	tool    string
	skillID string // 该工具属于哪个技能
}{
	{"repair_machine", "维修一台机器", "repair_machine", "engineer"},
	{"query_machine", "查询机器状态", "query_machine", "engineer"},
	{"harvest_crops", "收获庄稼", "harvest_crops", "farmer"},
	{"buy_item", "购买商品", "buy_item", "trader"},
	{"sell_item", "卖出商品", "sell_item", "trader"},
	{"deliver_package", "配送包裹", "deliver_package", "courier"},
	{"collect_data", "采集数据", "collect_data", "courier"},
	{"medical_treatment", "医疗救治", "medical_treatment", "doctor"},
	{"mine_ore", "开采矿石", "mine_ore", "miner"},
	{"cook_meal", "烹饪餐食", "cook_meal", "chef"},
	{"buy_skill", "在技能市场购买一门新技能（M5 Skill Economy）", "buy_skill", ""},
}

// BuildCapability 构造经济世界的能力（含全部工具，供 main.go 注册到 Runtime）。
func BuildCapability() *capability.Capability {
	caps := &capability.Capability{
		Name: "economy_machine",
		Desc: "经济世界工具：维修 / 送货 / 研究 / 交易等",
	}
	for _, t := range economyTools {
		caps.Tools = append(caps.Tools, &capability.Tool{
			Name:        t.tool,
			Description: t.desc,
			Parameters:  []capability.Parameter{{Name: "target", Type: "string", Required: false}},
			Backend:     &mockBackend{toolName: t.tool},
			Hints:       map[string]bool{},
		})
	}
	return caps
}

// SkillToTool 把工作技能映射到对应的 MCP 工具名（Executor 调用用）。
func SkillToTool(skillID string) string {
	for _, t := range economyTools {
		if t.skillID == skillID {
			return t.tool
		}
	}
	return skillID
}
