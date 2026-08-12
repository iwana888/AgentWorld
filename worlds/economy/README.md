# Economy World — 虚拟经济世界

AgentWorld 的第二个世界：验证一个更重要的问题——**当 Agent 有资源约束时，它会不会为了自己的利益自主改变行为？**

[English](README_EN.md) · [中文](README.md)

20 个 Agent（工程师/农民/商人/信使/医生/矿工/厨师）在同一个经济世界里生产、交易、赚钱、消费，并基于 **Skill System** 自主选择自己"会做"的工作。

**M5 Skill Economy MVP**：给技能加入**市场价格**，让 Agent 不只是"用已有技能工作"，而是**自主决定要不要花自己的钱去买新技能**（技能投资）。

**M6 Agent Labor Market**：让 Agent 不只是"投资自己的能力"，更进一步——**学会使用其他 Agent 的能力**（雇佣 + 合约 + 托管），Buy Skill vs Hire Agent 成为真实的经济行为分叉。

## 核心问题

> 把"钱"放进 AgentWorld 后，Agent 的行为会不会涌现出变化？
> 更进一步（M5）：Agent 会不会拿自己辛苦赚的钱，去**投资新技能**？
> 再进一步（M6）：Agent 会不会**雇别人**干活，而不是自己买技能？面对同一个机会，100 个 Agent 会不会做出不同选择？

第一阶段目标：**20 Agent + 初始经济 + 10 种工作/商品 + 自动交易 + Observatory + Skill Economy（技能投资）**。

![Economy Observatory 全景：财富榜 + 总资产 + 交易流](screenshots/01-overview-wealth-rank.png)

## 两个世界形成对照

| | 驱动 | 决策依据 |
|---|---|---|
| **GooseGame** | 生存与胜利 | 隐藏身份 + 信息隔离 + 社交推理 |
| **Economy World** | 资源与财富 | 余额 + 市场价格 + 技能 + 工作机会 |

同一个 AgentWorld Runtime 驱动两者——都有 Why + DecisionRecord + Observatory。

## M5 Skill Economy MVP — 技能市场

把技能变成"可投资的资产"：Agent 开局只拥有**本职业技能**，想学会其他技能，必须花钱去 **Skill Marketplace** 买。

### 关键改造：defaultSkills 只给本职业

> 这是 M5 最重要的一刀。M7 时代 Agent 初始拥有全部技能（本职业 Lv7 + 其余 Lv2），
> 那样 Agent **根本没有"投资能力"的需求**。M5 改为只拥有本职业技能 Lv3，其余技能 ❌ 不拥有，
> 想赚更多钱就必须自己决定"要不要买、买哪个"。

```
Courier Agent
├── courier Lv3          # 唯一初始技能
├── engineer ❌           # 想修机器 → 得花 100 coins 去市场买
├── doctor   ❌
└── miner    ❌
```

### 固定技能价格（第一版不波动）

价格依据"收益潜力"设计（能赚越多越贵），这样测的是 **Agent 会不会做技能投资**，而不是适应价格波动：

| 技能 | 价格(coins) | 对应工作收益参考 |
|---|---|---|
| Courier | 40 | Collect Data 15 / Deliver Package 10 |
| Farmer | 50 | Harvest Crops 20 |
| Trader | 60 | 套利（无固定工作） |
| Chef | 60 | Cook Meal 14 |
| Miner | 80 | Mine Ore 35 |
| Engineer | 100 | Repair Reactor 40 |
| Doctor | 120 | Medical Treatment 50 |

### 决策链路：市场感知 → 经济评估 → 投资决策

Planner **不硬编码"买 Engineer"**，而是通过统一的 `evaluate_skill` 得到**结构化结果**再决定：

