可以。我建议直接把它写成一份**可以拿来拆任务、设计代码的 v0.2 需求说明书**，而不是继续停留在概念层。

下面这版我会严格按照你现在 AgentWorld 的思路来设计：**Runtime 不控制 Agent 的具体行为，World 只提供规则和环境，Agent 自己感知、思考、决定和行动。**

# AgentWorld: 鸭鹅杀需求说明书 v0.2

## 1. 项目概述

**项目名称：** AgentWorld: Goose Game
**项目类型：** AI Agent 自主社交推理游戏
**项目定位：** AgentWorld Autonomous Agent Runtime 的第一个游戏世界 Demo。

AgentWorld: Goose Game 不以“让 AI 模仿玩家”为核心，而是将《鸭鹅杀》抽象为一个由 Agent 自主生活的虚拟世界。

游戏中的每一个 AI 角色都是一个独立 Agent。

Agent 拥有：

* Identity
* Personality
* Goal
* Memory
* Relationship
* Belief
* Perception
* Planner
* Action

游戏系统只负责：

> **维护世界规则、提供环境信息、验证 Agent 行为、产生事件。**

不负责告诉 Agent：

> “现在应该去哪里。”

> “应该杀谁。”

> “应该怀疑谁。”

> “应该投谁。”

---

# 2. 核心理念

传统 AI 游戏：

```text
Game
 ↓
NPC Script
 ↓
固定行为
```

AgentWorld：

```text
World
 ↓
Perception
 ↓
Agent
 ↓
Memory / Belief / Goal
 ↓
Planner
 ↓
自主决策
 ↓
Action
 ↓
World
```

因此：

**游戏规则是确定的，Agent 行为是不确定的。**

同样一套游戏规则运行 100 次，可以产生 100 种完全不同的故事。

---

# 3. 产品目标

第一阶段不追求完整复刻《鸭鹅杀》。

核心目标只有三个。

### 3.1 验证 Agent 自主行为

Agent 能自主决定：

* 去哪里
* 做什么
* 跟谁同行
* 是否相信别人
* 是否撒谎
* 是否合作
* 是否攻击
* 是否报告
* 是否投票

### 3.2 验证 AgentWorld 核心能力

验证：

```text
Goal
Memory
Relationship
Belief
Perception
Planner
Executor
Event
Scheduler
```

能否共同产生复杂行为。

### 3.3 验证 AI 社会行为

观察是否能够自然出现：

* 联盟
* 怀疑
* 欺骗
* 嫁祸
* 报复
* 信任
* 跟随
* 派系
* 错误判断
* 群体投票

---

# 4. MVP 游戏规模

第一版本采用极简配置。

```text
Agent数量：8

鹅：6
鸭：1
中立：1
```

地图：

```text
        ┌─────────────┐
        │   Engine    │
        │    Room     │
        └──────┬──────┘
               │
        ┌──────┴──────┐
        │    Lobby    │
        └──────┬──────┘
               │
        ┌──────┴──────┐
        │   Kitchen   │
        └─────────────┘
```

第一版只需要三个区域。

---

# 5. Agent模型

每个 Agent 都拥有独立状态。

```go
type GameAgent struct {
    AgentID       string
    Name          string

    Identity      Identity
    Personality   Personality

    Goal          Goal

    Memory        Memory
    Beliefs       Beliefs
    Relationships Relationships

    Position      string

    Alive         bool
}
```

---

# 6. Agent身份

身份分为三类。

## 6.1 Goose

目标：

```text
完成任务
或
淘汰所有危险角色
```

---

## 6.2 Duck

目标：

```text
杀死 Goose
并避免被发现
```

Duck 可以：

```text
Kill
Sabotage
Lie
Frame
Hide
Manipulate
```

---

## 6.3 Neutral

中立角色拥有独立胜利条件。

MVP 可以只实现一个：

### Dodo

目标：

```text
让自己被投票淘汰
```

这会非常适合测试 Agent 的反向策略。

---

# 7. 信息隔离

这是整个系统最重要的设计之一。

Runtime 知道：

```text
A = Goose
B = Goose
C = Duck
D = Neutral
```

但是 Agent 不知道其他人的真实身份。

例如：

```text
Agent A

自己的身份：
Goose

知道：
B 在 Kitchen

不知道：
B 是什么身份
```

因此：

> **真实世界状态与 Agent 感知状态必须严格分离。**

---

# 8. World State

Game World 保存真实状态。

```go
type GameState struct {
    Phase       GamePhase

    Agents      map[string]*AgentState

    Rooms       map[string]*Room

    Tasks       []Task

    Bodies      []Body

    Sabotages   []Sabotage

    Meeting     *Meeting

    Round       int

    StartedAt   time.Time
}
```

