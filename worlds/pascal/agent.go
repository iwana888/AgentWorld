package pascal

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agentworld/internal/context"
	"agentworld/internal/db"
	"agentworld/internal/llm"

	stdctx "context"

	"gorm.io/gorm"
)

// Pascal intent types used by this World (mapped onto frozen M8 intent types
// for retrieval: IMPLEMENT_FEATURE -> code/self/bug/review).
const (
	intentInvestigate = "INVESTIGATE"
	intentReadCode    = "READ_CODE"
	intentModifyCode  = "MODIFY_CODE"
	intentCompile     = "COMPILE"
	intentTest        = "TEST"
	intentDebug       = "DEBUG"
	intentSubmit      = "SUBMIT"
	intentWait        = "WAIT"
	intentWrite       = "write_file"
)

// Agent 是 Pascal World 里的一个自主 Agent。它不被告知“怎么写 Pascal”，
// 而是进入真实工程环境，通过 Runtime 注入的 Context 自主决定下一步动作。
type Agent struct {
	ID    int64
	Name  string
	Proj  *PascalProject
	gdb   *gorm.DB
	llm   *llm.Client
	obs   *Observatory

	compiler  *context.Compiler
	retriever *context.MemoryRetriever
	shell     []string // 编译/测试执行外壳，如 ["bash","-c"] 或 ["wsl","bash","-c"]

	// 运行期状态
	issue      Issue
	state      AgentState
	memAgent   string // 写入/检索 Memory 用的 agent id（与 Proj 绑定）
	toolHits   int    // Agent 真正调用工具的次数（区分“用工具”还是“只聊 LLM”）
	lastResult string // 上一次工具结果（编译/测试错误），注入 Context 让 LLM 改策略
	phase      string // 当前闭环阶段（investigate / afterFail / test / submit）

	// 经验表示模式（A/B/C 实验的唯一变量）。默认 MemRaw。
	memMode MemMode
	replay  []ReplayFrame // 本次 RunIssue 的行为回放链
}

// NewAgent 构造 Agent。gdb 用于真实 Memory 读写（db.AddMemory / QueryMemories）。
// llmClient 可为 nil（仅规则路径），但 Pascal World 需要真实 LLM 才能闭环。
func NewAgent(id int64, name string, proj *PascalProject, gdb *gorm.DB, llmClient *llm.Client) *Agent {
	store := db.NewDBMemoryStore(gdb)
	return &Agent{
		ID:       id,
		Name:     name,
		Proj:     proj,
		gdb:      gdb,
		llm:      llmClient,
		obs:      NewObservatory(name),
		compiler: context.NewCompiler(nil),
		retriever: context.NewMemoryRetriever(store, nil),
		memAgent:  fmt.Sprintf("%d", id),
		shell:     detectShell(),
	}
}

// detectShell 决定编译/测试执行的真实外壳。
// 默认直接调用宿主 shell（bash -c）；若在 Windows 且设置了 PASCAL_USE_WSL，
// 则通过 WSL 调用真实 fpc（FPC 仍是物理规律，只是经由 WSL 执行）。
// 需注意：必须显式指定发行版（-d），否则默认 wsl 可能落在无 bash 的环境。
func detectShell() []string {
	if strings.EqualFold(os.Getenv("PASCAL_USE_WSL"), "1") {
		distro := os.Getenv("PASCAL_WSL_DISTRO")
		if distro == "" {
			distro = "Ubuntu-22.04"
		}
		return []string{"wsl", "-d", distro, "bash", "-c"}
	}
	return []string{"bash", "-c"}
}

// ResetProject 把工作工程从 pristine(initial) 还原，使每个 Issue 互不干扰。
func (a *Agent) ResetProject() error {
	initDir := a.Proj.RootPath + ".initial"
	return copyDir(initDir, a.Proj.RootPath)
}