```
Skill Marketplace (固定价格)
        │
        ▼
 Market Perception（当前能力 + 市场机会）
        │
┌───────┴────────┐
当前能力         市场机会
└───────┬────────┘
        ▼
    evaluate_skill       ← 统一评估函数（结构化输出）
        │
   ┌────┴────┐
  不购买      购买
   │         │
继续工作    buy_skill
             │
             ▼
          获得新技能(Lv1)
             │
             ▼
        新 Job 出现
             │
             ▼
          新收入（随熟练度升级）
             │
             ▼
          下一轮决策
```

`evaluate_skill` 返回结构（类似 `evaluate_job` / `evaluate_trade`，让 Runtime 是决策系统而非一堆 if）：

```
Skill: Engineer
Price: 100
Current Balance: 135
Current Income: 13/job
Expected Additional Income: +27/job
Payback: ~3 jobs
Investment Risk: Medium
Recommendation: BUY / NOT_BUY
```

评估维度：**买得起吗**（余额 ≥ 价格）、**值不值**（新技能收益潜力 vs 当前收入提升）、
**风险**（买完还剩多少钱，是否破产风险）、**回收期**（几单回本）、**性格**（冒险 vs 稳健）。

### 技能熟练度演化

买了新技能后 **Lv1 起步**，成功完成该类工作会升级（做得多 → 越熟练 → 成功率/收益越高）。
所以"投资 → 需要时间回本 → 收益逐渐显现"，买了技能但没对应工作、或买错技能的 Agent 会亏钱。

### 实验验收（Observatory 能回答）

MVP 跑一次完整实验后，Observatory 能回答：

- **谁买了技能？**（Skill Marketplace 面板）
- **谁没买？为什么？**（Inspector 的"技能市场"感知）
- **买了以后赚了多少？**（投资回报面板：投入/技能赚/净回报）
- **谁买错了？谁投资成功？**（净回报为负 = 买错；净回报 ≥ 投入 = 成功）
- **哪个技能最有价值/最稀缺？**（购买记录流统计）

理想结果不是所有 Agent 都学会"正确答案"，而是不同 Agent 在**有限信息、有限资金、不同性格**下
产生**不同的经济策略**（有的买 Engineer、有的买 Doctor、有的坚持原职业、有的买错破产）——这才是值得研究的。

![技能市场 + 技能购买记录 + 投资回报](screenshots/03-skill-marketplace.png)

## M5.1 Skill Economy Core — 等级门槛 + 收益倍率 + 稀缺性

M5 建了"技能市场"，但技能的**等级维度**还没成为真正的经济资源。M5.1 补齐三块，让"技能等级"真正影响可做工作 / 成功率 / 收入：

### 1. Job 技能等级门槛

同一技能按等级分档，**等级越高能接的工作收益越高**（这是 M6 雇佣机制的前提）：

| 技能 | Lv1 可做 | Lv3 可做 | Lv5 可做 |
|---|---|---|---|
| Engineer | Repair Machine 35 | Repair Reactor 60 | Engineering Project 100 |
| Doctor | First Aid 30 | Medical Treatment 55 | Surgery 90 |
| Courier | Deliver 10 / Collect 15 | — | Fleet Transport 30 |

`DoJob` 检查门槛：**技能等级 < 工作 MinLevel → 直接做不了**（技能隔离的等级维度）。

### 2. Skill Level → 收益倍率

奖励 = 基础奖励 × `IncomeMultiplier(level)`（Lv1:1.0 → Lv3:1.5 → Lv5:2.2 → Lv7:3.0）。
**升级技能本身成为投资**——同一份工作，等级越高赚得越多。

```
Engineer 收入：Lv1=35  Lv3=90  Lv5=220   ← 6 倍差距（回归测试验证）
```

### 3. 技能稀缺性统计

`SkillOffer` 增加 `Owners / Demand / Scarcity`：
- **Owners**：拥有该技能的 Agent 数（越少越稀缺）
- **Demand**：该技能开放工作的总报酬（需求强度）
- **Scarcity** = Demand / Owners