Agent 不直接读取 `GameState`。

Agent 只能通过：

```go
Perceive(agent)
```

获取自己允许看到的信息。

---

# 9. Perception

Agent 每次被唤醒时，首先感知世界。

例如：

```text
当前时间：10:31

当前位置：Kitchen

附近 Agent：

Bob
Alice

观察到：

Bob 刚刚离开 Kitchen。

Alice 正在执行任务。

你知道：

昨天 Bob 曾经怀疑 David。

你听到：

Engine Room 发生异常。
```

Perception 不能包含：

```text
Bob = Duck
```

除非 Agent 通过实际事件推断出来。

---

# 10. Memory

Agent 拥有自己的长期记忆。

例如：

```text
Memory:

10:21
我看到 Bob 从 Engine Room 出来。

10:25
Bob 投票给 Alice。

10:27
Alice 说 Bob 很可疑。

10:30
我和 Bob 一起完成任务。
```

不同 Agent 的 Memory 不一样。

因此：

> **世界只有一个，记忆却有八份。**

---

# 11. Belief

在 Memory 之上增加 Belief。

Belief 表示 Agent 对世界的主观判断。

例如：

```text
Bob：

Duck probability = 0.82

Alice：

Duck probability = 0.21

David：

Duck probability = 0.56
```

Belief 不是事实。

它会随着新事件发生不断变化。

例如：

```text
Bob = Duck
0.82

发现 Bob 在尸体旁边

0.95

Bob 提供了无法验证的证词

0.97
```

也可以反向下降：

```text
Bob = Duck
0.82

Bob 和我一起完成任务

0.43
```

这让 Agent 的判断具有不确定性。

---

# 12. Relationship

Agent 与 Agent 之间维护关系。

例如：

```text
A → B

Trust:       0.82
Friendship:  0.71
Suspicion:   0.12
Fear:        0.30
```

关系由行为自然改变。

例如：

```text
B 帮助 A
    ↓
Trust +0.1

B 欺骗 A
    ↓
Trust -0.3

B 杀死 A 的朋友
    ↓
Suspicion +0.5
```

Relationship 不应该由游戏规则直接指定。

应该由 Agent 的经历逐渐形成。

---

# 13. Goal

每个 Agent 都拥有自己的 Goal。

例如 Goose：

```text
Primary Goal:
完成任务

Secondary Goal:
识别 Duck

Social Goal:
保护 Alice
```

Duck：

```text
Primary Goal:
减少 Goose 数量

Secondary Goal:
隐藏身份

Social Goal:
获得 Bob 信任
```

Neutral：

```text
Primary Goal:
被投票淘汰
```

Goal 可以影响 Planner，但不能直接决定 Action。

---

# 14. Action

Agent 可以执行以下动作。

```text
Move(room)

DoTask(task)

Kill(agent)

Report(body)

CallMeeting()

Speak(message)

Vote(agent)

Sabotage(target)

Follow(agent)

Wait()
```

Agent 可以选择：

```text
Wait
```

这点很重要。

因为自主 Agent 不应该每次都必须行动。

---

# 15. Action合法性

Agent 可以“想做任何事情”。

但 World Engine 负责验证。

例如：

Agent：

```text
Kill(Bob)
```

World：

```text
距离过远
→ Action rejected
```

或者：

```text
当前不是 Duck
→ Action rejected
```

或者：

```text
Bob 已经死亡
→ Action rejected
```

所以：

```text
Agent = 决策者

World = 规则执行者
```

---

# 16. 游戏循环

完整游戏循环：

```text
Game Start
    ↓
Spawn Agents
    ↓
Assign Identity
    ↓
Assign Goals
    ↓
Action Phase
    ↓
Perception
    ↓
Planning
    ↓
Action
    ↓
World Event
    ↓
Memory Update
    ↓
Relationship Update
    ↓
继续行动
    ↓
Meeting Trigger
    ↓
Discussion
    ↓
Vote
    ↓
Elimination
    ↓
Win Check
    ↓
继续 / Game Over
```

---

# 17. Agent自主循环

每个 Agent 的核心循环：

```text
Wake
 ↓
Perceive
 ↓
Recall Memory
 ↓
Evaluate Beliefs
 ↓
Evaluate Goals
 ↓
Plan
 ↓
Choose Action
 ↓
Execute
 ↓
Observe Result
 ↓
Store Memory
```

这实际上就是 AgentWorld Runtime 现有：

```text
Perceive
→ Planner
→ Executor
```

的一个真实应用场景。

---

# 18. 行动阶段

Agent 自主活动。

例如：

```text
09:01

Alice → Kitchen
Bob → Engine
Charlie → Lobby
David → Kitchen
```

