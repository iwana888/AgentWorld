// Package m8 implements the M8 first experiment round.
//
// Goal of this round: measure what the Context Runtime ITSELF produces.
// No real LLM is called. Both Baseline and Context paths share the SAME
// injectable TokenEstimator, so the only variable being compared is whether
// the Context Runtime (Retrieve -> Compile -> Compact -> Adapt) sits between
// Perception and the TokenEstimator.
//
//   Baseline:  Economy Perception -> raw prompt -> TokenEstimator
//   Context :  Economy Perception -> Context Runtime -> CompiledContext
//              -> Adapter -> TokenEstimator
//
// Memory is synthetic and controlled (see context.SyntheticMemoryStore), so we
// can assert Intent -> Retrieval mapping precisely. This round does NOT touch
// the frozen M8 API and does NOT modify Economy decision logic.
package m8

import (
	stdctx "context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"agentworld/internal/context"
	"agentworld/internal/llm"
	"agentworld/internal/skill"
	"agentworld/sdk"
	economy "agentworld/worlds/economy/economy"
)

// contextStdCtx returns a background context for the standard library.
func contextStdCtx() stdctx.Context {
	return stdctx.Background()
}

// ThinkMetrics is the per-Think record produced by both paths. It mirrors the
// experiment design: Runtime Context tokens and Provider Input tokens are kept
// as separate dimensions (M8 already split them correctly).
type ThinkMetrics struct {
	// Routing
	Intent string
	Action string

	// M8 Runtime Context tokens (what Context Runtime assembled)
	StableTokens     int
	StateTokens      int
	RetrievedTokens  int
	EventTokens      int
	DecisionTokens   int
	ContextTokens    int // = total Runtime Context tokens
	CompactedTokens  int // tokens removed by compaction (0 in round 1 expected)

	// Provider layer tokens (what the Adapter produced, fed to TokenEstimator)
	InputTokens  int
	OutputTokens int // not produced this round (no LLM); recorded as 0
	TotalTokens  int

	// Retrieval evidence
	RetrievedMemoryCount   int
	RetrievedMemoryTypes   []string
	TotalMemoryCount       int

	// KV-Cache readiness
	StablePrefixHash string

	// Cost/Think split (per experiment design: keep Context/Provider/Output/Total)
	ContextCost       int
	ProviderInputCost int
	OutputCost        int
	TotalCost         int
}

// Experiment drives one A/B run for a fixed number of Thinks.
type Experiment struct {
	est  context.TokenEstimator
	mem  *context.SyntheticMemoryStore
	comp *context.Compiler
	adp  context.ContextAdapter

	// agent identity used to build synthetic Perceptions
	agentID    int64
	agentIDStr string // must match SyntheticMemoryStore.AgentID for retrieval
	agentName  string
	profession string
	personality string
	goal     string

	// intent schedule: which intent each Think uses (for controlled distribution)
	intentPlan []string
}

// NewExperiment builds an experiment with the given TokenEstimator and synthetic
// memory config. estimator is injected (RoughEstimator now, real tokenizers later).
func NewExperiment(est context.TokenEstimator, memCfg context.SyntheticConfig, agentName, profession, personality, goal string, intentPlan []string) *Experiment {
	mem := context.NewSyntheticMemoryStore(memCfg)
	comp := context.NewCompiler(context.EstimatorFromToken(est))
	adp := context.NewOpenAICompatibleAdapter()
	return &Experiment{
		est:        est,
		mem:        mem,
		comp:       comp,
		adp:        adp,
		agentID:    1,
		agentIDStr: memCfg.AgentID,
		agentName:  agentName,
		profession: profession,
		personality: personality,
		goal:       goal,
		intentPlan: intentPlan,
	}
}

// buildPerception synthesizes an economy.Perception for the given intent. We do
// NOT replay a real World; we want controlled, reproducible Intent inputs.
func (e *Experiment) buildPerception(intent string) *economy.Perception {
	p := &economy.Perception{
		AgentID:     e.agentID,
		Name:        e.agentName,
		Profession:  e.profession,
		Personality: e.personality,
		Goal:        e.goal,
		Balance:     120,
		Inventory:   map[string]int{},
		Skills:      []skill.AgentSkill{{SkillID: "engineer", Level: 3}},
		WealthRank:  1,
		AgentCount:  5,
		TotalWealth: 600,
	}
	if intent == "HIRE_AGENT" {
		p.Balance = 300 // richer -> more likely to hire
		p.OpenJobs = nil
	} else {
		p.OpenJobs = []economy.JobPublic{{ID: 1, Title: "collect_data", Reward: 8, Skill: "courier"}}
	}
	return p
}

