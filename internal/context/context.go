// Package context 是 AgentWorld 的运行时基础设施（Context Runtime / M8）。
//
// 设计原则：
//   - ContextBlock 是一等公民。所有喂给 LLM 的内容都是 Block，绝不用
//     systemPrompt += xxx 这种 Prompt 拼接器写法——否则半年后又退化成 Prompt Builder。
//   - ContextEngine 是 Runtime 层，只产出 CompiledContext（结构化），不产出
//     LLM Provider 协议（[]Message）。Provider Adapter 负责把 CompiledContext
//     转成 OpenAI / DeepSeek / Claude / Local 各自需要的消息，避免核心架构被
//     OpenAI-compatible API 绑死。
//   - Intent 进入 Runtime。Compile 接收 DecisionIntent，Retriever / Planner 靠
//     Intent 知道"该找什么"，而不是把 Agent 的 Memory 全部倒进 Context。
//   - 第一版 Compiler 刻意"不聪明"：只做 计算 Token → 按 Budget 分类 → 输出。
//     不做自动排序 / 压缩 / RAG / 摘要 / 动态预算。先验证"Context 应该长什么样"。
//
// 第一阶段范围（M8.1~M8.5）：
//   M8.1  ContextBlock / AgentState / ContextBudget / CompiledContext
//   M8.2  Immutable / Semi-Stable / Dynamic 三层分离
//   M8.3  DecisionIntent
//   M8.4  DecisionContext
//   M8.5  ContextCompiler + TokenBudget（机械实现）
//
// 明确不做（后续里程碑）：Retriever(M8.6/7)、Compaction(M8.8)、
// Provider Cache(M8.9)、Token 成本进 Economy(M8.10)。
//
// ─────────────────────────────────────────────────────────────────────────────
// M8 API FREEZE（2026-08-17 架构审计后冻结）
//
// 以下接口在 M8 收口后冻结，禁止修改（属于"已完成的 M8 能力"，不是待开发项）：
//   - ContextBlock 结构体字段
//   - Compiler.Compile 主流程（分类 / 预算截断 / compactAndRecompile / strictTrim）
//   - Retriever 接口 + RetrieveRequest + MemoryStore + MemoryRow
//   - Compactor 接口 + ReducePolicy
//   - ContextAdapter 接口 + OpenAICompatibleAdapter 映射规则
//   - TokenUsage / TokenAccount / TokenLedger / AccountCompile 语义
//
// 冻结后【允许】：
//   - 实现已有接口（如 db 实现 MemoryStore、World 注入 MemoryRetriever）
//   - 实验代码 / 观测代码（1000-Think、Observatory 消费 CompiledContext）
//   - Bug fix（不改变上述语义）
//
// 冻结后【禁止】：
//   - 新增 Context 能力（不写 M8.11）
//   - 修改上述任何稳定接口签名 / 语义
//
// 理由：1000-Think 实验必须在稳定的 M8 API 上测量真实 Runtime 行为，
// 不能在实验过程中漂移接口定义。审计确认无 P0/P1 边界问题，故可冻结。
// ─────────────────────────────────────────────────────────────────────────────
package context

// BlockType 内容块的类型，决定它在三层模型中的位置与稳定性。
type BlockType string

const (
	// Immutable 层：运行时规则 + 世界规则 + 身份 + 性格 + 工具 schema。
	// 几乎不随单次决策变化，适合缓存（但本阶段不做 Cache）。
	TypeRuntimeRules  BlockType = "runtime_rules"  // AgentWorld 运行时规则
	TypeWorldRules    BlockType = "world_rules"    // 当前 World 的规则（如 Economy 规则）
	TypeAgentIdentity BlockType = "agent_identity" // Agent 的身份（名字/职业/ID）
	TypePersonality   BlockType = "personality"    // 性格
	TypeToolSchema    BlockType = "tool_schema"    // 工具 schema（技能隔离后可见的工具）

	// Semi-Stable 层：技能 / 能力 / 配置。可能变化（如 Economy 买技能），不可当 Immutable 缓存。
	TypeSkill    BlockType = "skill"    // Agent 拥有的技能
	TypeCapability BlockType = "capability" // 能力
	TypeConfig   BlockType = "config"   // 配置

	// Dynamic 层：随单次决策变化。
	TypeAgentState   BlockType = "agent_state"   // 余额/位置/目标/意图等运行时状态
	TypeWorldState   BlockType = "world_state"   // 世界状态（天气/时间/热度）
	TypeEvent        BlockType = "event"         // 近期世界/社会事件
	TypeRetrieved    BlockType = "retrieved"     // 检索到的 Memory（M8.7 才真正接入）
	TypeDecision     BlockType = "decision"      // Decision Context：候选行动（M8.4）
)

