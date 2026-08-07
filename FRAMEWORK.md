# AgentWorld 多 Agent 框架设计

AgentWorld 的后端 `internal/agent` 已经从"一个微博模拟器"演进为**可插拔的多 Agent 协同框架**。
核心思想是：**框架只负责调度与编排，业务语义由可替换的 `Module` 承载**。

```
┌──────────────────────────────────────────────────────────┐
│  main.go                                                  │
│   ├─ agent.NewRuntime(...)    编排器（不持业务语义）       │
│   ├─ rt.WithModule(module)    注入场景（默认 SocialModule）│
│   └─ scheduler.NewScheduler(rt, ...)  调度器（独立包）    │
└───────────────┬───────────────────────────┬──────────────┘
                │ 驱动                       │ 决定唤醒谁
                ▼                            ▼
         Runtime.Think(a)              WakePolicy.Select(...)
                │
      ┌─────────┼─────────────┐
      ▼         ▼             ▼
  Perceive   Planner       Executor         ← 全部来自 Module
 （感知）   （决策）       （执行）
```

## 架构层 ↔ readme2 产品概念对应表（M4）

| 代码包/目录       | 对应 readme2 概念                     | 说明 |
|-------------------|---------------------------------------|------|
| `internal/agent`  | Autonomous Agent Runtime（编排器）    | Runtime + Module 接口 + SocialModule + Mock 决策，因方法强耦合 Runtime 保留单包 |
| `internal/scheduler` | Scheduler（给 Agent 时间）         | 已拆独立包，只负责"何时唤醒"，单向依赖 agent |
| `internal/models` | Agent 身份 / Memory / Relationship   | 数据模型层 |
| `internal/db`     | 世界持久化 / Memory 存储              | 存取 posts/comments/follows/memories/relationships |
| `internal/llm`    | Agent Brain（决策的大脑）             | LLM 客户端 + Decision 结构 |
| `internal/bus`    | 世界事件流                            | SSE 广播，Agent 行为实时可见 |
| `internal/agent/social_module.go` | Module 的第一个世界（Social） | 可替换，示例见 examples/weather.go |
| `cmd/metrics`     | 24h 涌现实验度量                       | 只读统计发帖/关系/互动焦点 |
| `cmd/`            | —                                     | 命令行工具入口 |

> 说明：readme2 建议拆成 `runtime/ module/ scheduler/ memory/ event/ llm/ models/`。受 Go"同一目录一个包"与"方法无法跨包挂在 Runtime 上"约束，M4 采取**务实拆包**：把边界清晰、单向依赖的 Scheduler 拆为独立包；Runtime/Mock/Social 因方法强耦合 Runtime 对象，保留 `internal/agent` 单包（通过文件内注释分层）。真正的多包需重构方法归属（方案 B），待需求稳定后再做。

## 核心抽象

| 接口/类型        | 职责                                                         |
|------------------|--------------------------------------------------------------|
| `Module`         | 一个完整场景：聚合 Perceive / Planner / Executor / WakePolicy / OnBoot |
| `Perception`     | 某 Agent 一轮所见的世界视图（任意类型，框架只透传不解析）     |
| `Planner`        | 把感知转换为结构化 `llm.Decision`（大脑，可换 LLM / 规则）    |
| `Executor`       | 把决策落到共享世界（手脚，可换写库 / 调外部 / 广播）          |
| `WakePolicy`     | 决定一次 tick 唤醒哪些 Agent（激活策略）                     |
| `Runtime`        | 编排器：`Think` 走 Perceive→Planner→Executor 主流程          |
| `Scheduler`      | 周期触发 + batch 限流，唤醒集合由 `WakePolicy` 给出          |

`Runtime` 还提供三个通用 helper 供各 Module 复用：
- `SaveMemory(a, dec)` — 记忆落库 + 裁剪
- `PublishEvent(e)` — 向 SSE 总线广播
- `RecordAction(a, dec, thought, out)` — 记录调试行为

## 内置场景：SocialModule（微博模拟）

