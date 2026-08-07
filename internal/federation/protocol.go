// Package federation —— M12.4：AgentWorld Federation（分布式 Agent Runtime Network）。
//
// 定位：把"单机 Runtime 内 Agent 通信（A2A）"升级为"跨 Runtime 的 Agent 通信（A2A + 网络）"。
// 类比 Docker → Kubernetes：单机容器编排 → 容器集群。AgentWorld：单 Runtime Agent 世界 →
// 多 Runtime Agent 世界网络。
//
// 设计原则：
//   - 不破坏现有 A2A（本地 Send/Inbox/Discover/Select 完全不变）。
//   - Federation 只新增"远端视角"：用 endpoint（实例地址）寻址远端 Agent，消息语义与本地一致
//     （intent + payload + 状态机），远端通过自己的 Bus 落库进 Inbox，复用 A2A 闭环。
//   - 能力发现靠 Agent Manifest：每个实例暴露 GET /.well-known/agent.json，
//     声明本世界的名称与各 Agent 的能力（skill）。这是"分布式通讯录"。
//
// 传输：当前以 HTTPS（走实例自身 HTTP server，挂 /api/federation/*）为主。
// 后续可替换为 WebSocket / gRPC，只需实现 Transport 接口。
package federation

// Manifest 一个实例对外暴露的 Agent Manifest（Agent 能力声明 / 分布式通讯录）。
// 对应 GET /.well-known/agent.json 的返回体。
type Manifest struct {
	// Name 本世界（实例）的名称，如 "Shanghai Hotel World"。
	Name string `json:"name"`
	// Runtime 运行时标识，当前恒为 "agentworld"。
	Runtime string `json:"runtime"`
	// Endpoint 本实例的对外地址（HTTP 基准 URL），供远端回发消息用。
	Endpoint string `json:"endpoint"`
	// Agents 本实例中可被远端寻址的 Agent 能力声明。
	Agents []ManifestAgent `json:"agents"`
}

// ManifestAgent Manifest 中一个 Agent 的能力声明。
type ManifestAgent struct {
	// ID 本实例内的 AgentID。
	ID int64 `json:"id"`
	// Name Agent 名（可观测用）。
	Name string `json:"name"`
	// Skills 该 Agent 可提供的能力（点分版本格式，如 "hotel.booking.v1"）。
	Skills []string `json:"skills"`
}

// RemoteAddr 一个"远端"寻址：目标实例 + 目标世界中的 Agent。
// 这是 Federation 的最小寻址单元——本实例的 Agent 需要知道"发往哪个世界、哪个 Agent"。
type RemoteAddr struct {
	// Endpoint 目标实例的 HTTP 基准地址，如 "http://127.0.0.1:18081"。
	// 空表示与发送方同实例（本地寻址，退化为现有 A2A）。
	Endpoint string `json:"endpoint"`
	// World 目标世界（实例）名，如 "shanghai-hotel"。
	World string `json:"world"`
	// AgentID 目标世界内的 AgentID。
	AgentID int64 `json:"agent"`
}

// RemoteMessage 跨实例传递的 A2A 消息信封（wire 格式）。
// 语义与 sdk.Message 一致（intent 驱动 + payload + 状态机），但 From 是复合的
// （world + agent），因为远端需要知道"消息来自哪个世界的哪个 Agent"以便回信。
type RemoteMessage struct {
	// Intent 意图，如 "hotel.booking.v1"。
	Intent string `json:"intent"`
	// From 发送方（远端视角：world + agent）。
	From FromRef `json:"from"`
	// To 接收方 AgentID（0 表示由接收实例按能力寻址）。
	To int64 `json:"to"`
	// Payload 载荷。
	Payload map[string]interface{} `json:"payload,omitempty"`
	// ReplyTo 回信时指向被回复的请求消息 ID（跨实例异步协作续接）。
	ReplyTo int64 `json:"reply_to,omitempty"`
	// CorrelationID 业务级关联键：回信与请求共享，跨实例透传。
	CorrelationID string `json:"correlation_id,omitempty"`
	// CreatedAt 发送方落的时间戳。
	CreatedAt string `json:"created_at,omitempty"`
}

// FromRef 远程消息的发送方标识。
type FromRef struct {
	// World 发送方所在世界（实例）名。
	World string `json:"world"`
	// Agent 发送方 AgentID。
	Agent int64 `json:"agent"`
}

// SendResult 远端发送的结果（transport 层返回）。
type SendResult struct {
	// Delivered 是否送达（远端实例已接收并落库）。
	Delivered bool `json:"delivered"`
	// MessageID 远端落库后的消息 ID（若可知）。
	MessageID int64 `json:"message_id,omitempty"`
	// Error 失败原因（delivered=false 时）。
	Error string `json:"error,omitempty"`
}