// Stability 三层模型：内容随决策变化的频率。
type Stability int

const (
	// Stable 几乎不变（Immutable + Semi-Stable），进 StableBlocks。
	Stable Stability = iota
	// Dynamic 随单次决策变化，进 DynamicBlocks。
	Dynamic
)

// Priority 预定义层级（Context Reduction Policy, M8.8）。
// Compaction 按此决策"谁可以被压缩/丢弃"：数值越大越不可动。
// 设计理念：AgentWorld 不会随意压缩 Agent 的记忆——Runtime 依据"决策价值"
// 决定可压缩对象。Stable/State/Decision 永不压缩；Retrieved/Old 优先压缩。
const (
	PriorityImmutableCore int = 100 // Immutable Core（rules/identity/personality/tool）：永不压缩
	PriorityCurrentState  int = 90  // 当前状态（Agent/World State）：永不压缩
	PriorityCurrentDecision int = 80 // 当前 Decision Context（候选行动）：永不压缩
	PriorityRecentEvent   int = 60  // 近期事件：尽量保留
	PriorityRetrieved     int = 40  // 检索记忆：可压缩
	PriorityOldEvent      int = 20  // 旧事件：优先压缩
	PriorityLowMemory     int = 10  // 低重要度记忆：最先压缩
)

// ContextBlock 是 Context 的一等公民。
// 每个 Block 知道自己是什么、从哪来、是否稳定、占多少 Token。
type ContextBlock struct {
	ID       string    `json:"id"`       // 唯一 id（如 "world.rules"），便于缓存命中判定
	Type     BlockType `json:"type"`     // 内容类型
	Source   string    `json:"source"`   // 来源（如 "world.rules" / "economy.candidates" / "agent.state"）
	Content  string    `json:"content"`  // 实际文本
	Priority int       `json:"priority"` // 优先级（数值大=越重要）；Budget 超限时先丢低优先级
	Stable   bool      `json:"stable"`   // 是否稳定（Immutable/Semi-Stable）
	Tokens   int       `json:"tokens"`   // 该 Block 的 Token 数（由 Compiler 估算/回填）
}

// AgentState Agent 的运行时状态（Dynamic 层核心）。
// 注意：这是 Runtime 视角的状态，与具体 World 解耦——World 负责填充。
type AgentState struct {
	AgentID string `json:"agent_id"`
	Balance int    `json:"balance"` // Token 是 Agent 的资源（M8.10 才进 Economy 成本）
	Location string `json:"location"`
	Goal     string `json:"goal"`
	Intent   string `json:"intent"` // 当前意图（与 DecisionIntent.Type 对应）
	// Extra 透传 World 特有的其它状态字段（如 Economy 的 WealthRank/Inventory）。
	Extra map[string]string `json:"extra,omitempty"`
}

// ContextBudget Token 预算（硬约束）。
// 各分类上限之和应 ≤ MaxTotal。Compiler 机械地按分类截断。
type ContextBudget struct {
	MaxTotal    int `json:"max_total"`    // 总上限
	System      int `json:"system"`       // Immutable 层（rules/identity/personality/tool）
	State       int `json:"state"`        // Agent/World State
	Retrieved   int `json:"retrieved"`    // 检索 Memory
	WorldEvent  int `json:"world_event"`  // 事件
	Decision    int `json:"decision"`     // Decision Context（候选行动）
	Reserved    int `json:"reserved"`     // 预留（输出/工具预留）
}

// DefaultBudget 返回一份默认预算（3000 总上限，分类上限之和 ≤ 3000）。
func DefaultBudget() ContextBudget {
	return ContextBudget{
		MaxTotal:   3000,
		System:     1500,
		State:      400,
		Retrieved:  400,
		WorldEvent: 300,
		Decision:   300,
		Reserved:   100,
	}
}

