package m8exp2

import (
	"fmt"
	"strings"

	"agentworld/internal/skill"
	"agentworld/worlds/economy/economy"
)

// Candidate is the experiment-side action candidate shared by arms A and B.
// It mirrors the Economy action space so the LLM sees the same choices in both
// arms. Defined locally because worlds/economy's EconomicOption is not exported.
type Candidate struct {
	Action  string // do_job | hire_agent | buy_skill | wait
	Content string
	Score   float64
}

// StateSnapshot captures a frozen World state + Perception for one Agent.
// A/B/C all consume the SAME snapshot, so any decision divergence is caused
// by the Context construction, not by state drift.
type StateSnapshot struct {
	ID         string
	AgentID    string
	Perception economy.Perception
	Candidates []*Candidate
}

// NewSnapshot builds a deterministic snapshot from an Economy perception.
// id lets the caller reproduce / replay the exact same world state.
func NewSnapshot(id, agentID string, p economy.Perception) *StateSnapshot {
	return &StateSnapshot{
		ID:         id,
		AgentID:    agentID,
		Perception: p,
		Candidates: defaultCandidates(),
	}
}

// defaultCandidates returns the fixed candidate action set the experiment
// presents to the LLM (shared by A and B).
func defaultCandidates() []*Candidate {
	return []*Candidate{
		{Action: "do_job", Content: "Take on available jobs to earn money.", Score: 0.9},
		{Action: "hire_agent", Content: "Hire another agent for a contract.", Score: 0.7},
		{Action: "buy_skill", Content: "Purchase a needed skill.", Score: 0.6},
		{Action: "wait", Content: "Do nothing this turn, observe the market.", Score: 0.4},
	}
}

// SnapshotInfo renders a short, key-only view of the snapshot for logging. It
// never includes the LLM key or full prompt text.
func (s *StateSnapshot) SnapshotInfo() string {
	p := s.Perception
	var b strings.Builder
	fmt.Fprintf(&b, "snapshot=%s agent=%s balance=%d skills=%d jobs=%d others=%d",
		s.ID, s.AgentID, p.Balance, len(p.Skills), len(p.OpenJobs), len(p.Names))
	return b.String()
}

// DefaultPerception builds a representative Economy perception for the agent.
// In a real run this would come from worlds/economy; here it is synthesized so
// the experiment is self-contained and reproducible without a live World server.
// It uses the REAL economy.Perception field names so the snapshot is faithful
// to the Economy data model.
func DefaultPerception(agentID string, balance int64, owned []string, jobs int, others int) economy.Perception {
	p := economy.Perception{
		AgentID:    1, // snapshot agent
		Name:       agentID,
		Profession: "freelancer",
		Personality: "cautious but ambitious",
		Goal:       "grow capital and reputation",
		Balance:    balance,
		Skills:     nil,
		Names:      map[int64]string{},
	}
	for _, s := range owned {
		p.Skills = append(p.Skills, skill.AgentSkill{SkillID: s, Level: 3})
	}
	p.OpenJobs = make([]economy.JobPublic, 0, jobs)
	for i := 0; i < jobs; i++ {
		p.OpenJobs = append(p.OpenJobs, economy.JobPublic{
			ID:       int64(i),
			Title:    fmt.Sprintf("Task %d", i),
			Reward:   100 + int64(i)*10,
			Skill:    "courier",
			MinLevel: 1,
		})
	}
	for i := 0; i < others; i++ {
		id := int64(100 + i)
		p.Names[id] = fmt.Sprintf("Agent-%d", i)
	}
	return p
}
