// Command m8exp2 runs Experiment 2: Real LLM + Real Memory.
//
//	Does Context Runtime preserve agent decision quality while reducing context cost?
//
// Usage:
//
//	LLM_API_KEY=sk-... [LLM_BASE_URL=...] [LLM_MODEL=...] \
//	  go run ./experiments/m8exp2/cmd/m8exp2
//
// It builds one frozen State Snapshot, then runs the three arms on it:
//
//	A Full Context   + same LLM
//	B Context Runtime + same LLM
//	C Rule Planner   (control, no LLM)
//
// A and B share snapshot + candidates + LLM, so any decision divergence is
// attributable to Context construction, not state drift.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	m8exp2 "agentworld/experiments/m8exp2"
)

func main() {
	agentID := flag.String("agent", "alice", "agent id used in the snapshot")
	repeats := flag.Int("repeat", 1, "how many times to reuse the same snapshot (LLM variance)")
	money := flag.Float64("money", 320, "agent money in the snapshot")
	exp21 := flag.Bool("exp21", false, "run Experiment 2.1 (Memory-Driven Decision: Retriever behavior + Cold/Warm loop)")
	flag.Parse()

	exp, err := m8exp2.NewExperiment(*agentID)
	if err != nil {
		fatal(err)
	}

	if !exp.LLMReady() {
		fmt.Fprintln(os.Stderr, "hint: LLM_API_KEY not set — arms A (Full) and B (Runtime) will be skipped; only C (Rule) runs.")
		fmt.Fprintln(os.Stderr, "      set LLM_API_KEY to enable the full A/B/C comparison.")
	}

	// One frozen snapshot drives all arms.
	snap := m8exp2.NewSnapshot("eco-001", *agentID,
		m8exp2.DefaultPerception(*agentID, int64(*money), []string{"courier"}, 4, 3))

	fmt.Printf("snapshot: %s\n\n", snap.SnapshotInfo())

	if *exp21 {
		fmt.Println(exp.RunMemoryExperiment21(context.Background(), snap, *repeats))
		return
	}

	rpt, err := exp.Run(context.Background(), snap, *repeats)
	if err != nil {
		fatal(err)
	}

	fmt.Println(rpt.String())
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
	os.Exit(1)
}
