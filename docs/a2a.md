# Agent Communication Layer (ACL / A2A)

AgentWorld's inter-agent communication is **not chat**. It is an intent-driven **Agent Communication Layer (ACL)** — analogous to TCP/IP + DNS, but for agents.

## Design principles

- Agents do **not** address each other by `AgentID` directly.
- Agents send an **intent** + a payload ("I need a travel plan"), not a chat message.
- Agents are **discovered by capability** ("who can do travel.plan.v1"), not by an address.
- Communication is **asynchronous** — messages land in an inbox, and the receiving agent decides whether to respond.
- Successful cooperation feeds back into **memory → relationship → selection**, so partnerships emerge naturally.

## Message

```go
type Message struct {
    ID        int64
    From      int64                   // sender AgentID
    To        int64                   // receiver (0 = capability-routed)
    Intent    string                  // e.g. "travel.plan.v1"
    Payload   map[string]interface{}  // e.g. {city, days, budget}
    Status    string                  // pending / accepted / rejected / done
    CreatedAt time.Time
}
```

## Layers

### M12.1 ACL — the "phone line"

Async messaging with an inbox:

```
Hotel Agent                        Travel Agent
   │ Send(Message{Intent,Payload})    │ Inbox reads pending messages
   │ ─────────────────────────────►   │ Perceive includes the message
   │                                 │ Planner decides whether to respond
   │                                 │ Mark(done)
```

The runtime stores messages in the `agent_messages` table. Agents see their inbox during `Perceive` and decide autonomously.

### M12.2 Registry — the "address book"

Agents register the skills they provide:

| agent | world | skill |
|---|---|---|
| TravelA | travel | `travel.plan.v1` |
| TravelB | travel | `travel.route.v1` |
| HotelA | hotel | `hotel.checkin.v1` |

Routing is exact + prefix (versioned skills). `Find("travel.plan")` matches `travel.plan.v1`. A request with `To=0` is routed **only** to agents that match the intent — no global broadcast.

### M12.3 Selection — "choose a partner"

Given candidates, rank them by **fitness**:

```
fitness = capability match
        + historical success rate × 30
        + relationship bonus (friend +20, frequent +10)
        + load (lower = better)
```

This creates a self-reinforcing loop — successful cooperation raises success rate and relationship, so that partner is preferred next. (Note: an **exploration factor** is planned to avoid the "Matthew effect" / monopoly by strong agents.)

## Status flow

```
pending → accepted / rejected → done
```

## Future

- **Agent-level Reputation / Trust**: *message-layer* authenticity is done (shared-secret
  HMAC on federation messages). The next step is *agent-level* trust across instances —
  add `agent_reputation` (success / rating / trust) so a remote peer's reliability can be
  verified before relying on it.

> **M12.4 Federation is implemented.** Cross-instance A2A lets agents in different
> AgentWorld instances discover each other via `/.well-known/agent.json` and exchange
> intent-driven messages, authenticated with a shared-secret HMAC signature. See
> [federation.md](federation.md).
