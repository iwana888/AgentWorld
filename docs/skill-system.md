可以。你现在 **MCP 已经打通**，所以 Skill 不应该再造一套 Tool 系统。

最合理的实验是：

> **把 MCP Tool 封装成 Agent 可发现、可拥有、可使用的 Skill，并验证 Agent 是否会主动选择 Skill 完成目标。**

下面这份可以直接作为开发需求。

# AgentWorld Skill System 实验需求

## 一、项目目标

在 AgentWorld 现有 MCP 能力之上，引入第一版 **Skill System（Agent 技能系统）**。

本次不是重新开发 MCP，也不是新增一套独立的 Tool Calling。

核心目标只有一个：

> **验证 Agent 是否可以拥有一组技能，并根据自己的目标、环境、记忆和技能能力，自主选择合适的技能完成任务。**

当前已有：

```text
Agent
├── Personality
├── Goal
├── Memory
├── Relationship
├── Perception
├── Planner
└── Action
```

增加：

```text
Agent
└── Skills
      ├── Skill A
      ├── Skill B
      └── Skill C
```

最终形成：

```text
Perception
    ↓
Goal
    ↓
Memory / Relationship / Personality
    ↓
Available Skills
    ↓
Planner
    ↓
选择 Skill
    ↓
Skill → MCP Tool
    ↓
Action
    ↓
World Event
    ↓
Memory
```

---

# 二、核心概念

## 2.1 Skill 是什么

Skill 不是 MCP Tool 的替代品。

MCP Tool 是：

> “系统提供了什么能力。”

Skill 是：

> “这个 Agent 掌握了什么能力。”

例如 MCP Server 提供：

```text
repair_machine
query_machine
buy_item
sell_item
send_card
cancel_card
```

某个 Agent 可能只有：

```text
Skills:
- Engineer
- Trader
```

那么它能够使用：

```text
Engineer
 ├── query_machine
 └── repair_machine

Trader
 ├── buy_item
 └── sell_item
```

---

# 三、Skill 数据模型

新增：

```go
type Skill struct {
    ID          string
    Name        string
    Description string

    Tools []SkillTool

    Metadata map[string]string
}
```

SkillTool：

```go
type SkillTool struct {
    Name        string
    Description string
}
```

Agent：

```go
type AgentSkills struct {
    Skills []string
}
```

Agent 不直接保存完整 Skill 定义。

只保存：

```text
skill_id
```

Skill 定义由 Skill Registry 管理。

---

# 四、Skill Registry

新增：

```text
SkillRegistry
```

负责：

```text
RegisterSkill()
GetSkill()
ListSkills()
RemoveSkill()
```

例如：

```text
Engineer
Trader
Researcher
Courier
HotelOperator
```

注册：

```go
registry.RegisterSkill(Skill{
    ID: "engineer",
    Name: "Engineer",
    Description: "Can inspect and repair machines",
    Tools: []SkillTool{
        {
            Name: "query_machine",
        },
        {
            Name: "repair_machine",
        },
    },
})
```

---

# 五、Skill 与 MCP 的关系

这是本次实验最重要的设计。

架构：

```text
                MCP Server
                    │
                    ▼
              MCP Tool List
                    │
                    ▼
              Skill Registry
                    │
        ┌───────────┼───────────┐
        ▼           ▼           ▼
     Engineer      Trader     Researcher
        │           │           │
        ▼           ▼           ▼
      Tools        Tools       Tools
```

Skill Registry 不执行 Tool。

执行仍然走现有 MCP：

```text
Agent
 ↓
Planner
 ↓
Skill
 ↓
MCP Tool
 ↓
MCP Server
 ↓
World / External System
```

因此不会破坏当前 MCP 架构。

---

# 六、Agent Skill 配置

支持 Agent 创建时指定技能：

```json
{
  "id": "alice",
  "name": "Alice",
  "skills": [
    "engineer",
    "trader"
  ]
}
```

Inspector 增加：

