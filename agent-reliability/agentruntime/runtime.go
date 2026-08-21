// Package agentruntime is the MVP3 face of the Autonomous Agent Runtime:
// a single, importable safety boundary an agent mounts in front of every
// tool call.
//
// MVP3 scope (deliberately small — see VALIDATION.md):
//
//	runtime := agentruntime.New()
//	runtime.UseReliability(agentreliability.NewGuard())
//
//	decision := runtime.Check(action)
//	switch decision.Type {
//	case agentruntime.ALLOW:
//	    runtime.Execute(decision, action, do)   // runs original action
//	case agentruntime.DENY:
//	    reject(decision)                         // nothing runs
//	case agentruntime.ASK:
//	    if approve(decision) { runtime.Execute(...) }
//	case agentruntime.MODIFY:
//	    runtime.Execute(decision, *decision.Suggested, do) // runs suggested
//	}
//
// Why this exists (product narrative):
//
//	LLM decides what an agent WANTS to do.
//	Runtime decides what it MAY SEE and MAY DO.
//
// The Reliability Runtime is framework- and model-agnostic: Claude Code, Codex,
// Cursor, enterprise / ops / finance / browser agents all share the same real
// problem — once an agent can act, who constrains it? The Runtime enforces the
// rule at execution time, so the agent "forgetting" a rule in its prompt or
// memory no longer matters.
//
// This package re-exports the core types from the `agentreliability` package so
// a consumer imports exactly one module. Context Runtime (retrieval / budget /
// compaction / provider adapter) is the planned second module; the seam is the
// UseContext method, intentionally left as a documented extension point for MVP4.
package agentruntime

import (
	"context"
	"sync"

	"agent-reliability"
)

// Re-exported core types — a consumer only needs to import agentruntime.
type (
	// Action is what an agent wants to do (world-agnostic, framework-neutral).
	Action = agentreliability.Action
	// Decision is the Runtime's verdict on an Action.
	Decision = agentreliability.Decision
	// Policy is a single, code-enforced rule.
	Policy = agentreliability.Policy
)

// Four decisions. The agent's executor is only called for ALLOW, approved ASK,
// or adopted MODIFY — never for DENY, and never with a silently rewritten action.
const (
	ALLOW  = agentreliability.Allow
	DENY   = agentreliability.Deny
	ASK    = agentreliability.Ask
	MODIFY = agentreliability.Modify
)

// DoFunc is the agent's real executor. It is invoked only when the Runtime
// permits the action. The Runtime never calls it for a blocked action.
type DoFunc func(ctx context.Context, a Action) error

// Approver decides whether an ASK may proceed. Return true to allow execution.
// The default approver always denies — ASK is safe out of the box during a trial.
type Approver func(ctx context.Context, a Action, d Decision) bool

// AuditEvent is one recorded check/execute through the Runtime. It is the raw
// material for the "who constrained the agent, and why" story during validation.
type AuditEvent struct {
	Action   Action
	Decision Decision
	Ran      bool   // did the executor actually run?
	Err      string // executor error, if any
	Note     string // free-form (e.g. "approved by human")
}

// Runtime is the MVP3 face: Reliability now; Context later.
type Runtime struct {
	rel     agentreliability.Runtime
	approve Approver
	mu      sync.Mutex
	events  []AuditEvent
	cap     int
}

// New builds an empty Runtime. Chain UseReliability (and later UseContext).
func New() *Runtime {
	return &Runtime{
		approve: func(ctx context.Context, a Action, d Decision) bool { return false },
		cap:     1000,
	}
}

// UseReliability mounts the Reliability Runtime (ALLOW/DENY/ASK/MODIFY engine).
func (r *Runtime) UseReliability(rt agentreliability.Runtime) *Runtime {
	r.rel = rt
	return r
}

// WithApprover overrides the human-approval callback for ASK decisions.
func (r *Runtime) WithApprover(a Approver) *Runtime {
	r.approve = a
	return r
}

// Check evaluates an Action through the mounted Reliability Runtime.
// No Reliability mounted -> everything is ALLOWED (fail-open, so a trial cannot
// accidentally block an agent before the user wires a guard).
func (r *Runtime) Check(ctx context.Context, a Action) Decision {
	if r.rel == nil {
		return Decision{Type: ALLOW}
	}
	return r.rel.Check(ctx, a)
}

// Execute runs the appropriate action per the Decision, via do.
//   - ALLOW / adopted MODIFY -> runs the given action (MODIFY: pass decision.Suggested)
//   - DENY  -> returns a blocked error, do is NOT called
//   - ASK   -> asks the Approver; runs only if approved
//
// Every call is recorded via Audit.
func (r *Runtime) Execute(ctx context.Context, d Decision, a Action, do DoFunc) error {
	var runErr error
	ran := false
	note := ""

	switch d.Type {
	case DENY:
		runErr = &BlockedError{Decision: d}
	case ASK:
		if r.approve != nil && r.approve(ctx, a, d) {
			ran = true
			note = "approved by human"
			runErr = do(ctx, a)
		} else {
			runErr = &BlockedError{Decision: d}
			note = "ask denied"
		}
	case MODIFY, ALLOW:
		ran = true
		runErr = do(ctx, a)
	}

	r.record(AuditEvent{
		Action: a, Decision: d, Ran: ran,
		Err: errString(runErr), Note: note,
	})
	return runErr
}

// Audit returns a copy of the recorded events (most recent last).
func (r *Runtime) Audit() []AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AuditEvent, len(r.events))
	copy(out, r.events)
	return out
}

func (r *Runtime) record(e AuditEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	if len(r.events) > r.cap {
		r.events = r.events[len(r.events)-r.cap:]
	}
}

// BlockedError is returned when the Runtime blocks an action. Nothing executed.
type BlockedError struct {
	Decision Decision
}

func (e *BlockedError) Error() string {
	if e.Decision.Reason != "" {
		return "runtime blocked (" + string(e.Decision.Type) + "): " + e.Decision.Reason
	}
	return "runtime blocked (" + string(e.Decision.Type) + ")"
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
