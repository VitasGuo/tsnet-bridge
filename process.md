# tsnet-bridge

> 通过 Tailscale 隧道桥接远程 LLM 到本地端口，无需安装 Tailscale 客户端。

## 当前版本: v0.2.0 (HTTPS + Tailscale DNS)

## 项目目标

一个 30MB 的单文件程序，通过 Tailscale 加密隧道（tsnet 嵌入式 WireGuard）把远程 GPU 主机上的大模型（LM Studio / Ollama / vLLM / OpenClaw）桥接到本地端口。任何 AI agent（opencode、cursor、trae、cline 等）以标准 OpenAI 兼容 API 直连，对 Tailscale 零感知。

## 版本历史

### v0.2.0 (2026-07-25)
- **新增**: HTTPS 支持 — target 新增 `scheme: https` 字段
- **新增**: Tailscale DNS — address 支持 MagicDNS 域名
- **修复**: ICO 图标 AND mask 行对齐（32→64 bytes），Windows 11 托盘图标正常渲染
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

## 发布

```powershell
.\release-v0.2.0.ps1
```