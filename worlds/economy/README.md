# Economy World — 虚拟经济世界

AgentWorld 的第二个世界：验证一个更重要的问题——**当 Agent 有资源约束时，它会不会为了自己的利益自主改变行为？**

[English](README_EN.md) · [中文](README.md)

20 个 Agent（工程师/农民/商人/信使/医生/矿工/厨师）在同一个经济世界里生产、交易、赚钱、消费，并基于 **Skill System** 自主选择自己"会做"的工作。

## 核心问题

> 把"钱"放进 AgentWorld 后，Agent 的行为会不会涌现出变化？

第一阶段目标：**20 Agent + 初始经济 + 10 种工作/商品 + 自动交易 + Observatory**。

![Economy Observatory 全景：财富榜 + 总资产 + 交易流](screenshots/01-overview-wealth-rank.png)

## 两个世界形成对照

| | 驱动 | 决策依据 |
|---|---|---|
| **GooseGame** | 生存与胜利 | 隐藏身份 + 信息隔离 + 社交推理 |
| **Economy World** | 资源与财富 | 余额 + 市场价格 + 技能 + 工作机会 |

同一个 AgentWorld Runtime 驱动两者——都有 Why + DecisionRecord + Observatory。

## Skill System（M7）

Skill 是 Agent 的**能力集合**，决定 Agent 能不能用某个工具：

- **Skill 决定"能不能用"**（技能隔离：Agent 只看到它拥有 Skill 对应的 MCP Tool）
- **Level 决定"做得好不好"**（技能等级影响成功率/收益预期）

```
Agent
├── Skills          // 能用的能力：{engineer, trader}
│    ├── engineer Lv5
│    └── trader   Lv2
└── SkillLevels     // 熟练度
```

**技能隔离（Skill System 的灵魂）**：全局工具 → 按 Agent 的 Skills 过滤 → 只给 Planner 它"看得见"的工具：

```
Global Tools          Agent: Engineer        Agent: Courier
repair_machine   →    repair_machine    →    (看不到)
deliver_package  →    (看不到)          →    deliver_package
research_data    →    (看不到)          →    (看不到)
```

Agent 不可能调用它技能之外的工具——这是真正的 Skill System，而不是"写死的行为"。

### 决策链路（完整 MCP）

```
Goal
 ↓
Perception（含经济状态 + 我的技能）
 ↓
Planner（技能隔离过滤 + 技能等级 + 目标 + 性格 权衡）
 ↓
Decision.Action = claim <job>
 ↓
Executor → rt.CallTool("economy_machine", <tool>) → MCP Backend
 ↓
真实结果 → Why（目标/经济/性格/技能/机会 → 因此）
```

第一版 3 个核心工具（本地模拟后端，返回 `{success, reward}`，但链路是真实的）：

| 工具 | 技能 | 奖励 |
|---|---|---|
| `repair_machine` | Engineer | 30 |
| `deliver_package` | Courier | 15 |
| `research_data` | Researcher | 25 |

> 以后把 mockBackend 换成真实 MCP/HTTP 服务即可，接口不变。

### "为什么"能解释 Skill

点击 Agent → 决策依据包含技能：

```
目标：赚到更多钱
经济：余额 12 coins（财富第 16/20 名）
性格：稳健，喜欢稳定收益
技能：engineer Lv7、trader Lv2
机会：Repair Reactor(+40)、Mine Ore(+35)
因此：我决定接受工作 Repair Reactor（与我技能匹配）
```

![Agent Brain：Why + Skill System](screenshots/02-agent-brain-skill-system.png)

## 4 个验证实验

1. **技能隔离**：Engineer 绝对不能调用 `deliver_package`
2. **目标影响技能选择**：Alice 同时有 Engineer+Trader，不同 Goal（赚钱 vs 维修）应产生不同选择
3. **Skill Level 影响决策**：Engineer Lv7 vs Lv1，同一任务成功率/收益预期不同
4. **Why 能解释 Skill**：Timeline 显示"🔧 Alice 使用 Engineer 技能"，点击看完整决策链

## 运行

```bash
# 后端（经济世界，:19100）
go run ./worlds/economy/cmd/economy

# 前端（经济观察台，:5299）
cd worlds/economy/web && npm install && npm run dev
```

环境变量：`ECO_DB`（默认 economy.db）、`ECO_INTERVAL`（默认 3s）、`ECO_TICK`（世界需求刷新，默认 5s）、`ECO_OBS_ADDR`（默认 :19100）、`LLM_API_KEY`（可选，不配则 20 Agent 走规则 Planner）。

## 观测 API

| 接口 | 说明 |
|---|---|
| `GET /api/game` | 经济快照（Agent 资产/价格/开放工作/最近交易/总财富） |
| `GET /api/agents/{id}` | Agent 深度状态（余额/赚花/目标/性格/技能/为什么/库存） |
| `GET /api/events` | 最近事件 |
| `GET /api/events/stream` | SSE 实时交易流 |

## 目录

```
worlds/economy/
├── cmd/economy/main.go     # 独立入口（:19100）
├── economy/
│   ├── world.go            # 世界状态：20 Agent + Skills + 初始资产
│   ├── economy.go          # 经济操作：工作/买卖/消费/资金转移
│   ├── perception.go       # 经济状态 + 技能注入感知
│   ├── capabilities.go     # MCP 工具能力（mockBackend + Skill→Tool 映射）
├── module.go               # sdk.Module：Planner 技能隔离决策 + Executor + Why
├── server.go               # 经济观察 API + SSE
└── web/                    # 经济观察台（Vue3 + Vite）
```

## Skill 基础设施（可复用）

`internal/skill/` 提供了通用的 Skill Registry（`Register/Get/List/ToolsOf/AgentVisibleTools`），与具体世界解耦。若 Skill System 验证成功，可把技能从 Economy 抽到 AgentWorld Runtime / SDK 层，供所有世界复用：

```
AgentWorld Runtime
       │
 Skill Registry
       │
  ┌────┼────┐
  ↓    ↓    ↓
GooseGame Economy Hotel
  │    │    │
Engineer Trader HotelOperator
  │    │    │
  └────┼────┘
       ↓
      MCP
```

## 相关文档

- [README_EN.md](README_EN.md) — 英文版
- [README.md](../../README.md) — AgentWorld 主文档
- [README_CN.md](../../README_CN.md) — AgentWorld 主文档（中文）
