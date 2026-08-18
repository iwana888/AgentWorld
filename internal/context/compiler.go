package context

import (
	"context"
	"fmt"
	"strings"
)

// ContextEngine Context Runtime 的核心接口。
// Compile 接收带 Intent 的请求，产出结构化 CompiledContext（不是 LLM Message）。
type ContextEngine interface {
	Compile(ctx context.Context, req *ContextRequest) (*CompiledContext, error)
}

// Estimator Token 估算器（极简实现，第一版够用）。
// 真实 tokenizer 后续可替换（M8.10 / Provider 适配），接口稳定即可。
type Estimator func(s string) int

// roughEstimate 粗糙估算：中文按字符，英文按词，约 1 token/4 字符兜底。
// 目的是验证"Context 长什么样"，而非精确计费。
// 第一版 TokenEstimator 接口实现名为 RoughEstimator（见 estimator.go 的变量），
// 为避免同名冲突，旧的 Estimator func 默认实现重命名为 roughEstimate。
func roughEstimate(s string) int {
	if s == "" {
		return 0
	}
	// 中文字符数
	var cjk, other int
	for _, r := range s {
		if r > 0x2E80 && r < 0x9FFF { // CJK 区间（粗略）
			cjk++
		} else {
			other++
		}
	}
	// CJK 约 1 字 1 token；其它按 4 字符 1 token
	return cjk + (other+3)/4
}

// Compiler 机械版 Context 编译器（M8.5）+ Compaction 防线（M8.8）。
// 行为刻意简单：
//   输入 Blocks（Stable + Dynamic）→ 估算 Token → 按 Budget 分类截断 → 输出。
// 当首次编译超出 MaxTotal 且注入了 Compactor 时，触发 Compaction（第二优化手段），
// 再二次 Compile。仍超限则严格裁剪（丢最低优先级），实在无法再标记 OverBudget。
// 不做：自动排序（仅按 Priority 降序做 Budget 内截断）、RAG、动态预算。
type Compiler struct {
	est       Estimator
	compactor Compactor // 可选；nil 时不启用 Compaction 防线
}

// NewCompiler 构造编译器。
func NewCompiler(est Estimator) *Compiler {
	if est == nil {
		est = roughEstimate
	}
	return &Compiler{est: est}
}

// WithCompactor 注入 Compactor（M8.8 防线）。返回自身便于链式构造。
func (c *Compiler) WithCompactor(cp Compactor) *Compiler {
	c.compactor = cp
	return c
}

// Compile 编译 Context（M8.5 主流程 + M8.8 二次 Compile 防线）。
func (c *Compiler) Compile(ctx context.Context, req *ContextRequest) (*CompiledContext, error) {
	if req == nil {
		return nil, fmt.Errorf("context: nil request")
	}
	budget := DefaultBudget()
	if req.Budget != nil {
		budget = *req.Budget
	}

	// 首次编译。
	cc, usedDynamic, err := c.compileOnce(ctx, req, budget)
	if err != nil {
		return nil, err
	}

	// M8.8 防线：首次超预算 → 触发 Compaction（仅当注入了 Compactor）。
	if cc.TokenUsage.OverBudget && c.compactor != nil {
		cc, err = c.compactAndRecompile(ctx, req, budget, usedDynamic)
		if err != nil {
			return nil, err
		}
	}

	// 安全网：Compaction 后仍超限 → 严格裁剪（丢最低优先级）直到 ≤ MaxTotal。
	if cc.TokenUsage.Total > budget.MaxTotal {
		cc = c.strictTrim(cc, budget)
	}
	return cc, nil
}

