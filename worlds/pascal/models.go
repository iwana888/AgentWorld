package pascal

import "time"

// PascalProject 是一个真实 FreePascal 工程，由 World 持有。
// FPC 是物理规律：Agent 的 compile/test 工具直接调用 fpc 二进制，不模拟。
type PascalProject struct {
	ID       string
	Name     string
	RootPath string // 工程根目录（含 src/、tests/）
	Compiler string // 固定 "fpc"
	Language string // 固定 "FreePascal"
}

// Issue 是 Agent 的主要工作来源。描述里不指明要改哪个函数——Agent 必须自己找。
type Issue struct {
	ID           string
	Title        string
	Description  string
	Status       string // open / resolved
	Difficulty   int
	RelatedFiles []string // 提示，不强制
	TestFiles    []string // 该 Issue 应验证的测试（仅这些测试决定 success）
}

// AgentState 是 Agent 的持久状态，进入 Context，但不等于完整项目。
type AgentState struct {
	AgentID  int64
	Name     string
	Role     string
	IssueID  string
	Intent   string
	LastTool string
	Status   string // working / resolved / failed

	// Reliability Runtime 用：Guard 做决策所需的运行时快照。
	CompileOK   bool `json:"compile_ok"`
	TestOK      bool `json:"test_ok"`
	ModifiedSrc bool `json:"modified_src"` // 本轮是否已修改生产代码
}

// ToolCall 是一次工具调用请求（由 LLM 在 Runtime Context 下产出）。
type ToolCall struct {
	Action string            `json:"action"`
	Args   map[string]string `json:"args"`
}

// ToolResult 是一次工具执行结果。
type ToolResult struct {
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Output  string `json:"output"`
}

// TimelineEvent 是 Observatory 轨迹的一个节点。
type TimelineEvent struct {
	At      time.Time `json:"at"`
	IssueID string    `json:"issue_id"`
	Step    string    `json:"step"` // Issue/Read/Modify/Compile/Test/Memory/Submit...
	Detail  string    `json:"detail"`
	OK      bool      `json:"ok"`
}

// SmokeRecord 是 Smoke Test 对一个 Issue 的汇总指标。
type SmokeRecord struct {
	Issue           string `json:"issue"`
	Thinks           int    `json:"thinks"`
	Compiles         int    `json:"compiles"`
	CompileFailures  int    `json:"compile_failures"`
	TestFailures     int    `json:"test_failures"`
	FinalSuccess     bool   `json:"final_success"`
	ContextTokens    int    `json:"context_tokens"`
	RetrievedMemory  int    `json:"retrieved_memory"`
	OutputTokens     int    `json:"output_tokens"`
	DurationMs       int64  `json:"duration_ms"`

	// ---- Experience → Behavior 实验扩展指标 ----
	RecoveryAttempts   int    `json:"recovery_attempts"`   // 失败后重新尝试修复的次数
	RepeatedFailure    int    `json:"repeated_failure"`    // 相同错误重复失败次数
	FirstActionCorrect bool   `json:"first_action_correct"` // 首次 write 是否直达成功（无 afterFail）
	MemoryMode         string `json:"memory_mode"`         // raw / operational / none
	Replay             []ReplayFrame `json:"replay,omitempty"` // 行为回放链

	// Error 记录该 Issue 运行失败的原因（如 LLM 超时）。失败时 FinalSuccess=false
	// 且本字段非空；实验不因单 Issue 失败而中止，保证整组 JSON 可产出。
	Error string `json:"error,omitempty"`

	// GuardEvents 是 Reliability Runtime 拦截事件的完整轨迹。每个事件证明：
	// “Agent 想做违规动作 → Runtime 在执行前拦下 → 动作未执行 → Agent 自行 Recovery”。
	GuardEvents []GuardEvent `json:"guard_events,omitempty"`
}

// GuardEvent 记录一次 Reliability Runtime 拦截（或放行）事件。
// 字段对应 MVP 要验证的链路：Think → Plan/Intent → Tool → Rule → Decision → Execution → Verification。
type GuardEvent struct {
	Think     int         `json:"think"`     // 第几次决策循环
	Plan      string      `json:"plan"`      // Agent 的意图/计划（来自 LLM 决策）
	Tool      string      `json:"tool"`      // Agent 想调用的工具
	Target    string      `json:"target"`    // 目标（如文件路径、单元名）
	Rule      string      `json:"rule"`      // 触发的规则（放行则为空）
	Phase     string      `json:"phase"`     // Tool / Code / Outcome
	Decision  string      `json:"decision"`  // ALLOW / DENY
	Execution string      `json:"execution"` // EXECUTED / NOT_EXECUTED
	Reason    string      `json:"reason,omitempty"`
	Recovery  string      `json:"recovery,omitempty"` // Agent 被拦后下一步做了什么
}

// ReplayFrame 是单次决策回放：检索到的经验 → Context → 决策 → 动作 → 结果。
// 用于事后对比 B 看到了什么、C 看到了什么、C 为何做了不同决定。
type ReplayFrame struct {
	IssueID    string   `json:"issue_id"`
	Think      int      `json:"think"`
	Phase      string   `json:"phase"`
	Retrieved  []string `json:"retrieved"` // 检索到的经验文本（前若干字符）
	ContextTok int      `json:"context_tokens"`
	Decision   string   `json:"decision"` // Agent 选择的动作
	Action     string   `json:"action"`
	Result     string   `json:"result"`  // OK / FAIL + 摘要
	Outcome    bool     `json:"outcome"`
}