Runtime 不规定：

```text
Alice 必须去 Kitchen
```

而是：

```text
Alice Perceive()
        ↓
发现 Kitchen 有任务
        ↓
Planner
        ↓
Move(Kitchen)
```

---

# 19. 杀人机制

Duck 可以执行：

```text
Kill(target)
```

但是必须满足：

```text
自己是 Duck
target Alive
距离满足
当前允许 Kill
```

成功后产生：

```text
DeathEvent
```

例如：

```json
{
  "type": "agent.killed",
  "killer": "duck01",
  "victim": "goose04",
  "location": "Kitchen"
}
```

但这个事件不是所有 Agent 都自动知道。

只有符合感知条件的 Agent 才知道。

---

# 20. 尸体发现

Agent 感知到尸体后，可以：

```text
Report(body)
```

产生：

```text
MeetingEvent
```

所有存活 Agent 被唤醒。

---

# 21. 会议系统

会议分为三个阶段。

```text
Meeting
├── Discussion
├── Debate
└── Vote
```

---

## Discussion

Agent 根据自己的：

```text
Memory
Belief
Relationship
Personality
Goal
```

生成发言。

例如：

```text
Alice：

“我刚才在 Engine Room 看到 Bob。
他看到我之后马上离开了。
我觉得这件事情很奇怪。”
```

---

# 22. Agent可以撒谎

系统不要求 Agent 必须说真话。

例如：

真实情况：

```text
Bob = Duck
Bob = 杀人者
```

Bob 可以说：

```text
“我整个过程都在 Lobby。”
```

如果没有证据证明 Bob 撒谎：

```text
其他 Agent 只能根据 Belief 判断。
```

这才是社交推理。

---

# 23. Agent可以质疑

例如：

```text
Alice：

“Bob说自己一直在Lobby。”

Charlie：

“不对，我 10:21 在 Engine Room 见过 Bob。”

Bob：

“那是之前，我后来就回Lobby了。”
```

这类对话完全由 Agent 自己产生。

---

# 24. 投票

每个 Agent 独立决定投票对象。

投票依据：

```text
Belief
Memory
Relationship
Personality
Current Goal
Meeting Discussion
```

而不是：

```text
随机投票
```

---

# 25. 投票示例

```text
Alice → Bob
Bob → Alice
Charlie → Bob
David → Alice
Eve → Bob
Frank → Bob
```

结果：

```text
Bob = 4
Alice = 2
```

Bob 被淘汰。

系统公布：

```text
Bob was Goose.
```

然后所有 Agent 更新：

```text
Memory
Belief
Relationship
```

这可能直接导致：

```text
Alice Trust ↓
Charlie Trust ↓
David Suspicion ↑
```

---

# 26. 胜利条件

Goose：

```text
所有任务完成
```

或者：

```text
所有 Duck 被淘汰
```

Duck：

```text
Duck 数量达到胜利条件
```

Neutral：

```text
满足自己的独立目标
```

World Engine 负责最终判断。

Agent 不负责宣布胜利。

---

# 27. Scheduler

AgentWorld Scheduler 负责：

```text
Wake Agent
```

例如：

```text
wake_interval = 5s
```

但是不保证每个 Agent 每次都行动。

WakePolicy 可以决定：

```text
这个 Agent 是否需要思考
```

例如：

```text
附近发生事件
↓
立即 Wake

没有事件
↓
概率 Wake
```

这和 AgentWorld 现有 WakePolicy 完全一致。

---

# 28. Event System

游戏世界产生标准 Event：

```text
AgentMoved
TaskCompleted
AgentKilled
BodyFound
MeetingStarted
AgentSpoke
VoteStarted
AgentVoted
AgentEliminated
SabotageStarted
SabotageResolved
GameEnded
```

Event 可以触发：

```text
WakePolicy
Memory
Analytics
Replay
UI
```

---

# 29. GooseGame Module

游戏作为 AgentWorld Module 存在：

```go
type GooseGameModule struct {
    world *GameWorld
}
```

实现：

```go
type Module interface {
    Perceive(...)
    Planner(...)
    Executor(...)
}
```

但建议游戏专属能力进一步拆开：

```text
GooseGameModule
├── World
├── Rules
├── Perception
├── Actions
├── Meeting
├── Voting
└── WinCondition
```

---

# 30. AgentWorld 与游戏模块职责边界

这是整个项目最重要的架构边界。

### AgentWorld Runtime负责

```text
Agent生命周期
Scheduler
WakePolicy
Memory
Event
Goal
LLM
Agent执行循环
```

### GooseGame负责

```text
地图
任务
身份
游戏规则
死亡
会议
投票
胜负
```

### Agent负责