// RunIssue 跑通一个 Issue 的最小闭环，返回 Smoke 指标。
func (a *Agent) RunIssue(it Issue) (*SmokeRecord, error) {
	if err := a.ResetProject(); err != nil {
		return nil, err
	}
	a.issue = it
	a.state = AgentState{AgentID: a.ID, Name: a.Name, Role: "Pascal Developer", IssueID: it.ID, Status: "working"}
	a.toolHits = 0
	a.replay = nil

	rec := &SmokeRecord{Issue: it.ID, MemoryMode: string(a.memMode)}
	start := time.Now()
	if debugPascal() {
		fmt.Printf("[debug] issue=%s root=%s target=%s\n", it.ID, a.Proj.RootPath, a.targetFile(it))
	}
	defer func() { rec.DurationMs = time.Since(start).Milliseconds() }()

	// 最小闭环阶段机：
	//   investigate →（修改后）→ compile →（pass）→ test →（pass）→ submit
	//   compile/test 失败 → 回到 afterFail（LLM 读失败 Memory 改变修改策略）
	// LLM 自由决定 read/search/modify（策略所在），但物理规律动作
	// （compile/test/submit）由阶段门控，保证闭环必然推进并终止。
	const (
		phaseInvestigate = "investigate"
		phaseAfterFail   = "afterFail"
	)
	compileOK, testOK := false, false
	phase := phaseInvestigate
	phaseThinks := 0
	wroteThisPhase := false
	enteredAfterFail := false // 是否曾因失败进入修复阶段
	lastCompileErr := ""      // 上一次编译错误摘要（用于 RepeatedFailure 判定）
	firstWriteThink := -1     // 首次 write 发生的 think 序号（-1 表示尚未写）
	const (
		investigateBudget = 3 // 最多自由调查几轮后强制下笔写修复
		afterFailBudget   = 2 // 失败后最多自由修改几轮后强制换一种修复
	)

	for think := 0; think < 16; think++ {
		rec.Thinks++
		intent := a.decideIntent(phase, compileOK, testOK)

		// ---- Context Runtime：Perception → Intent → Retrieval → Compile → Adapt ----
		req := a.buildContextRequest(intent)
		cc, err := a.compiler.Compile(stdctx.Background(), req)
		if err != nil {
			return rec, fmt.Errorf("compile context: %w", err)
		}
		rec.ContextTokens += cc.TokenUsage.Total
		rec.RetrievedMemory = max(rec.RetrievedMemory, countRetrieved(cc))
		// Replay：捕获本轮检索到的经验（用于事后对比 B/C 看到了什么）
		retrieved := extractRetrieved(cc)

		msgs, err := context.NewOpenAICompatibleAdapter().CompileMessages(stdctx.Background(), cc)
		if err != nil {
			return rec, fmt.Errorf("adapt messages: %w", err)
		}
		sys, usr := messagesToText(msgs)

		// ---- LLM：在 Runtime Context 下决定下一步 Tool ----
		raw, err := a.llmDecide(sys, usr)
		if err != nil {
			return rec, err
		}
		rec.OutputTokens += roughTokens(raw)

		if debugPascal() {
			fmt.Printf("[debug][%s] think=%d phase=%s\n---RAW---\n%s\n", it.ID, think, phase, raw)
		}

		tc, err := parseToolCall(raw)
		if err != nil {
			if debugPascal() {
				fmt.Printf("[debug][%s] parse FAIL: %v\n", it.ID, err)
			}
			a.remember("debug", "LLM 未产出合法工具调用: "+err.Error(), 2)
			continue
		}
		if debugPascal() {
			fmt.Printf("[debug][%s] action=%s args=%v\n", it.ID, tc.Action, tc.Args)
			fmt.Printf("[debug][%s] lastResult=%s\n", it.ID, firstLine(a.lastResult))
		}

		// 阶段门控：Agent 卡住（预算耗尽仍未下笔）时，Runtime 强制其撰写修复。
		// 阶段门控：
		//  - 第一次修复允许 LLM 自由 write_file（它亲自撰写）。
		//  - 之后若仍需修改（编译/测试失败后的修复），统一经由 requestFix
		//    由 LLM 产出“完整且干净”的修正文件（依据 issue+当前文件+上次错误），
		//    避免 LLM 自由重写时回归到错误版本。LLM 始终是作者，Runtime
		//    只决定“何时写”以及“写完整文件”。
		action := tc.Action
		if action == intentWrite {
			wroteThisPhase = true
			if firstWriteThink < 0 {
				firstWriteThink = think
			}
			if phase == phaseAfterFail {
				rec.RecoveryAttempts++ // 失败后的修复尝试
			}
		}
		switch phase {
		case phaseInvestigate:
			if action == intentCompile || action == intentTest || action == intentSubmit {
				// 允许提前编译/测试
			} else if wroteThisPhase {
				action = intentCompile // 已写过修复：强制编译验证
			} else if phaseThinks >= investigateBudget {
				// 预算耗尽仍未下笔：Runtime 强制其撰写修复（LLM 仍是作者）
				a.applyForcedFix(it, &action, tc, &wroteThisPhase)
			}
		case phaseAfterFail:
			if action == intentCompile || action == intentTest || action == intentSubmit {
				// 允许编译/测试
			} else if wroteThisPhase {
				action = intentCompile // 已改过：强制编译验证本次修改
			} else {
				// 失败后修复：统一经 requestFix，确保干净且针对错误
				a.applyForcedFix(it, &action, tc, &wroteThisPhase)
			}
		}

		a.state.Intent = action
		a.state.LastTool = action
		a.toolHits++

		res := a.executeTool(&ToolCall{Action: action, Args: tc.Args})
		a.obs.Publish(TimelineEvent{
			At: time.Now(), IssueID: it.ID, Step: action,
			Detail: firstLine(res.Output), OK: res.Success,
		})
		// Replay：记录本次决策链（检索 → 决策 → 动作 → 结果）
		a.replay = append(a.replay, ReplayFrame{
			IssueID:    it.ID,
			Think:      think,
			Phase:      phase,
			Retrieved:  retrieved,
			ContextTok: cc.TokenUsage.Total,
			Decision:   action,
			Action:     action,
			Result:     map[bool]string{true: "OK", false: "FAIL"}[res.Success] + " " + firstLine(res.Output),
			Outcome:    res.Success,
		})
		// 记录上一次工具结果（含编译/测试错误），注入下一轮 Context，
		// 让 LLM 在失败后改变修改策略。
		a.lastResult = fmt.Sprintf("[%s] %s\n%s", action, map[bool]string{true: "OK", false: "FAIL"}[res.Success], res.Output)
		a.phase = phase

		phaseThinks++
		switch action {
		case intentCompile:
			rec.Compiles++
			phaseThinks = 0
			if res.Success {
				compileOK = true
				phase = "test" // 进入测试阶段（由下方统一处理）
				// 直接继续到 test 处理
			} else {
				rec.CompileFailures++
				// RepeatedFailure：与上一次编译错误相同
				errSum := firstLine(res.Output)
				if errSum == lastCompileErr {
					rec.RepeatedFailure++
				}
				lastCompileErr = errSum
				enteredAfterFail = true
				a.rememberFailure(it, "compile("+a.targetFile(it)+")", "Compile failed: "+errSum,
					"root cause visible in compiler error above",
					"read the FPC error line, fix that exact identifier/syntax, then recompile", 3)
				phase = phaseAfterFail
				wroteThisPhase = false
			}
		case intentTest:
			phaseThinks = 0
			if res.Success {
				testOK = true
				if compileOK {
					// 进入提交阶段
					phase = "submit"
					wroteThisPhase = false
				}
			} else {
				rec.TestFailures++
				a.rememberFailure(it, "test", "Test failed: "+firstLine(res.Output),
					"behavior mismatch between implementation and test expectation",
					"re-read the failing assertion, align the function output to the expected value", 3)
				phase = phaseAfterFail
				wroteThisPhase = false
			}
		case intentSubmit:
			if compileOK && testOK {
				a.state.Status = "resolved"
				rec.FinalSuccess = true
				rec.FirstActionCorrect = !enteredAfterFail // 首次 write 直达成功，未经历修复
				a.rememberSuccess(it, "write_file("+a.targetFile(it)+")",
					"applied fix to "+a.targetFile(it)+" and verified by real FPC compile+test", 3)
				rec.Replay = a.replay
				return rec, nil
			}
			a.remember("debug", "Attempted submit before compile+test PASS", 2)
			phase = phaseAfterFail
			wroteThisPhase = false
		case intentWait:
			a.remember("debug", "Agent chose WAIT, forcing progress", 1)
		}

		// 阶段收口：test / submit 阶段强制对应物理动作
		if phase == "test" && action != intentTest {
			// 强制测试
			res := a.executeTool(&ToolCall{Action: intentTest})
			if debugPascal() {
				fmt.Printf("[debug][%s] FORCED TEST => success=%v\n%s\n", it.ID, res.Success, res.Output)
			}
			a.obs.Publish(TimelineEvent{At: time.Now(), IssueID: it.ID, Step: intentTest, Detail: firstLine(res.Output), OK: res.Success})
			phaseThinks = 0
			if res.Success {
				testOK = true
				phase = "submit"
				wroteThisPhase = false
			} else {
				rec.TestFailures++
				a.rememberFailure(it, "test", "Test failed: "+firstLine(res.Output),
					"behavior mismatch between implementation and test expectation",
					"re-read the failing assertion, align the function output to the expected value", 3)
				phase = phaseAfterFail
				wroteThisPhase = false
			}
		}
		if phase == "submit" && action != intentSubmit {
			if compileOK && testOK {
				a.state.Status = "resolved"
				rec.FinalSuccess = true
				rec.FirstActionCorrect = !enteredAfterFail
				a.rememberSuccess(it, "write_file("+a.targetFile(it)+")",
					"applied fix to "+a.targetFile(it)+" and verified by real FPC compile+test", 3)
				rec.Replay = a.replay
				return rec, nil
			}
			phase = phaseAfterFail
			wroteThisPhase = false
		}
	}

	a.state.Status = "failed"
	rec.Replay = a.replay
	return rec, nil
}

