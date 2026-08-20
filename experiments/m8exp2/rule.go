package m8exp2

import (
	"agentworld/worlds/economy/economy"
)

// runRule is Arm C: the current Economy rule-based planner, reimplemented as a
// minimal control group. It does NOT import worlds/economy/module.go (its
// planner/decideEconomically are unexported) and does NOT call the LLM. The
// logic here mirrors the documented Economy heuristic (work when short on cash
// or skills; hire when cash is healthy and workers exist; buy a missing skill;
// otherwise wait) so the experiment stays self-contained and reproducible.
func (e *Experiment) runRule(snap *StateSnapshot) *RunResult {
	res := &RunResult{Arm: "C-rule", Intent: "RULE"}

	p := snap.Perception
	label, invalid := ruleDecide(p)
	res.Decision = label
	res.Invalid = invalid
	res.DecisionText = "rule: " + label
	return res
}

// ruleDecide reproduces the Economy planner preference order.
func ruleDecide(p economy.Perception) (string, bool) {
	hasCourier := false
	for _, s := range p.Skills {
		if s.SkillID == "courier" {
			hasCourier = true
		}
	}
	switch {
	case p.Balance < 200 && len(p.OpenJobs) > 0:
		return "WORK", false
	case !hasCourier && p.Balance >= 100:
		return "BUY", false
	case p.Balance >= 200 && len(p.Names) > 0:
		return "HIRE", false
	case len(p.OpenJobs) > 0:
		return "WORK", false
	default:
		return "WAIT", false
	}
}
