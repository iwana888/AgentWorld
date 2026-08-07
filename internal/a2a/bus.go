// Package a2a —— M12：Agent Communication Layer（ACL）。
//
// Agent 间消息总线（World Bus）。定位：内部神经系统，独立于 bus.Broker（SSE 前端推送）。
//
// 设计原则（Intent 驱动，非聊天）：
//   - Agent 不直连 AgentID 发"聊天"；它发送 Intent + Payload。
//   - 消息异步进入目标 Agent 的 Inbox，由 Agent 在 Perceive 中自行决定是否响应。
//   - 消息落库（agent_messages 表），状态机 pending → accepted / rejected → done。
//   - 支持按能力寻址（Discover）：To=0 时按 Intent 匹配有能力的 Agent。
//
// 一个 Agent 是"发件人 + 收件人"：通过 Send 发出，通过 Inbox 读取别人发给它的消息。
package a2a

import (
	"time"

	"agentworld/internal/db"
	"agentworld/internal/models"
	"agentworld/sdk"
	"gorm.io/gorm"
)

// Bus 是 A2A 消息总线的实现。它只依赖 db 落库，与 SSE bus 完全分离。
// M12.2：Bus 持有 Agent Registry，Send 时 To=0 走能力寻址（通讯录），不再广播全部。
type Bus struct {
	db       *gorm.DB
	registry *Registry
}

// NewBus 创建 A2A 消息总线（含 Agent Registry）。
func NewBus(d *gorm.DB) *Bus {
	return &Bus{db: d, registry: NewRegistry(d)}
}

// Registry 返回 Bus 持有的 Agent 能力注册表（供外部注册/查找能力）。
func (b *Bus) Registry() *Registry { return b.registry }

// Send 发送一条 A2A 消息到目标 Agent 的 Inbox。
// 若 msg.To == 0，则按 Intent 做能力寻址（Discover）：找到能处理该意图的 Agent，
// 若找到多个则广播到所有匹配者；找不到返回错误。
func (b *Bus) Send(msg sdk.Message) error {
	m := &models.AgentMessage{
		From:          msg.From,
		To:            msg.To,
		Intent:        msg.Intent,
		Payload:       msg.Payload,
		Status:        sdk.MsgStatusPending,
		ReplyTo:       msg.ReplyTo,
		CorrelationID: msg.CorrelationID,
		CreatedAt:     time.Now(),
	}
	// To=0：能力寻址（通讯录）
	if m.To == 0 {
		refs := b.registry.Find(msg.Intent)
		if len(refs) == 0 {
			return errNoMatch(msg.Intent)
		}
		// 只发给能力匹配的候选（M12.2：不再广播全部 Agent）。
		for _, ref := range refs {
			copy := *m
			copy.To = ref.AgentID
			if err := db.InsertMessage(b.db, &copy); err != nil {
				return err
			}
		}
		return nil
	}
	return db.InsertMessage(b.db, m)
}

// Inbox 返回某 Agent 的收件箱（status 空=全部）。
func (b *Bus) Inbox(agentID int64, status string) []sdk.Message {
	rows, _ := db.InboxFor(b.db, agentID, status)
	return toSDKMessages(rows)
}

// Mark 更新消息状态。
func (b *Bus) Mark(id int64, status string) error {
	return db.UpdateMessageStatus(b.db, id, status)
}

// Discover 按能力（Intent）寻址：返回能处理该意图的候选 Agent（含评分）。
// 供外部查询 Agent 通讯录（M12.2）。
func (b *Bus) Discover(intent string) []AgentRef {
	return b.registry.Find(intent)
}

// Select 从请求方视角做 Agent Selection（M12.3）：按 fitness 排序候选，可取其首位为 BestAgent。
func (b *Bus) Select(from int64, intent string) []AgentRef {
	return b.registry.Select(from, intent)
}

// AgentName 返回某 Agent 的名字（供 Manifest / 日志用）。查不到返回空。
func (b *Bus) AgentName(agentID int64) (string, error) {
	a, err := db.GetAgent(b.db, agentID)
	if err != nil {
		return "", err
	}
	return a.Name, nil
}

// toSDKMessages 把内部消息记录转为 sdk.Message。
func toSDKMessages(rows []models.AgentMessage) []sdk.Message {
	out := make([]sdk.Message, 0, len(rows))
	for _, r := range rows {
		out = append(out, sdk.Message{
			ID:            r.ID,
			From:          r.From,
			To:            r.To,
			Intent:        r.Intent,
			Payload:       r.Payload,
			Status:        r.Status,
			ReplyTo:       r.ReplyTo,
			CorrelationID: r.CorrelationID,
			CreatedAt:     r.CreatedAt,
		})
	}
	return out
}

// errNoMatch 没有 Agent 能处理该意图。
func errNoMatch(intent string) error {
	return &NoMatchError{Intent: intent}
}

// NoMatchError 找不到能处理该意图的 Agent。
type NoMatchError struct{ Intent string }

func (e *NoMatchError) Error() string {
	return "A2A: 没有 Agent 能处理意图 " + e.Intent
}
