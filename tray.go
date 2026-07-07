package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/getlantern/systray"
)

// TrayApp owns the systray lifecycle and wires menu items to the Bridge.
type TrayApp struct {
	bridge *Bridge

	mu     sync.Mutex
	cfg    Config

	mStart     *systray.MenuItem
	mStop      *systray.MenuItem
	mEdit      *systray.MenuItem
	mReload    *systray.MenuItem
	mUsage     *systray.MenuItem
	mAdmin     *systray.MenuItem
	mStatus    *systray.MenuItem
	mAutoStart *systray.MenuItem
	mQuit      *systray.MenuItem
}

func newTrayApp(bridge *Bridge, cfg Config) *TrayApp {
	return &TrayApp{bridge: bridge, cfg: cfg}
}

func (t *TrayApp) run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *TrayApp) onReady() {
	systray.SetTitle("tsnet-bridge")
	systray.SetTooltip("tsnet-bridge")
	systray.SetIcon(iconIdle)

	t.mStatus = systray.AddMenuItem("状态: 未启动", "")
	t.mStatus.Disable()

	systray.AddSeparator()

	t.mStart = systray.AddMenuItem("启动", "连接到 Tailscale 尾网")
	t.mStop = systray.AddMenuItem("停止", "断开连接")
	t.mStop.Hide()

	systray.AddSeparator()

	t.mEdit = systray.AddMenuItem("编辑配置", "用系统编辑器打开 config.yaml")
	t.mReload = systray.AddMenuItem("重新加载配置", "从磁盘重新读取配置")
	t.mUsage = systray.AddMenuItem("使用说明", "查看 Agent 配置方法")
	t.mAdmin = systray.AddMenuItem("Tailscale 管理后台", "在浏览器打开 admin console")
	t.mAutoStart = systray.AddMenuItem("启动时自动连接", "程序启动后自动连接")
	if t.cfg.autostart() {
		t.mAutoStart.Check()
	}

	systray.AddSeparator()

	t.mQuit = systray.AddMenuItem("退出", "退出 tsnet-bridge")

	// Menu click handlers (run in systray's goroutine)
	go t.handleClicks()

	// Status poller
	go t.pollStatus()

	// Auto-start if enabled
	if t.cfg.autostart() {
		go func() {
			time.Sleep(500 * time.Millisecond)
			t.start()
		}()
	}
}

func (t *TrayApp) onExit() {
	_ = t.bridge.Stop()
}

