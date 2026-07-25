# tsnet-bridge 审查报告

> 审查日期: 2026-07-25
> 审查范围: 项目整体架构 + WebSocket 1006 错误根因分析
> 状态: 只读审查，未修改任何代码

---

## 一、项目概况

tsnet-bridge 是一个通过 Tailscale WireGuard 隧道（`tsnet` 嵌入式库）把远程服务桥接到本地端口的 Go 单文件程序。核心架构：

```
Agent / Browser  ──HTTP──►  tsnet-bridge  ──WireGuard──►  Remote Service (LLM / Web UI)
                   localhost:18900                  100.x.x.x:1234
```

**技术栈：**
- Go 1.24，依赖 `tailscale.com v1.82.0`（tsnet）、`getlantern/systray`（系统托盘）、`yaml.v3`
- 两种运行模式：系统托盘（默认）和 Web UI（`--web`）
- 支持单 target 和多 target 路由
- HTTPS 后端支持（`scheme: https`），WebSocket 隧道

**文件结构：**

| 文件 | 职责 |
|------|------|
| `main.go` | 入口，解析 flag，启动 tray 或 web 模式 |
| `bridge.go` | 核心：Config 加载、Bridge 生命周期、HTTP handler、反向代理、WebSocket 隧道 |
| `webui.go` | Web UI 服务端（`--web` 模式），提供 `/api/*` 接口 |
| `tray.go` | 系统托盘 UI，菜单交互，状态轮询 |
| `util.go` | YAML 序列化辅助函数 |
| `icon.go` | 动态生成 16x16 ICO 图标（四种状态颜色） |
| `web/index.html` | Web UI 前端（单文件，内嵌 CSS/JS） |

---

## 二、问题描述

**用户操作：** 通过 `http://localhost:18900/openclaw/chat?session=agent%3Amain%3Amain` 访问远程 OpenClaw Web UI 的聊天页面，填入 token 后报错。

**错误信息：**
```
disconnected (1006): no reason
浏览器无法完成 Gateway 连接。重试凭据前请检查目标和传输方式。
使用 openclaw status 或 openclaw gateway run 确认 Gateway 正在运行。
检查 WebSocket URL；当 Gateway 位于 HTTPS/Tailscale Serve 后面时使用 wss://。
```

**症状：** HTTP 页面加载成功，WebSocket 升级失败（1006 = 连接异常关闭）。

---

## 三、请求流转分析

### 3.1 HTTP 页面加载（成功）

```
浏览器 GET /openclaw/chat?session=agent%3Amain%3Amain
  → bridge 匹配 /openclaw/ 前缀
  → 路径剥离: /chat?session=agent%3Amain%3Amain
  → Director 设置 Host: localhost:18900
  → ReverseProxy 转发到远程 OpenClaw (HTTPS via Tailscale)
  → OpenClaw 返回 HTML 页面 ✓
```

### 3.2 WebSocket 升级（失败）

```
浏览器 WS /openclaw/chat?session=agent%3Amain%3Amain (Upgrade: websocket)
  → isWebSocketUpgrade(r) = true
  → handleWebSocketTunnel() 被调用
  → Hijack() 获取原始 TCP 连接
  → srv.Dial() 通过 Tailscale 连接到远程
  → TLS 握手 (SNI = Tailscale DNS 域名)
  → r.Write(backend) 写入 HTTP 升级请求
  → 读取 101 响应
  → 双向数据转发
  → ??? → 连接断开 (1006)
```

---

## 四、根因分析

### 原因 1（最可能）：WebSocket URL 路径不匹配

**问题：** OpenClaw 前端生成 WebSocket URL 时，路径与 bridge 路由不匹配。

**分析：**

OpenClaw 的 JavaScript 代码在建立 WebSocket 连接时，可能使用以下方式之一生成 URL：

