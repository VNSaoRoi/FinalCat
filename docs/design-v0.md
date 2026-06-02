# FinalCat v0 — Design spec (agreed)

Status: **implemented** (control + Route data plane). Pivot/coordination only — no deploy automation.

## Product goal

**Pivot / coordination tool** for controlled lab and red-team ops — not a full C2 framework.

- **Control plane:** lightweight WebSocket (register, liveness, upstream, bind attach).
- **Data plane:** separate (tab **Route**); bulk traffic never multiplexed over control JSON.

## Server (Linux only)

Two listeners in one process:

| Flag | Default | Bind | Purpose |
|------|---------|------|---------|
| `-l` | `0.0.0.0:31747` | Any (operator choice) | Agent control: reverse dial-in, bind-bridge attach |
| `-admin` | `127.0.0.1:31891` | **127.0.0.1 only** (not overridable to `0.0.0.0`) | Operator Web UI + REST + `/ws/ui` |

```text
LAN/Internet ──► server -l          ← agents (reverse -r)
localhost only ─► server -admin     ← browser, REST on operator host
```

### Security

- Admin **must** listen on loopback; reject or ignore non-`127.0.0.1` bind for `-admin`.
- Optional password / cookie session on admin routes.
- Remote operator: SSH local forward, e.g. `ssh -L 31891:127.0.0.1:31891 user@server`.

### Agent listener (`-l`)

| Path / role | Consumer |
|-------------|----------|
| `WS /ws/agent` | Client reverse + post-bridge control |
| Route data plane | SOCKS / TCP forward on agent or server-side SOCKS gateway |

### Admin listener (`-admin`)

| Path / role | Consumer |
|-------------|----------|
| Embedded Web UI | Dashboard, Agents, Topology, Route |
| `GET/POST /api/*` | REST (clients, upstreams, bind-attach, routes) |
| `WS /ws/ui` | Live registry + routes snapshot for UI |

## Client

| Mode | Flags | Behaviour |
|------|-------|-----------|
| **Reverse** | `-r host:port` | Dial **server `-l`**, register, maintain control WS, reconnect with upstream list |
| **Bind** | `-l ip:port` | Listen for control WS; advertised as **LISTENING** until server/operator attach |
| **Hybrid** | `-r` + `-l` | Reverse to server + downstream listen |

### Session paths

```text
(A) Reverse:  client ──WS──► server -l
(B) Bind:     server ──WS──► client -l   (inbound bridge / operator-initiated attach)
```

After bind attach, bind agents appear in the same registry as reverse agents (**ONLINE**).

## Web UI — tabs

| Tab | Purpose |
|-----|---------|
| **Dashboard** | Counts, short agent table, links to other tabs |
| **Agents** | Per-agent details, edit upstreams, attach bind agents |
| **Topology** | View-only operator → agents graph |
| **Route** | SOCKS5 (agent or server bind) and TCP listen→forward |

## Control API (minimum)

| Endpoint | Purpose |
|----------|---------|
| `WS /ws/agent` on `-l` | Register, ping/pong, `set_upstream`, route open/close/tunnel |
| `WS /ws/ui` on `-admin` | UI snapshot stream |
| `GET /api/clients` | Registry |
| `PATCH /api/clients/:id/upstreams` | Push upstream list to agent |
| `POST /api/jobs/bind-attach` | Attach bind client |
| `GET/POST/DELETE /api/routes/*` | Route management |

See [route-preview.md](route-preview.md) for Route API detail.

## Implementation phases

| Phase | Scope |
|-------|--------|
| **1** | ✅ Server `-l` + `-admin`, reverse agent, registry, UI shell, Topology read-only |
| **2** | ✅ Bind mode + inbound bridge, hybrid `-r`+`-l`, upstream failover |
| **3** | ✅ Route tab + data plane: SOCKS5 on agent/server + TCP forward |
| **4** | Multi-hop relay (`client -l` chain) |

## Explicit non-goals (v0)

- Full C2: interactive shell, file manager, plugin catalog.
- In-UI or API **deploy** of agents (operator copies binaries manually).
- Server on Windows/macOS.
- Admin listener on non-loopback addresses.
- TLS inside core (use reverse proxy / `wss` externally if needed).
- Automatic multi-hop chain planner.

## Defaults (locked)

- Agent/control port: **31747**
- Admin/UI port: **31891** on **127.0.0.1**
- WebSocket agent path: `/ws/agent`
- WebSocket UI path: `/ws/ui`
