// Package m8exp2 — Experiment 2: Real LLM + Real Memory.
//
// Question under test:
//
//	Does Context Runtime preserve agent decision quality while reducing context cost?
//
// Design (per spec):
//   - A: Full Context + same LLM
//   - B: Context Runtime (Intent -> Retrieval -> Compiler -> Adapter) + same LLM
//   - C: Rule Planner (current Economy heuristic) — control group, no LLM
//
// A and B share the SAME World State snapshot, SAME candidate actions and SAME
// LLM client; only the Context construction differs. This makes Decision
// Agreement attributable to Context, not to state drift.
//
// Boundaries (must NOT touch):
//   - worlds/economy/module.go (production planner / decision logic)
//   - M8 Context Runtime API (Compiler / Retriever / Adapter / TokenLedger)
//
// Reused only:
//   - llm.Client
//   - context.NewCompiler / OpenAICompatibleAdapter / NewMemoryRetriever /
//     AccountCompile / TokenLedger / EstimatorFromToken / RoughEstimator
//   - db.NewDBMemoryStore (real memory store)
//   - economy.Perception (data model only) + economy.EconomicOption
package m8exp2

import (
	stdctx "context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"agentworld/internal/context"
	"agentworld/internal/db"
	"agentworld/internal/llm"

	"gorm.io/gorm"
)

// tokenPrice is a coarse per-1k-token price used only for relative cost
// reporting. It intentionally matches nothing specific; the experiment reports
// raw token counts as the primary metric.
const tokenPrice = 0.0005 / 1000.0

// Experiment holds the reusable dependencies for one experiment run.
type Experiment struct {
	agentID    string // human-facing id (used for logging / identity)
	memAgentID string // int64 agent id as string — the key the Memory table uses
	llm        *llm.Client
	mem        *db.DBMemoryStore
	retriever  *context.MemoryRetriever
	est        context.TokenEstimator
	compiler   *context.Compiler
	adapter    *context.OpenAICompatibleAdapter
	candidates []*Candidate
	gdb        *gorm.DB
}

// NewExperiment wires the reused components. LLM config is read from the same
// environment variables the production runtime uses (LLM_API_KEY / LLM_BASE_URL
// / LLM_MODEL). No key is printed or persisted.
func NewExperiment(agentID string) (*Experiment, error) {
	if agentID == "" {
		agentID = "alice"
	}
	cfg := lmCfg()

	// 真实 memory store，挂在内存 SQLite 上（实验可复现、零外部依赖）。
	gdb, err := db.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("open memory db: %w", err)
	}

	// 单一 store 实例：mem 与 retriever 必须共享同一个底层库，否则写入的
	// 经验不会被检索到（原先 retriever 用了独立 store，是一处隐藏 bug）。
	sharedStore := db.NewDBMemoryStore(gdb)

	est := context.RoughEstimator
	estTok := context.EstimatorFromToken(est)
	e := &Experiment{
		agentID:    agentID,
		memAgentID: "1", // default scenario uses Perception.AgentID = 1; overridden per-snapshot below
		llm:        llm.New(cfg.baseURL, cfg.apiKey, cfg.model),
		mem:        sharedStore,
		retriever:  context.NewMemoryRetriever(sharedStore, nil),
		est:        est,
		compiler:   context.NewCompiler(estTok),
		adapter:    context.NewOpenAICompatibleAdapter(),
		candidates: defaultCandidates(),
		gdb:        gdb,
	}
	return e, nil
}

// SetMemoryAgent binds the int64 agent id (as string) used by the Memory table.
// The production Memory store keys rows by int64 agent_id, while the experiment's
// human-facing id is a name; this keeps retrieval and writes on the same key.
func (e *Experiment) SetMemoryAgent(intID int64) {
	e.memAgentID = fmt.Sprintf("%d", intID)
}

// RecordExperience writes a World-generated experience into Memory. This is the
// "World produces Experience -> Memory" step of the Agent Loop. It goes through
// the real production db.AddMemory, not a hand-fed fixture.
func (e *Experiment) RecordExperience(agentID int64, typ, content string, importance int) error {
	return db.AddMemory(e.gdb, agentID, typ, content, importance)
}

// LLMReady reports whether a real LLM is configured (key present).
func (e *Experiment) LLMReady() bool { return e.llm != nil && e.llm.Enabled() }

type lmCfgT struct{ baseURL, apiKey, model string }

func lmCfg() lmCfgT {
	apiKey := os.Getenv("LLM_API_KEY")
	baseURL := os.Getenv("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/v1"
	}
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "deepseek-chat"
	}
	return lmCfgT{baseURL: baseURL, apiKey: apiKey, model: model}
}

// Run executes the three arms on the same snapshot and returns the comparison.
// It is the single entry point used by cmd/m8exp2/main.go. nRepeat reuses the
// same snapshot multiple times (e.g. to measure LLM variance).
func (e *Experiment) Run(ctx stdctx.Context, snap *StateSnapshot, nRepeat int) (*Report, error) {
	if nRepeat < 1 {
		nRepeat = 1
	}
	// Bind the Memory key to this snapshot's int64 agent id so B's retrieval
	// matches any experiences recorded against the same agent.
	e.SetMemoryAgent(snap.Perception.AgentID)
	llmReady := e.LLMReady()

	rpt := &Report{AgentID: e.agentID, SnapshotID: snap.ID, Model: lmCfg().model}
	rpt.SkippedAB = !llmReady
	if !llmReady {
		rpt.Note = "LLM_API_KEY not set — arms A (Full) and B (Runtime) skipped; only C (Rule) ran. Set LLM_API_KEY to enable A/B."
	}

	for i := 0; i < nRepeat; i++ {
		// C — Rule Planner (no LLM), always runs.
		c := e.runRule(snap)
		rpt.Rule = append(rpt.Rule, c)

		if !llmReady {
			continue
		}

		// A — Full Context + LLM.
		a, err := e.runBaseline(ctx, snap)
		if err != nil {
			return nil, fmt.Errorf("arm A failed: %w", err)
		}
		rpt.Full = append(rpt.Full, a)

		// B — Context Runtime + LLM. It reuses A's intent so that any
		// decision divergence is attributable to Context construction, not
		// to the runtime guessing a different intent than the full agent.
		intent := intentFromDecision(a.Decision)
		b, err := e.runRuntime(ctx, snap, intent)
		if err != nil {
			return nil, fmt.Errorf("arm B failed: %w", err)
		}
		rpt.Runtime = append(rpt.Runtime, b)
	}

	rpt.Aggregate()
	return rpt, nil
}

// cost converts total tokens into a coarse relative cost number.
func cost(total int) float64 { return float64(total) * tokenPrice }

// decideText calls the LLM WITHOUT forcing JSON mode. The production
// llm.Client.Decide hardcodes response_format=json_object, which the
// deepseek-v4-flash model (the backend routes deepseek-chat to it) rejects
// with HTTP 400. This experiment path intentionally avoids JSON mode so it
// works with that model family. It reuses the same endpoint/key/model the
// production runtime reads from env, so A/B still share the SAME LLM.
func (e *Experiment) decideText(ctx stdctx.Context, system, user string) (string, error) {
	cfg := lmCfg()
	reqBody := map[string]interface{}{
		"model":       cfg.model,
		"messages":    []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
		"temperature": 0.9,
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.baseURL, "/")+"/chat/completions", strings.NewReader(string(b)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
	hc := &http.Client{Timeout: 25 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm http %d", resp.StatusCode)
	}
	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", err
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("llm empty choices")
	}
	return cr.Choices[0].Message.Content, nil
}