**情况 A — 基于 Host 头生成（无前缀）：**
```javascript
// OpenClaw 前端代码（假设）
const wsUrl = `ws://${window.location.host}/chat?session=${sessionId}`;
// 结果: ws://localhost:18900/chat?session=agent%3Amain%3Amain
```
- WebSocket 请求路径 = `/chat`
- 不匹配 bridge 的 `/openclaw/` 前缀路由（`bridge.go:417`）
- 落入兜底 handler（`bridge.go:431`），返回 JSON 而非建立隧道
- **结果：WebSocket 握手失败**

**情况 B — 基于页面 URL 生成（有前缀）：**
```javascript
const wsUrl = `ws://${window.location.host}${window.location.pathname}?session=${sessionId}`;
// 结果: ws://localhost:18900/openclaw/chat?session=agent%3Amain%3Amain
```
- WebSocket 请求路径 = `/openclaw/chat`
- 匹配 `/openclaw/` 前缀，前缀被剥离后 `/chat` 转发给后端
- **理论上应该成功**，但如果 OpenClaw 后端有额外认证/Origin 检查，仍可能失败

**关键代码（`bridge.go:417-428`）：**
```go
mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
    r.URL.Path = r.URL.Path[len("/"+name):]  // 剥离 /openclaw → /chat
    if r.URL.Path == "" {
        r.URL.Path = "/"
    }
    if isWebSocketUpgrade(r) {
        b.handleWebSocketTunnel(w, r, target)  // WebSocket 走隧道
        return
    }
    proxy.ServeHTTP(w, r)  // 普通 HTTP 走反向代理
})
```

**兜底 handler（`bridge.go:431-443`）：**
```go
mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    // 返回 JSON 服务信息，不会建立 WebSocket 隧道
    _ = json.NewEncoder(w).Encode(map[string]any{...})
})
```

### 原因 2：TLS 握手与 Host 头不匹配

**问题：** WebSocket 隧道中 TLS SNI 和 HTTP Host 头指向不同目标。

**分析（`bridge.go:496-520`）：**

TLS 握手时：
```go
host := "vitasguo-g16-pro.tailc66d5e.ts.net"  // Tailscale DNS 域名
tlsConn := tls.Client(backendConn, &tls.Config{
    ServerName: host,  // SNI = Tailscale DNS
})
```

但 `r.Write(backend)` 发出的 HTTP 请求中：
```
Host: localhost:18900
```

OpenClaw 的反向代理（Caddy/Nginx/Traefik）可能校验 Host 头是否与 SNI 匹配。如果不匹配，可能拒绝连接。

### 原因 3：traps.md 记录的修复可能不完整

**问题：** traps.md #5 声称 v0.2.0 已修复此问题，但用户仍遇到。

**已实施的修复（`bridge.go`）：**
1. Director 中恢复原始 Host 头 → 使后端收到 `Host: localhost:18900` ✓
2. `X-Forwarded-Proto` 设为 `http` → 避免后端生成 `wss://` ✓
3. 新增 `handleWebSocketTunnel()` → 原始 TCP 隧道 ✓

**可能的遗留问题：**
- `X-Forwarded-Proto: http` 可能不满足某些后端对 `wss://` 的强制要求
- Origin 头未处理 — 浏览器发送 `Origin: http://localhost:18900`，但 OpenClaw 可能校验 Origin 是否匹配其自身域名
- WebSocket 隧道中 `r.Write(backend)` 的 HTTP 版本可能与后端期望不一致

### 原因 4：query parameter `session` 传递问题

**问题：** session 参数在路径剥离过程中可能丢失。

**分析：**

`bridge.go:418` 只修改 Path，不修改 Query：
```go
r.URL.Path = r.URL.Path[len("/"+name):]  // 只改 Path
```

`r.Write(backend)` 应保留 query string（通过 `r.URL.RawQuery`）。

**但有一个隐患：** `httputil.ReverseProxy` 的 Director 在 `origDirector(req)` 中可能修改 `r.URL.RawQuery`。WebSocket 隧道绕过了 ReverseProxy（直接 `r.Write`），所以 query string 应该完整。需要验证。

---

## 五、排查建议

### 5.1 确认 WebSocket 是否到达隧道

查看 bridge 运行日志，看是否出现以下输出：
```
websocket tunnel established: <target address>
```

