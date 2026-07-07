# tsnet-bridge

Bridge a local port to a remote LLM over Tailscale — without installing Tailscale on your machine.

Any AI agent (opencode, cursor, trae, cline, …) connects to `http://localhost:18900/v1` as if it were a local OpenAI-compatible LLM. The bridge transparently tunnels traffic through an embedded Tailscale (WireGuard) connection to your GPU host on the tailnet.

```
Agent  ──HTTP──►  tsnet-bridge  ──WireGuard──►  Remote LLM (LM Studio / Ollama / vLLM)
        localhost:18900                  100.x.x.x:1234
```

## Why

- **No system-wide Tailscale install** — uses `tsnet` (embedded Go library)
- **Zero agent integration** — agent just points to a local URL, nothing to learn
- **End-to-end encrypted** — WireGuard between bridge and remote host
- **Single binary, no dependencies** — one 30 MB `.exe`, drop in and run
- **Web UI included** — configure, start, stop, test from a browser

## Quick start

1. Download `tsnet-bridge.exe` (or build from source — see below).
2. On your GPU host: install Tailscale + LM Studio / Ollama, note the host's Tailscale IP (`100.x.x.x`) and port.
3. Generate an **ephemeral** Tailscale auth key at <https://login.tailscale.com/admin/settings/keys>.
4. Double-click `tsnet-bridge.exe`. A browser window opens with the Web UI.
5. Fill in authkey, target `100.x.x.x:1234`, optional API key. Click **启动**.
6. Point your agent at `http://localhost:18900/v1` with any API key.

## Build from source

Requires Go 1.22+.

```powershell
$env:GOPROXY = "https://goproxy.cn,direct"   # if behind GFW
go build -ldflags "-H windowsgui -s -w" -o tsnet-bridge.exe .
```

`-H windowsgui` hides the console window on Windows. Drop the flag on macOS / Linux.

## Configuration

The bridge looks for config in this order:

1. `./tsnet-bridge.yaml` (current directory — handy for development)
2. `~/.tsnet-bridge/config.yaml` (user home — recommended for daily use)

The Web UI saves to `~/.tsnet-bridge/config.yaml` automatically. You never edit YAML by hand unless you want to.

```yaml
# ~/.tsnet-bridge/config.yaml
authkey: "tskey-auth-xxxxx-xxxxx"   # ephemeral key from Tailscale admin
hostname: "tsnet-bridge"             # this node's name on the tailnet
ephemeral: true                      # auto-remove node on exit
listen: ":18900"                     # local HTTP server address

targets:
  - name: default
    address: "100.93.126.41:1234"    # remote LLM host:port on tailnet
    apikey: "sk-lm-xxx"              # optional, injected as Bearer token
```

### Multiple targets

```yaml
targets:
  - name: gpu1
    address: "gpu1:11434"
  - name: gpu2
    address: "gpu2:11434"
```

With multiple targets, each is served at `http://localhost:18900/<name>/v1/...`.

## Security

- **Software ships with no credentials.** All user data lives in `~/.tsnet-bridge/`, never in the binary's directory.
- **Auth key** is an *ephemeral* Tailscale key — the node disappears from your tailnet the moment the bridge exits.
- **WireGuard** encrypts all traffic between bridge and remote host end-to-end.
- **Local port** binds to `0.0.0.0:18900` by default; restrict to `127.0.0.1:18900` in `listen` if you don't want other LAN machines to reach it.

## What gets ignored by git

See [`.gitignore`](./.gitignore). Notably:

- `tsnet-bridge.yaml` / `config.yaml` — user config with real credentials
- `.tsnet-bridge/` / `state/` — Tailscale node identity (private keys)
- `*.exe` — build artifacts

## Roadmap

- [ ] System tray icon (Windows / macOS)
- [ ] Auto-reconnect on tailnet dropout
- [ ] Per-target latency / token throughput stats in Web UI
- [ ] Optional SOCKS5 mode for non-HTTP agents

## License

MIT
