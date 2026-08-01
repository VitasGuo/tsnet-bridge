# tsnet-bridge

> 通过 Tailscale 隧道桥接远程服务到本地端口，无需安装 Tailscale 客户端。

## 当前版本: v0.2.1 (代码审查优化)

## 项目目标

一个 30MB 的单文件程序，通过 Tailscale 加密隧道（tsnet 嵌入式 WireGuard）把远程 GPU 主机上的大模型（LM Studio / Ollama / vLLM / OpenClaw）桥接到本地端口。任何 AI agent（opencode、cursor、trae、cline 等）以标准 OpenAI 兼容 API 直连，对 Tailscale 零感知。

## 版本历史

### v0.2.1 (2026-08-01)
- **修复**: WebSocket 隧道 bufio.Reader 数据丢失 — 101 响应后缓冲区内的 WebSocket 数据未被转发到客户端（1006 错误的真正根因）
- **修复**: `defaultProxy` 硬编码 `"default"` 改为使用 `default_target` 配置或第一个 target
- **修复**: 单 target 模式去除冗余 `/v1/` 路由（`/` 已覆盖所有路径）
- **修复**: WebSocket 隧道失败时通过 `writeRawError` 向客户端发送错误响应（之前 hijack 后直接 return，客户端收到 EOF）
- **修复**: bridge.go 缩进混乱（gofmt 规范化）
- **修复**: GitHub Actions Go 版本从 1.22 升级到 1.24（匹配 go.mod）
- **修复**: webui.go import 顺序 + struct 字段对齐
- **清理**: 删除 AUDIT.md（审查报告）和 release-v0.2.0.ps1（一次性脚本）
- **文档**: config.example.yaml 新增 `default_target` 说明

### v0.2.0 (2026-07-25)
- **新增**: HTTPS 支持 — target 新增 `scheme: https` 字段
- **新增**: Tailscale DNS — address 支持 MagicDNS 域名
- **新增**: WebSocket 隧道 — 原始 TCP 隧道处理 HTTPS 后端的 WebSocket 升级（OpenClaw Gateway 等）
- **修复**: ICO 图标 AND mask 行对齐（32→64 bytes），Windows 11 托盘图标正常渲染
- **修复**: 保留原始 Host 头通过 ReverseProxy，修复 OpenClaw WebSocket URL 生成
- **修复**: X-Forwarded-Proto 设为 `http`，避免后端生成 `wss://` URL
- **文档**: README / config.example.yaml / 托盘使用说明 更新 DNS/scheme 示例
- **项目**: process.md + traps.md 文档初始化
- **发布**: GitHub Release v0.2.0 (https://github.com/VitasGuo/tsnet-bridge/releases/tag/v0.2.0)

### v0.1.0 (2026-07-24)
- 系统托盘作为默认界面
- 配置编辑 / 重新加载 / 启动 / 停止
- 自动启动开关
- 仅本机访问开关 (localhost-only toggle)
- 使用说明弹窗
- Tailscale 管理后台链接
- Web UI 模式 (`--web`)
- 多目标路由
- BSD 3-Clause 许可证
- 已知问题: Windows 11 托盘图标不显示（已在 v0.2.0 修复）

## 已知问题

见 traps.md

## 构建

```powershell
$env:GOPROXY = "https://goproxy.cn,direct"
go build -ldflags "-H windowsgui -s -w" -o tsnet-bridge.exe .
```