// decideIntent 是 Planner：根据当前阶段选择检索用的 Intent 类型。
// 始终映射为冻结的 IMPLEMENT_FEATURE，驱动 Retriever 找 code/self/bug/review。
func (a *Agent) decideIntent(phase string, compileOK, testOK bool) string {
	_ = phase
	_ = compileOK
	_ = testOK
	return "IMPLEMENT_FEATURE"
}

// buildContextRequest 构造带 Intent 的 ContextRequest。
// Intent 驱动 Retriever 找相关 Memory（code/self/bug/review）。
func (a *Agent) buildContextRequest(action string) *context.ContextRequest {
	intentType := "IMPLEMENT_FEATURE" // 映射到冻结的检索类型
	stateJSON, _ := json.Marshal(a.state)
	return &context.ContextRequest{
		AgentID: a.memAgent,
		AgentState: &context.AgentState{
			AgentID: a.memAgent,
			Balance: 0,
		},
		DecisionIntent: &context.DecisionIntent{
			Type:       intentType,
			SkillID:    "pascal",
			Complexity: 2,
		},
		CandidateActions: pascalCandidateActions(),
		StableBlocks:     a.stableBlocks(),
		DynamicBlocks: []context.ContextBlock{
			{
				ID: "pascal.issue", Type: context.TypeWorldState, Source: "pascal.issue",
				Content: "CURRENT ISSUE " + a.issue.ID + ": " + a.issue.Title + "\n" + a.issue.Description,
				Priority: 90, Stable: false,
			},
			{
				ID: "pascal.agent_state", Type: context.TypeAgentState, Source: "pascal.agent",
				Content: string(stateJSON), Priority: 90, Stable: false,
			},
			{
				ID: "pascal.phase", Type: context.TypeWorldState, Source: "pascal.phase", Priority: 95, Stable: false,
				Content: phaseInstruction(a.phase, a.lastResult),
			},
			{
				ID: "pascal.last_result", Type: context.TypeWorldState, Source: "pascal.result", Priority: 95, Stable: false,
				Content: lastResultBlock(a.lastResult),
			},
		},
		Retriever: a.retriever,
	}
}

