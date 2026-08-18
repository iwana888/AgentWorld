package context

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// M8 Experiment: SyntheticMemoryStore is an in-memory, deterministic
// implementation of MemoryStore used by the first experiment round.
//
// Rationale (per experiment design): we are NOT studying an Agent's real
// behavioral history. We are validating Intent -> Retrieval -> Context. So we
// generate controlled, reproducible data instead of dumping real runs.
//
// For a subject agent "Alice" it produces:
//   - WORK-related:   work / self / skill_exp
//   - HIRE_AGENT-related: hire / about_agent / contract
//   - NOISE: a large number of unrelated memories
//
// This makes it trivial to assert that WORK intent retrieves work/self/skill_exp
// and HIRE_AGENT retrieves hire/about_agent/contract, and to measure
// Retrieved / Total memory ratio.

// SyntheticConfig controls how many memories of each kind are generated.
type SyntheticConfig struct {
	AgentID       string
	AboutAgentID  string // the agent whose "about_agent" memories we synthesize
	WorkPerType   int    // memories per WORK-related subtype
	HirePerType   int    // memories per HIRE_AGENT-related subtype
	NoiseCount    int    // unrelated memories
	ImportanceMax int    // max importance for noise (keep low so relevant wins)
	SeedBase      int64  // base timestamp offset for deterministic ordering
}

// DefaultSyntheticConfig returns the canonical experiment dataset:
// 5 relevant per subtype, 100 unrelated noise, subject "Alice",
// about-agent target "Bob".
func DefaultSyntheticConfig() SyntheticConfig {
	return SyntheticConfig{
		AgentID:       "alice",
		AboutAgentID:  "bob",
		WorkPerType:   5,
		HirePerType:   5,
		NoiseCount:    100,
		ImportanceMax: 3,
		SeedBase:      1_700_000_000,
	}
}

// SyntheticMemoryStore is a deterministic in-memory MemoryStore.
type SyntheticMemoryStore struct {
	rows []MemoryRow
	cfg  SyntheticConfig
}

var _ MemoryStore = (*SyntheticMemoryStore)(nil)

// NewSyntheticMemoryStore builds the synthetic dataset for the given config.
func NewSyntheticMemoryStore(cfg SyntheticConfig) *SyntheticMemoryStore {
	s := &SyntheticMemoryStore{cfg: cfg}
	s.generate()
	return s
}

// generate fills the in-memory row set deterministically.
func (s *SyntheticMemoryStore) generate() {
	var id int64
	add := func(agentID, typ, content string, importance int, created time.Time) {
		id++
		s.rows = append(s.rows, MemoryRow{
			ID:         id,
			AgentID:    agentID,
			Type:       typ,
			Content:    content,
			Importance: importance,
			CreatedAt:  created,
		})
	}

	base := time.Unix(s.cfg.SeedBase, 0)
	// WORK-related: high importance, increasing recency so they rank first.
	workTypes := []string{"work", "self", "skill_exp"}
	for i, t := range workTypes {
		for j := 0; j < s.cfg.WorkPerType; j++ {
			created := base.Add(time.Duration(i*10+j) * time.Minute)
			add(s.cfg.AgentID, t,
				fmt.Sprintf("[%s] Alice %s memory #%d: completed task and earned reward", t, t, j+1),
				8+((i+j)%3), // 8..10 importance
				created)
		}
	}

	// HIRE_AGENT-related: hire / about_agent / contract.
	hireTypes := []string{"hire", "about_agent", "contract"}
	for i, t := range hireTypes {
		for j := 0; j < s.cfg.HirePerType; j++ {
			created := base.Add(time.Duration(100+i*10+j) * time.Minute)
			if t == "about_agent" {
				// about_agent memories are about the target agent, owned by Alice.
				add(s.cfg.AgentID, t,
					fmt.Sprintf("[about_agent] Alice's note on %s: reputation %d, success rate high", s.cfg.AboutAgentID, 70+j*2),
					7+((i+j)%3),
					created)
			} else {
				add(s.cfg.AgentID, t,
					fmt.Sprintf("[%s] Alice %s record #%d: contract negotiated with worker", t, t, j+1),
					7+((i+j)%3),
					created)
			}
		}
	}

	// NOISE: unrelated memories, low importance, spread across random types.
	noiseTypes := []string{"chitchat", "weather", "gossip", "log", "misc"}
	for j := 0; j < s.cfg.NoiseCount; j++ {
		t := noiseTypes[j%len(noiseTypes)]
		created := base.Add(time.Duration(500+j) * time.Minute)
		importance := 1 + (j % s.cfg.ImportanceMax)
		add(s.cfg.AgentID, t,
			fmt.Sprintf("[%s] unrelated background event %d with no decision value", t, j+1),
			importance,
			created)
	}
}

// QueryMemories implements MemoryStore. It filters by agent + types, appends
// about_agent rows targeting aboutAgentID when provided, then sorts by
// importance desc, created_at desc and truncates to limit.
func (s *SyntheticMemoryStore) QueryMemories(ctx context.Context, agentID string, types []string, aboutAgentID string, limit int) ([]MemoryRow, error) {
	if agentID != s.cfg.AgentID {
		return nil, nil
	}
	typeSet := make(map[string]struct{}, len(types))
	for _, t := range types {
		typeSet[t] = struct{}{}
	}

	var out []MemoryRow
	for _, r := range s.rows {
		if r.AgentID != agentID {
			continue
		}
		if _, ok := typeSet[r.Type]; ok {
			out = append(out, r)
			continue
		}
		// about_agent rows about the target agent are included when requested.
		if aboutAgentID != "" && r.Type == "about_agent" {
			out = append(out, r)
		}
	}

	sort.SliceStable(out, func(i, k int) bool {
		if out[i].Importance != out[k].Importance {
			return out[i].Importance > out[k].Importance
		}
		return out[i].CreatedAt.After(out[k].CreatedAt)
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// All returns the full synthetic row set (used by the experiment to compute the
// total memory size for Retrieval/Total ratio).
func (s *SyntheticMemoryStore) All() []MemoryRow {
	return append([]MemoryRow(nil), s.rows...)
}
