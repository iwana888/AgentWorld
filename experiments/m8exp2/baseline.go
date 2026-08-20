package m8exp2

import (
	"context"
	"fmt"
	"strings"
)

// RunResult is the per-arm outcome for one snapshot execution.
type RunResult struct {
	Arm            string // "A-full" | "B-runtime" | "C-rule"
	Intent         string
	ContextTokens  int
	RetrievalTokens int
	InputTokens    int
	OutputTokens   int
	TotalTokens    int
	ThinkCost      float64
	Retrieved      int
	Decision       string // normalized decision label (WORK/HIRE/BUY/WAIT)
	DecisionText   string // raw model/rule output (no prompt, no key)
	Invalid        bool
	Err            string
}

// runBaseline is Arm A: build the FULL decision context from the perception
// (everything, no retrieval) and call the SAME LLM. This is the cost reference.
func (e *Experiment) runBaseline(ctx context.Context, snap *StateSnapshot) (*RunResult, error) {
	res := &RunResult{Arm: "A-full"}

	system, user := buildFullContext(snap)

	out, err := e.decideText(ctx, system, user)
	if err != nil {
		res.Err = err.Error()
		return res, fmt.Errorf("llm decide (full): %w", err)
	}

	res.Intent = "FULL"
	res.InputTokens = e.est.EstimateText(system + "\n" + user)
	res.OutputTokens = e.est.EstimateText(out)
	res.TotalTokens = res.InputTokens + res.OutputTokens
	res.ContextTokens = res.InputTokens
	res.ThinkCost = cost(res.TotalTokens)
	res.DecisionText = out
	res.Decision, res.Invalid = normalizeDecision(res.DecisionText, snap.Candidates)

	return res, nil
}

// buildFullContext dumps the entire perception into one prompt. This is exactly
// what a naive agent would do — load the whole world. Intentionally un-retrieved.
func buildFullContext(snap *StateSnapshot) (string, string) {
	p := snap.Perception
	var sys, usr strings.Builder
	sys.WriteString("You are an autonomous economic agent. Decide your next action based only on the world state below.")

	usr.WriteString("## World State\n")
	fmt.Fprintf(&usr, "Agent: %s\n", p.Name)
	fmt.Fprintf(&usr, "Balance: %d\n", p.Balance)
	fmt.Fprintf(&usr, "Goal: %s\n", p.Goal)
	usr.WriteString("Owned skills: ")
	sk := make([]string, 0, len(p.Skills))
	for _, s := range p.Skills {
		sk = append(sk, s.SkillID)
	}
	usr.WriteString(strings.Join(sk, ", "))
	usr.WriteString("\n\n### Available jobs\n")
	for _, j := range p.OpenJobs {
		fmt.Fprintf(&usr, " - [%d] %s reward=%d need=%s\n", j.ID, j.Title, j.Reward, j.Skill)
	}
	usr.WriteString("\n### Other agents\n")
	for id, name := range p.Names {
		fmt.Fprintf(&usr, " - [%d] %s\n", id, name)
	}
	usr.WriteString("\n## Candidate actions\n")
	for _, c := range snap.Candidates {
		fmt.Fprintf(&usr, " - %s: %s\n", c.Action, c.Content)
	}
	usr.WriteString("\nRespond with exactly one action label (do_job/hire_agent/buy_skill/wait) and a short reason.")
	return sys.String(), usr.String()
}

// normalizeDecision extracts the canonical decision label from free text.
func normalizeDecision(text string, cands []*Candidate) (string, bool) {
	up := strings.ToUpper(text)
	for _, c := range cands {
		if strings.Contains(up, strings.ToUpper(c.Action)) {
			return strings.ToUpper(c.Action), false
		}
	}
	return "UNKNOWN", true
}