// stableBlocks 是 Immutable/Semi-Stable 上下文（规则/身份/工具）。
// 它们是稳定前缀，不随单次决策变化。
func (a *Agent) stableBlocks() []context.ContextBlock {
	return []context.ContextBlock{
		{
			ID: "pascal.rules", Type: context.TypeWorldRules, Source: "pascal.rules", Stable: true,
			Priority: 100,
			Content: "You are an autonomous Pascal developer agent in AgentWorld. " +
				"You solve ONE issue at a time using tools. Never invent code you cannot verify. " +
				"FPC is the physical law: you MUST compile and test before submit. " +
				"To fix a bug you MUST call write_file with the COMPLETE corrected file — reading alone changes nothing. " +
				"Use your past failure memories to change strategy.",
		},
		{
			ID: "pascal.identity", Type: context.TypeAgentIdentity, Source: "pascal.identity", Stable: true,
			Priority: 100, Content: "Name=" + a.Name + " Role=Pascal Developer",
		},
		{
			ID: "pascal.tools", Type: context.TypeToolSchema, Source: "pascal.tools", Stable: true,
			Priority: 100,
			Content: toolSchema(),
		},
	}
}

// SetMemMode 设置经验表示模式（A/B/C 实验的唯一变量）。
// 不改变 Retriever / Compiler / LLM / Agent 决策。
func (a *Agent) SetMemMode(m MemMode) {
	a.memMode = m
}

// remember 把一次经验写入真实 Memory（生产 db.AddMemory 路径）。
// 当 memMode == MemOperational 时，把经验渲染成 OPERATIONAL 结构文本——
// 这是 C 组与 B 组的唯一差异（写入内容形态），Retriever 完全不变。
func (a *Agent) remember(typ, content string, importance int) {
	if a.memMode == MemOperational {
		content = "[OPERATIONAL] " + content
	}
	_ = db.AddMemory(a.gdb, a.ID, typ, content, importance)
}