// buildDecision synthesizes a Decision whose Action matches the intent. The
// Decision itself is NOT used for Economy logic here; it only drives Intent
// classification and the Decision block content.
func (e *Experiment) buildDecision(intent string) *sdk.Decision {
	if intent == "HIRE_AGENT" {
		return &sdk.Decision{Action: "hire_agent", Target: 2, Content: "collect_data", Reason: "缺 courier 技能，雇人代做更划算"}
	}
	return &sdk.Decision{Action: "do_job", Target: 1, Content: "collect_data", Reason: "有可做的 courier 工作，先赚眼前钱"}
}

// buildContextRequest mirrors worlds/economy observeContext block composition
// (Stable/State/Event) but is self-contained: it does not depend on the
// unexported planner, and it injects the synthetic retriever. This is the
// Context path input.
func (e *Experiment) buildContextRequest(perc *economy.Perception, dec *sdk.Decision, intent *context.DecisionIntent) *context.ContextRequest {
	stable := []context.ContextBlock{
		{ID: "world.rules", Type: context.TypeWorldRules, Source: "world.rules",
			Content: "经济世界规则：打工赚币、市场买技能、劳动力市场雇人、余额决定投资能力。", Priority: 100, Stable: true},
		{ID: "agent.identity", Type: context.TypeAgentIdentity, Source: "agent.identity",
			Content: fmt.Sprintf("名字: %s 职业: %s ID: %d", perc.Name, perc.Profession, perc.AgentID), Priority: 95, Stable: true},
		{ID: "agent.personality", Type: context.TypePersonality, Source: "agent.personality",
			Content: "性格: " + perc.Personality, Priority: 90, Stable: true},
	}
	for _, s := range perc.Skills {
		stable = append(stable, context.ContextBlock{
			ID: "skill:" + s.SkillID, Type: context.TypeSkill, Source: "skill." + s.SkillID,
			Content: fmt.Sprintf("技能: %s (Lv%d)", s.SkillID, s.Level), Priority: 80, Stable: true,
		})
	}
	dyn := []context.ContextBlock{
		{ID: "agent.state", Type: context.TypeAgentState, Source: "agent.state", Priority: 70, Stable: false,
			Content: fmt.Sprintf("余额: %d coins 财富排名: %d/%d 目标: %s", perc.Balance, perc.WealthRank, perc.AgentCount, perc.Goal)},
	}
	if len(perc.OpenJobs) > 0 {
		dyn = append(dyn, context.ContextBlock{
			ID: "recent.events", Type: context.TypeEvent, Source: "recent.events", Priority: 50, Stable: false,
			Content: fmt.Sprintf("市场开放工作数: %d", len(perc.OpenJobs)),
		})
	}
	retriever := context.NewMemoryRetriever(e.mem, context.EstimatorFromToken(e.est))
	req := &context.ContextRequest{
		AgentID:        e.agentIDStr,
		AgentState:     &context.AgentState{AgentID: e.agentIDStr, Balance: int(perc.Balance), Location: "economy", Goal: perc.Goal, Intent: intent.Type},
		DecisionIntent: intent,
		StableBlocks:   stable,
		DynamicBlocks:  dyn,
		Retriever:      retriever,
	}
	return req
}

// buildBaselinePrompt builds the "raw prompt" for the Baseline path: the same
// text blocks concatenated in order, with NO Context Runtime intervention.
func (e *Experiment) buildBaselinePrompt(perc *economy.Perception, dec *sdk.Decision) string {
	var b strings.Builder
	b.WriteString("经济世界规则：打工赚币、市场买技能、劳动力市场雇人、余额决定投资能力。\n")
	b.WriteString(fmt.Sprintf("名字: %s 职业: %s ID: %d\n", perc.Name, perc.Profession, perc.AgentID))
	b.WriteString("性格: " + perc.Personality + "\n")
	for _, s := range perc.Skills {
		b.WriteString(fmt.Sprintf("技能: %s (Lv%d)\n", s.SkillID, s.Level))
	}
	b.WriteString(fmt.Sprintf("余额: %d coins 财富排名: %d/%d 目标: %s\n", perc.Balance, perc.WealthRank, perc.AgentCount, perc.Goal))
	if len(perc.OpenJobs) > 0 {
		b.WriteString(fmt.Sprintf("市场开放工作数: %d\n", len(perc.OpenJobs)))
	}
	// Baseline also dumps ALL memory text (it has no retriever to filter).
	for _, r := range e.mem.All() {
		b.WriteString(r.Content + "\n")
	}
	b.WriteString(fmt.Sprintf("决策: %s %s\n", dec.Action, dec.Reason))
	return b.String()
}

// decisionIntent builds the DecisionIntent directly from the controlled plan.
// (There is no external IntentAnalyzer in the codebase; the Economy World infers
// intent internally. For a controlled experiment we set it from the schedule so
// the Intent distribution is reproducible and A/B comparable.)
func (e *Experiment) decisionIntent(intentType string) *context.DecisionIntent {
	di := &context.DecisionIntent{Type: intentType}
	if intentType == "HIRE_AGENT" {
		di.TargetAgentID = "2"
		di.SkillID = "courier"
	}
	return di
}

