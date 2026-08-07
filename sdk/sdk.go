// Package sdk —— M10：AgentWorld Module SDK（对外可扩展接口）。
//
// 目标：让第三方通过 `import "agentworld/sdk"` 就能注册自己的世界/场景，
// 无需接触 internal/* 内部实现。SDK 定义了一套"公共契约"（接口 + 轻量数据模型），
// 运行时（Runtime）负责调度与编排，具体"感知-决策-执行"逻辑由第三方 Module 承载。
//
// 用法：
//
//	type MyWorld struct{ ... }
//	func (m *MyWorld) Name() string { return "myworld" }
//	func (m *MyWorld) Perceive(ctx, a sdk.Agent) (sdk.Perception, error) { ... }
//	func (m *MyWorld) Planner() sdk.Planner { return myPlanner{} }
//	func (m *MyWorld) Executor() sdk.Executor { return myExecutor{} }
//	func (m *MyWorld) WakePolicy() sdk.WakePolicy { return sdk.NewEventWakePolicy() }
//	func (m *MyWorld) OnBoot(rt sdk.Runtime) error { ... }
//
//	func main() {
//	    sdk.RegisterModule(&MyWorld{})
//	    // 交给运行时启动
//	}
//
// 依赖方向：sdk 只依赖外部公共类型，不依赖 internal/*；internal/agent 通过适配器
// 将 sdk.Module 桥接到内部 Runtime（单向依赖，无循环）。
package sdk

import (
	"context"
	"time"
)

// Agent 一个可被调度的智能体（SDK 视角的轻量视图）。
// 对应内部 models.Agent，字段按需裁剪；若要访问完整字段可用 Extra 透传。
type Agent struct {
	ID          int64
	Name        string
	World       string
	Kind        string // agent / human
	SystemPrompt string
	Goal        string
	UseLLM      bool
	Interests   string
	// Extra 运行时透传的完整内部对象（类型由具体运行时定义），可选。
	Extra interface{}
}

// Decision 一次决策（SDK 视角）。
// 对应内部 llm.Decision。Action 的具体语义由第三方 Module 的 Executor 解释。
type Decision struct {
	Action     string
	Target     int64
	TargetKind string
	Content    string
	Reason     string
	Memory     string
	MemoryType string
	Importance int
	ToolArgs   map[string]interface{}
}

// Perception 某个 Agent 在一轮 Think 中所感知的世界视图。
// 具体场景可自由扩展（用自定义结构体，类型断言取回），框架层只透传不解析。
type Perception interface{}

// Module 一个场景/世界的所有可插拔行为收敛到一个 Module。
// 这是 SDK 的核心接口，第三方实现它即可注册自己的世界。
type Module interface {
	// Name 场景名，用于日志与可观测性。
	Name() string
	// Perceive 为某个 Agent 构建感知上下文。
	Perceive(ctx context.Context, a Agent) (Perception, error)
	// Planner 返回决策器（把感知转换为动作）。
	Planner() Planner
	// Executor 把决策落地到共享世界。
	Executor() Executor
	// WakePolicy 决定哪些 Agent 应该被唤醒。
	WakePolicy() WakePolicy
	// OnBoot 可选钩子：世界启动时执行一次。
	OnBoot(rt Runtime) error
}

// Planner 把感知转换为决策（大脑，可替换）。
type Planner interface {
	Decide(ctx context.Context, a Agent, p Perception) (*Decision, error)
}

// Executor 把决策落到共享世界（手脚，可替换）。
type Executor interface {
	Execute(ctx context.Context, rt Runtime, a Agent, p Perception, dec *Decision) (string, error)
}

// WakePolicy 激活策略：决定一次 tick 中哪些 Agent 被唤醒。
type WakePolicy interface {
	// Select 从候选 Agent 中选出本轮应该唤醒的列表。
	Select(ctx context.Context, rt Runtime, triggered, all []Agent) []Agent
}

// StateDelta 一次状态变化（M5/M7）。字段可选（0 表示不变）。
type StateDelta struct {
	Mood       int
	Energy     int
	Curiosity  int
	SocialNeed int
	NeedSocial        int
	NeedKnowledge     int
	NeedAchievement   int
	NeedEntertainment int
	Attention  *string
	Var        map[string]interface{}
}

