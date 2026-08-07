package sdk

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Runtime 运行时上下文接口：暴露给第三方 Module 在 Executor / OnBoot 中使用的能力。
// 由内部运行时实现并注入。这样 Module 无需 import internal/*，只依赖 sdk 与 gorm。
type Runtime interface {
	// DB 返回底层数据库句柄，供 Module 读写自己的表。
	DB() *gorm.DB

	// SaveMemory 把一条记忆写库并裁剪到上限（importance>0 才有效）。
	SaveMemory(a Agent, dec *Decision)

	// ApplyStateDelta 应用一次状态变化（M5/M7），返回新状态（可选）。
	ApplyStateDelta(a Agent, d StateDelta) error

	// LoadState 读取 Agent 状态（含自然衰减）。返回值为具体运行时类型，可断言。
	LoadState(a Agent) (interface{}, error)

	// PublishEvent 向事件总线广播一条 Agent 行为，供前端实时监控。
	PublishEvent(e interface{})

	// CallTool 调用已注册能力下的一个工具（如 PMS 发卡、天气查询）。
	// capability: 能力名（如 "pms" / "weather"），tool: 工具名，args: 参数。
	// 返回人类可读结果文本。能力未注册/调用失败返回错误。
	CallTool(capability, tool string, args map[string]interface{}) (string, error)

	// CapabilityNames 返回已注册的能力名列表（供 Module 自查可用能力）。
	CapabilityNames() []string

	// UseLLM 判断该 Agent 是否应走真实 LLM（框架能力：LLM 已启用且该 Agent 允许）。
	// Module 据此决定用 LLM 决策还是规则/Mock 决策。
	UseLLM(a Agent) bool

	// GoalEnabled 是否启用 Agent 自主目标（Goal）驱动行为（框架配置）。
	GoalEnabled() bool

	// WorldEvents 返回最近 since 时间内的世界事件（框架通用，非业务对象）。
	WorldEvents(since time.Duration) []Event

	// Capabilities 返回已注册能力的描述（能力名 + 工具名 + 说明），
	// 供 Module 在 prompt 里告知 Agent 可用的外部工具。返回 nil 表示无能力。
	Capabilities() []CapabilityInfo

	// Send 发送一条 A2A 消息到目标 Agent 的 Inbox（异步）。
	// Message.ID 由运行时分配；Message.CreatedAt 由运行时填充。
	// 返回错误表示消息未被接受（如目标不存在/路由失败）。
	Send(msg Message) error

	// Inbox 返回某 Agent 的收件箱消息（按状态过滤，空=全部）。
	// 用于 Agent 在 Perceive 中读取别人发给它的消息，自主决定是否响应。
	Inbox(agentID int64, status string) []Message

	// MarkMessage 更新一条消息状态（accepted / rejected / done）。
	MarkMessage(id int64, status string) error

	// Discover 按能力（skill）发现候选 Agent（Agent Registry / 通讯录）。
	// 返回按评分降序的候选列表；没找到返回空切片。用于"找人"而非直连 AgentID。
	Discover(skill string) []AgentRef

	// Select 按能力 + 请求方视角做候选排序（Agent Selection，M12.3）。
	// from 是发起选择的 AgentID；fitness 综合能力匹配 + 历史成功率 + 关系 + 负载。
	// 返回按 Fitness 降序的候选；调用方可取第一个作为 BestAgent。
	Select(from int64, skill string) []AgentRef

	// SendRemote 发送一条跨实例（Federation）消息到远端 Agent 的 Inbox（M12.4）。
	// ref 描述远端目标（endpoint + world + agentID）；消息语义与 Send 一致（intent 驱动）。
	// 返回错误表示消息未被远端接受（如 endpoint 不可达 / 路由失败）。
	SendRemote(ctx context.Context, ref RemoteRef, msg RemoteMessage) error

	// DiscoverRemote 拉取并缓存远端实例的 Agent Manifest（分布式通讯录）。
	// 之后可用 RemoteAgents 在远端实例中按能力找 Agent。
	DiscoverRemote(ctx context.Context, endpoint string) error

	// RemoteAgents 在已发现远端通讯录中，按 skill 查找候选远端 Agent。
	// 返回空切片表示没找到或尚未发现任何远端。
	RemoteAgents(skill string) []RemoteRef
}

// ------------------- 模块注册表 -------------------

// 进程内已注册的 SDK 模块（由第三方 main() 调用 RegisterModule 填充）。
// 运行时启动时通过 LoadSDKModules 拉取，注册到调度器。
var (
	registered []Module
	lock       = &sync.Mutex{}
)

// RegisterModule 注册一个第三方世界模块。
// 通常在第三方程序的 main() 中调用；随后交由 AgentWorld 运行时启动。
func RegisterModule(m Module) {
	if m == nil {
		return
	}
	lock.Lock()
	defer lock.Unlock()
	for i, x := range registered {
		if x.Name() == m.Name() {
			registered[i] = m // 同名覆盖
			return
		}
	}
	registered = append(registered, m)
}

// LoadSDKModules 返回所有已注册的 SDK 模块（运行时启动时调用）。
func LoadSDKModules() []Module {
	lock.Lock()
	defer lock.Unlock()
	out := make([]Module, len(registered))
	copy(out, registered)
	return out
}

// RegisteredCount 已注册 SDK 模块数量（观测用）。
func RegisteredCount() int {
	lock.Lock()
	defer lock.Unlock()
	return len(registered)
}