// rememberFailure 在编译/测试失败时记录经验。C 组会写入结构化经验。
func (a *Agent) rememberFailure(it Issue, action, failure, cause, resolution string, importance int) {
	if a.memMode == MemOperational {
		op := BuildFailureExperience(it, action, failure, cause, resolution)
		_ = db.AddMemory(a.gdb, a.ID, "bug", op.Format(), importance)
		return
	}
	a.remember("bug", failure, importance)
}

// rememberSuccess 在成功修复后记录经验。C 组写入“哪类问题→哪种修复→成功”。
func (a *Agent) rememberSuccess(it Issue, action, resolution string, importance int) {
	if a.memMode == MemOperational {
		op := BuildSuccessExperience(it, action, resolution)
		_ = db.AddMemory(a.gdb, a.ID, "code", op.Format(), importance)
		return
	}
	a.remember("code", "Resolved "+it.ID+" via Runtime loop", importance)
}

// ---- LLM（复用实验中验证过的非 JSON 路径，避免 deepseek json mode 400）----

func (a *Agent) llmDecide(system, user string) (string, error) {
	if a.llm == nil || !a.llm.Enabled() {
		return "", fmt.Errorf("pascal: LLM not configured (set LLM_API_KEY)")
	}
	// 优先用生产 Decice（JSON），失败回退非 JSON。本 World 只需非 JSON 文本。
	return decideTextHTTP(system, user)
}

// ---- 工具执行 ----

// executeTool 执行一个 ToolCall。这是 Agent 与世界交互的唯一方式。
func (a *Agent) executeTool(tc *ToolCall) *ToolResult {
	switch tc.Action {
	case "list_files":
		return a.toolListFiles()
	case "read_file":
		return a.toolReadFile(tc.Args["path"])
	case "search_code":
		return a.toolSearchCode(tc.Args["query"])
	case "write_file":
		return a.toolWriteFile(tc.Args["path"], tc.Args["content"])
	case intentCompile:
		return a.toolCompile()
	case intentTest:
		return a.toolTest()
	case intentSubmit:
		return &ToolResult{Action: intentSubmit, Success: true, Output: "submit requested"}
	default:
		return &ToolResult{Action: tc.Action, Success: false, Output: "unknown tool: " + tc.Action}
	}
}

func (a *Agent) toolListFiles() *ToolResult {
	var lines []string
	walk(a.Proj.RootPath, func(p string) {
		rel, _ := filepath.Rel(a.Proj.RootPath, p)
		lines = append(lines, rel)
	})
	return &ToolResult{Action: "list_files", Success: true, Output: strings.Join(lines, "\n")}
}

func (a *Agent) toolReadFile(path string) *ToolResult {
	full := safeJoin(a.Proj.RootPath, path)
	b, err := os.ReadFile(full)
	if err != nil {
		return &ToolResult{Action: "read_file", Success: false, Output: err.Error()}
	}
	return &ToolResult{Action: "read_file", Success: true, Output: string(b)}
}

func (a *Agent) toolSearchCode(query string) *ToolResult {
	if query == "" {
		return &ToolResult{Action: "search_code", Success: false, Output: "empty query"}
	}
	var out []string
	walk(a.Proj.RootPath, func(p string) {
		if !strings.HasSuffix(p, ".pas") {
			return
		}
		b, _ := os.ReadFile(p)
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, query) {
				rel, _ := filepath.Rel(a.Proj.RootPath, p)
				out = append(out, fmt.Sprintf("%s:%d: %s", rel, i+1, line))
			}
		}
	})
	if len(out) == 0 {
		return &ToolResult{Action: "search_code", Success: true, Output: "no match"}
	}
	return &ToolResult{Action: "search_code", Success: true, Output: strings.Join(out, "\n")}
}

func (a *Agent) toolWriteFile(path, content string) *ToolResult {
	if path == "" || content == "" {
		return &ToolResult{Action: "write_file", Success: false, Output: "path/content required"}
	}
	// 归一化写入路径：LLM 常给出裸单元名（如 "unit Money;" 或 "Money.pas"），
	// 统一解析到 src/<name>.pas；非法路径回退到当前 Issue 目标文件。
	norm := a.normalizeWritePath(path)
	full := safeJoin(a.Proj.RootPath, norm)
	if full == a.Proj.RootPath {
		// safeJoin 拒绝（越界/非法），回退目标文件
		full = safeJoin(a.Proj.RootPath, a.targetFile(a.issue))
	}
	content = normalizeUnitHeader(filepath.Base(full), content)
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		return &ToolResult{Action: "write_file", Success: false, Output: err.Error()}
	}
	return &ToolResult{Action: "write_file", Success: true, Output: "wrote " + norm}
}