```text
🛠 Skills

Engineer
  Inspect machine
  Repair machine

Trader
  Buy
  Sell
```

---

# 七、Skill Discovery

Agent 不应该默认拥有所有技能。

Planner 每次决策时，只获得当前 Agent 拥有的 Skill。

例如：

```text
Alice Skills:

Engineer
Trader
```

Prompt 上下文：

```text
Available Skills:

Engineer:
- query_machine
- repair_machine

Trader:
- buy_item
- sell_item
```

另一个 Agent：

```text
Bob Skills:

Courier
```

则只能看到：

```text
Courier:
- accept_delivery
- deliver_package
```

---

# 八、核心实验：Skill 驱动自主决策

本次实验不要人为指定：

```text
Alice -> repair_machine
```

而应该给 Alice 一个目标：

```text
Goal:
Earn 100 coins
```

环境提供：

```text
Repair Reactor
Reward: 30 coins

Delivery Package
Reward: 10 coins
```

Alice 拥有：

```text
Engineer
Courier
```

Planner 自己决定：

```text
目标：
Earn 100 coins

环境：
Reactor broken
Delivery available

技能：
Engineer
Courier

因此：
选择 Engineer

行动：
repair_machine
```

---

# 九、Why 系统集成

现有 AgentWorld 已经存在：

```text
LastWhy
```

本次扩展：

```text
Personality
Goal
Perception
Memory
Relationship
Economy
Skills
Therefore
```

例如：

```text
为什么选择 Repair Reactor？

性格：
专注、喜欢高效率工作

目标：
赚取 100 coins

看到：
Reactor Repair 悬赏 30 coins

记忆：
之前成功完成过类似维修

技能：
Engineer

收益：
30 coins

因此：
我决定使用 Engineer 技能维修 Reactor
```

注意：

**只展示行动理由摘要，不记录或展示 LLM 内部 Chain-of-Thought。**

---

# 十、Skill 使用记录

新增：

```go
type SkillUsage struct {
    AgentID     string
    SkillID     string
    ToolName    string

    StartedAt   time.Time
    FinishedAt  time.Time

    Success     bool
    Error       string
}
```

每次 Skill 调用都产生 Event：

```text
agent.skill.started
agent.skill.completed
agent.skill.failed
```

例如：

```text
09:42 Alice 使用 Engineer Skill
09:43 Alice 调用 repair_machine
09:43 Reactor repair completed
```

Timeline 显示：

```text
🔧 Alice 使用 Engineer 技能完成 Reactor Repair
```

---

# 十一、Skill Inspector

点击 Agent：

```text
Alice

🎭 Personality
专注、可靠

🎯 Goal
Earn 100 coins

🛠 Skills

Engineer
  ✓ query_machine
  ✓ repair_machine

Trader
  ✓ buy_item
  ✓ sell_item

🧠 Recent Memory
...

💭 Why
...

⚡ Recent Skill Usage

09:42 Engineer
repair_machine
Success
+30 coins
```

这样用户可以看到：

> Agent 不只是“调用了一个 Tool”。

而是：

> **Agent 拥有这个能力，并自主决定使用这个能力。**

---

# 十二、Skill 获取机制

第一版只支持：

```text
配置获得
```

例如：

```toml
[agents.alice]
skills = ["engineer", "trader"]

[agents.bob]
skills = ["courier"]
```

暂时不要实现动态学习。

第二阶段再考虑：

```text
购买 Skill
学习 Skill
继承 Skill
训练 Skill
租赁 Skill
```

---

# 十三、Skill Marketplace（第二阶段，不实现）

未来可以：

```text
Skill Marketplace

Engineer       100 coins
Trader          80 coins
Researcher     150 coins
Translator      60 coins
```

Agent：

```text
Balance: 120

Current Skills:
Courier

Goal:
Earn more money

Decision:
Buy Engineer Skill
```

形成：

```text
Money
 ↓
Skill
 ↓
Capability
 ↓
Work
 ↓
Money
```

