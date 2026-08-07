package sdk

import (
	"context"
	"math/rand"
)

// 本文件提供官方通用的 WakePolicy 实现，让第三方 Module 无需自己实现唤醒策略。
//
// 说明：sdk 不依赖 internal，因此这里提供的是"策略纯逻辑"（基于传入的
// triggered / all 切片），不涉及数据库事件判定。需要"基于 DB 事件"判定时，
// 可在自己的 Module 里实现（参考内置 Social/Hotel 的 EventWakePolicy）。

// EventWakePolicy 事件驱动激活策略：triggered 全选，再按 Chance 随机补充 idle。
type EventWakePolicy struct {
	Chance float64 // idle 保底概率（0~1）
}

// NewEventWakePolicy 构造事件驱动激活策略。
func NewEventWakePolicy(chance float64) *EventWakePolicy {
	return &EventWakePolicy{Chance: chance}
}

// Select 选出本轮唤醒集合：triggered 全选，再按 Chance 随机补充 idle。
func (w *EventWakePolicy) Select(ctx context.Context, rt Runtime, triggered, all []Agent) []Agent {
	chosen := make([]Agent, 0, len(triggered))
	chosen = append(chosen, triggered...)

	// idle 候选 = 在 all 中但不在 triggered 中的 Agent
	idleSet := make(map[int64]struct{}, len(triggered))
	for _, t := range triggered {
		idleSet[t.ID] = struct{}{}
	}
	for _, a := range all {
		if _, ok := idleSet[a.ID]; ok {
			continue
		}
		if rand.Float64() < w.Chance {
			chosen = append(chosen, a)
		}
	}
	return chosen
}

// AlwaysWakePolicy 始终唤醒所有 Agent（适合小世界 / 演示）。
type AlwaysWakePolicy struct{}

// NewAlwaysWakePolicy 构造始终唤醒策略。
func NewAlwaysWakePolicy() *AlwaysWakePolicy { return &AlwaysWakePolicy{} }

// Select 返回全部 Agent。
func (a *AlwaysWakePolicy) Select(ctx context.Context, rt Runtime, triggered, all []Agent) []Agent {
	return all
}
