package hotel

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"agentworld/internal/capability"
)

// MCPToolBackend M8.4：接入真实 PMS/门锁 MCP server 的工具后端。
//
// 对接真实工具（已通过 ListTools 探测确认）：
//   - send_room_key    发放房卡（办理入住/加同行卡/手机钥匙）→ cardType/keyKind/roomNumber/arrivalDate/departureDate
//   - cancel_room_key  注销房间所有房卡（退房/销卡）
//   - read_room_key    读取房卡状态
//   - rag_search       酒店知识库检索
//
// 调用方式与 test-agent 一致：Streamable HTTP + access_token header。
// 以后接真实 PMS / 门锁，只需替换 MCP URL。
type MCPToolBackend struct {
	mcp *capability.MCPBackend
}

// NewMCPToolBackend 创建基于 MCP 的工具后端。
// 鉴权：透传 access_token（来自 HOTEL_PMS_MCP_KEY 或 MCP_USER_API_KEY）。
// 连接时拉取工具列表验证连通性。
func NewMCPToolBackend(url, mode string) (*MCPToolBackend, error) {
	key := os.Getenv("HOTEL_PMS_MCP_KEY")
	if key == "" {
		key = os.Getenv("MCP_USER_API_KEY")
	}
	headers := map[string]string{}
	if key != "" {
		headers["access_token"] = key
	}
	mcp := capability.NewMCPBackendWithMode(url, headers, mode)
	tools, err := mcp.ListTools()
	if err != nil {
		return nil, fmt.Errorf("MCP 工具列表拉取失败: %w", err)
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	log.Printf("[hotel] 连接真实 PMS MCP: %s (%s)，工具: %v", url, mode, names)
	return &MCPToolBackend{mcp: mcp}, nil
}

// SendRoomKey 发放房卡（办理入住 / 加同行卡）。
//
//	roomNumber: 房间号，如 "101"
//	cardType: 1=新卡(主卡)/办理入住, 2=同行卡(副卡)
//	keyKind:  1=物理卡(默认), 2=手机钥匙
//	arrivalDate / departureDate: 格式 "YYYY-MM-DD HH:mm:ss"
func (b *MCPToolBackend) SendRoomKey(roomNumber string, cardType, keyKind int, arrivalDate, departureDate string, replacePrevious bool) (map[string]interface{}, bool) {
	args := map[string]interface{}{
		"_tool":           "send_room_key",
		"roomNumber":      roomNumber,
		"cardType":        cardType,
		"arrivalDate":     arrivalDate,
		"departureDate":   departureDate,
		"keyKind":         keyKind,
		"replacePrevious": replacePrevious,
	}
	return b.call(args)
}

// CancelRoomKey 注销房间所有房卡（退房 / 销卡）。
func (b *MCPToolBackend) CancelRoomKey(roomNumber, lockNumber string) (map[string]interface{}, bool) {
	args := map[string]interface{}{
		"_tool":       "cancel_room_key",
		"roomNumber":  roomNumber,
		"lockNumber":  lockNumber,
	}
	return b.call(args)
}

// ReadRoomKey 读取房间房卡状态。
func (b *MCPToolBackend) ReadRoomKey(roomNumber, lockNumber string) (map[string]interface{}, bool) {
	args := map[string]interface{}{
		"_tool":      "read_room_key",
		"roomNumber": roomNumber,
		"lockNumber": lockNumber,
	}
	return b.call(args)
}

// RagSearch 酒店知识库检索。
func (b *MCPToolBackend) RagSearch(query, hotelID string, topK int) (map[string]interface{}, bool) {
	args := map[string]interface{}{
		"_tool":   "rag_search",
		"query":   query,
	}
	if hotelID != "" {
		args["hotel_id"] = hotelID
	}
	if topK > 0 {
		args["top_k"] = topK
	}
	return b.call(args)
}

// call 调用 MCP 工具，返回解析后的 map。
func (b *MCPToolBackend) call(args map[string]interface{}) (map[string]interface{}, bool) {
	out, err := b.mcp.Execute(args)
	if err != nil {
		log.Printf("[hotel] MCP 调用失败: %v", err)
		return nil, false
	}
	// MCP 返回可能是 JSON 文本，尝试解析
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return map[string]interface{}{"message": out}, true
	}
	return m, true
}