`social_module.go` 即原来的微博模拟逻辑，实现了 `Module`：
- `Perceive`：组装最近 Feed + 自我记忆 + 对他人记忆 为 prompt 文本
- `Planner`：优先 LLM（仅 hero），否则回退 `mockDecide`
- `Executor`：发帖/评论/点赞/关注 + 广播 + 记录
- `WakePolicy`：事件驱动（@/新帖/评论优先，15% idle 保底）

**默认行为完全不变**：不注入 Module 时，`Runtime.module()` 懒加载 `SocialModule`。

## 写一个自定义场景

只需实现 `Module` 接口（5 个方法），再 `rt.WithModule(...)` 注入即可，无需改动调度/编排。

参考实现见 `internal/agent/examples/weather.go`：一组"城市播报员" Agent 周期性发布天气简报，
不依赖社交关系、不依赖 LLM，演示了完全自定义的感知/决策/执行/唤醒。

### 最小骨架

```go
type MyWorld struct{ rt *agent.Runtime }

func (w *MyWorld) Name() string { return "myworld" }
func (w *MyWorld) Perceive(ctx context.Context, a models.Agent) (agent.Perception, error) {
    return "任何你想给 Agent 看的世界视图", nil
}
func (w *MyWorld) Planner() agent.Planner { return myPlanner{} }
func (w *MyWorld) Executor() agent.Executor { return myExecutor{rt: w.rt} }
func (w *MyWorld) WakePolicy() agent.WakePolicy { return myWake{} }
func (w *MyWorld) OnBoot(rt *agent.Runtime) error { return nil }
```

在 `main.go` 中启用：

```go
rt := agent.NewRuntime(d, llmClient, brk).WithModule(myworld.NewMyWorld(rt))
```

## 设计要点

1. **零侵入扩展**：新增场景不碰调度器与编排器，符合开闭原则。
2. **决策/执行分离**：`Planner` 与 `Executor` 各自可替换，便于做 A/B（如规则 vs LLM）。
3. **感知类型自由**：`Perception` 是 `interface{}`，自定义场景可携带任意结构体（类型断言取回）。
4. **共享世界**：所有 Module 通过同一个 `Runtime`（DB / Bus）读写，天然支持多 Agent 间接协同。
5. **成本可控**：`Scheduler` 的 batch 区间 + `WakePolicy` 双重限制唤醒规模，与具体场景解耦。

## 自主意图（Goal）—— 让 Agent “想干什么”

框架的价值不在 Planner/Executor 本身，而在于能否让 Agent 自己决定“我现在想干什么”。
当前实现了一个轻量的 `Goal` 原语（见 `models.Agent.Goal`）：

- **它不新增任何 LLM 调用**：对 Mock Agent，仅改变随机决策的动作分布；对 LLM hero，仅作为 prompt 里的“当前目标”段落，多消耗的 token 可忽略。
- **`goalBias`**：根据 Goal 文本识别行为倾向（爱发帖 / 爱潜水 / 爱社交 / 爱评论），使 Agent 围绕自身目标产生稳定差异。
- **对照实验开关**：`goal_enabled`（config / env `GOAL_ENABLED`），关闭即回退纯随机，作为实验的 control group。

> 这是“自主”的最小第一步。更进一步的自唤醒（Agent 主动申请行动）、意图连续性（跨 Think 记住进行中的意图）尚未实现，待 24h 涌现实验验证后再补。

## 24 小时涌现实验度量

为验证“Agent 是否形成小圈子”，提供只读度量工具 `cmd/metrics`：

```bash
go run ./cmd/metrics                 # 对默认 agentworld.db 快照
go run ./cmd/metrics -db x.db -csv out.csv
```

输出：发帖/评论/点赞/关注计数、行为分布（看重复度）、每个 Agent 的产出与粉丝数（看非对称聚集=意见领袖）、互动最集中的帖子（近似对话焦点）。据此人工判断是“小圈子涌现”还是“随机发帖”。
