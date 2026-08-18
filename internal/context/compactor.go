package context

import (
	"context"
	"fmt"
	"strings"
)

// Compactor M8.8：Context Reduction 的最后一道防线。
//
// 设计定位（你定的）：
//   - Retrieval 是第一优化手段（少加载），Compaction 是第二优化手段（已加载太多怎么办）。
//   - Compaction 绝不绕过 Retrieval：不是"100 条全加载→全摘要→塞 LLM"，而是
//     Retriever 先按需取少量 → Budget 够就直接 Compile → 不够才 Compaction。
//   - 第一版不接 LLM 摘要，只做 deterministic 压缩（FakeCompactor），先验证
//     "超预算→低优先级先处理→重要 Context 保留→最终 ≤ MaxTotal" 的架构必要性。
//
// 压缩策略（Context Reduction Policy）由 ReducePolicy 表达：
//   Priority ≥ PriorityCurrentDecision（State/Decision/Immutable）→ 永不压缩
//   其余（Event/Retrieved/Old Memory）→ 可压缩，越低的越先被压缩
type Compactor interface {
	// Compact 把输入块压缩到目标 token 预算内，返回压缩后的块集合。
	// 实现应保证：高 Priority / Stable 块被保留或原样返回，不被破坏。
	Compact(ctx context.Context, blocks []ContextBlock, targetTokens int) ([]ContextBlock, error)
}

// ReducePolicy 判断某块是否可被压缩。
// 规则：Stable 块永不压缩；Priority ≥ PriorityCurrentDecision 永不压缩
//（当前 State/Decision 是不可动的决策依据）；其余可压缩。
func ReducePolicy(b ContextBlock) bool {
	if b.Stable {
		return false // Immutable/Semi-Stable：永不压缩
	}
	if b.Priority >= PriorityCurrentDecision {
		return false // 当前 State/Decision：永不压缩
	}
	return true // 事件/检索记忆/旧记忆：可压缩
}

// FakeCompactor M8.8：deterministic 压缩（不调 LLM）。
// 把所有"可压缩"的块合并成一条 summary 块，内容形如：
//
//	[Summary: N historical items] 主题1, 主题2 ...
//
// 验证的是"超预算→低优先级先处理→重要保留→≤MaxTotal"的机制，
// 而非摘要质量。真实 LLM 摘要以后可作为另一实现替换。
type FakeCompactor struct {
	est Estimator
}

// NewFakeCompactor 构造确定型压缩器。
func NewFakeCompactor(est Estimator) *FakeCompactor {
	if est == nil {
		est = roughEstimate
	}
	return &FakeCompactor{est: est}
}

// Compact 把可压缩块合并为一条 summary；不可压缩块原样返回。
// 若全部不可压缩且仍超预算，调用方负责后续严格裁剪。
func (c *FakeCompactor) Compact(ctx context.Context, blocks []ContextBlock, targetTokens int) ([]ContextBlock, error) {
	var kept, reducible []ContextBlock
	for _, b := range blocks {
		if ReducePolicy(b) {
			reducible = append(reducible, b)
		} else {
			kept = append(kept, b)
		}
	}
	if len(reducible) == 0 {
		// 没有可压缩对象，原样返回（交由严格裁剪处理）。
		return blocks, nil
	}
	// 把可压缩块合并成一条 summary。
	var topics []string
	for _, b := range reducible {
		// 取 source 作为主题线索（去重）。
		t := b.Source
		if t == "" {
			t = b.Type.String()
		}
		dup := false
		for _, x := range topics {
			if x == t {
				dup = true
				break
			}
		}
		if !dup {
			topics = append(topics, t)
		}
	}
	summary := fmt.Sprintf("[Summary: %d compressed items] %s", len(reducible), strings.Join(topics, ", "))
	summaryBlock := ContextBlock{
		ID:      "compacted.summary",
		Type:    TypeRetrieved,
		Source:  "compacted.summary",
		Content: summary,
		Priority: PriorityRetrieved, // 压缩产物保留中等优先级
		Stable:  false,
		Tokens:  c.est(summary),
	}
	return append(kept, summaryBlock), nil
}

// String 便于 BlockType 打印（测试/日志用）。
func (bt BlockType) String() string { return string(bt) }
