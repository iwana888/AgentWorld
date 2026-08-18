# AgentWorld M0 → M8 Architecture Review

> M8 收口里程碑。M8.10 是 M8 的最后一个阶段；M8 之后进入**冻结期**，不立即推 M9。
> 本文是 M0→M8 的架构总审，记录 AgentWorld 已经从"Agent Simulation"演化为
> **A Runtime for Autonomous Agents** 的判断与依据。

---

## 1. 结论

AgentWorld **不应再被定义为 Agent Simulation 项目**。

更准确的定义：

```
AgentWorld
A Runtime for Autonomous Agents
```

World 是 Runtime 的**运行环境**，而不是产品本身。这是 M0 到 M8 最大的变化。

---

## 2. M0 → M8 到底发生了什么

表面是功能越来越多，实质是一条非常完整的演化链：

```
M0  Framework
M1  Memory
M2  Relationship
M3  Human Agent
M4  Packaging / Scheduler
M5  Skill Marketplace
M6  Agent Labor Market
M7  Economic Decision
M8  Context Runtime
```

真正的语义演化：

```
"让 Agent 做事情"
        ↓
"让 Agent 自己决定做什么"
        ↓
"让 Agent 根据自己的经历做决定"
        ↓
"让 Agent 在资源约束下做决定"
        ↓
"让 Agent 只获得完成当前决定所需要的信息"
```

---

## 3. M0：Agent 从"角色"变成 Runtime Entity

最初：`Agent → Prompt → Action`
后来：

```
Agent
├── Identity
├── Personality
├── Memory
├── Goal
├── Relationship
└── Action
```

Agent 不再是一个 Prompt，而成为**持续存在的 Runtime Entity**。

---

## 4. M1～M3：Agent 获得"连续性"

- **Memory**：我经历过什么？
- **Relationship**：我怎么看别人？
- **Goal**：我想得到什么？
- **Human Agent**：谁在控制这个身份？

Agent 开始具备 **过去 → 现在 → 未来**，而非每次调用 LLM 都从零开始。

---

## 5. M5～M7：Agent 获得"经济约束"

Economy World 出现后：

```
Agent
├── Money
├── Skill
├── Job
├── Contract
├── Reputation
└── Opportunity
```

Decision 不再只是"什么回答最好"，而是"在我现有资源下，什么行为最值得"。
`BUY / HIRE / WORK / WAIT` 成为真实候选行动，Agent 的"自主性"开始有约束。

---

## 6. M8：Context Runtime —— 最大的架构升级

以前：

```
World → Perception → 全部信息 → Prompt → LLM
```

现在：

```
World
  ↓
Perception
  ↓
Intent
  ↓
Retriever
  ↓
Budget
  ↓
Compactor
  ↓
CompiledContext
  ↓
Adapter
  ↓
Provider
```

Agent 不再直接面对 World，而是面对 **Runtime 根据当前决策构建出来的"可认知世界"**。
这一区别非常深：**Agent 能不能自主，不只取决于模型多聪明，还取决于它在做决定的那一刻被允许看到什么。**

---

## 7. 六个问题的正式回答

### ① Context Runtime 要不要独立成 internal/context？

**要，且已证明应该独立。**

它在 M8 已拥有完整生命周期：`Request → Retrieve → Compile → Compact → Adapt → Provider`。
它已不是 `prompt.go`，而是 **Agent Runtime 的 Cognitive Infrastructure**。

未来目录可趋向：

```
internal/
├── agent/
├── context/
├── memory/
├── economy/
├── scheduler/
└── world/
```

> 代码事实核对（M8.10）：`internal/context` 包零反向依赖（不 import economy/world/agent），
> 而 `worlds/economy` 单向 import `context`。运行时边界成立。

### ② AgentState / Intent 是否应该脱离 World？

**方向上应该，但现在不动。**

World 负责"Agent 所处的世界"；AgentState 负责"Agent 当前状态"；Intent 负责
"Agent 当前想解决的问题"。三者不要永久绑定。最终应趋向：

```
World      → Observation
Agent      → State
Decision   → Intent
```

这样 Pascal / Hotel / Economy 才能共享同一 Runtime。

### ③ Retriever 是否应该成为 Runtime Extension？

**是。** 现有接口边界良好。未来可扩展：

- 语义：`MemoryRetriever / RelationRetriever / EventRetriever / KnowledgeRetriever / EconomyRetriever / WorldRetriever`
- 实现：`VectorRetriever / SQLRetriever / HybridRetriever`

Context Runtime 不需要知道数据来自哪里 —— 这正是当前接口设计的价值。

### ④ Compactor 是否应该支持 LLM？

**最终应该，但现在不要。** 当前 `FakeCompactor` 正确，因为先验证的是
"什么时候应该压缩"，而不是"哪个模型摘要最好"。未来可：`DeterministicCompactor / LLMCompactor / HybridCompactor`，接口不绑定模型。

### ⑤ Token Accounting 最终是不是 Economy Resource？

**很可能是，且是 AgentWorld 非常有意思的方向，但现在不接。**

未来可形成：

