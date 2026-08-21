// Package agentreliability — see runtime.go.
//
// This file adds the one-line integration shim a real developer needs to mount
// the Reliability Runtime in front of an agent (Claude Code, Codex, Cursor, …)
// without forking the SDK:
//
//	rt := agentreliability.NewGuard()
//	gate := agentreliability.UseReliability(rt)
//
//	// wrap every tool call the agent wants to make:
//	err := gate.Run(ctx, agentreliability.Action{
//	    Tool:   "shell",
//	    Target: "rm -rf /",
//	}, func(ctx context.Context) error {
//	    return runTheActualTool() // your executor
//	})
//	// err != nil  -> the Runtime blocked it; nothing ran.
//
// The agent's executor is only ever called for ALLOW, approved ASK, or when the
// caller adopts a MODIFY suggestion. The Runtime never silently rewrites the
// agent's action.

package agentreliability

import "context"

// DoFunc is the agent's actual executor. It is only invoked when the Runtime
// permits the action. The Gate never calls it for a blocked action.
type DoFunc func(ctx context.Context) error

// Approver decides whether an ASK decision may proceed. Return true to allow the
// action to execute. The default approver always denies, so ASK is safe out of
// the box during a trial.
type Approver func(ctx context.Context, a Action, d Decision) bool

// Gate wraps the Runtime with a single safe entry point for an agent's tool
// calls. It is the production seam between "what the agent wants" and "what may
// run".
type Gate struct {
	rt       Runtime
	approver Approver
	// AdoptModify, when true, runs dec.Suggested instead of the original action
	// for MODIFY decisions. Default false: MODIFY is treated like DENY unless you
	// opt in — the agent's intent is never rewritten behind your back.
	AdoptModify bool
}

// UseReliability mounts the Runtime as a gate in front of an agent's actions.
// This is the one-line integration point for product validation.
func UseReliability(rt Runtime) *Gate {
	return &Gate{
		rt: rt,
		approver: func(ctx context.Context, a Action, d Decision) bool {
			return false // safe default: always require explicit approval
		},
	}
}

// WithApprover overrides the human-approval callback used for ASK decisions.
// Wire this to your real UI / CLI prompt during a trial.
func (g *Gate) WithApprover(a Approver) *Gate {
	g.approver = a
	return g
}

// Run evaluates the action through the Runtime and, only if permitted, invokes
// do. It returns:
//   - nil                         the action was allowed / approved / adopted and ran
//   - ErrDenied / ErrAskDenied    the action was blocked (nothing executed)
//   - the error from do           the action ran but the executor failed
func (g *Gate) Run(ctx context.Context, a Action, do DoFunc) error {
	dec := g.rt.Check(ctx, a)
	switch dec.Type {
	case Allow:
		return do(ctx)
	case Deny:
		return &GateError{Kind: "DENY", Decision: dec}
	case Ask:
		if g.approver != nil && g.approver(ctx, a, dec) {
			return do(ctx)
		}
		return &GateError{Kind: "ASK_DENIED", Decision: dec}
	case Modify:
		if g.AdoptModify && dec.Suggested != nil {
			return do(ctx) // caller opted in; run the suggested action
		}
		return &GateError{Kind: "MODIFY", Decision: dec}
	default:
		return do(ctx)
	}
}

// GateError is returned when the Runtime blocks an action. Nothing was executed.
type GateError struct {
	Kind     string
	Decision Decision
}

func (e *GateError) Error() string {
	if e.Decision.Reason != "" {
		return "reliability gate blocked (" + e.Kind + "): " + e.Decision.Reason
	}
	return "reliability gate blocked (" + e.Kind + ")"
}
