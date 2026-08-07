# Federation (M12.4) — A Distributed Agent Runtime Network

AgentWorld Federation connects **multiple AgentWorld instances** into a single,
distributed Agent network. It turns a single-runtime "agent simulation world"
into an **Agent Runtime Network** — the same jump as from a standalone container
(Docker) to a container cluster (Kubernetes).

```
Now:                          Next:
+--------AgentWorld A--------+   +--------AgentWorld B--------+
|  Hotel Agent                |   |  Travel Agent               |
|       |                     |   |                             |
|       |  ACL (same process) |   |  ACL (same process)         |
|       v                     |   |                             |
|  Travel Agent               |   |                             |
+-----------------------------+   +-----------------------------+
                |   HTTPS / WebSocket   |
                +----------/ API -------+
                           |
                          ACL
```

Federation does **not** change how agents communicate *within* an instance. It
adds a **remote addressing** layer on top: an agent in World A can send an
intent-driven message to an agent in World B, using the same `intent + payload`
semantics, and the message lands in World B's inbox for the target agent to
decide on.

## Why this matters

Federation forces you to lock down the real boundaries of the runtime:

- **SDK boundary** — what a module may call (now including `SendRemote`).
- **Message boundary** — what a message is (intent + payload + composite sender).
- **Runtime boundary** — what an instance owns (its world, agents, inbox).
- **Module boundary** — which world a module belongs to.

Once these are pinned down by an actual wire protocol, a package refactor
(Phase 2) is no longer guesswork — it follows the protocol.

## Protocol

### 1. Agent Manifest — the distributed address book

Every federated instance exposes its agents' capabilities at:

```
GET /.well-known/agent.json
```

Response:

```json
{
  "name": "Shanghai Hotel World",
  "runtime": "agentworld",
  "endpoint": "http://127.0.0.1:18081",
  "agents": [
    { "id": 13, "name": "Front Desk Zhou", "skills": ["hotel.booking.v1"] },
    { "id": 14, "name": "Housekeeper",     "skills": ["hotel.housekeeping.v1"] }
  ]
}
```

A remote instance fetches this to build a **remote address book**. From it,
a module can discover "who in the network can handle `hotel.booking.v1`".

### 2. Remote message

Local messaging upgrades `Send(Message)` → **remote messaging**:

```json
POST /api/federation/messages   # on the target instance

{
  "intent": "hotel.booking.v1",
  "from":   { "world": "travel-world", "agent": 6 },
  "to":     13,
  "payload": { "city": "Shanghai", "nights": 2 }
}
```

Notes:

- `intent` + `payload` are identical to a local A2A message.
- `from` is a **composite** `{ world, agent }`, because the receiver must know
  *which world* the message came from to reply.
- `to` is the target agent in the **receiving** instance (0 = capability-routed
  within that instance).
- The receiving instance decodes the sender into a stable **negative** `from_agent`
  id (FNV hash of world + agent), so it never collides with local positive ids,
  and the message lands in the target's inbox exactly like a local message.
- The response `{ "delivered": true }` confirms the message was accepted and
  persisted.

#### Async collaboration continuation (`reply_to` / `correlation_id`)

A remote message can carry `reply_to` (the request message id this is a reply to)
and `correlation_id` (a business-level key shared between a request and its reply).
They are persisted with the message and exposed back to the SDK inbox, so a planner
can correlate a reply with the request it issued and **resume a waiting step** after
a long-running remote task — turning federation into true async social collaboration
rather than blocking RPC. (The waiting-step planner state is the next phase; the
transport layer already propagates these fields end-to-end.)

### 3. Transport

The current transport is **HTTPS** (each instance's own HTTP server). The
`federation.Transport` interface isolates the wire layer, so WebSocket / gRPC
can be swapped in later without touching the protocol logic.

### 4. Security (message authenticity)

Federation endpoints are **not exposed unauthenticated**. To prevent a public
network from injecting forged messages, `POST /api/federation/messages` requires
a shared-secret **HMAC-SHA256 signature** over the raw body:

- Sender signs the message body with the shared secret and sends it in the
  `X-AgentWorld-Signature` header.
- Receiver verifies the signature before routing the message into any agent's
  inbox. A missing/invalid signature returns `401 invalid federation signature`.
- The target agent (`to`) must **exist** in the receiving instance; otherwise the
  message is rejected with `400 unknown to agent`.

Configure the **same** `FEDERATION_SECRET` on every interconnected instance. If
it is left empty, signature verification is skipped (trusted-internal-only);
production should always set it. `/.well-known/agent.json` (the address book) is
public by design — it only reveals capability names, not secrets.

## SDK surface

Federation is exposed to modules through `sdk.Runtime`:

```go
// SendRemote sends an intent-driven message to an agent in another instance.
SendRemote(ctx, ref sdk.RemoteRef, msg sdk.RemoteMessage) error

// DiscoverRemote pulls and caches a remote instance's Agent Manifest.
DiscoverRemote(ctx, endpoint string) error

// RemoteAgents looks up candidates by skill across discovered remotes.
RemoteAgents(skill string) []sdk.RemoteRef
```

`sdk.RemoteRef` is `{ Endpoint, World, AgentID, Name, Skill }`; `sdk.RemoteMessage`
is `{ Intent, Payload, From sdk.RemoteFrom }` where `sdk.RemoteFrom = { World, Agent }`.

## Configuration

| Env var | Default | Meaning |
|---|---|---|
| `FEDERATION_ENABLED` | `false` | Enable federation (expose manifest + receive remote msgs). |
| `WORLD_NAME` | `default-world` | This instance's world name (used in manifest & remote addressing). |
| `FEDERATION_ENDPOINT` | `""` | This instance's public HTTP base URL (for remotes to reply). |
| `FEDERATION_PEERS` | `""` | Comma-separated remote instances to auto-discover at startup. |
| `FEDERATION_SECRET` | `""` | Shared secret for HMAC-signed remote messages (authenticate senders). Set the same value on all interconnected instances. |

Example:

```bash
# Instance A (hotel world)
FEDERATION_ENABLED=true WORLD_NAME=shanghai-hotel \
  FEDERATION_ENDPOINT=http://127.0.0.1:18081 \
  FEDERATION_SECRET=shared-secret-please-change \
  PORT=18081 DB_PATH=a.db

# Instance B (travel world)
FEDERATION_ENABLED=true WORLD_NAME=travel-world \
  FEDERATION_ENDPOINT=http://127.0.0.1:18080 \
  FEDERATION_PEERS=http://127.0.0.1:18081 \
  FEDERATION_SECRET=shared-secret-please-change \
  PORT=18080 DB_PATH=b.db
```

## Verification

Two instances were started (hotel world on `:18081`, travel world on `:18080`).
Instance B's `/.well-known/agent.json` listed 4 hotel agents with their skills.
A travel agent sent `hotel.booking.v1` to the hotel front-desk (id 13); the
message was persisted in instance B's `agent_messages` table:

```
id=1, from_agent=-7771936676695982286, to_agent=13, intent=hotel.booking.v1, status=pending
```

The negative `from_agent` proves the composite sender was decoded correctly, and
the `pending` row in the target inbox means the receiving agent can now perceive
and respond to it — a full cross-instance A2A round-trip.

## Roadmap

- **Agent-level Reputation / Trust**: *message-layer* authenticity is handled by
  `FEDERATION_SECRET`. The next step is *agent-level* trust — verify a remote
  agent's past reliability before relying on it (success/failure/rating).
- **WebSocket transport**: persistent connections for lower-latency federation.
- **Remote Selection**: run agent selection (M12.3) across instances, not just locally.