```
Agent Wealth
├── Coins
├── Skills
├── Reputation
└── Compute / Tokens
```

于是 Agent 面对：**深入思考 vs 快速行动**、**更多检索 vs 节省 Token**、
**重新规划 vs 直接执行**。"思考成本"成为真实经济约束，可能成为 Economy World
独特机制。

### ⑥ 首页定位要不要改？

**必须改，且是叙事级改变，不只是换一句标语。**

建议：

```
AgentWorld
A Runtime for Autonomous Agents

Build worlds where agents can perceive, remember, decide, interact, work and evolve on their own.
```

产品组成部分：World / Memory / Skills / Economy / Relationships / Context / Decision / Action。

---

## 8. AgentWorld 新定位：World 是"实验场"

现有 World 全部获得统一意义——不是 AgentWorld 的终点，而是观察 Agent
自主行为的实验环境：

```
Social World   → 研究社交行为
Economy World  → 研究资源约束下的自主决策
Hotel World    → 研究现实业务 Agent
Goose World    → 研究信念、欺骗、合作
Pascal World   → 研究 Agent Software Engineering
```

这也解释了**为什么不应急着做 Pascal World**：基础设施（Runtime / Memory /
Relationship / Goal / Skill / Economy / Context）稳定后，Pascal World 才能从
"Agent 写 Pascal 的 Demo" 升级为 "Agent 自主软件生产实验场"。

---

## 9. M0 → M8 的真正成果（一图）

```
                    AgentWorld
                        │
              Autonomous Agent Runtime
                        │
       ┌────────────────┼────────────────┐
       │                │                │
    Identity          Memory          Goal
       │                │                │
    Skills        Relationship       Economy
       └────────────────┼────────────────┘
                        │
                    Perception
                        │
                      Intent
                        │
                ┌───────┴───────┐
                │ Context Runtime│
                │               │
                │ Retrieval     │
                │ Budget        │
                │ Compaction    │
                │ Compilation   │
                │ Adapter       │
                └───────┬───────┘
                        │
                      LLM
                        │
                   Decision
                        │
                     Action
                        │
                      World
                        │
                        └──────→ next perception
```

这已是一套完整的 Agent Runtime 理论模型。

---

## 10. M8 之后：冻结期，先不推 M9

接下来做三件事：

### 第一：架构审计
- 哪些接口真的稳定？
- 哪些抽象是过度设计？
- 哪些 World 逻辑泄漏进 Runtime？（例：economy module 在 `observeContext` 里
  手工硬编码 StableBlocks —— World 层拼了本应由 Identity/Personality 模块提供的块）
- 哪些 Runtime 能力实际上还依赖 Economy？

### 第二：真实运行数据
让 Economy World 跑一批 Agent，观察（而非只看单元测试）：
- Context size / Retrieval rate / Compaction rate
- Decision distribution / Token usage
- 按 Intent 分桶：WORK / HIRE / BUY / WAIT 各占多少 Context

`internal/context` 的 `TokenLedger` 已提供 `Avg / P50 / P90 / P99` 统计接口，
可直接支撑这次实验。

### 第三：重新做首页
首页叙事从"我有什么功能"转为"**为什么 AgentWorld 存在**"。

---

## 10.5. M8 API Freeze（架构审计后）

审计无 P0/P1，七个边界全部成立。M8 接口于 2026-08-17 冻结。

**冻结（禁止修改）**：`ContextBlock` 字段 / `Compiler.Compile` 主流程 /
`Retriever`+`RetrieveRequest`+`MemoryStore` / `Compactor`+`ReducePolicy` /
`ContextAdapter` / `TokenUsage`+`TokenLedger` 语义。

**允许**：实现已有接口（db 实现 `MemoryStore`、World 注入 `MemoryRetriever`）、
实验/观测代码、Bug fix（不改语义）。

**禁止**：新增 Context 能力（不写 M8.11）。

1000-Think 实验必须在稳定 API 上测量，不能在实验中漂移接口定义。

**已完成的接线（冻结前最后一笔，属"实现已有接口"而非"扩展"）**：
- `internal/db/memory_store.go`：`DBMemoryStore` 实现 `context.MemoryStore`，
  仅用 (AgentID + Type IN + 可选 aboutAgentID + Importance/CreatedAt desc + Limit)，
  不引入 embedding / vector / keyword。
- `worlds/economy` `Module.WithRetriever(r)`：把真实 `MemoryRetriever` 注入
  `observeContext` 旁路观察（仍不参与决策）。

---

## 11. 一句话判断

最初做它时，可能是"我想做一个 AI Agent 世界"。
做到 M8 后，它已变成：

> **我想研究：如果给 Agent 持续的身份、记忆、目标、关系、技能、资源和一个
> 可运行的世界，它会自己形成什么行为？**

这已经是一个更大的问题。Context Runtime 是其中非常关键的一块——因为 Agent
能不能自主，不只取决于模型多聪明，还取决于它在做决定的那一刻，**到底被允许看到什么**。

**M8 是 AgentWorld 的第一个架构分水岭。现在停下来，把它讲清楚、跑起来、观察它到底会产生什么行为。**