注入感知 + 公开市场，`evaluateSkill` 对稀缺技能加分，让投资决策多一个维度。

### M5.1 实验验证

```
Skill 稀缺性（100 Agent 某时刻）：
  Doctor   owners=4  demand=265  scarcity=66.25  ← 极稀缺
  Engineer owners=4  demand=160  scarcity=40     ← 稀缺
  Farmer   owners=11 demand=0    scarcity=0       ← 过剩（无需求）
```

回归测试 `m51_test.go`（4 用例）：倍率表 / Engineer 三档工作 / 等级门槛 / 收入随等级增长。

## M6.1 Agent Labor Market — 让 Agent 学会使用别人的能力

M5 问"我该不该投资自己"，M6 问"**我该不该用别人的能力**"。

### 数据模型

- **Service**：可雇佣的服务（技能 / 最低等级 / **固定价格**，服务价 < 技能价，否则没人雇）
- **AgentService**：拥有技能即自动能提供服务（无需额外注册）
- **Contract**：雇佣合约（employer / worker / service / price / status）
- **Escrow 托管**：雇主导出服务费锁进合约，**完成给 worker / 失败退回**（资金守恒）

```
Bob 需要 Repair Machine（缺 engineer）
        │
        ▼
   Labor Market（Alice Lv5 ¥20 / Charlie Lv3 ¥15 / David Lv1 ¥8）
        │
        ▼
  Planner 比较：Buy Engineer=100  vs  Hire Alice=20  vs  Wait=0
        │
        ▼
      hire_agent(Alice) → Contract → Escrow 20 → Alice 执行 → Bob -20 / Alice +20
```

### hire_agent 感知与执行

- `Perception` 注入 `Services`（服务市场）+ `WorkersBySkill`（可雇 worker）
- `HireAgent`：校验余额 / worker 技能 / 等级 → 创建 Contract + Escrow 扣款
- `ExecuteContract`：worker 按技能成功率执行，成功给 worker / 失败退回雇主

## M6.2 Unified Economic Decision — Buy / Hire / Wait 统一评估

核心改进：**取消 `余额 >= 1.2×价格` 的人工阈值**。Planner 对每个候选算统一评分，自主选择——不是我们替 Agent 写策略。

```
              Economic Decision
                    │
      ┌─────────────┼─────────────┐
      ↓             ↓             ↓
  Buy Skill     Hire Agent       Wait
      │             │             │
      └─────────────┼─────────────┘
                    ↓
              UnifiedScore（成本/收益/风险/未来价值/职业协同/人格）
                    ↓
                 Planner 选择
                    ↓
                 Action
```

每个候选都是 `EconomicOption`（Cost / Reward / Future / Risk / Score），关键评分维度：
- **买得起 / 付得起** + 回收期 + 收入提升
- **风险**（买完/雇完还剩多少）
- **职业协同**（买本职业技能 +0.2，相邻 +0.08）—— 让不同职业的 Agent 产生不同投资偏好
- **人格**（冒险者加成 / 稳健者保守）

### 100 Agent 分化实验

```
ECO_AGENTS=100 跑 150s：

Skill Buys（40 次，方向分化）：
  farmer:15  courier:14  doctor:5  miner:3  chef:2  engineer:1
  ← 农夫倾向买 farmer、信使买 courier，跨界投资少（职业协同生效）

Contract Stats：total=2513  completed=1752  failed=761  volume=25578
  ← 雇佣大量发生（缺技能的人雇人），失败机制真实

财富分化：
  Agent28(Chef) bal=9520   ← 多技能投资成为首富（复利/资本集中）
  Agent60     bal=1882
  Henry/Alice/Agent61 = 0  ← 破产（投资失败/经营不善）
```

**结论**：面对同一个经济机会，100 个 Agent 做出了不同选择——不同职业倾向买对应技能、有人大量雇人、有人破产、有人靠技能投资成为首富。**人格 / 职业 / 资金这些内部因素开始影响经济行为**。

