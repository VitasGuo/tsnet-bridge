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
- **System tray UI** — right-click to start/stop/edit config, icon color reflects state
- **Web UI fallback** — run with `--web` for browser-based control

## Quick start

1. Download `tsnet-bridge.exe` (or build from source — see below).
2. On your GPU host: install Tailscale + LM Studio / Ollama, note the host's Tailscale IP (`100.x.x.x`) and port.
3. Generate an **ephemeral** Tailscale auth key at <https://login.tailscale.com/admin/settings/keys>.
4. Double-click `tsnet-bridge.exe`. A tray icon appears in the bottom-right notification area.
5. Right-click the tray icon → **编辑配置**. Notepad opens `~/.tsnet-bridge/config.yaml`. Fill in authkey, target `100.x.x.x:1234`, optional apikey. Save and close.
6. Right-click the tray icon → **重新加载配置** → **启动**. The icon turns green when connected.
7. Point your agent at `http://localhost:18900/v1` with any API key.

Prefer a browser UI? Run `tsnet-bridge.exe --web` instead — same functionality, just served at `http://127.0.0.1:18901`.

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
autostart: false                     # auto-connect on launch (tray mode)

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

## Point your agent at the bridge

Once the tray icon turns green, any OpenAI-compatible agent can use the bridge. The bridge auto-injects the real API key, so fill in any non-empty string for `apiKey`.

| Field | Value |
|-------|-------|
| Base URL | `http://localhost:18900/v1` |
| API Key | any non-empty string (e.g. `sk-bridge`) |
| Model | one of the IDs returned by `/v1/models` |

### OpenCode (`opencode.json`)

```json
{
  "provider": {
    "openai": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "http://localhost:18900/v1",
        "apiKey": "sk-bridge"
      },
      "models": {
        "google/gemma-4-12b-qat": { "name": "gemma-4-12b" }
      }
    }
  }
}
```

### Cursor

Settings → Models → Override OpenAI Base URL:
```
http://localhost:18900/v1
```
API Key: `sk-bridge` · Model ID: `google/gemma-4-12b-qat`

### Cline / Continue / VSCode extensions

```json
{
  "apiProvider": "openai",
  "apiBase": "http://localhost:18900/v1",
  "apiKey": "sk-bridge",
  "model": "google/gemma-4-12b-qat"
}
```

### Python (openai SDK)

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:18900/v1", api_key="sk-bridge")
resp = client.chat.completions.create(
    model="google/gemma-4-12b-qat",
    messages=[{"role": "user", "content": "你好"}],
)
print(resp.choices[0].message.content)
```

### curl

```bash
curl http://localhost:18900/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-bridge" \
  -d '{"model":"google/gemma-4-12b-qat","messages":[{"role":"user","content":"hi"}]}'
```

### Notes

- Model IDs must match `/v1/models` exactly (case-sensitive, including slashes).
- Embedding models (e.g. `text-embedding-nomic-embed-text-v1.5`) only work with `/v1/embeddings`, not chat.
- For multiple targets, use `http://localhost:18900/<name>/v1` as the Base URL.

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

- [x] System tray icon (Windows / macOS / Linux)
- [ ] Auto-reconnect on tailnet dropout
- [ ] Per-target latency / token throughput stats
- [ ] Optional SOCKS5 mode for non-HTTP agents

## License

BSD 3-Clause License — see [LICENSE](./LICENSE).

This project embeds [tailscale.com](https://github.com/tailscale/tailscale) (BSD 3-Clause) via the `tsnet` package, [systray](https://github.com/getlantern/systray) (Apache 2.0), and [yaml.v3](https://github.com/go-yaml/yaml) (Apache 2.0). Many thanks to the Tailscale team for opening up `tsnet` — without it this project would not be possible.
