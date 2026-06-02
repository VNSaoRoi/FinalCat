# Route tab — implemented (phase 3)

Two pivot capabilities on the **data plane** (raw TCP on the agent, not over control WS):

## 1. SOCKS5 on any agent

The server sends `route_open_socks` over the control WS → the agent opens listener `bind_addr` (default `0.0.0.0:1080`).  
The operator/tool connects directly to agent IP:port — SOCKS5 no-auth, CONNECT only.

**API:** `POST /api/routes/socks`  
```json
{ "agent_id": "...", "bind_addr": "0.0.0.0:1080", "bind_on": "agent" }
```

## 1b. SOCKS5 on server (egress via agent)

The server opens a SOCKS listener (default `127.0.0.1:1080`). Each CONNECT is relayed through the agent (WS binary tunnel) — the operator connects to the server; traffic exits via the agent network.

```json
{ "agent_id": "...", "bind_addr": "127.0.0.1:1080", "bind_on": "server" }
```

Registry: `kind=socks_server`, `bind_on=server`.

## 2. TCP listen + forward on any agent

The server sends `route_open_forward` → the agent listens on `listen_addr` → each connection dials `target_host:target_port`.

**API:** `POST /api/routes/forward`  
```json
{ "agent_id": "...", "listen_addr": "0.0.0.0:4444", "target_host": "10.0.0.5", "target_port": 3389 }
```

## Control messages

| Type | Direction | Purpose |
|------|-----------|---------|
| `route_open_socks` | server → agent | Open SOCKS listener |
| `route_open_forward` | server → agent | Open TCP forward |
| `route_tunnel_open` | server → agent | Open tunnel dial to target (server SOCKS) |
| `route_tunnel_ack` | agent → server | Tunnel ready / fail |
| `route_tunnel_close` | both | Close tunnel |
| *(binary WS)* | both | Tunnel payload `[8 byte id][data]` |

## REST

| Method | Path | Action |
|--------|------|--------|
| GET | `/api/routes` | List routes |
| POST | `/api/routes/socks` | Open SOCKS |
| POST | `/api/routes/forward` | Open forward |
| DELETE | `/api/routes/:id` | Close route |

UI tab **Route** + live snapshot via `/ws/ui` (`clients` + `routes`).

## Pivot chain (example)

```text
Operator → SOCKS on agent A:1080 → curl internal network
Operator → agent B:4444 (forward) → RDP 3389 on DC
Reverse agent A + bind agent C → server coordinates both
```

Phase 4: multi-hop relay chain (`client -l` chain).