// ContextRequest 编译请求：Compile 的输入。
// 关键：必须带 DecisionIntent 和 CandidateActions——Intent 进入 Runtime 的灵魂。
type ContextRequest struct {
	AgentID         string
	AgentState      *AgentState
	DecisionIntent  *DecisionIntent
	CandidateActions []*CandidateAction
	Budget          *ContextBudget
	// StableBlocks 由 World/Runtime 预构建的 Immutable+Semi-Stable 块
	// （规则/身份/性格/工具/技能）。它们不依赖 Intent，每次决策可复用。
	StableBlocks []ContextBlock
	// DynamicBlocks 由 World 注入的 Dynamic 块（State/WorldState/Event）。
	// Decision 块由 Compiler 依据 Intent+CandidateActions 生成。
	DynamicBlocks []ContextBlock
	// Retriever 可选注入。若非 nil，Compile 会自动按 Intent 检索记忆，
	// 把结果作为 TypeRetrieved 块并入 DynamicBlocks（受 Budget.Retrieved 约束）。
	// 留空则完全跳过检索——World 也可在调用 Compile 前自行 Retrieve 再塞进 DynamicBlocks。
	Retriever Retriever
}

// CompiledContext Compiler 的输出（结构化，不等同于 LLM Message）。
// Provider Adapter 负责把它转成各 LLM 的消息格式。
type CompiledContext struct {
	StableBlocks   []ContextBlock `json:"stable_blocks"`
	DynamicBlocks  []ContextBlock `json:"dynamic_blocks"`
	DecisionBlocks []ContextBlock `json:"decision_blocks"` // Decision Context（M8.4）
	TokenUsage     TokenUsage     `json:"token_usage"`
	Budget         ContextBudget  `json:"budget"`
}

// TokenUsage 编译后的 Token 统计（M8.10 Token Accounting）。
//
// M8.10 字段分两层，刻意不提前合并：
//
//	[A] Runtime 编译成本（Context 视角）
//	  StableTokens     Immutable/Semi-Stable 块（rules/identity/personality/tool/skill）
//	  StateTokens      Agent/World State 块
//	  RetrievedTokens  Retriever 注入的记忆块（M8.7）——"Agent 到底看了多少记忆"
//	  EventTokens      事件块
//	  DecisionTokens   Decision Context（候选行动）
//	  CompactedTokens  被 Compaction 压掉的 token 量（原始-压缩后，未压缩=0）
//	  ContextTokens    编译产物总量（= Stable+State+Retrieved+Event+Decision）
//
//	[B] Provider 实际成本（发送/响应视角）—— 与 [A] 可能不同，不合并
//	  InputTokens   真正发给 Provider 的 input token（由 Adapter 后的 Message 估算）
//	  OutputTokens  Provider 返回的 output token
//	  TotalTokens   Input + Output
//
// 兼容别名（旧测试/代码仍可用）：
//   Stable   = StableTokens
//   Dynamic  = StateTokens + RetrievedTokens + EventTokens
//   Decision = DecisionTokens
//   Total    = ContextTokens
type TokenUsage struct {
	// [A] Runtime 编译成本
	StableTokens     int `json:"stable_tokens"`
	StateTokens      int `json:"state_tokens"`
	RetrievedTokens  int `json:"retrieved_tokens"`
	EventTokens      int `json:"event_tokens"`
	DecisionTokens   int `json:"decision_tokens"`
	CompactedTokens  int `json:"compacted_tokens"`
	ContextTokens    int `json:"context_tokens"`

	// [B] Provider 实际成本（M8.10 仅保留接口，由 Accounting 回填，不调 LLM）
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`

	// 兼容别名
	Stable   int `json:"stable"`
	Dynamic  int `json:"dynamic"`
	Decision int `json:"decision"`
	Retrieved int `json:"retrieved"`
	Compacted int `json:"compacted"`
	Total    int `json:"total"`

	// OverBudget 是否超出 MaxTotal（超了也输出，但标记，供上层告警）。
	OverBudget bool `json:"over_budget"`
}
