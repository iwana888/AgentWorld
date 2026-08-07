# SDK —— 创建你自己的世界

AgentWorld 的 SDK 让你创建一个世界（一个 `Module`）并运行在运行时上，**无需接触任何 `internal/*` 代码**。`import "agentworld/sdk"` 并实现几个方法即可。

## 依赖方向

```
agentworld/sdk   （公共契约 —— 不依赖 internal）
        ▲
        │ 实现 sdk.Module / 使用 sdk.Runtime
        │
    你的模块      或官方模块（Social / Hotel）
```

`internal/agent` 依赖 `sdk`，反向则不行。从 runtime 视角看，官方与第三方模块**完全一致**。

## 最小世界

```go
package main

import (
    "context"
    "agentworld/sdk"
)

type MyWorld struct{ rt sdk.Runtime }

func (m *MyWorld) Name() string { return "myworld" }

func (m *MyWorld) OnBoot(rt sdk.Runtime) error {
    m.rt = rt
    return rt.DB().Exec(`CREATE TABLE IF NOT EXISTS my_table(id INTEGER PRIMARY KEY)`).Error
}

func (m *MyWorld) Perceive(ctx context.Context, a sdk.Agent) (sdk.Perception, error) {
    // 返回该 Agent 本轮"所见"的世界视图。
    return map[string]any{"state": "ok"}, nil
}

func (m *MyWorld) Planner() sdk.Planner       { return myPlanner{} }
func (m *MyWorld) Executor() sdk.Executor     { return myExecutor{m} }
func (m *MyWorld) WakePolicy() sdk.WakePolicy { return myWake{} }

func main() {
    sdk.RegisterModule(&MyWorld{})
}
```

## 接口

### Module

```go
type Module interface {
    Name() string
    Perceive(ctx context.Context, a Agent) (Perception, error)
    Planner() Planner
    Executor() Executor
    WakePolicy() WakePolicy
    OnBoot(rt Runtime) error
}
```

### Planner

```go
type Planner interface {
    Decide(ctx context.Context, a Agent, p Perception) (*Decision, error)
}
```

### Executor

```go
type Executor interface {
    Execute(ctx context.Context, rt Runtime, a Agent, p Perception, dec *Decision) (string, error)
}
```

### WakePolicy

```go
type WakePolicy interface {
    Select(ctx context.Context, rt Runtime, triggered, all []Agent) []Agent
}
```

## sdk.Runtime 能力

| 方法 | 用途 |
|---|---|
| `DB()` | 读写你自己的表 |
| `SaveMemory(a, dec)` | 持久化 Agent 记忆 |
| `ApplyStateDelta(a, delta)` | 应用 Mood / Energy / Need 变化 |
| `LoadState(a)` | 读取 Agent 状态 |
| `CallTool(cap, tool, args)` | 调用已注册能力（PMS、天气…） |
| `Capabilities()` / `CapabilityNames()` | 列出可用能力 |
| `UseLLM(a)` / `GoalEnabled()` | 框架开关 |
| `Send(msg)` / `Inbox(id, status)` / `MarkMessage(id, status)` | A2A 消息 |
| `Discover(skill)` / `Select(from, skill)` | 能力发现 + 合作伙伴选择 |

## 保持世界无关

SDK 刻意保持**世界无关** —— 不引入 `Post`、`Comment`、`Room` 等业务对象。世界专属数据放在你的 module 自己的表里。这样核心 SDK 保持精简，多种世界类型可以共存。