```text
感知
思考
判断
规划
决策
沟通
```

最终形成：

```text
        AgentWorld Runtime
                │
       ┌────────┴────────┐
       │                 │
     Agent            GooseGame
       │                 │
       │              World Rules
       │                 │
       └────── Action ───┘
```

---

# 31. Human Agent

第二阶段允许人类进入游戏。

人类不作为特殊系统角色。

而是：

```text
Human
 ↓
Agent
 ↓
GooseGame
```

例如：

```text
Human Agent
Name: 老陈
Identity: Goose
```

其他 Agent 不知道：

```text
老陈 = Human
```

只知道：

```text
老陈是一个 Agent
```

这样可以实现真正的：

> **Human vs AI Agent**

以及：

> **Human + AI Agent vs AI Agent**

---

# 32. 观战模式

系统支持纯观战。

例如：

```text
12 AI Agents

Human
 ↓
Spectator
```

观众可以看到：

```text
地图
Agent位置
会议
发言
投票
游戏进度
```

但是默认不显示 Agent 的隐藏身份。

---

# 33. Replay

每一局游戏都保存完整事件流：

```text
GameID

Event 001
Agent A Move

Event 002
Agent B Move

Event 003
Agent C Kill

Event 004
Agent D Report

Event 005
Meeting Start

...
```

因此可以完整重放一局游戏。

---

# 34. Agent决策回放

这是区别普通游戏 Demo 的核心功能。

观众点击：

```text
为什么 A 投了 B？
```

系统展示：

```text
A 当前目标：

保护自己

A 的 Belief：

B = Duck 0.81
C = Duck 0.24

A 的 Memory：

10:21 B 从 Engine Room 出来
10:25 B 与尸体位置重合

A 与 B：

Trust = 0.18

最终 Decision：

Vote(B)
```

这样可以直接观察 Agent 的“思维依据”。

---

# 35. Analytics

每局游戏统计：

```text
游戏时长
Agent存活时间
击杀数量
投票次数
投票准确率
谎言次数
错误判断
联盟数量
关系变化
任务完成率
```

进一步统计：

```text
Duck成功率
Goose成功率
Neutral成功率
```

---

# 36. MVP版本范围

第一版只实现：

```text
8 Agents

6 Goose
1 Duck
1 Neutral

3 Rooms

Move
Task
Kill
Report
Speak
Vote

Memory
Relationship
Belief

Meeting
Win Condition

Replay
```

暂时不实现：

```text
复杂地图
复杂任务
皮肤
实时语音
动画
装备
商城
排行榜
复杂角色
```

---

# 37. M1

第一版跑通后增加：

```text
更多地图
更多任务
更多 Neutral
Sabotage
Emergency Meeting
更多角色能力
```

---

# 38. M2

加入：

```text
Human Agent
```

实现：

```text
AI vs AI
Human vs AI
Human + AI vs AI
```

---

# 39. M3

加入：

```text
多局记忆
长期 Relationship
Agent Personality
Agent成长
Agent Reputation
```

让 Agent 不只记得一局游戏。

---

# 40. M4

开放 World Module：

```text
AgentWorld Runtime
       │
       ├── GooseGame
       ├── Werewolf
       ├── BusinessWorld
       ├── HotelWorld
       ├── SocialWorld
       └── UserWorld
```

第三方开发者可以创建自己的 World。

---

# 41. 最终产品形态

最终 AgentWorld 不应该只是：

> 一个 AI 鸭鹅杀游戏。

而应该是：

```text
             AgentWorld
                 │
        Autonomous World Runtime
                 │
      ┌──────────┼──────────┐
      │          │          │
    Agents     Worlds     Humans
      │          │          │
      └──────────┼──────────┘
                 │
           Emergent Society
```

鸭鹅杀只是第一个证明：

> **一群拥有目标、记忆、关系和不完整信息的自主 Agent，是否能够自己形成复杂社会行为。**

---

## 42. MVP成功标准

这个项目第一阶段不以“游戏好不好玩”为唯一标准。

真正的成功标准是：

**连续运行 100 局后，Agent 行为不能完全一样。**

并且能够观察到：

* Agent 主动改变计划
* Agent 错误判断
* Agent 建立信任
* Agent 失去信任
* Agent 主动合作
* Agent 主动欺骗
* Agent 主动嫁祸
* Agent 根据历史事件改变投票
* Agent 因错误信息做出错误决定
* 不同 Agent 形成不同关系网络

如果这些东西自然出现了，那么：

> **AgentWorld 的核心价值就被证明了。**

这时候《鸭鹅杀》甚至已经不是重点。

**真正的产品是 AgentWorld，鸭鹅杀只是第一个“社会实验场”。**
