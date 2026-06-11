# FinalCat

Lightweight pivot control plane: agents check in over WebSocket; you manage them from a local web UI and open SOCKS or TCP forwards on hosts you control.

## Legal / authorized use

FinalCat is intended for **authorized security testing, research, and lab environments** only. You must have explicit permission before deploying agents or opening routes on any system you do not own or control. The authors and contributors are not responsible for misuse. See [LICENSE](LICENSE) (MIT).

---

## 1. Build

From the project root:

```bash
./build.sh          # Linux / macOS / Git Bash
```

```powershell
.\build.ps1         # Windows (cross-builds Linux + Windows agents)
```

Outputs:

| Binary | Path |
|--------|------|
| Server (Linux amd64 / 386) | `dist/server_linux_*` |
| Server (Windows amd64 / 386) | `dist/server_windows_*.exe` |
| Agent (Linux amd64 / 386) | `dist/client/agent_linux_*` |
| Agent (Windows amd64 / 386) | `dist/client/agent_windows_*.exe` |

**Tips**

- Builds use `CGO_ENABLED=0` (static Linux agents — no GLIBC mismatch on old targets).
- If `./build.sh` fails with `bash\r`, run: `sed -i 's/\r$//' build.sh`
- Re-run `./build.sh` after every pull that changes code.

---

## 2. Run the server

On your **operator machine**:

```bash
./dist/server_linux_amd64 -l 0.0.0.0:31747 -admin 127.0.0.1:31891
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-l` | `0.0.0.0:31747` | Agents connect here (reverse / control) |
| `-admin` | `127.0.0.1:31891` | Web UI — **loopback only** |
| `-p` | (none) | Optional UI password |
| `-data` | `finalcat-data` | Route catalog persistence (auto-restore after agent reconnect) |

Open the UI: **http://127.0.0.1:31891**

**Remote operator** (UI stays on the server host):

```bash
ssh -L 31891:127.0.0.1:31891 user@your-server
# then browse http://127.0.0.1:31891 on your laptop
```

The Windows server binary works for control + Route; SOCKS/TCP forwarding needs a **Linux** server for full tunnel features.

---

## 3. Run agents

Pick one mode (or hybrid).

### Reverse (most common)

Agent dials the server:

```bash
./agent_linux_amd64 -r 10.10.16.22:31747
```

Multiple upstreams (failover):

```bash
./agent_linux_amd64 -r 10.10.16.22:31747,backup.example.com:31747
```

### Bind only

Agent listens; you attach from the UI (**Agents → Attach bind agent**):

```bash
./agent_linux_amd64 -l 0.0.0.0:18229
```

### Hybrid

Reverse + bind listen at the same time:

```bash
./agent_linux_amd64 -r 10.10.16.22:31747 -l 0.0.0.0:18229
```

After connect, the agent appears under **Agents** in the UI.

---

## 4. Web UI (quick tour)

| Tab | Use it for |
|-----|------------|
| **Dashboard** | Online / offline counts and agent list |
| **Agents** | Agent details, edit upstreams, attach bind agents |
| **Topology** | Simple view of operator → agents |
| **Route** | Create SOCKS5 or TCP forwards; **Live lines** show active paths with direction toward/away from operator (pivot layer — use **ligolo-ng**, **chisel**, **proxychains**, etc. on top as needed) |

Copy agent binaries to targets yourself (`scp`, `ssh`, impacket, or your own playbook). Use the server’s reachable IP and port **31747** for `-r`.

**Agents → Attach bind agent**

Use when the target only listens (bind mode) and the server can reach `bind_host:bind_port` (or use a reachable jump host IP).

---

## 5. Pivot (Route tab)

All pivot traffic is **raw TCP on the agent** (or on the server for server-side SOCKS), not inside the control WebSocket.

### Live lines

Active routes appear as readable paths in **Live lines** (direction `<--` = toward operator, `-->` = away). Pending, error, and closed routes are listed under **Not live**. Agents may report **multiple local IPs**; chain detection uses all of them.

### SOCKS5

1. Go to **Route** → **+ Create SOCKS5**.
2. Select an **online agent** (egress).
3. **Bind on**:
   - **agent** — SOCKS listens on the agent; you connect to `agent_ip:port` (default `0.0.0.0:1080`). Pick an agent IP from the dropdown to fill bind addr quickly.
   - **server** — SOCKS listens on the server (default `127.0.0.1:1080`); traffic exits via the agent (good when you only have SSH to the server).
4. Click **Open SOCKS**.
5. Wait until state is **active**, then point tools at the SOCKS address:

```bash
curl --socks5 127.0.0.1:1080 http://internal-host/
# or proxychains / browser SOCKS5
```

Plain SOCKS5 (no TLS, no auth).

### TCP forward

1. **Route** → **+ Create TCP forward**.
2. Choose agent and optional **Agent IP** (listen addr helper).
3. Set **listen addr** on the agent (e.g. `0.0.0.0:4444`).
4. Set **target host** and **port** (e.g. internal RDP `10.0.0.5:3389`).
5. **Open forward** → connect to `agent_ip:4444` from your machine.

### Close a route

Use **Close** on a row in **Live lines** or **Not live**. That removes the route from the server catalog permanently.

### Agent disconnect and restore

Each agent sends a stable **`persistent_id`** (stored in `~/.finalcat/agent.id` or `%APPDATA%\FinalCat\agent.id`). The server saves desired routes in **`<data>/route-catalog.json`**.

| Event | Behavior |
|-------|----------|
| Agent goes offline (network blip or dead) | Live tunnels are torn down immediately; desired routes stay in the catalog |
| Agent reconnects (same `persistent_id`) | Server re-sends open commands for all catalog routes |
| Operator **Close** | Route removed from catalog and agent |

Legacy agents without `persistent_id` use a server-side fingerprint (`hostname` + OS + local IPs).

### Multi-hop

When forward targets match another agent’s listen (including `0.0.0.0:port` resolved via agent IPs), **Live lines** chains hops automatically. You can also combine SOCKS + forward manually.

---

## 6. Troubleshooting

| Problem | What to do |
|---------|------------|
| `GLIBC_2.xx not found` on target | Rebuild with `./build.sh` (`CGO_ENABLED=0`). Do not reuse old dynamic binaries. |
| Agent binary missing on target | Build with `./build.sh` and copy `dist/client/agent_*` to the target. |
| Agent not in UI | Check firewall to `-l` port; verify `-r` points to server IP:31747; read server logs. |
| Route stays pending | Agent offline or listen failed (port in use / permissions). Check agent log. |
| Cannot reach agent SOCKS | You must have network path to `agent_ip:port`, or use **Bind on: server**. |

---

## 7. Defaults

| Item | Value |
|------|--------|
| Agent control port | `31747` |
| Admin UI | `127.0.0.1:31891` |
| WebSocket path | `/ws/agent` |
