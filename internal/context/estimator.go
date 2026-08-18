package context

import (
	"agentworld/internal/llm"
)

// M8 Experiment: TokenEstimator is an injectable tokenizer boundary.
//
// The first experiment round MUST NOT call a real LLM. TokenEstimator lets the
// experiment compare Context Runtime vs Baseline purely on token accounting,
// without mixing in Provider Tokenization / model output / API latency / price
// / randomness. Swap RoughEstimator for DeepSeekTokenizer / OpenAITokenizer /
// AnthropicTokenizer later without touching experiment code.
//
// This interface is part of the M8 public surface and is intentionally frozen
// together with the rest of the M8 API (see FREEZE note in context.go).
type TokenEstimator interface {
	// EstimateText estimates token count for a single text blob.
	EstimateText(text string) int
	// EstimateMessages estimates the total token count for a compiled message
	// list (the output of an Adapter). This is what feeds Provider InputTokens.
	EstimateMessages(messages []llm.Message) int
}

// RoughTokenEstimator is the first TokenEstimator implementation.
//
// It uses a coarse heuristic (chars / 4) that is deterministic and provider
// independent. It is NOT a real BPE tokenizer — it exists only so the first
// experiment round can measure Context Runtime composition without a real LLM.
//
// Replace with a real tokenizer later; experiment code only depends on the
// TokenEstimator interface, so no experiment changes are required.
type RoughTokenEstimator struct{}

var _ TokenEstimator = RoughTokenEstimator{}

// EstimateText implements TokenEstimator.
func (RoughTokenEstimator) EstimateText(text string) int {
	t := len([]rune(text)) / 4
	if t < 1 && len([]rune(text)) > 0 {
		t = 1
	}
	return t
}

// EstimateMessages implements TokenEstimator by summing EstimateText over each
// message's Role + Content, so message structure itself contributes tokens.
func (r RoughTokenEstimator) EstimateMessages(messages []llm.Message) int {
	total := 0
	for _, m := range messages {
		total += r.EstimateText(m.Role + "\n" + m.Content)
	}
	return total
}

// RoughEstimator is the canonical first-round TokenEstimator used by
// experiments. It is intentionally an interface value (not the internal
// Estimator func) so swapping in DeepSeekTokenizer / OpenAITokenizer later
// requires no experiment changes.
var RoughEstimator TokenEstimator = RoughTokenEstimator{}

// EstimatorFromToken wraps a TokenEstimator so it satisfies the internal
// Estimator func(string) int contract used by Compiler. This keeps the existing
// Compiler API unchanged while letting experiments inject a TokenEstimator.
func EstimatorFromToken(t TokenEstimator) Estimator {
	return func(s string) int {
		return t.EstimateText(s)
	}
}
