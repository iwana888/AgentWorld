package m8exp2

import (
	"fmt"
	"strings"
)

// Report aggregates the three arms over all repeats of one snapshot.
type Report struct {
	AgentID    string
	SnapshotID string
	Model      string
	SkippedAB  bool
	Note       string

	Rule    []*RunResult
	Full    []*RunResult
	Runtime []*RunResult

	// Aggregated metrics (filled by Aggregate).
	AgreementRuntimeVsFull float64 // B decision == A decision (rate)
	AgreementRuleVsFull    float64 // C decision == A decision (rate)
	AvgContextFull        float64
	AvgContextRuntime     float64
	AvgTotalFull          float64
	AvgTotalRuntime       float64
	ContextReduction      float64 // AvgContextFull / AvgContextRuntime
	AvgCostFull           float64
	AvgCostRuntime        float64
	InvalidFull           int
	InvalidRuntime        int
	InvalidRule           int
}

// Aggregate computes cross-arm metrics. The key number is Decision Agreement:
// does Context Runtime keep the SAME decision as Full Context while using far
// fewer tokens?
func (r *Report) Aggregate() {
	if len(r.Full) == 0 {
		return
	}
	n := len(r.Full)

	var sumCF, sumCR, sumTF, sumTR, sumCostF, sumCostR float64
	agreeBR, agreeCR := 0, 0
	for i := range r.Full {
		sumCF += float64(r.Full[i].ContextTokens)
		sumTF += float64(r.Full[i].TotalTokens)
		sumCostF += r.Full[i].ThinkCost
		if r.Full[i].Invalid {
			r.InvalidFull++
		}

		if i < len(r.Runtime) {
			sumCR += float64(r.Runtime[i].ContextTokens)
			sumTR += float64(r.Runtime[i].TotalTokens)
			sumCostR += r.Runtime[i].ThinkCost
			if r.Runtime[i].Invalid {
				r.InvalidRuntime++
			}
			if r.Runtime[i].Decision == r.Full[i].Decision && r.Full[i].Decision != "UNKNOWN" {
				agreeBR++
			}
		}
		if i < len(r.Rule) {
			if r.Rule[i].Invalid {
				r.InvalidRule++
			}
			if r.Rule[i].Decision == r.Full[i].Decision && r.Full[i].Decision != "UNKNOWN" {
				agreeCR++
			}
		}
	}

	r.AvgContextFull = sumCF / float64(n)
	r.AvgContextRuntime = sumCR / float64(n)
	r.AvgTotalFull = sumTF / float64(n)
	r.AvgTotalRuntime = sumTR / float64(n)
	r.AvgCostFull = sumCostF / float64(n)
	r.AvgCostRuntime = sumCostR / float64(n)
	r.AgreementRuntimeVsFull = float64(agreeBR) / float64(n)
	r.AgreementRuleVsFull = float64(agreeCR) / float64(n)
	if r.AvgContextRuntime > 0 {
		r.ContextReduction = r.AvgContextFull / r.AvgContextRuntime
	}
}

// String renders a human-readable report. It never prints the LLM key.
func (r *Report) String() string {
	s := strings.Builder{}
	fmt.Fprintf(&s, "Experiment 2 — %s\n", r.Model)
	fmt.Fprintf(&s, "Snapshot: %s  Agent: %s  Repeats: %d\n", r.SnapshotID, r.AgentID, len(r.Rule))
	if r.Note != "" {
		fmt.Fprintf(&s, "\n[note] %s\n", r.Note)
	}
	s.WriteString("\n")
	fmt.Fprintf(&s, "%-18s %10s %10s %10s %10s\n", "Arm", "Context", "Total", "Cost", "Invalid")
	fmt.Fprintf(&s, "%-18s %10.0f %10.0f %10.4f %10d\n", "A Full", r.AvgContextFull, r.AvgTotalFull, r.AvgCostFull, r.InvalidFull)
	fmt.Fprintf(&s, "%-18s %10.0f %10.0f %10.4f %10d\n", "B Runtime", r.AvgContextRuntime, r.AvgTotalRuntime, r.AvgCostRuntime, r.InvalidRuntime)
	fmt.Fprintf(&s, "%-18s %10s %10s %10s %10d\n", "C Rule", "-", "-", "0.0000", r.InvalidRule)
	fmt.Fprintf(&s, "\nContext reduction (A/B): %.2fx\n", r.ContextReduction)
	fmt.Fprintf(&s, "Decision Agreement  Runtime vs Full: %.1f%%\n", r.AgreementRuntimeVsFull*100)
	fmt.Fprintf(&s, "Decision Agreement  Rule    vs Full: %.1f%%\n", r.AgreementRuleVsFull*100)
	fmt.Fprintf(&s, "\nInterpretation:\n")
	fmt.Fprintf(&s, "  Q1 Does an LLM produce reasonable Economy decisions?  -> A decisions above (non-UNKNOWN = valid).\n")
	fmt.Fprintf(&s, "  Q2 Does Context Runtime preserve LLM decision quality? -> %.1f%% agreement vs Full.\n", r.AgreementRuntimeVsFull*100)
	fmt.Fprintf(&s, "  Q3 If quality holds, how much context cost is saved?   -> %.2fx fewer context tokens.\n", r.ContextReduction)
	return s.String()
}