// compileOnce 单次编译（不含 Compaction）。供首次与二次复用。
// 返回编译结果与"实际参与编译的 Dynamic 块全集"（含 Retriever 注入的 Retrieved 块），
// 供后续 Compaction 收集可压缩对象——否则 Retriever 注入的块会绕过 Compaction。
func (c *Compiler) compileOnce(ctx context.Context, req *ContextRequest, budget ContextBudget) (*CompiledContext, []ContextBlock, error) {
	// 1) Stable 块：估算 Token，按 System 预算截断（低优先级先丢）。
	stable := c.estimateAll(req.StableBlocks)
	stable = c.truncateByBudget(stable, budget.System, true)

	// 2) Dynamic 块：先估算 Token（注：estimateAll 返回新切片，必须用它作为
	//    后续 usedDynamic 的来源——否则 Compaction 收集到的块 Tokens=0，压缩量统计失真）。
	//    若注入了 Retriever，再按 Intent 检索记忆并入。
	dynInput := c.estimateAll(req.DynamicBlocks)
	if req.Retriever != nil && req.DecisionIntent != nil {
		retrieved, err := req.Retriever.Retrieve(ctx, &RetrieveRequest{
			AgentID:         req.AgentID,
			Intent:          *req.DecisionIntent,
			RelatedAgentIDs: req.DecisionIntent.relatedIDs(),
			SkillIDs:        req.DecisionIntent.skillIDList(),
			BudgetTokens:    budget.Retrieved,
			Limit:           0,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("context: retrieve failed: %w", err)
		}
		dynInput = append(dynInput, c.estimateAll(retrieved)...)
	}
	dyn := c.classifyDynamic(dynInput, budget)

	// 3) Decision 块（M8.4）：由 Intent + CandidateActions 生成，按 Decision 预算截断。
	dec := c.buildDecision(req.DecisionIntent, req.CandidateActions)
	dec = c.estimateAll(dec)
	dec = c.truncateByBudget(dec, budget.Decision, true)

	return c.assemble(stable, dyn, dec, budget), dynInput, nil
}

// assemble 汇总动态包 + Decision 块为 CompiledContext 并计算 TokenUsage。
func (c *Compiler) assemble(stable []ContextBlock, dyn dynamicBundle, dec []ContextBlock, budget ContextBudget) *CompiledContext {
	stableTok := sumTokens(stable)
	decTok := sumTokens(dec)
	ctxTok := stableTok + dyn.total + decTok
	usage := TokenUsage{
		// [A] 精确分项
		StableTokens:    stableTok,
		StateTokens:     dyn.state,
		RetrievedTokens: dyn.retrieved,
		EventTokens:     dyn.event,
		DecisionTokens:  decTok,
		ContextTokens:   ctxTok,
		// 兼容别名
		Stable:    stableTok,
		Dynamic:   dyn.total,
		Decision:  decTok,
		Retrieved: dyn.retrieved,
		Total:     ctxTok,
	}
	if ctxTok > budget.MaxTotal {
		usage.OverBudget = true
	}
	return &CompiledContext{
		StableBlocks:   stable,
		DynamicBlocks:  dyn.blocks,
		DecisionBlocks: dec,
		TokenUsage:     usage,
		Budget:         budget,
	}
}

// compactAndRecompile M8.8：把可压缩的 Dynamic/检索块交给 Compactor 压缩，
// 用压缩结果替换原 DynamicBlocks 后二次 Compile。
// usedDynamic 是首次编译实际使用的 Dynamic 块全集（含 Retriever 注入的 Retrieved 块），
// 必须用这个集合收集 reducible，否则 Retriever 注入的块会绕过 Compaction。
func (c *Compiler) compactAndRecompile(ctx context.Context, req *ContextRequest, budget ContextBudget, usedDynamic []ContextBlock) (*CompiledContext, error) {
	// 收集可压缩块（来自首次实际使用的 Dynamic 块，含 Retrieved）。
	// Stable/State/Decision 永不压缩（ReducePolicy 已保证）。
	var reducible []ContextBlock
	for _, b := range usedDynamic {
		if ReducePolicy(b) {
			reducible = append(reducible, b)
		}
	}
	if len(reducible) == 0 {
		cc, _, err := c.compileOnce(ctx, req, budget)
		return cc, err
	}
	compressed, err := c.compactor.Compact(ctx, reducible, budget.Retrieved)
	if err != nil {
		return nil, fmt.Errorf("context: compact failed: %w", err)
	}
	// 记录被压缩掉的 token 量（原始可压缩总量 - 压缩后总量），供 M8.10 统计压缩率。
	beforeTokens := sumTokens(reducible)
	afterTokens := sumTokens(compressed)
	compacted := beforeTokens - afterTokens
	if compacted < 0 {
		compacted = 0
	}
	// 用压缩产物替换原 DynamicBlocks 中可压缩部分（保留不可压缩项）。
	var kept []ContextBlock
	for _, b := range req.DynamicBlocks {
		if !ReducePolicy(b) {
			kept = append(kept, b)
		}
	}
	newReq := *req
	newReq.DynamicBlocks = append(kept, compressed...)
	// 关键：二次 Compile 不再调 Retriever——检索已在首次完成，压缩阶段只重组已有块，
	// 否则 Retriever 会再次拉满 Retrieved，把刚压掉的又拉回来。
	newReq.Retriever = nil
	cc, _, err := c.compileOnce(ctx, &newReq, budget)
	if err == nil {
		cc.TokenUsage.CompactedTokens = compacted
		cc.TokenUsage.Compacted = compacted // 兼容别名
	}
	return cc, err
}

// strictTrim 严格裁剪：按 Priority 升序逐个丢弃块，直到 Total ≤ MaxTotal。
// 永不丢弃 Priority ≥ PriorityCurrentDecision（Stable/State/Decision）。
// 若实在无法（Stable 自身就超 MaxTotal），保留并标记 OverBudget 返回明确状态。
func (c *Compiler) strictTrim(cc *CompiledContext, budget ContextBudget) *CompiledContext {
	// 合并所有块（保留顺序），按 Priority 升序排列待裁列表。
	all := append([]ContextBlock{}, cc.DynamicBlocks...)
	all = append(all, cc.DecisionBlocks...)
	// 升序：最低优先级在前。
	for i := 0; i < len(all); i++ {
		min := i
		for j := i + 1; j < len(all); j++ {
			if all[j].Priority < all[min].Priority {
				min = j
			}
		}
		all[i], all[min] = all[min], all[i]
	}
	used := cc.TokenUsage.Stable
	dynOut, decOut := []ContextBlock{}, []ContextBlock{}
	usedDyn, usedDec := 0, 0
	for _, b := range all {
		if b.Type == TypeDecision {
			if used+usedDyn+usedDec+b.Tokens <= budget.MaxTotal || b.Priority >= PriorityCurrentDecision {
				decOut = append(decOut, b)
				usedDec += b.Tokens
			}
			continue
		}
		if used+usedDyn+usedDec+b.Tokens <= budget.MaxTotal || b.Priority >= PriorityCurrentDecision {
			dynOut = append(dynOut, b)
			usedDyn += b.Tokens
		}
	}
	total := used + usedDyn + usedDec
	return &CompiledContext{
		StableBlocks:   cc.StableBlocks,
		DynamicBlocks:  dynOut,
		DecisionBlocks: decOut,
		TokenUsage: TokenUsage{
			StableTokens:    cc.TokenUsage.StableTokens,
			StateTokens:     sumTokensByType(dynOut, TypeAgentState) + sumTokensByType(dynOut, TypeWorldState),
			RetrievedTokens: sumTokensByType(dynOut, TypeRetrieved),
			EventTokens:     sumTokensByType(dynOut, TypeEvent),
			DecisionTokens:  usedDec,
			ContextTokens:   total,
			Stable:          cc.TokenUsage.StableTokens,
			Dynamic:         usedDyn,
			Decision:        usedDec,
			Total:           total,
			OverBudget:      total > budget.MaxTotal,
		},
		Budget: budget,
	}
}

// dynamicBundle Dynamic 块按分类聚合后的结果（含分项 token 统计）。
type dynamicBundle struct {
	blocks    []ContextBlock
	total     int
	state     int // AgentState + WorldState
	retrieved int
	event     int
}

// classifyDynamic 把 Dynamic 块按 BlockType 归入 State/WorldEvent/Retrieved 预算。
func (c *Compiler) classifyDynamic(blocks []ContextBlock, budget ContextBudget) dynamicBundle {
	byCat := map[BlockType][]ContextBlock{}
	for _, b := range blocks {
		byCat[b.Type] = append(byCat[b.Type], b)
	}
	var out []ContextBlock
	// State 类
	out = append(out, c.truncateByBudget(c.estimateAll(byCat[TypeAgentState]), budget.State, true)...)
	out = append(out, c.truncateByBudget(c.estimateAll(byCat[TypeWorldState]), budget.State, true)...)
	// Event 类
	out = append(out, c.truncateByBudget(c.estimateAll(byCat[TypeEvent]), budget.WorldEvent, true)...)
	// Retrieved 类
	out = append(out, c.truncateByBudget(c.estimateAll(byCat[TypeRetrieved]), budget.Retrieved, true)...)
	return dynamicBundle{
		blocks:    out,
		total:     sumTokens(out),
		state:     sumTokensByType(out, TypeAgentState) + sumTokensByType(out, TypeWorldState),
		retrieved: sumTokensByType(out, TypeRetrieved),
		event:     sumTokensByType(out, TypeEvent),
	}
}

// buildDecision 依据 Intent + 候选行动生成 Decision Context 块（M8.4）。
// 这正是"Intent 进入 Runtime"的体现：不同 Intent 产生明显不同的 Decision 内容。
func (c *Compiler) buildDecision(intent *DecisionIntent, cands []*CandidateAction) []ContextBlock {
	var blocks []ContextBlock

	// 意图块（始终存在，说明"这次在决策什么"）。
	if intent != nil {
		var sb strings.Builder
		sb.WriteString("当前意图: ")
		sb.WriteString(intent.Type)
		if intent.SkillID != "" {
			sb.WriteString(" / 技能: " + intent.SkillID)
		}
		if intent.TargetAgentID != "" {
			sb.WriteString(" / 目标 Agent: " + intent.TargetAgentID)
		}
		if intent.Complexity > 0 {
			sb.WriteString(fmt.Sprintf(" / 复杂度: %d", intent.Complexity))
		}
		blocks = append(blocks, ContextBlock{
			ID:      "decision.intent",
			Type:    TypeDecision,
			Source:  "decision.intent",
			Content: sb.String(),
			Priority: 100,
			Stable:  false,
		})
	}

	// 候选行动块：把 Planner 的候选结构化呈现给 LLM。
	if len(cands) > 0 {
		var sb strings.Builder
		sb.WriteString("候选行动:\n")
		for i, cd := range cands {
			sb.WriteString(fmt.Sprintf("  %d. [%s] %s", i+1, cd.Action, cd.Label))
			if cd.Cost != 0 || cd.Reward != 0 {
				sb.WriteString(fmt.Sprintf(" (成本 %d, 收益 %d)", cd.Cost, cd.Reward))
			}
			if cd.Score != 0 {
				sb.WriteString(fmt.Sprintf(" 评分 %.2f", cd.Score))
			}
			if cd.Detail != "" {
				sb.WriteString(" — " + cd.Detail)
			}
			sb.WriteString("\n")
		}
		blocks = append(blocks, ContextBlock{
			ID:      "decision.candidates",
			Type:    TypeDecision,
			Source:  "decision.candidates",
			Content: sb.String(),
			Priority: 90,
			Stable:  false,
		})
	}

	return blocks
}

// estimateAll 对所有块估算并回填 Tokens。
func (c *Compiler) estimateAll(blocks []ContextBlock) []ContextBlock {
	for i := range blocks {
		blocks[i].Tokens = c.est(blocks[i].Content)
	}
	return blocks
}

// truncateByBudget 按 Priority 降序保留，累计 Token 不超 limit。
// 当 limit<=0 表示不限制该分类（极端配置），直接全留。
func (c *Compiler) truncateByBudget(blocks []ContextBlock, limit int, byPriority bool) []ContextBlock {
	if limit <= 0 {
		return blocks
	}
	if byPriority {
		// 稳定排序：Priority 大的在前（简单选择排序，块数很少无所谓）。
		for i := 0; i < len(blocks); i++ {
			max := i
			for j := i + 1; j < len(blocks); j++ {
				if blocks[j].Priority > blocks[max].Priority {
					max = j
				}
			}
			blocks[i], blocks[max] = blocks[max], blocks[i]
		}
	}
	var out []ContextBlock
	used := 0
	for _, b := range blocks {
		if used+b.Tokens > limit {
			// 不可压缩块（Stable 或 Priority≥CurrentDecision）即使超分类小预算也强制保留：
			// 它们永不压缩，宁可越过分类预算，由 MaxTotal 硬约束在末尾统一兜底。
			if !ReducePolicy(b) {
				out = append(out, b)
				used += b.Tokens
			}
			continue
		}
		out = append(out, b)
		used += b.Tokens
	}
	return out
}

// sumTokens 累加块的 Token。
func sumTokens(blocks []ContextBlock) int {
	s := 0
	for _, b := range blocks {
		s += b.Tokens
	}
	return s
}

// sumTokensByType 仅累加指定类型的块 Token（用于 RetrievedTokens 统计）。
func sumTokensByType(blocks []ContextBlock, t BlockType) int {
	s := 0
	for _, b := range blocks {
		if b.Type == t {
			s += b.Tokens
		}
	}
	return s
}

// String 把 CompiledContext 渲染成人类可读的快照（Observatory 用）。
// 这正是你说的"一打印出来就非常漂亮"的形态。
func (cc *CompiledContext) String() string {
	var sb strings.Builder
	sb.WriteString("CompiledContext\n")
	sb.WriteString(strings.Repeat("─", 32) + "\n\n")

	printGroup := func(title string, blocks []ContextBlock) {
		if len(blocks) == 0 {
			return
		}
		sb.WriteString("[" + title + "]\n")
		for _, b := range blocks {
			sb.WriteString(fmt.Sprintf("  %s\n    %d tokens\n", b.Source, b.Tokens))
		}
		sb.WriteString("\n")
	}
	printGroup("STABLE", cc.StableBlocks)
	// Dynamic 内部按语义再分组，便于肉眼看 Context 形态。
	var dynState, dynEvent, dynRetr []ContextBlock
	for _, b := range cc.DynamicBlocks {
		switch b.Type {
		case TypeAgentState, TypeWorldState:
			dynState = append(dynState, b)
		case TypeEvent:
			dynEvent = append(dynEvent, b)
		case TypeRetrieved:
			dynRetr = append(dynRetr, b)
		default:
			dynState = append(dynState, b)
		}
	}
	printGroup("STATE", dynState)
	printGroup("EVENT", dynEvent)
	printGroup("RETRIEVED", dynRetr)
	printGroup("DECISION", cc.DecisionBlocks)

	sb.WriteString(strings.Repeat("─", 32) + "\n")
	flag := ""
	if cc.TokenUsage.OverBudget {
		flag = " [OVER BUDGET!]"
	}
	sb.WriteString(fmt.Sprintf("Total: %d tokens (stable %d / dynamic %d / decision %d)%s\n",
		cc.TokenUsage.Total, cc.TokenUsage.Stable, cc.TokenUsage.Dynamic, cc.TokenUsage.Decision, flag))
	sb.WriteString(fmt.Sprintf("Budget: %d\n", cc.Budget.MaxTotal))
	return sb.String()
}