// normalizeUnitHeader 把 LLM 写入的 unit 声明行强制对齐到文件名，避免
// 其写回 "unit DateUtils;" 之类的旧名/错名（与文件名不一致会导致 FPC
// 报 unit name mismatch）。这是 post-write 归一化：LLM 仍是内容作者，
// Runtime 只修正头一行声明以匹配物理文件名。
func normalizeUnitHeader(fileName, content string) string {
	base := strings.TrimSuffix(fileName, ".pas")
	if base == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(trimmed), "unit ") {
			continue
		}
		// 取 unit 与 ; 之间的名字词
		rest := strings.TrimSpace(trimmed[5:])
		name := rest
		if i := strings.IndexAny(rest, " ;\t"); i >= 0 {
			name = strings.TrimSpace(rest[:i])
		}
		if strings.EqualFold(name, base) {
			break // 已正确，无需改
		}
		lines[i] = "unit " + base + ";"
		break
	}
	return strings.Join(lines, "\n")
}

// normalizeWritePath 把 LLM 给出的各种写法归一到 src/<name>.pas。
func (a *Agent) normalizeWritePath(path string) string {
	p := strings.TrimSpace(path)
	p = strings.TrimSuffix(p, ";")
	p = strings.TrimSpace(p)
	// "unit Money" / "unit Money;" -> Money
	if strings.HasPrefix(strings.ToLower(p), "unit ") {
		p = strings.TrimSpace(p[5:])
	}
	// 去掉可能的引号/空格
	p = strings.Trim(p, `"' `)
	// 仅文件名（无目录）且以 .pas 结尾 -> 放到 src/
	if !strings.Contains(p, "/") && !strings.Contains(p, "\\") {
		if strings.HasSuffix(strings.ToLower(p), ".pas") {
			return "src/" + p
		}
		// 裸单元名 -> src/<name>.pas
		return "src/" + p + ".pas"
	}
	return p
}

// toolCompile 真实调用 fpc 编译工程。FPC 是物理规律，不模拟。
// 各单元独立编译（逐个单独调用 fpc，避免复杂 shell 脚本）；成功与否以
// “当前 Issue 的目标单元”为准（例如 #005 的 Broken.pas 故意编译失败，
// 直到被修复）。其它单元的编译状态会列出但不阻塞——测试只编译各自引用
// 的单元，互不影响。
func (a *Agent) toolCompile() *ToolResult {
	units, _ := filepath.Glob(filepath.Join(a.Proj.RootPath, "src", "*.pas"))
	target := a.shellPath(safeJoin(a.Proj.RootPath, a.targetFile(a.issue)))
	var report []string
	targetOK := false
	for _, u := range units {
		out, ok := a.runFPCUnit(a.shellPath(u))
		if ok {
			report = append(report, "OK: "+filepath.Base(u))
			if a.shellPath(u) == target {
				targetOK = true
			}
		} else {
			report = append(report, "FAIL: "+filepath.Base(u)+"\n"+out)
		}
	}
	if targetOK {
		return &ToolResult{Action: intentCompile, Success: true, Output: strings.Join(report, "\n")}
	}
	return &ToolResult{Action: intentCompile, Success: false, Output: strings.Join(report, "\n")}
}

// toolTest 真实编译并运行当前 Issue 指定的测试程序（仅 TestFiles），
// 而不是整个工程的全部测试——每个 Issue 只负责修复一个单元。
func (a *Agent) toolTest() *ToolResult {
	tests := a.issue.TestFiles
	if len(tests) == 0 {
		// 该 Issue 无单元测试（如纯编译错误），仅以编译通过为判定。
		return &ToolResult{Action: intentTest, Success: true, Output: "no unit tests for this issue (compile-only)"}
	}
	var report []string
	allOK := true
	for _, t := range tests {
		tp := safeJoin(a.Proj.RootPath, t)
		if tp == a.Proj.RootPath {
			continue
		}
		// 测试二进制输出到临时目录，避免污染工程。WSL 下用 Linux /tmp。
		base := strings.TrimSuffix(filepath.Base(t), filepath.Ext(t))
		bin := "/tmp/pascal_" + base
		if a.shell[0] != "wsl" {
			bin = filepath.Join(os.TempDir(), "pascal_"+base)
		}
		// -Fusrc 让 FPC 按单元名找到 src/ 下的单元；-Sa 启用断言（否则 Assert 默认被忽略）
		out, ok := a.runFPCUnit(a.shellPath(tp) + " -Sa -Fusrc -o" + bin)
		if !ok {
			allOK = false
			report = append(report, "COMPILE_FAIL: "+filepath.Base(t)+"\n"+out)
			continue
		}
		// 运行测试二进制
		ro, rerr := a.runShell(bin)
		if !rerr {
			allOK = false
			report = append(report, "RUN_FAIL: "+filepath.Base(t)+"\n"+ro)
		} else {
			report = append(report, "PASS: "+filepath.Base(t))
		}
	}
	if allOK {
		return &ToolResult{Action: intentTest, Success: true, Output: strings.Join(report, "\n")}
	}
	return &ToolResult{Action: intentTest, Success: false, Output: strings.Join(report, "\n")}
}

