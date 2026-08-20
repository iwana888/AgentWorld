package m8exp2

import (
	"fmt"
	"strings"

	stdctx "context"

	"agentworld/internal/context"
	"agentworld/internal/skill"
)

// runRuntime is Arm B: the Context Runtime pipeline.
//
//	Perception -> Intent -> Retrieval -> Compiler -> Adapter -> same LLM -> Decision
//
// It uses the SAME llm.Client as Arm A. Only the Context is constructed
// differently: intent-driven retrieval + compiled context instead of a full dump.
func (e *Experiment) runRuntime(ctx stdctx.Context, snap *StateSnapshot, intent *context.DecisionIntent) (*RunResult, error) {
	res := &RunResult{Arm: "B-runtime"}

	// 1) Intent — passed in from Arm A (same decision view), so B does not
	// guess a different intent than the full-context agent.

	// 2) Build the ContextRequest for the frozen M8 Compiler.
	req := e.buildRuntimeRequest(snap, intent)

	// 3) Compile + Retrieval (Retriever is injected in the request).
	cc, err := e.compiler.Compile(stdctx.Background(), req)
	if err != nil {
		res.Err = err.Error()
		return res, fmt.Errorf("compile (runtime): %w", err)
	}

	// 4) Adapter -> provider messages.
	msgs, err := e.adapter.CompileMessages(stdctx.Background(), cc)
	if err != nil {
		res.Err = err.Error()
		return res, fmt.Errorf("adapt (runtime): %w", err)
	}

	var system, user string
	for _, m := range msgs {
		if m.Role == "system" {
			system = m.Content
		} else {
			user += m.Content + "\n"
		}
	}

	res.Intent = intent.Type
	res.ContextTokens = cc.TokenUsage.ContextTokens
	res.RetrievalTokens = cc.TokenUsage.RetrievedTokens
	res.Retrieved = retrievedCount(cc) // number of memory entries surfaced (not de-duplicated types)

	// 5) same LLM (non-JSON mode; v4-flash rejects json_object).
	out, err := e.decideText(ctx, system, user)
	if err != nil {
		res.Err = err.Error()
		return res, fmt.Errorf("llm decide (runtime): %w", err)
	}

	res.InputTokens = e.est.EstimateMessages(msgs)
	res.OutputTokens = e.est.EstimateText(out)
	res.TotalTokens = res.InputTokens + res.OutputTokens
	res.ThinkCost = cost(res.TotalTokens)
	// Account for the runtime-context actual send cost (Provider [B] side).
	context.AccountCompile(cc, e.agentID, snap.ID)

	res.DecisionText = out
	res.Decision, res.Invalid = normalizeDecision(res.DecisionText, snap.Candidates)

	return res, nil
}

// decideIntent maps the perception to a single intent (used only as a
// fallback when Arm A did not run).
func (e *Experiment) decideIntent(snap *StateSnapshot) *context.DecisionIntent {
	p := snap.Perception
	// Hire when cash is healthy and other agents are available; else work.
	if p.Balance >= 200 && len(p.Names) > 0 {
		return &context.DecisionIntent{Type: "HIRE_AGENT", TargetAgentID: "agent-0", SkillID: "courier"}
	}
	return &context.DecisionIntent{Type: "WORK"}
}

// intentFromDecision derives the runtime intent from Arm A's decision so that
// B reasons about the SAME intent A chose. This removes intent-guessing as a
// confound: A/B divergence is then attributable to Context, not to intent.
func intentFromDecision(decision string) *context.DecisionIntent {
	if decision == "HIRE_AGENT" {
		return &context.DecisionIntent{Type: "HIRE_AGENT", TargetAgentID: "agent-0", SkillID: "courier"}
	}
	return &context.DecisionIntent{Type: "WORK"}
}

// buildRuntimeRequest assembles the frozen-M8 ContextRequest from the snapshot.
func (e *Experiment) buildRuntimeRequest(snap *StateSnapshot, intent *context.DecisionIntent) *context.ContextRequest {
	stable := []context.ContextBlock{
		{ID: "runtime.rules", Type: context.TypeRuntimeRules, Source: "runtime.rules", Priority: 100, Stable: true,
			Content: "AgentWorld runtime: agents perceive, form intent, retrieve, decide, act."},
		{ID: "agent.identity", Type: context.TypeAgentIdentity, Source: "agent.identity", Priority: 100, Stable: true,
			Content: fmt.Sprintf("Agent: %s", snap.Perception.Name)},
	}
	var dyn []context.ContextBlock
	dyn = append(dyn, context.ContextBlock{
		ID: "agent.state", Type: context.TypeAgentState, Source: "agent.state", Priority: 90, Stable: false,
		Content: fmt.Sprintf("Balance: %d skills: %s", snap.Perception.Balance, skillList(snap.Perception.Skills)),
	})

	cands := make([]*context.CandidateAction, 0, len(snap.Candidates))
	for _, c := range snap.Candidates {
		cands = append(cands, &context.CandidateAction{ID: c.Action, Action: c.Action, Label: c.Action, Detail: c.Content})
	}

	return &context.ContextRequest{
		AgentID:         e.memAgentID,
		AgentState:      &context.AgentState{AgentID: e.agentID, Balance: int(snap.Perception.Balance), Location: "economy", Intent: intent.Type},
		DecisionIntent:  intent,
		CandidateActions: cands,
		StableBlocks:    stable,
		DynamicBlocks:   dyn,
		Retriever:       e.retriever,
	}
}

// skillList renders owned skills as a comma string.
func skillList(sk []skill.AgentSkill) string {
	parts := make([]string, 0, len(sk))
	for _, s := range sk {
		parts = append(parts, s.SkillID)
	}
	return strings.Join(parts, ",")
}

// retrievedTypes returns the memory types the retriever surfaced, for reporting.
func retrievedTypes(cc *context.CompiledContext) []string {
	seen := map[string]bool{}
	var out []string
	all := append(append([]context.ContextBlock{}, cc.StableBlocks...), cc.DynamicBlocks...)
	for _, blk := range all {
		if blk.Type == context.TypeRetrieved {
			t := strings.TrimPrefix(blk.Source, "retrieved.")
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// retrievedCount returns the number of memory entries (blocks) the retriever
// surfaced — distinct from retrievedTypes, which de-duplicates by type.
func retrievedCount(cc *context.CompiledContext) int {
	n := 0
	all := append(append([]context.ContextBlock{}, cc.StableBlocks...), cc.DynamicBlocks...)
	for _, blk := range all {
		if blk.Type == context.TypeRetrieved {
			n++
		}
	}
	return n
}