// RunContext executes one Context-path Think and returns its metrics.
func (e *Experiment) RunContext(i int) (ThinkMetrics, error) {
	intentType := e.intentPlan[i%len(e.intentPlan)]
	perc := e.buildPerception(intentType)
	dec := e.buildDecision(intentType)
	intent := e.decisionIntent(intentType)

	req := e.buildContextRequest(perc, dec, intent)
	cc, err := e.comp.Compile(contextStdCtx(), req)
	if err != nil {
		return ThinkMetrics{}, err
	}

	msgs, err := e.adp.CompileMessages(contextStdCtx(), cc)
	if err != nil {
		return ThinkMetrics{}, err
	}
	inputTokens := e.est.EstimateMessages(msgs)

	retrieved := retrievedBlocks(cc)
	m := ThinkMetrics{
		Intent:     intent.Type,
		Action:     dec.Action,
		StableTokens:     cc.TokenUsage.StableTokens,
		StateTokens:      cc.TokenUsage.StateTokens,
		RetrievedTokens:  cc.TokenUsage.RetrievedTokens,
		EventTokens:      cc.TokenUsage.EventTokens,
		DecisionTokens:   cc.TokenUsage.DecisionTokens,
		ContextTokens:    cc.TokenUsage.ContextTokens,
		CompactedTokens:  cc.TokenUsage.CompactedTokens,
		InputTokens:      inputTokens,
		TotalTokens:      inputTokens,
		RetrievedMemoryCount: len(retrieved),
		TotalMemoryCount:     len(e.mem.All()),
		StablePrefixHash: hashSystemPrefix(msgs),
		ContextCost:    cc.TokenUsage.ContextTokens,
		ProviderInputCost: inputTokens,
		TotalCost:      inputTokens,
	}
	for _, r := range retrieved {
		// r.Source is "retrieved.<originalMemoryType>" (set by MemoryRetriever);
		// the original memory type is what we assert against Intent.
		m.RetrievedMemoryTypes = append(m.RetrievedMemoryTypes, strings.TrimPrefix(r.Source, "retrieved."))
	}
	return m, nil
}

// retrievedBlocks extracts the Retriever-injected blocks from a CompiledContext.
func retrievedBlocks(cc *context.CompiledContext) []context.ContextBlock {
	var out []context.ContextBlock
	for _, b := range cc.DynamicBlocks {
		if b.Type == context.TypeRetrieved {
			out = append(out, b)
		}
	}
	return out
}

// RunBaseline executes one Baseline-path Think and returns its metrics.
func (e *Experiment) RunBaseline(i int) (ThinkMetrics, error) {
	intentType := e.intentPlan[i%len(e.intentPlan)]
	perc := e.buildPerception(intentType)
	dec := e.buildDecision(intentType)
	intent := e.decisionIntent(intentType)

	raw := e.buildBaselinePrompt(perc, dec)
	inputTokens := e.est.EstimateText(raw)

	m := ThinkMetrics{
		Intent:     intent.Type,
		Action:     dec.Action,
		// Runtime Context breakdown is unavailable in Baseline (no Runtime).
		ContextTokens:   0,
		InputTokens:     inputTokens,
		TotalTokens:     inputTokens,
		RetrievedMemoryCount: len(e.mem.All()), // Baseline dumps everything
		TotalMemoryCount:     len(e.mem.All()),
		StablePrefixHash: hashText(raw),
		ContextCost:       0,
		ProviderInputCost: inputTokens,
		TotalCost:         inputTokens,
	}
	for _, r := range e.mem.All() {
		m.RetrievedMemoryTypes = append(m.RetrievedMemoryTypes, r.Type)
	}
	return m, nil
}

// hashSystemPrefix hashes the system message content (Stable Prefix). Ideal
// result across all Thinks is unique count == 1.
func hashSystemPrefix(msgs []llm.Message) string {
	for _, m := range msgs {
		if m.Role == "system" {
			return hashText(m.Content)
		}
	}
	return ""
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// uniqueCounts returns a map of value -> occurrence count.
func uniqueCounts[T comparable](vals []T) map[T]int {
	m := map[T]int{}
	for _, v := range vals {
		m[v]++
	}
	return m
}

// sortedUnique returns the number of distinct values in vals.
func sortedUnique[T comparable](vals []T) int {
	return len(uniqueCounts(vals))
}

// percentile computes the p-th percentile (0..100) of a sorted-required slice.
func percentile(vals []int, p float64) int {
	if len(vals) == 0 {
		return 0
	}
	s := append([]int(nil), vals...)
	sort.Ints(s)
	idx := int(p / 100 * float64(len(s)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx]
}
