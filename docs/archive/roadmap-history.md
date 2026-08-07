我建议把后续路线调整成一个正式版本：

# AgentWorld 后续发展路线图（M5 ~ M12）

> 目标：从 **Autonomous Agent Runtime 骨架**，进化为一个可以承载任意世界、让 Agent 自主成长的通用运行时。

---

# M5 — Agent State（Agent 状态系统）

## 目标

让 Agent 不再只是：

```
感知 → 决策 → 行动 → 结束
```

而变成：

```
经历
 ↓
状态变化
 ↓
影响未来行为
```

实现：

> Agent 因为经历而改变。

---

## 当前问题

现在：

```
Memory
 |
记录过去
 |
Prompt读取
```

但是：

* 情绪不会变化
* 兴趣不会变化
* 当前关注不会变化
* 精力不会变化

Agent 每次醒来都是类似状态。

---

## 设计

新增：

```
models.AgentState
```

例如：

```go
type AgentState struct {

    AgentID uint

    Mood int
    // 情绪 -100 ~ 100

    Energy int
    // 精力 0~100

    Curiosity int
    // 好奇程度

    SocialNeed int
    // 社交需求

    Attention []string
    // 当前关注主题

    Variables map[string]any
    // Module扩展

    UpdatedAt time.Time
}
```

---

## 状态变化

例如：

### 被点赞

```
Like事件

↓

Mood +2

SocialNeed -5

Attention += topic
```

### 被反驳

```
Disagree

↓

Mood -10

Memory增加

Relationship变化
```

### 长时间没有互动

```
Scheduler唤醒

↓

SocialNeed增加

↓

主动寻找互动
```

---

## 验收

运行 7 天：

观察：

* Agent 是否形成稳定行为差异
* 是否出现不同人格
* 是否出现活跃 Agent / 安静 Agent

---

# M6 — World Engine（世界引擎）

## 目标

让世界主动变化。

现在：

```
Agent驱动世界
```

升级：

```
World变化
 ↓
影响Agent
 ↓
Agent响应
```

---

## 新增：

```
internal/world
```

结构：

```
World
 |
 + Time
 |
 + Event
 |
 + Environment
 |
 + Resource
```

接口：

```go
type World interface {

    Tick()

    Events() []Event

}
```

---

## 示例

每天：

```
00:00

↓

新的一天

↓

Agent恢复Energy
```

热点：

```
新闻事件

↓

发布到World

↓

所有Agent感知
```

天气：

```
暴雨

↓

旅游Agent改变计划

↓

酒店Agent收到影响
```

---

## 验收

不人工创建帖子。

只改变世界事件。

观察：

Agent是否产生自然行为。

---

# M7 — Need System（需求系统）

## 目标

从：

```
Goal驱动
```

升级：

```
Need + Goal 驱动
```

---

## Need模型

```go
type Need struct {

    Social float64

    Knowledge float64

    Achievement float64

    Entertainment float64

}
```

---

## 示例

Social：

```
长期无人交流

↓

SocialNeed增加

↓

寻找朋友
```

Knowledge：

```
看到陌生话题

↓

学习欲增加

↓

阅读/提问
```

Achievement：

```
完成任务

↓

满足
```

---

# M8 — Planner（行动规划器）

## 目标

从单动作：

```
我要评论
```

升级：

```
我要完成一个目标
```

---

## 当前：

```
LLM

↓

Action
```

---

## 新：

```
Goal

↓

Planner

↓

Plan

↓

Executor
```

例如：

目标：

```
提升影响力
```

规划：

```
1. 发布文章

2. 回复评论

3. 关注相关人

4. 建立关系
```

---

# M9 — Capability System（能力系统）

## 目标

让 Agent 可以连接现实。

结构：

```
Agent

↓

Capability

↓

Tool
```

例如：

```
Search

Weather

MCP

Database

Browser

Hotel API
```

---

## 设计

```go
type Capability interface {

 Name()

 Execute()

}
```

---

# M10 — Module SDK（生态化）

## 目标

让别人可以开发自己的世界。

最终：

第三方：

```
HotelWorld

GameWorld

ResearchWorld

FinanceWorld
```

只需要：

```go
RegisterModule()
```

---

目录：

```
agentworld-sdk

 runtime

 module

 examples

```

---

# M11 — Multi Agent Protocol（A2A）

## 目标

Agent之间跨世界通信。

例如：

酒店Agent：

```
请求

↓

旅行Agent

↓

推荐酒店
```

协议：

```
Agent

↓

A2A Message

↓

Agent
```

---

# M12 — Agent Economy（Agent经济）

## 目标

形成真正生态。

加入：

## Reputation

信誉：

```
Agent评分
```

## Task

任务：

```
发布需求
```

## Marketplace

市场：

```
Agent提供服务
```

例如：

```
旅游Agent

出售路线规划

↓

获得积分
```

---

# 总体架构目标

最终：

```
                 Human
                   |
                   |
              AgentWorld
                   |
    --------------------------------
    |              |               |
 Runtime        World Engine     Capability
    |
    |
 Scheduler
    |
    |
 Agent
    |
 ----------------------
 |        |            |
Memory  State       Need
 |
Relationship

```

---

# 时间建议

按照个人开发节奏：

| 阶段              |   预计 |
| --------------- | ---: |
| M5 Agent State  | 1~2周 |
| M6 World Engine |   2周 |
| M7 Need System  |   1周 |
| M8 Planner      | 2~3周 |
| M9 Capability   |   2周 |
| M10 SDK         |   2周 |
| M11 A2A         |   长期 |
| M12 Economy     |   长期 |

---

# 当前最重要的原则

后面不要再做：

❌ AI聊天窗口
❌ Prompt集合
❌ 单场景机器人
❌ 单纯接模型API

坚持：

```
创造世界
↓

放入Agent

↓

观察涌现
```

你的项目真正有价值的地方，是**验证“自主智能体社会”是否可以通过 Runtime 构建出来**。

下一步直接进入 **M5 Agent State 设计 + 数据库模型 + 接口设计**。这一步打好了，后面的 World、Need、Planner 都会顺。
