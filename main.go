// tsnet-bridge: Bridge a local port to a remote LLM server over Tailscale —
// without installing Tailscale on your machine.
//
// It joins a Tailscale tailnet using tsnet (embedded WireGuard), then exposes
// a local HTTP server that looks like a standard OpenAI-compatible LLM API.
// Any AI agent (opencode, cursor, trae, cline, ...) can connect to it as if
// it were a local LLM, with zero awareness of the underlying network.
//
// Default mode: system tray icon (systray). Click "启动" to connect, right-click
// to edit config / stop / quit. Run with --web for the browser-based UI instead.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

const (
	DefaultWebUIPort = ":18901"
)

func main() {
	webMode := flag.Bool("web", false, "Run Web UI mode instead of system tray")
	webPort := flag.String("web-port", DefaultWebUIPort, "Web UI listen address (only with --web)")
	flag.Parse()

	// Load existing config (if any)
	cfg, err := loadConfig()
	if err != nil {
		log.Printf("warning: could not load config: %v", err)
	}

	bridge := NewBridge()

	if *webMode {
		runWebUI(bridge, cfg, *webPort)
		return
	}

	// Default: system tray
	app := newTrayApp(bridge, cfg)
	app.run()
}

func runWebUI(bridge *Bridge, cfg Config, port string) {
	webui := newWebUI(bridge, cfg)
	log.Printf("tsnet-bridge Web UI: http://127.0.0.1%s", port)
	go openBrowser(fmt.Sprintf("http://127.0.0.1%s", port))
	server := &http.Server{
		Addr:    port,
		Handler: webui.handler(),
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Web UI server error: %v", err)
	}
}