// Message 一条 Agent 间消息（A2A，Intent 驱动）。
// 设计原则：不直连 AgentID 的"聊天"，而是"意图 + 载荷"。接收方是 To（可选），
// 也可用 Discover 按能力寻址。状态机：pending → accepted / rejected → done。
type Message struct {
	ID            int64                  `json:"id"`
	From          int64                  `json:"from"`   // 发送方 AgentID
	To            int64                  `json:"to"`     // 接收方 AgentID（0=广播/能力寻址）
	Intent        string                 `json:"intent"` // 意图，如 "travel.recommend" / "request_travel_plan"
	Payload       map[string]interface{} `json:"payload,omitempty"`
	Status        string                 `json:"status"` // pending / accepted / rejected / done
	ReplyTo       int64                  `json:"reply_to,omitempty"`       // 回信：指向被回复的请求消息 ID
	CorrelationID string                 `json:"correlation_id,omitempty"` // 业务级关联键：回信与请求共享，跨实例透传
	CreatedAt     time.Time              `json:"created_at"`
}

// 消息状态常量。
const (
	MsgStatusPending  = "pending"  // 待处理
	MsgStatusAccepted = "accepted" // 已接受
	MsgStatusRejected = "rejected" // 已拒绝
	MsgStatusDone     = "done"     // 已完成
)

// Event 一条世界事件（框架通用，非业务对象）。
// 对应内部 world.Event；sdk 定义等价结构以保持"世界无关"。
type Event struct {
	Type      string // 事件类型：weather / hotspot / market / time ...
	Title     string
	Detail    string
	TargetTag string // 目标标签（可选）
	CreatedAt time.Time
}

// CapabilityInfo 一个已注册能力的描述（供 prompt 提示，非业务对象）。
type CapabilityInfo struct {
	Name  string // 能力名，如 "pms" / "weather"
	Desc  string
	Tools []ToolInfo
}

// ToolInfo 一个工具的描述。
type ToolInfo struct {
	Name        string
	Description string
	Parameters  []ParamInfo
}

// ParamInfo 一个参数的定义。
type ParamInfo struct {
	Name        string
	Type        string
	Description string
	Required    bool
	Default     interface{}
}

// AgentRef 一个可寻址的候选 Agent（能力发现的结果，含选择评分）。
type AgentRef struct {
	AgentID      int64   // 候选 AgentID
	Name         string  // 名字
	World        string  // 所在世界
	Skill        string  // 匹配到的能力
	Score        float64 // 能力匹配分（Registry 基础分）
	Fitness      float64 // 综合适应度（能力 + 历史成功率 + 关系 + 负载），M12.3
	Relationship string  // 请求方对该 Agent 的关系（friend/frequent_discuss/...），空=无
	SuccessRate  float64 // 历史合作成功率（0~1），M12.3
}

// RemoteRef 一个"远端"可寻址的 Agent（Federation 能力发现的结果，M12.4）。
// 与 AgentRef 类似，但携带 Endpoint——目标实例的地址，用于跨 Runtime 寻址。
type RemoteRef struct {
	Endpoint string // 目标实例 HTTP 地址，如 "http://127.0.0.1:18081"
	World    string // 目标世界（实例）名
	AgentID  int64  // 目标世界内的 AgentID
	Name     string // 远端 Agent 名（可观测用）
	Skill    string // 匹配到的能力
}

// RemoteMessage 一条跨实例（Federation）A2A 消息（M12.4）。
// 语义与 Message 一致（intent 驱动 + payload），但发送方需带 world 信息，
// 因为远端需要知道"消息来自哪个世界的哪个 Agent"才能回信。
type RemoteMessage struct {
	Intent        string                 // 意图，如 "hotel.booking.v1"
	Payload       map[string]interface{} // 载荷
	From          RemoteFrom             // 发送方（本世界 + 本 Agent）
	ReplyTo       int64                  // 回信时指向被回复的请求消息 ID（跨实例异步协作续接）
	CorrelationID string                 // 业务级关联键：回信与请求共享，跨实例透传
}

// RemoteFrom RemoteMessage 的发送方标识。
type RemoteFrom struct {
	World string // 发送方所在世界（实例）名
	Agent int64  // 发送方 AgentID
}