// runFPCUnit 通过外壳调用 fpc 编译指定单元/测试（WSL 下路径已转换）。
// 命令在 cd 到工程根目录后执行，确保 -Fusrc 等相对路径正确解析。
func (a *Agent) runFPCUnit(arg string) (string, bool) {
	root := a.shellPath(a.Proj.RootPath)
	script := "cd " + shellQuote(root) + " && fpc " + arg
	args := append(append([]string{}, a.shell[1:]...), script)
	cmd := exec.Command(a.shell[0], args...)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// runShell 通过配置的外壳执行脚本（bash 或 wsl bash）。
func (a *Agent) runShell(script string) (string, bool) {
	root := a.shellPath(a.Proj.RootPath)
	full := "cd " + shellQuote(root) + " && " + script
	args := append(append([]string{}, a.shell[1:]...), full)
	cmd := exec.Command(a.shell[0], args...)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// shellQuote 对 bash 命令中的路径做单引号包裹转义。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellPath 把工程绝对路径转换为外壳可见的路径。WSL 模式下把 C:\x 转成 /mnt/c/x。
func (a *Agent) shellPath(winPath string) string {
	if len(a.shell) >= 1 && a.shell[0] == "wsl" {
		p := filepath.ToSlash(winPath)
		if len(p) >= 2 && p[1] == ':' {
			drive := strings.ToLower(string(p[0]))
			return "/mnt/" + drive + p[2:]
		}
	}
	return winPath
}

// ---- 工具辅助 ----

func toolSchema() string {
	return `Tools (call exactly one as JSON {"action":..., "args":{...}}):
- list_files            : {}
- read_file             : {"path": "src/StayCalc.pas"}
- search_code           : {"query": "CalculateStayDays"}
- write_file            : {"path": "src/StayCalc.pas", "content": "<COMPLETE corrected file>"}
- compile               : {}
- test                  : {}
- submit                : {}  (ONLY if compile AND test both PASS)`
}

// phaseInstruction 把当前闭环阶段与“下一步该做什么”注入 Context。
// 这不改变“LLM 撰写修改”的事实，只约束工作流推进，等价于工程师被告知
// “你现在处于 X 阶段，下一步应当 Y”。
func phaseInstruction(phase, last string) string {
	switch phase {
	case "afterFail":
		return "PHASE: FIX. The last compile/test FAILED (see LAST RESULT). " +
			"Read the error, then call write_file with a DIFFERENT fix. Do not keep reading."
	case "test":
		return "PHASE: TEST. Compile passed. Now call test to verify behavior."
	case "submit":
		return "PHASE: SUBMIT. Compile and test both passed. Call submit."
	default:
		return "PHASE: INVESTIGATE. Read the relevant unit, then call write_file with the FULL corrected file to apply your fix. " +
			"You MUST use write_file to fix the bug — reading alone changes nothing."
	}
}

// lastResultBlock 把上一轮工具结果（含错误）喂回 Context。
func lastResultBlock(last string) string {
	if last == "" {
		return "LAST RESULT: (none yet — this is your first action)"
	}
	return "LAST RESULT:\n" + last
}

// targetFile 返回当前 Issue 主要修改目标文件（取 RelatedFiles[0]）。
func (a *Agent) targetFile(it Issue) string {
	if len(it.RelatedFiles) > 0 {
		return it.RelatedFiles[0]
	}
	return "src/DateUtils.pas"
}

// forceWrite 当 Agent 卡住时，让 LLM 产出目标文件的完整修正内容。
// LLM 是作者；Runtime 只强制“现在写”。返回内容供 write_file 落地。
func (a *Agent) forceWrite(it Issue) (string, error) {
	path := a.targetFile(it)
	full := safeJoin(a.Proj.RootPath, path)
	b, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	content, err := requestFix(it.Title, it.Description, path, string(b), a.lastResult)
	if err != nil {
		return "", err
	}
	// 去掉可能的 markdown 围栏
	content = stripFences(content)
	if len([]rune(content)) < 20 {
		return "", fmt.Errorf("llm fix too short")
	}
	return content, nil
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	return strings.TrimSpace(s)
}

// applyForcedFix 在 Agent 卡住/失败修复时，让 LLM 产出目标文件的完整修正内容
// 并落到 write_file。LLM 是作者；Runtime 只强制“现在写完整文件”。
func (a *Agent) applyForcedFix(it Issue, action *string, tc *ToolCall, wrote *bool) {
	content, ferr := a.forceWrite(it)
	if ferr != nil {
		return
	}
	*action = intentWrite
	tc.Args = map[string]string{"path": a.targetFile(it), "content": content}
	*wrote = true
}

func pascalCandidateActions() []*context.CandidateAction {
	acts := []string{intentInvestigate, intentReadCode, intentModifyCode, intentCompile, intentTest, intentDebug, intentSubmit, intentWait}
	out := make([]*context.CandidateAction, 0, len(acts))
	for i, a := range acts {
		out = append(out, &context.CandidateAction{ID: a, Action: a, Label: a, Score: float64(i)})
	}
	return out
}

func parseToolCall(raw string) (*ToolCall, error) {
	s := strings.TrimSpace(raw)
	// 去掉可能的 ```json ... ``` 围栏
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
		s = strings.TrimSpace(s)
	}
	// 优先直接解析整段（Go 能正确处理字符串内的 } 与转义）
	var tc ToolCall
	if err := json.Unmarshal([]byte(s), &tc); err == nil && tc.Action != "" {
		return normalizeTC(tc), nil
	}
	// 退化：扫描平衡括号提取第一个完整 JSON 对象
	start := strings.Index(s, "{")
	if start < 0 {
		return nil, fmt.Errorf("no json object in LLM output")
	}
	depth, inStr := 0, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				if err := json.Unmarshal([]byte(s[start:i+1]), &tc); err == nil && tc.Action != "" {
					return normalizeTC(tc), nil
				}
				return nil, fmt.Errorf("invalid json object")
			}
		}
	}
	return nil, fmt.Errorf("unterminated json object")
}