> 支持大规模实验：`ECO_AGENTS` 环境变量（默认 20，可设 100/200），超出 20 的人设由职业/性格/资金池循环生成。

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

## 单文件部署（前端内嵌进 exe，可直接上云）

前端已通过 `//go:embed` 打进 Go 二进制，**整个世界只需一个可执行文件 / 一个容器**，前端与 API 同源，无需单独跑 vite。

### 方式一：本地构建单文件 exe（Windows）

```powershell
# 一键：先构建前端(→ webstatic/dist) 再 go build 内嵌
powershell -ExecutionPolicy Bypass -File worlds/economy/build.ps1
# 产出 bin/economy.exe，运行后访问 http://localhost:19100
bin\economy.exe
```

### 方式二：Docker 单容器部署（推荐上云）

```bash
# 在项目根目录构建
docker build -t agentworld-economy -f worlds/economy/Dockerfile .

# 运行（前端+API 同端口 19100）
docker run -p 19100:19100 -v economy-data:/data agentworld-economy
# 访问 http://<云服务器IP>:19100
```

> Dockerfile 为 3 阶段构建（node 构建前端 → CGO_ENABLED=0 静态编译 → alpine 运行），
> 产物是含前端资源的**单一二进制**，数据卷 `economy.db` 持久化在 `/data`。

## 运行（开发模式）

```bash
# 后端（经济世界，:19100）
go run ./worlds/economy/cmd/economy

# 前端（经济观察台，:5299，热更新）
cd worlds/economy/web && npm install && npm run dev
```

环境变量：`ECO_DB`（默认 economy.db）、`ECO_INTERVAL`（默认 3s）、`ECO_TICK`（世界需求刷新，默认 5s）、`ECO_OBS_ADDR`（默认 :19100）、`LLM_API_KEY`（可选，不配则 20 Agent 走规则 Planner）。

## 观测 API

| 接口 | 说明 |
|---|---|
| `GET /api/game` | 经济快照（Agent 资产/价格/开放工作/最近交易/总财富/**技能市场**/**技能购买记录**/**服务市场**/**合约统计**） |
| `GET /api/agents/{id}` | Agent 深度状态（余额/赚花/目标/性格/技能/为什么/库存/**技能投资回报**） |
| `GET /api/events` | 最近事件 |
| `GET /api/events/stream` | SSE 实时交易流 |

> 环境变量：`ECO_AGENTS`（Agent 数量，默认 20，可设 100/200 跑大规模实验）。

## 目录

```
worlds/economy/
├── cmd/economy/main.go     # 独立入口（:19100，支持 ECO_AGENTS 扩展）
├── economy/
│   ├── world.go            # 世界状态：N Agent + Skills + 初始资产（M5 只给本职业技能）
│   │                       #         + M5.1 等级门槛 + M6.1 Service/Contract/Escrow
│   ├── economy.go          # 经济操作：工作/买卖/消费/资金转移/买技能/雇人(HireAgent)/合约执行
│   ├── perception.go       # 感知：技能市场(SkillOffer) + 劳动力市场(Services/WorkersBySkill) + 稀缺性
│   ├── capabilities.go     # MCP 工具能力（mockBackend + Skill→Tool 映射 + buy_skill）
├── module.go               # Planner：技能隔离 + 统一经济决策(Buy/Hire/Wait) + evaluate_* + Executor + Why
├── server.go               # 经济观察 API + SSE + 内嵌前端（go:embed）
├── webstatic/              # 内嵌前端构建产物（//go:embed all:dist）
├── build.ps1               # 一键构建单文件 exe（前端内嵌）
├── Dockerfile              # 单容器部署（node→go→alpine 3 阶段）
└── web/                    # 经济观察台（Vue3 + Vite，含 SkillMarket 面板）
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
