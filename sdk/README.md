# AgentWorld Module SDK（M10）

让第三方通过 `import "agentworld/sdk"` 就能注册自己的世界/场景，无需接触 `internal/*` 内部实现。

## 核心思想

```
第三方程序                  AgentWorld 运行时
  import agentworld/sdk        │
  ┌──────────────────┐        │
  │ 实现 sdk.Module   │        │
  │  sdk.RegisterModule()───► LoadSDKModules()  ──► RegisterSDKModule() 桥接 ──► 调度
  └──────────────────┘        │
```

- `sdk` 包只定义公共契约（接口 + 轻量数据模型），**不依赖 internal/**。
- `internal/agent` 通过适配层（`sdk_adapter.go`）把 `sdk.Module` 桥接到内部 `Module`。
- 依赖方向：`agent → sdk`（单向，无循环）。

## 快速开始

### 1. 实现 sdk.Module

```go
import "agentworld/sdk"

type MyWorld struct{ rt sdk.Runtime }

func (m *MyWorld) Name() string { return "myworld" }

func (m *MyWorld) OnBoot(rt sdk.Runtime) error {
    m.rt = rt
    return rt.DB().Exec(`CREATE TABLE IF NOT EXISTS my_table(...)`).Error
}

func (m *MyWorld) Perceive(ctx context.Context, a sdk.Agent) (sdk.Perception, error) {
    // 返回该 Agent 本轮"所见"（自定义结构体）
    return map[string]any{"state": "..."}, nil
}

func (m *MyWorld) Planner() sdk.Planner { return myPlanner{} }      // 决策器
func (m *MyWorld) Executor() sdk.Executor { return myExecutor{m} }  // 执行器
func (m *MyWorld) WakePolicy() sdk.WakePolicy { return myWake{} }   // 唤醒策略
```

### 2. 注册

```go
func main() {
    sdk.RegisterModule(&MyWorld{})
    // 交由 AgentWorld 运行时启动；运行时通过 sdk.LoadSDKModules() 自动加载并调度
}
```

### 3. 在运行时中加载

运行时 `main.go` 已内置：

```go
for _, sm := range sdk.LoadSDKModules() {
    rt.RegisterSDKModule(sm)   // 桥接并调度
}
```

## sdk.Runtime 上下文

第三方 Module 通过 `sdk.Runtime` 访问运行时能力：

| 方法 | 说明 |
|---|---|
| `DB() *gorm.DB` | 读写自己的数据表 |
| `SaveMemory(a, dec)` | 写入 Agent 记忆 |
| `ApplyStateDelta(a, delta)` | 应用状态变化（Mood/Energy/Needs） |
| `LoadState(a)` | 读取 Agent 状态 |
| `PublishEvent(e)` | 广播事件（供前端监控） |
| `CallTool(cap, tool, args)` | 调用已注册能力（如 PMS 发卡、天气查询） |
| `CapabilityNames()` | 列出可用能力 |

## 内置接口速览

- `sdk.Module`：Name / Perceive / Planner / Executor / WakePolicy / OnBoot
- `sdk.Planner`：Decide(ctx, Agent, Perception) → *Decision
- `sdk.Executor`：Execute(ctx, Runtime, Agent, Perception, *Decision) → (string, error)
- `sdk.WakePolicy`：Select(ctx, Runtime, triggered, all []Agent) → []Agent
- `sdk.Decision`：Action / Target / TargetKind / Content / Reason / Memory / ToolArgs ...

## 完整示例

见 [`examples/gameworld`](../examples/gameworld/main.go)：一个"打怪升级"世界，演示了
自定义表、规则决策、调用天气能力、状态变化的全流程。
