// tsnet-bridge: Bridge a local port to a remote LLM server over Tailscale —
// without installing Tailscale on your machine.
//
// It joins a Tailscale tailnet using tsnet (embedded WireGuard), then exposes
// a local HTTP server that looks like a standard OpenAI-compatible LLM API.
// Any AI agent (opencode, cursor, trae, cline, ...) can connect to it as if
// it were a local LLM, with zero awareness of the underlying network.
//
// A built-in Web UI lets you configure and control the bridge from a browser.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

const DefaultWebUIPort = ":18901"

func main() {
	webPort := flag.String("web", DefaultWebUIPort, "Web UI listen address")
	flag.Parse()

	// Load existing config (if any)
	cfg, err := loadConfig()
	if err != nil {
		log.Printf("warning: could not load config: %v", err)
	}

	// Create bridge and Web UI
	bridge := NewBridge()
	webui := newWebUI(bridge, cfg)

	// Start Web UI server
	log.Printf("tsnet-bridge Web UI: http://127.0.0.1%s", *webPort)
	go openBrowser(fmt.Sprintf("http://127.0.0.1%s", *webPort))

	server := &http.Server{
		Addr:    *webPort,
		Handler: webui.handler(),
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Web UI server error: %v", err)
	}
}
