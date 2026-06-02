# FinalCat

Lightweight pivot control plane: agents check in over WebSocket; you manage them from a local web UI and open SOCKS or TCP forwards on hosts you control.

Full design notes: [docs/design-v0.md](docs/design-v0.md)

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
| Server (Linux) | `dist/server_linux_amd64` |
| Server (Windows) | `dist/server_windows_amd64.exe` |
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
| **Route** | Open SOCKS5 or TCP port-forward through an agent (pivot layer — use **ligolo-ng**, **chisel**, **proxychains**, etc. on top as needed) |

Copy agent binaries to targets yourself (`scp`, `ssh`, impacket, or your own playbook). Use the server’s reachable IP and port **31747** for `-r`.

**Agents → Attach bind agent**

Use when the target only listens (bind mode) and the server can reach `bind_host:bind_port` (or use a reachable jump host IP).

---

## 5. Pivot (Route tab)

All pivot traffic is **raw TCP on the agent** (or on the server for server-side SOCKS), not inside the control WebSocket.

### SOCKS5

1. Go to **Route**.
2. Select an **online agent** (egress).
3. **Bind on**:
   - **agent** — SOCKS listens on the agent; you connect to `agent_ip:port` (default `0.0.0.0:1080`).
   - **server** — SOCKS listens on the server (default `127.0.0.1:1080`); traffic exits via the agent (good when you only have SSH to the server).
4. Click **Open SOCKS**.
5. Wait until state is **active**, then point tools at the SOCKS address:

```bash
curl --socks5 127.0.0.1:1080 http://internal-host/
# or proxychains / browser SOCKS5
```

Plain SOCKS5 (no TLS, no auth).

### TCP forward

1. **Route** → **TCP forward** section.
2. Choose agent, **listen addr** on the agent (e.g. `0.0.0.0:4444`).
3. Set **target host** and **port** (e.g. internal RDP `10.0.0.5:3389`).
4. **Open forward** → connect to `agent_ip:4444` from your machine.

### Close a route

Use **Close** on the row in **Active routes**, or delete when the agent goes offline.

### Multi-hop

There is no automatic chain builder. Open several forwards manually (e.g. agent C → DC, agent B → C, connect to agent B) or combine SOCKS + forward as needed.

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
