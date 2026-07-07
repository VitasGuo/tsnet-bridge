package main

import (
	"log"
	"os/exec"
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
