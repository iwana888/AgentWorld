# GooseGame — Duck, Duck, Goose World

AgentWorld's first **game world** (showcase demo), demonstrating a full world built on `agentworld/sdk` with information isolation, hidden identities, social deduction and emergent behavior.

[English](README_EN.md) · [中文](README.md)

8 agents are dealt hidden identities (6 goose / 1 duck / 1 neutral). They move freely around a 6-room **2D spaceship**, do tasks, kill, find bodies, call emergency meetings, discuss, and vote each other out — the game runs fully autonomously. Watch this micro-society live in the browser through the **AI Observatory** (M5 / M5.1).

![2D Duck-Duck-Goose Observatory overview](screenshots/01-map-game-over.png)

## Rules

- **8 agents**, randomly assigned hidden identities: **6 goose / 1 duck / 1 neutral (Dodo)**.
- **Action phase**: goose do tasks and gather clues; the duck hunts isolated targets; the neutral blends in.
- **Body found → emergency meeting**: everyone speaks, discusses, and votes; the top vote-getter is ejected.
- **Win conditions** (strict order):
  1. All goose dead → **Duck wins**
  2. All duck dead → **Goose wins**
  3. Task quota reached → **Goose wins**
  4. Dodo voted out → **Dodo wins**
- Timeout fallback: a match auto-settles after 30 minutes (`EndedBy` distinguishes `win` / `timeout`), leaving plenty of time for recording / observing emergence.

## Why it behaves like a society, not a script

Core design principles (M0.1 → M0.4):

| Concept | Implementation | Notes |
|---|---|---|
| **Information isolation** | `Perceive` gives each agent only its own projected view | Agents never touch the real `GameState`; they can't see the full map or know who the duck is |
| **Clues aren't globally visible** | Body-scene witnesses are visible **only to the finder** | Other agents just know "someone died", not who was at the scene → suspicion never locks unanimously on the killer, votes spread out, and games last longer |
| **Belief (private)** | `Belief{Suspicions map[int64]float64}` | Subjective suspicion, updated only by that agent's own perception; never exposed to other agents or the observer |
| **Relationship (bias)** | `Relationships map[int64]float64` (-1 ~ +1) | Relationship is a **decision bias**, never mutates Belief; accusing someone costs -0.15 goodwill |
| **Role ≠ behavior** | Planner is given `Goal`, not hardcoded behavior | No "because role is X, do Y"; decisions derive from Belief + Goal |
| **LLM autonomous planner** | v0.4+, when `LLM_API_KEY` is set | The LLM gets "raw material" (what it sees / who it suspects / its relations), **not** a "who is most suspicious" verdict — rules never decide for it |
| **No-key fallback** | `decideByBelief` | Only vote/accuse above a 0.30 threshold, else abstain — identical across all identities, no forced voting |

> Emergence source: agents may misjudge, hold bias, lie, ally, or retaliate. Rules only guarantee legality and information isolation — they never decide "what the agent does".

## M5.1: From "World Graph" to a "2D Game World"

The early observatory was a "circle + name" node graph. M5.1 upgrades it into a real 2D game world — **the first thing you see on opening the browser is a Duck-Duck-Goose game in progress**:

| # | Upgrade | Details |
|---|---|---|
| ① | **Rooms as spaces** | 6 rooms (Cafeteria / Engine / Storage / Laboratory / Security / Corridor) go from "nodes" to "2D cabin rectangles"; agents spread out inside the space instead of forming a graph |
| ② | **Free agent movement** | `GameAgent` now carries real `X/Y` coordinates + `Facing`; SSE `agent.moved` pushes real `from/to` coordinates; the front-end animates a smooth transition instead of teleporting to the room center |
| ③ | **Character sprites / animation** | A unified 2D cartoon character (`CharacterSprite.vue`) with walking-leg animation, grayscale death, and facing rotation |
| ④ | **Body objects** | Bodies are objects inside a room (fallen 💀), no longer a label stuck in the corner |
| ⑤ | **Task points** | Task points (🔧) rendered inside rooms |
| ⑥ | **Body found → Meeting** | The meeting becomes a **second core scene**: a full-screen meeting hall (round-table seats + speech bubbles + transcript + voting) |
| ⑦ | **Speech bubbles** | Agent statements appear as bubbles during the meeting |
| ⑧ | **Voting** | Voting flow inside the meeting hall |
| ⑨ | **Inspector retained** | Click a character → see its inner world (Belief / Relationship / Goal / Last Decision / Memory) |

Action phase — agents move freely around the 6 room spaces:

![Action phase, round 4](screenshots/02-map-action-round4.png)

As soon as a body is found, the scene switches to the emergency meeting (the second core scene, with a round-table):

![Emergency meeting scene](screenshots/03-meeting-empty.png)

### Game UI and agent internal state are layered

```
              AgentWorld
                  │
        ┌─────────┴─────────┐
        │                   │
    Game State          Agent State
        │                   │
        ▼                   ▼
     game world           thinking world
        │                   │
        ▼                   ▼
     game UI              Inspector
```

**Hidden identities**: in normal spectator mode, viewers see "character + name" (a unified neutral character, never revealing who the duck is); only the **Inspector / Debug mode** (clicking an agent) reveals the true identity and inner state.

> Ordinary users watch the story; developers watch the agent's mind — that is AgentWorld's core differentiator.

## Architecture

