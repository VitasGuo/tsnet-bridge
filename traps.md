# tsnet-bridge 踩坑记录

## 1. ICO 图标 AND mask 行对齐错误

**现象**: Windows 11 托盘图标不显示（空白占位）。

**根因**: [icon.go:21](file:///c:/Users/even/Documents/SOLO-Even/tsnet-bridge/icon.go#L21)
AND mask 每行按 2 bytes 计算（16 bit = 2 bytes），但 ICO 格式要求每行 4-byte 对齐。2 bytes 不满足对齐要求，导致图标数据偏移，Windows 无法解析。
```
andMaskRowSize = 2  // 错误: 16 bits = 2 bytes，未 4-byte 对齐
andMaskRowSize = 4  // 正确: 2 bytes + 2 bytes padding = 4 bytes
```

**解决方案**: [icon.go:21](file:///c:/Users/even/Documents/SOLO-Even/tsnet-bridge/icon.go#L21)
将 `andMaskRowSize` 从 `2` 改为 `4`。AND mask 总大小从 `2 * 16 = 32 bytes` 变为 `4 * 16 = 64 bytes`。

---

## 2. GitHub Release 脚本 PowerShell 语法错误

**现象**: 执行 release 脚本时报错，Here-String 终止符不匹配。

**根因**: [release-v0.2.0.ps1](file:///c:/Users/even/Documents/SOLO-Even/tsnet-bridge/release-v0.2.0.ps1)
PowerShell Here-String 使用 `@'` 和 `'@` 作为定界符，但在脚本中使用了不匹配的引号风格。

**解决方案**: 改为使用 `@'` / `'@` 定界符（单引号 Here-String 禁止变量展开），确保内容中的反引号不被解释。

---

## 3. GitHub API 上传超时

**现象**: 使用 `gh release create` 命令时网络超时，无法连接 GitHub。

**根因**: 网络环境可能限制了对 `github.com` 某些端点的直连。

**解决方案**: 改用 GitHub REST API 直接上传（`Invoke-RestMethod`），通过 `api.github.com` 配合 Token 认证，支持重试机制。

---

## 4. 命令行过长导致 PowerShell 截断

**现象**: 内联 PowerShell 命令执行时被截断，报错 "The command length has exceeded the limit"。

**根因**: 复杂的 PowerShell 脚本通过 `-Command` 参数传递时超出命令行长度限制。

**解决方案**: 将脚本保存到 `.ps1` 文件，然后通过文件路径执行 (`.\release-v0.2.0.ps1`)。

---

## 5. HTTPS 后端 WebSocket 连接失败（OpenClaw）

**现象**: 浏览器通过桥接访问 OpenClaw（HTTPS 后端）时，页面能加载但 Gateway WebSocket 连接断开，报错 `disconnected (1006): no reason`，提示"检查 WebSocket URL；当 Gateway 位于 HTTPS/Tailscale Serve 后面时使用 wss://"。

**根因**: 两个问题：
1. [bridge.go](file:///c:/Users/even/Documents/SOLO-Even/tsnet-bridge/bridge.go) `httputil.NewSingleHostReverseProxy` 默认将 `req.Host` 改为后端地址（如 `vitasguo-g16-pro.tailc66d5e.ts.net:443`），OpenClaw 据此构造 WebSocket URL 指向 Tailscale DNS 域名，浏览器无法解析该域名（PC 未装 Tailscale）
2. Go 标准库的 `ReverseProxy` 对 HTTPS 后端的 WebSocket 升级支持不完善

**解决方案**:
1. [bridge.go:393](file:///c:/Users/even/Documents/SOLO-Even/tsnet-bridge/bridge.go#L393) 在 `Director` 中保存客户端原始 Host，调用 `origDirector` 后恢复 `req.Host = clientHost`，使后端收到 `Host: localhost:18900`，生成指向 localhost 的 WebSocket URL
2. 新增 `handleWebSocketTunnel()` 函数，检测到 `Upgrade: websocket` 时创建原始 TCP 隧道（经 Tailscale 连接 + TLS 握手 + 透传双向数据）
3. `X-Forwarded-Proto` 设为 `http`（客户端到 bridge 是 HTTP），避免后端生成 `wss://`
4. 修复多 target 路由中路径前缀剥离后 `//` 双斜杠 bug