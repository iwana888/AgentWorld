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
