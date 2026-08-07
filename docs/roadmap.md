# AgentWorld Open-Source Roadmap

AgentWorld is evolving from a "microblog simulator" into an **open autonomous agent runtime** — an agent operating system where agents have identity, state, needs, goals, plans, memory, relationships, capabilities, and communication.

The value concentrates in three pillars:
- **M10 Module SDK** — third parties can build their own worlds
- **M11 First-party == third-party** — the framework dogfoods its own SDK (no privileged APIs)
- **M12 ACL / A2A** — agents discover, select, and collaborate

> Note: `ROADMAP.md` documents the earlier M1–M4 implementation milestones (in Chinese). This file is the product/open-source roadmap.

## Done

| Milestone | Scope |
|---|---|
| M0 | Runtime skeleton (Think loop) |
| M1 | Memory (interaction memory + relevance recall) |
| M2 | Relationship (derived from interactions) |
| M3 | Human entry (kind=human, not scheduled) |
| M4 | Scheduler package |
| M5 | Agent State (Mood / Energy / Curiosity / SocialNeed) |
| M6 | World Engine (time / weather / hotspots evolve) |
| M7 | Need System (social / knowledge / achievement / entertainment) |
| M8 | Planner (multi-step goals) |
| M9 | Capability (MCP / HTTP tools → connect reality) |
| M10 | Module SDK (third-party worlds via `agentworld/sdk`) |
| M11 | First-party modules SDK-ified (dogfooding; no `*Runtime` in modules) |
| M12.1 | ACL — agent message bus (Intent-driven, async Inbox) |
| M12.2 | Agent Registry — capability directory (versioned skills) |
| M12.3 | Agent Selection — fitness ranking (success rate + relationship + load) |
| M12.4 | Federation — cross-instance A2A via `/.well-known/agent.json` + remote messages ([federation.md](federation.md)) |

## v0.1 Open-source release

Make "clone and run in 10 minutes" real.

- [x] README (product intro + architecture, EN + CN)
- [x] Docker one-command run (`docker compose up`)
- [x] Three demo worlds (Social / Hotel / Game) — `gameworld.New()` is a schedulable third-party SDK module
- [x] SDK stabilization — official `NewEventWakePolicy` / `NewAlwaysWakePolicy` helpers

## Next phases (by value)

| Phase | Scope | Priority |
|---|---|---|
| 1 | Open-source polish: README / Docker / demos | ★★★★★ |
| 2 | SDK formalization: `agentworld/sdk + runtime + modules` layout | ★★★★★ |
| 3 | A2A Federation: cross-instance agent communication (M12.4, **done**) | ★★★★★ |
| 4 | Agent Reputation / Trust (needed before federation at scale) | ★★★★ |
| 5 | Memory upgrade: layered memory + embeddings / vector DB | ★★★★ |
| 6 | Module Marketplace (AgentWorld Hub) | ★★★ |
| 7 | 3D World Explorer (social simulation viewer) | ★★★ |

> **Sequencing note**: Federation (Phase 3) deliberately came before the package
> refactor (Phase 2). The wire protocol pins down the real SDK / Message / Runtime /
> Module boundaries — so the later refactor follows the protocol instead of guessing.

## Design notes for future milestones

- **Exploration factor** (M12.3 follow-up): avoid the "Matthew effect" where strong agents monopolize — add ε-greedy exploration so new agents get a chance.
- **Agent-level Reputation / Trust**: *message-layer* authenticity is done (`FEDERATION_SECRET` HMAC on federation messages + per-instance JWT). The next step is *agent-level* trust across instances — add `agent_reputation` (success_count / failure_count / rating / trust_score) and fold it into `fitness`. Without it, an unauthenticated peer could still flood inboxes or pollute memory, so reputation should ride on top of the existing shared-secret auth.
- **SuccessRate → Quality**: `done/total` measures completion, not quality. Split into success rate + rating in the economy phase.
- **Agent Identity**: upgrade `AgentID` to a rich identity (`id / name / owner / world / skills / reputation`) — an "ID card" for the AI world.

## Long-term vision

```
              AgentWorld
       +----------------+
       |     Agent      |
       +----------------+
          /        \
        A2A        MCP
         |           |
    Other Agent   Real World
```

- **A2A** — social connection between agents (discovery, selection, cooperation, reputation)
- **MCP / Capability** — connection to the real world (tools, APIs, PMS systems)

Together they form the infrastructure for an **open agent society**.