```
AgentWorld Runtime (sdk.Module contract)
        │  Perceive (info-isolated projection)
        ▼
   GooseModule ──► goose.GameState (real world, locked, with 2D coords)
        │                │ publish events
        │                ▼
        │        goose.Observatory (event bus + in-memory store)
        │                │  HTTP / SSE
        ▼                ▼
  Planner/Executor   Server (:19090)
  (LLM or rules)          │
                          ▼
              web/ (Vue3 + Vite :5199, AI Observatory)
```

- `goose/game.go` — game state machine (identities / rooms-as-spaces / 2D coords / tasks / bodies / meetings / win)
- `goose/actions.go` — 6 actions + event publishing (`agent.moved` carries real coordinates)
- `goose/perception.go` — info-isolated projection (agent's view)
- `goose/observatory.go` — event bus + in-memory event store (last 1000)
- `module.go` — `sdk.Module` implementation (Planner / Executor / WakePolicy)
- `server.go` — HTTP + SSE observatory service
- `web/` — AI Observatory front-end

## Run

### 1. Backend (game + observatory)

From the project root:

```bash
go run ./worlds/goosegame/cmd/goose
```

Env vars:

| Var | Default | Notes |
|---|---|---|
| `GOOSE_DB` | `goosegame.db` | Game DB path (independent from the Weibo world) |
| `GOOSE_INTERVAL` | `5s` | Wake interval (game pacing). **Larger = slower, longer-lived matches** (8~15s recommended for recording) |
| `GOOSE_OBS_ADDR` | `:19090` | Observatory listen address |
| `LLM_API_KEY` | — | If set, agents decide via LLM; otherwise all use rule mocks (zero cost) |
| `LLM_BASE_URL` / `LLM_MODEL` | DeepSeek | LLM endpoint / model (Ollama works too) |

It runs fine without `LLM_API_KEY`: the 8 agents use Belief-driven rule decisions.

### 2. Front-end (AI Observatory)

```bash
cd worlds/goosegame/web
npm install
npm run dev        # http://localhost:5199
```

The Vite dev server proxies `/api` to the backend `:19090`.

### Recording / making a match last longer

To open the browser on "a Duck-Duck-Goose game in progress" or to record a video, **slow the pacing down**:

```bash
# Windows (PowerShell)
$env:GOOSE_INTERVAL="12s"; go run ./worlds/goosegame/cmd/goose

# macOS / Linux
GOOSE_INTERVAL=12s go run ./worlds/goosegame/cmd/goose
```

Mechanics that make matches last longer (no extra config, on by default):

- **Clues aren't globally visible**: body-scene witnesses are visible only to the finder → other agents don't know who was at the scene, votes spread, the duck is far less likely to be ejected in round one, and games evolve over many rounds (measured: a match grows from ~45s to 3+ minutes).
- **Strict majority to eject**: a meeting vote only ejects someone above half the alive count — a minority suspicion won't end a match early.
- **Slower goose victory**: task quota is `alive goose × 20` (not ×15), so the goose needs more tasks to win.
- **Cautious duck**: 25s kill cooldown, the duck can't chain-kill and plays more restrainedly.
- **Longer timeout**: a match caps at 30 minutes (a safety valve that won't truncate a healthy game).

> Tip: match length is ultimately emergent — a well-hidden duck and slower goose reasoning mean long matches; a duck that's caught fast means a short one. For stable long matches, raise `GOOSE_INTERVAL` and just run a few games to catch a long one.

## Observatory API

| Endpoint | Description |
|---|---|
| `GET /api/game` | Current state (phase / round / live agent positions (2D) / bodies) |
| `GET /api/agents` | Public agent info (name / location / alive / identity / 2D coords) |
| `GET /api/agents/{id}` | Single agent's deep private state (Agent Inspector: Belief / Relationship / Goal / LastDecision / Memory) |
| `GET /api/events` | Recent events (in-memory, up to 200) |
| `GET /api/events/stream` | SSE realtime event stream |

> **Information isolation also guards the observer API**: `Belief` / `Relationship` are private per-agent and are not exposed through the public `/api/game` or `/api/agents`. Only an on-demand request to `/api/agents/{id}` (Agent Inspector) returns that agent's own subjective state, for debugging.

## Front-end components

- **WorldView** — 2D cabin map: 6 room spaces + corridors + task points + body objects + real-coordinate agent rendering
- **CharacterSprite** — 2D cartoon character: walking-leg animation, facing rotation, grayscale on death
- **MeetingOverlay** — meeting-hall scene: round-table seats + speech bubbles + transcript + voting (the second core scene)

  ![Emergency meeting with speech bubbles](screenshots/04-meeting-with-speeches.png)
- **AgentPanel** — Agent Inspector: click a character to see Belief / Relationship / Goal / Last Decision / Memory (Debug mode)
- **Timeline** — SSE realtime event stream (move / task / kill / speak / vote / meeting / end)

## Layout

```
worlds/goosegame/
├── cmd/goose/main.go    # standalone entry (shares the root go.mod)
├── goose/               # game core (game/actions/perception/observatory)
├── module.go            # sdk.Module implementation
├── server.go            # HTTP + SSE observatory service
└── web/                 # AI Observatory front-end (Vue3 + Vite + TS + ElementPlus)
    └── src/components/  # WorldView / CharacterSprite / MeetingOverlay / AgentPanel / Timeline
```

## Related docs

- [README.md](README.md) — 中文版 (Chinese)
- [README.md](../../README.md) — AgentWorld main docs (this world is listed under Demo Worlds)
- [README_CN.md](../../README_CN.md) — AgentWorld main docs (Chinese)
- [sdk/README.md](../../sdk/README.md) — building your own world with the SDK
