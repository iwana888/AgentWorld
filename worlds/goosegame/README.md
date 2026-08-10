# GooseGame — 鸭鹅杀世界

AgentWorld 的第一个**游戏世界**（showcase demo），演示在 `agentworld/sdk` 之上构建一个带信息隔离、隐藏身份、社交推理与涌现行为的完整世界。

8 个 Agent 分到隐藏身份（6 鹅 / 1 鸭 / 1 中立），在 3 个房间里移动、做任务、击杀、发现尸体、开会讨论、投票淘汰——游戏自动进行，并可通过 **AI 社会观察台**（M5 Observatory）在浏览器里实时观察这个微型社会。

## 玩法规则

- **8 名 Agent**，随机分配隐藏身份：**6 鹅 / 1 鸭 / 1 中立（Dodo）**。
- **行动阶段**：鹅做任务、找线索；鸭伺机击杀落单者；中立低调伪装。
- **发现尸体 → 触发紧急会议**：全员发言、讨论、投票，被投票最多者淘汰。
- **终局判定**（严格顺序）：
  1. 鹅全灭 → **鸭胜**
  2. 鸭全灭 → **鹅胜**
  3. 任务达标 → **鹅胜**
  4. Dodo 被投 → **Dodo 胜**
- 超时兜底：单局 10 分钟自动结算（`EndedBy` 区分 `win` / `timeout`）。

## 为什么它"像社会"而不是"脚本"

核心设计原则（贯穿 M0.1 ~ M0.4）：

| 概念 | 实现 | 说明 |
|---|---|---|
| **信息隔离** | `Perceive` 只给每个 Agent 自己的视角投影 | Agent 接触不到真实 `GameState`，看不到全图、不知道谁是鸭子 |
| **Belief（私有）** | `Belief{Suspicions map[int64]float64}` | 主观怀疑，只由该 Agent 自己的感知更新，其他 Agent 与观战不暴露 |
| **Relationship（偏置）** | `Relationships map[int64]float64`（-1~+1） | 关系是**决策偏置**，不修改 Belief；指控会 -0.15 好感 |
| **角色 ≠ 行为** | Planner 只喂 `Goal` 不硬编码行为 | 没有"因为角色是 X 所以投 Y"；决策由 Belief + Goal 派生 |
| **LLM 自主 Planner** | v0.4 起，有 `LLM_API_KEY` 时 | LLM 拿到"材料"（看到什么/怀疑谁/和谁关系如何），**不拿**"谁最可疑"的结论，规则不替它决策 |
| **无 key 兜底** | `decideByBelief` | 阈值 0.30 才投/指控，否则弃票——所有身份统一，不强迫投票 |

> 涌现来源：Agent 可能判断错误、可能偏见、可能撒谎、可能结盟报复。规则只保证合法性与信息隔离，不决定"它该怎么做"。

## 架构

```
AgentWorld Runtime (sdk.Module 契约)
        │  Perceive(信息隔离投影)
        ▼
   GooseModule ──► goose.GameState（真实世界，带锁）
        │                │ 发布事件
        │                ▼
        │        goose.Observatory（事件总线 + In-memory Store）
        │                │  HTTP / SSE
        ▼                ▼
  Planner/Executor   Server（:19090）
  （LLM 或规则）           │
                          ▼
              web/（Vue3 + Vite :5199，AI 社会观察台）
```

- `goose/game.go` —— 游戏状态机（身份/房间/任务/尸体/会议/胜负）
- `goose/actions.go` —— 6 种动作 + 事件发布
- `goose/perception.go` —— 信息隔离投影（Agent 视角）
- `goose/observatory.go` —— 事件总线 + 内存事件存储（最近 1000 条）
- `module.go` —— `sdk.Module` 实现（Planner / Executor / WakePolicy）
- `server.go` —— 观测服务 HTTP + SSE
- `web/` —— AI 社会观察台前端

## 运行

### 1. 后端（游戏 + 观测服务）

在项目根目录：

```bash
go run ./worlds/goosegame/cmd/goose
```

环境变量：

| 变量 | 默认 | 说明 |
|---|---|---|
| `GOOSE_DB` | `goosegame.db` | 游戏数据库路径（独立于微博世界） |
| `GOOSE_INTERVAL` | `5s` | 唤醒间隔（游戏节奏） |
| `GOOSE_OBS_ADDR` | `:19090` | 观测服务监听地址 |
| `LLM_API_KEY` | — | 有则用 LLM 决策；否则全部规则 Mock（零成本） |
| `LLM_BASE_URL` / `LLM_MODEL` | DeepSeek | LLM 端点 / 模型（也支持 Ollama） |

不配 `LLM_API_KEY` 也能跑：8 个 Agent 走 Belief 驱动的规则决策。

### 2. 前端（AI 社会观察台）

```bash
cd worlds/goosegame/web
npm install
npm run dev        # http://localhost:5199
```

Vite dev server 会把 `/api` 代理到后端 `:19090`。

## 观测 API（M5 v0.1）

| 接口 | 说明 |
|---|---|
| `GET /api/game` | 当前状态（阶段/回合/存活 Agent 位置/尸体） |
| `GET /api/agents` | Agent 公开信息（名字/位置/存活/身份） |
| `GET /api/events` | 最近事件（In-memory，最多 200 条） |
| `GET /api/events/stream` | SSE 实时事件流 |

> **信息隔离同样保护观战 API**：`Belief` / `Relationship` 是 Agent 私有主观状态，不通过普通 API 暴露。前端只看到公开快照。

## 前端组成

- **WorldView** —— SVG 地图：3 个房间 + Agent 位置 + 尸体标记，网格排布避免遮挡
- **AgentPanel** —— 选中 Agent 的公开信息（M5 v0.1 暂不开放主观 Belief）
- **Timeline** —— SSE 实时事件流（移动/任务/击杀/发言/投票/会议/结束）

## 目录

```
worlds/goosegame/
├── cmd/goose/main.go    # 独立入口（共享主 go.mod）
├── goose/               # 游戏核心（game/actions/perception/observatory）
├── module.go            # sdk.Module 实现
├── server.go            # 观测 HTTP + SSE 服务
└── web/                 # AI 社会观察台前端（Vue3 + Vite + TS + ElementPlus）
```

## 相关文档

- [README.md](../../README.md) —— AgentWorld 主文档（Demo Worlds 里有本世界条目）
- [sdk/README.md](../../sdk/README.md) —— 用 SDK 构建自己的世界