func (t *TrayApp) handleClicks() {
	for {
		select {
		case <-t.mStart.ClickedCh:
			t.start()
		case <-t.mStop.ClickedCh:
			t.stop()
		case <-t.mEdit.ClickedCh:
			t.editConfig()
		case <-t.mReload.ClickedCh:
			t.reloadConfig()
		case <-t.mUsage.ClickedCh:
			t.showUsage()
		case <-t.mAdmin.ClickedCh:
			openBrowser("https://login.tailscale.com/admin/machines")
		case <-t.mAutoStart.ClickedCh:
			t.toggleAutostart()
		case <-t.mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

func (t *TrayApp) start() {
	t.mu.Lock()
	cfg := t.cfg
	t.mu.Unlock()

	if err := t.bridge.Start(cfg); err != nil {
		log.Printf("start failed: %v", err)
	}
}

func (t *TrayApp) stop() {
	if err := t.bridge.Stop(); err != nil {
		log.Printf("stop failed: %v", err)
	}
}

func (t *TrayApp) editConfig() {
	path := configFilePath()
	if err := openInEditor(path); err != nil {
		log.Printf("open editor failed: %v", err)
	}
}

func (t *TrayApp) reloadConfig() {
	cfg, err := loadConfig()
	if err != nil {
		log.Printf("reload config failed: %v", err)
		return
	}
	t.mu.Lock()
	t.cfg = cfg
	t.mu.Unlock()
	log.Printf("config reloaded")
}

func (t *TrayApp) toggleAutostart() {
	t.mu.Lock()
	t.cfg = t.cfg.withAutostart(!t.cfg.autostart())
	cfg := t.cfg
	t.mu.Unlock()

	if cfg.autostart() {
		t.mAutoStart.Check()
	} else {
		t.mAutoStart.Uncheck()
	}
	if err := saveConfig(cfg); err != nil {
		log.Printf("save autostart flag failed: %v", err)
	}
}

func (t *TrayApp) pollStatus() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		status := t.bridge.Status()
		t.updateUI(status)
	}
}

func (t *TrayApp) updateUI(s StatusInfo) {
	// Icon + title
	var icon []byte
	label := "状态: "
	switch s.State {
	case StateIdle:
		icon = iconIdle
		label += "未启动"
		t.mStart.Show()
		t.mStop.Hide()
		t.mStart.SetTitle("启动")
	case StateConnecting:
		icon = iconConnecting
		label += "连接中..."
		t.mStart.Hide()
		t.mStop.Show()
	case StateRunning:
		icon = iconRunning
		label += "运行中"
		if s.IP != "" {
			label += " · " + s.IP
		}
		t.mStart.Hide()
		t.mStop.Show()
	case StateError:
		icon = iconError
		label += "错误"
		if s.Error != "" {
			// Truncate long errors for the menu
			err := s.Error
			if len(err) > 40 {
				err = err[:37] + "..."
			}
			label += " · " + err
		}
		t.mStart.Show()
		t.mStop.Hide()
		t.mStart.SetTitle("重试启动")
	default:
		icon = iconIdle
		label += string(s.State)
	}

	systray.SetIcon(icon)
	t.mStatus.SetTitle(label)
}

// openInEditor opens a file in the system default editor.
func openInEditor(path string) error {
	switch runtime.GOOS {
	case "windows":
		// notepad is guaranteed to exist; fall back to it if no association.
		if err := exec.Command("notepad.exe", path).Start(); err != nil {
			return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
		}
		return nil
	case "darwin":
		return exec.Command("open", "-t", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

// showUsage writes a usage guide to a temp file and opens it in notepad.
func (t *TrayApp) showUsage() {
	content := `========================================
  tsnet-bridge 使用说明
========================================

【一、什么是 tsnet-bridge】

一个 30MB 的单文件程序，通过 Tailscale 加密隧道把远程 GPU 主机上的
大模型（LM Studio / Ollama / vLLM）桥接到本地端口。

对 Agent 来说，它就是一个本地 OpenAI 兼容 API，agent 完全感知不到
Tailscale 的存在。所有流量端到端加密（WireGuard）。

【二、首次配置】

1. GPU 主机：装好 Tailscale + LM Studio/Ollama，记下 Tailscale IP
   （形如 100.x.x.x）和端口（LM Studio 默认 1234）
2. 去 https://login.tailscale.com/admin/settings/keys 生成
   Ephemeral auth key（格式：tskey-auth-xxxxx-xxxxx）
3. 右键托盘 → 编辑配置 → 填写以下内容，保存关闭：

   authkey: "tskey-auth-xxxxx-xxxxx"
   hostname: "tsnet-bridge"
   ephemeral: true
   listen: ":18900"
   targets:
     - name: default
       address: "100.x.x.x:1234"
       apikey: "sk-lm-xxx"     # LM Studio 的 API key

4. 右键托盘 → 重新加载配置 → 启动
5. 图标变绿 = 连接成功

【三、Agent 配置（关键）】

桥接地址：  http://localhost:18900/v1
API Key：   任意非空字符串（如 sk-bridge），bridge 会自动注入真实 key
模型名：    必须精确匹配 /v1/models 返回的 ID（区分大小写）

--- OpenCode (opencode.json) ---
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

--- Cursor ---
Settings → Models → Override OpenAI Base URL:
http://localhost:18900/v1
API Key: sk-bridge
Model:   google/gemma-4-12b-qat

--- Cline / Continue / VSCode ---
{
  "apiProvider": "openai",
  "apiBase": "http://localhost:18900/v1",
  "apiKey": "sk-bridge",
  "model": "google/gemma-4-12b-qat"
}

--- Python ---
from openai import OpenAI
client = OpenAI(base_url="http://localhost:18900/v1", api_key="sk-bridge")
resp = client.chat.completions.create(
    model="google/gemma-4-12b-qat",
    messages=[{"role": "user", "content": "你好"}],
)

--- curl ---
curl http://localhost:18900/v1/chat/completions ^
  -H "Content-Type: application/json" ^
  -H "Authorization: Bearer sk-bridge" ^
  -d "{\"model\":\"google/gemma-4-12b-qat\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}"

【四、查看可用模型】

浏览器访问 http://localhost:18900/v1/models
或在终端：curl http://localhost:18900/v1/models

【五、状态图标颜色】

灰色 = 未启动
黄色 = 连接中
绿色 = 运行中（后面显示 Tailscale IP）
红色 = 错误（后面显示错误信息）

【六、多 GPU 主机路由】

配置多个 targets 时，每个 target 走独立路径：
http://localhost:18900/<name>/v1/...

【七、安全说明】

- 软件本身不含任何用户信息，凭据存在 ~/.tsnet-bridge/config.yaml
- Tailscale auth key 用 ephemeral 类型，程序退出后节点自动从尾网清除
- WireGuard 端到端加密，本地端口默认监听 0.0.0.0:18900
  （如只想本机访问，把 listen 改成 "127.0.0.1:18900"）

【八、许可证】

BSD 3-Clause License
内嵌 tailscale.com (BSD-3) / systray (Apache-2.0) / yaml.v3 (Apache-2.0)

项目地址：https://github.com/VitasGuo/tsnet-bridge
`

	// Write to temp file and open in notepad
	tmpFile := filepath.Join(os.TempDir(), "tsnet-bridge-usage.txt")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		log.Printf("write usage file failed: %v", err)
		return
	}
	if err := openInEditor(tmpFile); err != nil {
		log.Printf("open usage file failed: %v", err)
	}
}