func normalizeTC(tc ToolCall) *ToolCall {
	if tc.Args == nil {
		tc.Args = map[string]string{}
	}
	return &tc
}

func countRetrieved(cc *context.CompiledContext) int {
	n := 0
	for _, b := range cc.DynamicBlocks {
		if b.Type == context.TypeRetrieved {
			n++
		}
	}
	return n
}

// extractRetrieved 从编译后的 Context 中提取检索到的经验文本（截断到 200 字符），
// 用于 Replay 对比 B/C 两组各自看到了什么。
func extractRetrieved(cc *context.CompiledContext) []string {
	out := make([]string, 0)
	for _, b := range cc.DynamicBlocks {
		if b.Type == context.TypeRetrieved {
			s := b.Content
			if len(s) > 200 {
				s = s[:200]
			}
			out = append(out, s)
		}
	}
	return out
}

func walk(root string, fn func(string)) {
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			fn(p)
		}
		return nil
	})
}

func safeJoin(root, p string) string {
	clean := filepath.Clean(filepath.Join(root, p))
	if !strings.HasPrefix(clean, filepath.Clean(root)) {
		return root // 防目录穿越
	}
	return clean
}

func copyDir(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	var errRet error
	walk(src, func(p string) {
		rel, _ := filepath.Rel(src, p)
		tgt := filepath.Join(dst, rel)
		b, err := os.ReadFile(p)
		if err != nil {
			errRet = err
			return
		}
		if err := os.MkdirAll(filepath.Dir(tgt), 0755); err != nil {
			errRet = err
			return
		}
		if err := os.WriteFile(tgt, b, 0644); err != nil {
			errRet = err
		}
	})
	return errRet
}

func messagesToText(msgs []llm.Message) (sys, usr string) {
	for _, m := range msgs {
		if m.Role == "system" {
			sys = m.Content
		} else {
			usr = m.Content
		}
	}
	return
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	if len(s) > 120 {
		return s[:120]
	}
	return s
}

func roughTokens(s string) int {
	return len([]rune(s)) / 4 + 1
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func debugPascal() bool {
	return os.Getenv("PASCAL_DEBUG") == "1"
}