如果没有，说明 WebSocket 请求没有进入 `handleWebSocketTunnel`，问题在路由层。

### 5.2 用 curl 测试 WebSocket 握手

```bash
curl -v -N \
  -H "Upgrade: websocket" \
  -H "Connection: Upgrade" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Sec-WebSocket-Version: 13" \
  "http://localhost:18900/openclaw/chat?session=agent%3Amain%3Amain"
```

如果返回 `101 Switching Protocols`，说明隧道工作正常，问题在前端 JS 的 URL 生成。
如果返回非 101 响应，查看响应内容定位后端拒绝原因。

### 5.3 浏览器 DevTools 检查

1. 打开 F12 → Network 面板
2. 找到失败的 WebSocket 请求（WS 类型）
3. 查看：
   - **实际请求的 URL** — 是否有 `/openclaw/` 前缀？
   - **Response Headers** — 状态码是什么？
   - **Initiator** — 哪行 JS 代码发起的连接？

### 5.4 检查 OpenClaw 配置

- 查看 OpenClaw 是否配置了 `allowed_origins` 或 `CORS` 限制
- 查看 OpenClaw 的 WebSocket Gateway 是否有独立的认证要求
- 确认 OpenClaw 的 TLS 证书是否对 Tailscale DNS 域名有效

### 5.5 检查 bridge 配置

确认 `~/.tsnet-bridge/config.yaml` 中 openclaw target 的配置：
```yaml
targets:
  - name: openclaw
    address: "your-host.your-tailnet.ts.net:443"
    scheme: https
```

确保 `address` 和 `scheme` 正确。

---

## 六、代码质量审查

### 6.1 正面评价

- **架构清晰**：Bridge/WebUI/Tray 三模块分离，职责明确
- **配置灵活**：支持单/多 target、YAML 配置、Web UI 编辑
- **安全意识**：ephemeral key、config 文件权限 0600、localhost-only toggle
- **错误处理**：joinWithRetry 带退避重试、状态机管理
- **WebSocket 隧道**：针对 HTTPS 后端做了 TLS 握手处理

### 6.2 潜在问题

| 严重度 | 位置 | 问题 |
|--------|------|------|
| 中 | `bridge.go:501` | TLS `InsecureSkipVerify` 未设置，但证书校验可能因 Tailscale CA 不在系统信任链而失败 |
| 中 | `bridge.go:467-549` | WebSocket 隧道无超时/心跳，长连接可能静默断开 |
| 低 | `bridge.go:418` | 路径剥离后 `r.URL.Path = ""` 时设为 `/`，但未处理 `r.URL.RawQuery` 为空的情况 |
| 低 | `webui.go:151` | `testChat` 中字符串拼接构造 JSON，特殊字符未转义（注入风险） |
| 低 | `tray.go:274-411` | 使用中文硬编码的使用说明，国际化困难 |

### 6.3 建议改进

1. **WebSocket 隧道增加超时** — 防止静默断开
2. **增加 WebSocket 握手日志** — 记录 Upgrade 请求的完整 URL 和响应状态码
3. **`webui.go:151` 使用 `json.Marshal`** — 避免 JSON 注入
4. **考虑支持 `Sec-WebSocket-Protocol`** — 某些后端需要子协议协商

---

## 七、结论

WebSocket 1006 错误的**最可能根因**是：

> OpenClaw 前端生成的 WebSocket URL 路径与 bridge 的路由前缀不匹配，导致 WebSocket 请求落入兜底 handler 而非目标路由隧道。

**建议按以下优先级排查：**
1. 用浏览器 DevTools 确认实际 WebSocket URL
2. 用 curl 测试 WebSocket 握手
3. 检查 bridge 日志是否出现 `websocket tunnel established`
4. 检查 OpenClaw 的 Origin/Host 校验配置

确认根因后，修复方向可能是：在 bridge 中增加 WebSocket URL 重写逻辑，或调整路径剥离策略以兼容 OpenClaw 的 WebSocket URL 生成方式。
