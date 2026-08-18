// Command m8 runs the M8 first experiment round.
//
// It compares two configurations over N Thinks, WITHOUT calling any real LLM:
//
//	Baseline : Economy Perception -> raw prompt -> TokenEstimator
//	Context  : Economy Perception -> Context Runtime -> Adapter -> TokenEstimator
//
// Both use the SAME injected TokenEstimator (RoughEstimator now; real tokenizers
// later). This isolates the Context Runtime as the only variable.
//
// Memory is synthetic and controlled, so Intent -> Retrieval is fully asserted.
//
// Run phases:
//   Phase A: 100 Thinks per path — validates the experiment itself (data
//            completeness, estimator, retriever, intent spread, ledger, no
//            anomalous tokens, no over-budget, stable prefix truly stable).
//   Phase B: 1000 Thinks per path — produces the final comparison tables.
//
// This program does NOT modify the frozen M8 API and does NOT modify Economy
// decision logic.
package main

import (
	"fmt"
	"os"
	"sort"

	m8 "agentworld/experiments/m8"
	"agentworld/internal/context"
)

func main() {
	fmt.Println("=== M8 Experiment Round 1: Context Runtime A/B (no real LLM) ===")

	est := context.RoughEstimator
	memCfg := context.DefaultSyntheticConfig()

	// Controlled Intent schedule: alternate WORK / HIRE_AGENT so we get a clean
	// 50/50 distribution and can assert Intent -> Retrieval mapping.
	intentPlan := []string{"WORK", "HIRE_AGENT"}

	exp := m8.NewExperiment(est, memCfg, "Alice", "engineer", "理性且稳健", "攒到 100 coins", intentPlan)

	// ---- Phase A: 100 Thinks (validation only) ----
	fmt.Println("\n--- Phase A: 100 Thinks (validation) ---")
	if err := runPhase(exp, 100, true); err != nil {
		fmt.Fprintf(os.Stderr, "Phase A FAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Phase A passed: experiment is complete and stable. Proceeding to Phase B.")

	// ---- Phase B: 1000 Thinks (final data) ----
	fmt.Println("\n--- Phase B: 1000 Thinks (final) ---")
	if err := runPhase(exp, 1000, false); err != nil {
		fmt.Fprintf(os.Stderr, "Phase B FAILED: %v\n", err)
		os.Exit(1)
	}
}

// runPhase executes N Thinks on both paths, runs validation checks when
// validate=true, and prints the final report.
func runPhase(exp *m8.Experiment, n int, validate bool) error {
	base := make([]m8.ThinkMetrics, 0, n)
	ctxm := make([]m8.ThinkMetrics, 0, n)
	for i := 0; i < n; i++ {
		b, err := exp.RunBaseline(i)
		if err != nil {
			return fmt.Errorf("baseline think %d: %w", i, err)
		}
		base = append(base, b)
		c, err := exp.RunContext(i)
		if err != nil {
			return fmt.Errorf("context think %d: %w", i, err)
		}
		ctxm = append(ctxm, c)
	}

	if validate {
		if err := validatePhase(base, ctxm); err != nil {
			return err
		}
	}

	printReport(base, ctxm, n)
	return nil
}

// validatePhase checks the experiment's own integrity (per Phase A design).
func validatePhase(base, ctxm []m8.ThinkMetrics) error {
	checks := []struct {
		name string
		ok   bool
	}{
		{"data complete (no zero rows)", len(base) > 0 && len(ctxm) == len(base)},
		{"estimator normal (Context InputTokens > 0)", allPositive(ctxm, func(m m8.ThinkMetrics) int { return m.InputTokens })},
		{"retriever normal (Retrieved > 0 on every Context think)", allPositive(ctxm, func(m m8.ThinkMetrics) int { return m.RetrievedMemoryCount })},
		{"intent spread (both WORK and HIRE_AGENT present)", intentSpread(ctxm)},
		{"no over-budget / anomalous (ContextTokens <= budget cap)", noAnomalousTokens(ctxm)},
		{"stable prefix truly stable (unique hash == 1)", uniqueCount(hashes(ctxm)) == 1},
	}
	allOK := true
	for _, c := range checks {
		status := "OK"
		if !c.ok {
			status = "FAIL"
			allOK = false
		}
		fmt.Printf("  [%s] %s\n", status, c.name)
	}
	if !allOK {
		return fmt.Errorf("phase A validation failed")
	}
	return nil
}

func printReport(base, ctxm []m8.ThinkMetrics, n int) {
	fmt.Printf("\n=== Final Report (N=%d per path) ===\n", n)

	// Q1: average Context per Think (Average / P50 / P90 / P99)
	fmt.Println("\n[Q1] Context Runtime average Context per Think (Runtime Context tokens)")
	printPercentiles("Context Path ContextTokens", collect(ctxm, func(m m8.ThinkMetrics) int { return m.ContextTokens }))
	printPercentiles("Context Path Provider InputTokens", collect(ctxm, func(m m8.ThinkMetrics) int { return m.InputTokens }))
	printPercentiles("Baseline Path Provider InputTokens", collect(base, func(m m8.ThinkMetrics) int { return m.InputTokens }))

	// Q2: Intent -> Retrieval distribution
	fmt.Println("\n[Q2] Intent -> Retrieval mapping (types retrieved per intent)")
	printIntentRetrieval(ctxm)

	// Q3: Retrieved / Context ratio
	fmt.Println("\n[Q3] Retrieved Context / Total Context")
	ratio := avgRatio(ctxm,
		func(m m8.ThinkMetrics) int { return m.RetrievedTokens },
		func(m m8.ThinkMetrics) int { return m.ContextTokens })
	fmt.Printf("  RetrievedTokens / ContextTokens = %.1f%%\n", ratio*100)
	avgRetr := avg(collect(ctxm, func(m m8.ThinkMetrics) int { return m.RetrievedMemoryCount }))
	avgTotal := avg(collect(ctxm, func(m m8.ThinkMetrics) int { return m.TotalMemoryCount }))
	fmt.Printf("  RetrievedMemory / TotalMemory   = %.1f%%  (avg %d / %d)\n",
		safeDiv(float64(avgRetr), float64(avgTotal))*100, avgRetr, avgTotal)

	// Q4: Compaction happened?
	fmt.Println("\n[Q4] Compaction occurred?")
	compacted := sum(collect(ctxm, func(m m8.ThinkMetrics) int { return m.CompactedTokens }))
	if compacted == 0 {
		fmt.Println("  CompactedTokens = 0 (0%). Expected in round 1: Context never hit budget pressure.")
	} else {
		fmt.Printf("  Total CompactedTokens = %d\n", compacted)
	}

	// Q5: Stable Prefix stability
	fmt.Println("\n[Q5] Stable Prefix (KV-Cache readiness)")
	uniq := uniqueCount(hashes(ctxm))
	fmt.Printf("  Unique StablePrefixHash over %d Thinks = %d (ideal: 1)\n", n, uniq)
	if uniq == 1 {
		fmt.Println("  PASS: Stable block is truly stable -> KV Cache safe.")
	} else {
		fmt.Println("  WARN: Stable block is varying -> investigate before enabling KV Cache.")
	}

	// Cost/Think split
	fmt.Println("\n[Cost/Think] (Context / ProviderInput / Output / Total)")
	printCostSplit(ctxm)
}

// ---- helpers ----

func collect(ms []m8.ThinkMetrics, f func(m8.ThinkMetrics) int) []int {
	out := make([]int, len(ms))
	for i, m := range ms {
		out[i] = f(m)
	}
	return out
}

func sum(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

func avg(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	return sum(xs) / len(xs)
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func avgRatio(ms []m8.ThinkMetrics, num, den func(m8.ThinkMetrics) int) float64 {
	if len(ms) == 0 {
		return 0
	}
	var n, d int
	for _, m := range ms {
		n += num(m)
		d += den(m)
	}
	return safeDiv(float64(n), float64(d))
}

func hashes(ms []m8.ThinkMetrics) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.StablePrefixHash
	}
	return out
}

func uniqueCount[T comparable](vals []T) int {
	m := map[T]struct{}{}
	for _, v := range vals {
		m[v] = struct{}{}
	}
	return len(m)
}

func allPositive(ms []m8.ThinkMetrics, f func(m8.ThinkMetrics) int) bool {
	if len(ms) == 0 {
		return false
	}
	for _, m := range ms {
		if f(m) <= 0 {
			return false
		}
	}
	return true
}

func noAnomalousTokens(ms []m8.ThinkMetrics) bool {
	// ContextTokens should be positive and bounded (budget cap ~ a few hundred
	// Rough tokens given our small synthetic blocks). Anything absurdly large
	// would indicate a bug.
	for _, m := range ms {
		if m.ContextTokens <= 0 || m.ContextTokens > 5000 {
			return false
		}
	}
	return true
}

func intentSpread(ms []m8.ThinkMetrics) bool {
	has := map[string]bool{}
	for _, m := range ms {
		has[m.Intent] = true
	}
	return has["WORK"] && has["HIRE_AGENT"]
}

func printPercentiles(label string, xs []int) {
	if len(xs) == 0 {
		fmt.Printf("  %s: (no data)\n", label)
		return
	}
	s := append([]int(nil), xs...)
	sort.Ints(s)
	p := func(q float64) int {
		idx := int(q / 100 * float64(len(s)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(s) {
			idx = len(s) - 1
		}
		return s[idx]
	}
	fmt.Printf("  %s: avg=%d P50=%d P90=%d P99=%d max=%d\n",
		label, sum(xs)/len(xs), p(50), p(90), p(99), s[len(s)-1])
}

func printIntentRetrieval(ms []m8.ThinkMetrics) {
	byIntent := map[string]map[string]int{} // intent -> type -> count
	for _, m := range ms {
		if byIntent[m.Intent] == nil {
			byIntent[m.Intent] = map[string]int{}
		}
		for _, t := range m.RetrievedMemoryTypes {
			byIntent[m.Intent][t]++
		}
	}
	for _, intent := range []string{"WORK", "HIRE_AGENT"} {
		m := byIntent[intent]
		if m == nil {
			fmt.Printf("  %s: (none)\n", intent)
			continue
		}
		fmt.Printf("  %s -> ", intent)
		first := true
		for t, c := range m {
			if !first {
				fmt.Print(", ")
			}
			fmt.Printf("%s=%d", t, c)
			first = false
		}
		fmt.Println()
	}
}

func printCostSplit(ms []m8.ThinkMetrics) {
	ctx := avg(collect(ms, func(m m8.ThinkMetrics) int { return m.ContextCost }))
	pin := avg(collect(ms, func(m m8.ThinkMetrics) int { return m.ProviderInputCost }))
	out := avg(collect(ms, func(m m8.ThinkMetrics) int { return m.OutputCost }))
	tot := avg(collect(ms, func(m m8.ThinkMetrics) int { return m.TotalCost }))
	fmt.Printf("  avg ContextCost=%d ProviderInputCost=%d OutputCost=%d TotalCost=%d\n", ctx, pin, out, tot)
}
