package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"io"
	"log"
	"net/http"
	"os/exec"
	"runtime"
)

//go:embed web
var webFS embed.FS

// -------------------------------------------------------
// Web UI Server
// -------------------------------------------------------

type WebUI struct {
	bridge *Bridge
	cfg    Config
}

func newWebUI(bridge *Bridge, cfg Config) *WebUI {
	return &WebUI{bridge: bridge, cfg: cfg}
}

func (w *WebUI) handler() http.Handler {
	mux := http.NewServeMux()

	// Static files
	static, _ := fs.Sub(webFS, "web")
	fileServer := http.FileServer(http.FS(static))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			r.URL.Path = "/index.html"
		}
		fileServer.ServeHTTP(w, r)
	})

	// API endpoints
	mux.HandleFunc("/api/config", w.handleConfig)
	mux.HandleFunc("/api/status", w.handleStatus)
	mux.HandleFunc("/api/start", w.handleStart)
	mux.HandleFunc("/api/stop", w.handleStop)
	mux.HandleFunc("/api/models", w.handleModels)
	mux.HandleFunc("/api/test", w.handleTest)

	return mux
}

func (w *WebUI) handleConfig(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json")
	switch req.Method {
	case http.MethodGet:
		_ = json.NewEncoder(resp).Encode(w.cfg)
	case http.MethodPost:
		var cfg Config
		if err := json.NewDecoder(req.Body).Decode(&cfg); err != nil {
			http.Error(resp, `{"error":"invalid config"}`, http.StatusBadRequest)
			return
		}
		w.cfg = cfg
		if err := saveConfig(cfg); err != nil {
			http.Error(resp, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(resp).Encode(map[string]any{"ok": true})
	default:
		http.Error(resp, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (w *WebUI) handleStatus(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(resp).Encode(w.bridge.Status())
}

func (w *WebUI) handleStart(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json")
	if req.Method != http.MethodPost {
		http.Error(resp, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if err := w.bridge.Start(w.cfg); err != nil {
		http.Error(resp, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(resp).Encode(map[string]any{"ok": true})
}

func (w *WebUI) handleStop(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json")
	if req.Method != http.MethodPost {
		http.Error(resp, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	_ = w.bridge.Stop()
	_ = json.NewEncoder(resp).Encode(map[string]any{"ok": true})
}

func (w *WebUI) handleModels(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json")
	status := w.bridge.Status()
	if status.State != StateRunning {
		http.Error(resp, `{"error":"bridge not running"}`, http.StatusBadRequest)
		return
	}
	// Fetch models via the bridge proxy
	listenAddr := w.cfg.Listen
	url := fmt.Sprintf("http://127.0.0.1%s/v1/models", normalizePort(listenAddr))
	httpResp, err := http.Get(url)
	if err != nil {
		http.Error(resp, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer httpResp.Body.Close()
	resp.WriteHeader(httpResp.StatusCode)
	_, _ = io.Copy(resp, httpResp.Body)
}

func (w *WebUI) handleTest(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json")
	if req.Method != http.MethodPost {
		http.Error(resp, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Model    string `json:"model"`
		Message  string `json:"message"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(resp, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	if body.Message == "" {
		body.Message = "Hello"
	}

	status := w.bridge.Status()
	if status.State != StateRunning {
		http.Error(resp, `{"error":"bridge not running"}`, http.StatusBadRequest)
		return
	}

	listenAddr := w.cfg.Listen
	url := fmt.Sprintf("http://127.0.0.1%s/v1/chat/completions", normalizePort(listenAddr))
	chatReq := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"%s"}],"max_tokens":100}`, body.Model, body.Message)
	httpResp, err := http.Post(url, "application/json", stringReader(chatReq))
	if err != nil {
		http.Error(resp, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer httpResp.Body.Close()
	resp.WriteHeader(httpResp.StatusCode)
	_, _ = io.Copy(resp, httpResp.Body)
}

func normalizePort(addr string) string {
	// ":18900" → ":18900", "18900" → ":18900"
	if len(addr) > 0 && addr[0] == ':' {
		return addr
	}
	return ":" + addr
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = exec.Command("xdg-open", url).Start()
	}
	if err != nil {
		log.Printf("failed to open browser: %v", err)
	}
}