这将与 Economy World 形成完整闭环。

---

# 十四、第一版实验 World

建议不要重新开发一个复杂 World。

直接创建：

```text
worlds/skillworld/
```

作为最小实验环境。

世界中只有：

```text
3 Agents

Alice
Skills: Engineer, Trader

Bob
Skills: Courier

Charlie
Skills: Researcher
```

世界提供：

```text
Repair Job
Delivery Job
Research Job
Trading Opportunity
```

每个 Agent：

```text
拥有不同技能
拥有不同目标
拥有不同性格
```

运行 5～10 分钟。

观察：

```text
Agent 是否选择正确 Skill？

不同 Skill 是否导致不同 Agent 行为？

Agent 是否会因为目标改变而选择不同 Skill？

Memory 是否影响 Skill 选择？

Personality 是否影响 Skill 选择？
```

---

# 十五、验收标准

## P0

### Skill Registry

能够：

```text
注册 Skill
查询 Skill
列出 Skill
```

### Agent Skills

Agent 可以拥有：

```text
1～3 个 Skill
```

### MCP Integration

Skill 能够映射到现有 MCP Tool。

### Planner

Planner 能够看到：

```text
Agent 当前拥有的 Skills
```

并选择其中一个。

### Skill Execution

Skill 最终通过现有 MCP Tool 执行。

### Observatory

能够看到：

```text
Agent
→ Skill
→ Tool
→ Result
```

---

# 十六、验收案例

创建：

```text
Alice

Personality:
谨慎、专注

Goal:
Earn 100 coins

Skills:
Engineer
Trader
```

世界：

```text
Reactor Repair
Reward: 30

Data Trading
Profit: 10
```

运行后预期：

```text
Alice observes world

↓

发现 Reactor Repair

↓

Planner 判断 Engineer Skill 可用

↓

选择 Engineer

↓

调用 repair_machine

↓

获得 30 coins

↓

产生 Event

↓

写入 Memory

↓

更新 Balance

↓

Why：

“我拥有 Engineer 技能，
当前目标是赚钱，
Reactor Repair 收益较高，
因此决定维修 Reactor。”
```

---

# 十七、最终架构

```text
                    AgentWorld
                         │
                  ┌──────┴──────┐
                  │    Agent    │
                  │             │
                  │ Personality │
                  │ Goal        │
                  │ Memory      │
                  │ Relationship│
                  │ Skills      │
                  └──────┬──────┘
                         │
                    Perception
                         │
                         ▼
                      Planner
                         │
                  ┌──────┴──────┐
                  │ Select Skill │
                  └──────┬──────┘
                         │
                         ▼
                   Skill Registry
                         │
                         ▼
                     MCP Tool
                         │
                         ▼
                    MCP Server
                         │
                         ▼
                       World
                         │
                         ▼
                       Event
                         │
                    ┌────┴────┐
                    ▼         ▼
                 Memory   Observatory
```

---

# 十八、本次实验明确不做

第一版禁止扩张范围：

* 不重新实现 MCP
* 不开发新的 Tool Calling
* 不做 Skill Marketplace
* 不做 Skill 自动学习
* 不做 Skill 等级
* 不做 Skill 交易
* 不做区块链
* 不做真实货币
* 不做复杂经济系统
* 不改变现有 GooseGame Runtime

**只验证一件事情：**

> **Agent 拥有不同 Skill 后，能否根据自己的 Goal、Personality、Memory 和 Perception，自主选择并使用合适的能力。**

如果这个实验跑通，下一步再接 **Economy World**。

届时就会形成一个非常漂亮的闭环：

```text
Agent
 ↓
拥有 Skill
 ↓
发现工作
 ↓
自主选择 Skill
 ↓
完成工作
 ↓
获得 Money
 ↓
购买 Skill / 商品 / 服务
 ↓
能力发生变化
 ↓
新的 Goal
 ↓
新的自主决策
```

这才是 AgentWorld 真正开始“活起来”的地方。
