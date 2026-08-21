# 产品验证指南 (Validation Playbook)

> 当前阶段：**不做新功能，只验证需求**。
> 目标：找 5–10 个真实开发者试用最小版本，把"你让 Claude/Codex 写代码时最怕它做什么"的答案变成 Policy。

## 路线：从 AgentWorld 到 Agent Reliability SDK

```
AgentWorld
   ├── Context Runtime     ✅ 已验证（先不继续开发）
   ├── Reliability Runtime ✅ MVP2 已验证
   ├── Pascal / Economy / Goose → 实验场（已回答科研问题，暂停）
   └── Agent Reliability SDK   ← 下一步（MVP3）
              ↓
         真实 Agent 接入（Claude/Codex/Cursor…）
              ↓
         开发者试用 / 反馈（collect-feedback）
              ↓
         再决定商业产品
```

MVP3 范围（刻意小，不做 DSL，纯 Go API）：

- `runtime := agentruntime.New()`
- `runtime.UseReliability(agentreliability.NewGuard())`
- `runtime.Check(action)` → `ALLOW / DENY / ASK / MODIFY`
- `runtime.Execute(decision, action, do)` / `runtime.Audit(event)`
- Context Runtime 留 `UseContext` 接缝（MVP4），本版不实现

为什么现在做 SDK 比继续做 World 价值高：共同问题不是"AI 会不会生成代码"，
而是"AI 已经能做事情以后，谁负责约束它"。Runtime 在执行前拦截，而不是
再告诉 AI "你要记住规则"——这正好解决"规则写记忆会丢 / 写 prompt 被覆盖 /
读规则文件不可靠 / 干久了开始偷懒"的真实痛点。

---

## 1. 给开发者的"一行接入" (30 秒试用)

SDK 已是独立模块，零业务依赖。开发者只需把 agent 的每次工具调用包进一个 Gate：

```go
import "agent-reliability"

func main() {
    rt := agentreliability.NewGuard()        // 10 条默认 Policy 已 ON
    gate := agentreliability.UseReliability(rt) // 一行挂载安全边界

    // 把 Claude/Codex 想做的"动作"包进来
    err := gate.Run(ctx, agentreliability.Action{
        Tool:   "shell",
        Target: "rm -rf node_modules",
    }, func(ctx context.Context) error {
        return runTheActualTool() // 你的真实执行器
    })
    if err != nil {
        // err 即被 Runtime 拦截，什么都没跑。安全。
        log.Println("blocked:", err)
    }
}
```

三个要点：
- **DENY**：直接拦，执行器不调用。
- **ASK**：默认拒绝（安全），用 `gate.WithApprover(...)` 接你自己的审批 UI。
- **MODIFY**：默认**不**偷改 agent 意图；设 `gate.AdoptModify = true` 才采用建议动作。

试用成本 = 套一层 `gate.Run`，不碰 SDK 内部。

---

## 2. 收集痛点："你最怕它做什么"

跑反馈收集器，把答案结构化：

```bash
# 交互式：逐行粘贴开发者的原话，空行结束
go run ./cmd/collect-feedback

# 或批量：每行一条（纯文本或 JSON {"text":"...","who":"dev@x"}）
go run ./cmd/collect-feedback --answers answers.txt
```

收集器会：
1. 把每条原话**映射到已有的 10 条 Policy**（需求信号）。
2. 没匹配上的归为 **[new]**（潜在新 Policy）。
3. 输出 **DEMAND SIGNAL**：哪条 Policy 被多少人提到（即被验证）。
4. 输出 **POLICY BACKLOG**：可直接粘贴的 `AddPolicy` 骨架。

---

## 3. 决策：有需求 vs 没需求

| 信号 | 结论 | 下一步 |
|------|------|--------|
| ≥3 个开发者提到同一条内置 Policy | **需求成立** | 抽完整 SDK（`runtime.Execute` / `Audit` + `agent.UseReliability` 公开） |
| 10 人几乎 0 提及任何内置 Policy | **方向存疑** | 小成本排除，换赛道（如改做"可解释性 / 回滚"而非"预防性拦截"） |
| 大量 `[new]` 未映射痛点 | 发现新缺口 | 用收集器给的骨架补 `AddPolicy`，进入下一轮验证 |

---

## 4. 闭环节奏

```
试用 (gate.Run)  →  收集 (collect-feedback)  →  读 DEMAND SIGNAL
        ↑                                              │
        └────── 加/调 Policy (AddPolicy) ←─────────────┘
```

每轮只动 Policy，不动核心。核心 (`Guard`/`Check`/`Action`/`Decision`) 已通过
`scenarios_test.go` 的 10 个场景测试，保持稳定。

---

## 5. 快速命令清单

```bash
cd agent-reliability
go test ./...                 # 回归：10 个场景
go build ./...                # 编译
go run ./cmd/agentworld-guard # 看默认策略实际拦截效果
go run ./cmd/collect-feedback # 跑验证闭环
```
