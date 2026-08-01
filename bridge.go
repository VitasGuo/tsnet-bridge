package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tailscale.com/tsnet"
)

// -------------------------------------------------------
// State
// -------------------------------------------------------

type State string

const (
	StateIdle       State = "idle"
	StateConnecting State = "connecting"
	StateRunning    State = "running"
	StateError      State = "error"
)

// -------------------------------------------------------
// Config
// -------------------------------------------------------

type Target struct {
	Name    string `yaml:"name"    json:"name"`
	Address string `yaml:"address" json:"address"`
	APIKey  string `yaml:"apikey"  json:"apikey"`
	Scheme  string `yaml:"scheme"  json:"scheme"`
}

// targetScheme returns the URL scheme for this target, defaulting to "http".
func (t Target) targetScheme() string {
	if t.Scheme == "https" {
		return "https"
	}
	return "http"
}

type Config struct {
	AuthKey     string   `yaml:"authkey"        json:"authkey"`
	Hostname    string   `yaml:"hostname"       json:"hostname"`
	Ephemeral   *bool    `yaml:"ephemeral"      json:"ephemeral"`
	Listen      string   `yaml:"listen"         json:"listen"`
	StateDir    string   `yaml:"state-dir"      json:"stateDir"`
	Targets     []Target `yaml:"targets"        json:"targets"`
	DefaultName string   `yaml:"default_target" json:"default_target"`
	AutoStart   bool     `yaml:"autostart"      json:"autostart"`
}

func defaultConfig() Config {
	t := true
	return Config{
		Hostname:  "tsnet-bridge",
		Ephemeral: &t,
		Listen:    ":18900",
	}
}

// autostart returns the configured autostart flag.
func (c Config) autostart() bool { return c.AutoStart }

// withAutostart returns a copy with the autostart flag changed.
func (c Config) withAutostart(v bool) Config {
	c.AutoStart = v
	return c
}

// localhostOnly returns true if listen is bound to loopback only.
func (c Config) localhostOnly() bool {
	return strings.HasPrefix(c.Listen, "127.0.0.1:") || strings.HasPrefix(c.Listen, "localhost:")
}

// withLocalhostOnly toggles the listen address between ":port" and "127.0.0.1:port".
// Preserves the port portion. Returns a copy.
func (c Config) withLocalhostOnly(v bool) Config {
	port := c.Listen
	if i := strings.LastIndex(port, ":"); i >= 0 {
		port = port[i+1:]
	}
	if port == "" {
		port = "18900"
	}
	if v {
		c.Listen = "127.0.0.1:" + port
	} else {
		c.Listen = ":" + port
	}
	return c
}

func configFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".tsnet-bridge", "config.yaml")
}

func loadConfig() (Config, error) {
	cfg := defaultConfig()
	if data, err := os.ReadFile("tsnet-bridge.yaml"); err == nil {
		return cfg, yamlUnmarshal(data, &cfg)
	}
	p := configFilePath()
	if data, err := os.ReadFile(p); err == nil {
		return cfg, yamlUnmarshal(data, &cfg)
	}
	return cfg, nil
}

func saveConfig(cfg Config) error {
	p := configFilePath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := yamlMarshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}

// findDefaultTarget returns the target configured as default_target,
// or the first target if no default_target is set.
func findDefaultTarget(cfg Config) *Target {
	if cfg.DefaultName != "" {
		for i := range cfg.Targets {
			if cfg.Targets[i].Name == cfg.DefaultName {
				return &cfg.Targets[i]
			}
		}
	}
	if len(cfg.Targets) > 0 {
		return &cfg.Targets[0]
	}
	return nil
}

// -------------------------------------------------------
// Bridge
// -------------------------------------------------------

type StatusInfo struct {
	State    State              `json:"state"`
	Error    string             `json:"error,omitempty"`
	IP       string             `json:"ip,omitempty"`
	Hostname string             `json:"hostname,omitempty"`
	Targets  map[string]string  `json:"targets,omitempty"`
}

type Bridge struct {
	mu       sync.Mutex
	srv      *tsnet.Server
	listener net.Listener
	httpSrv  *http.Server
	state    State
	err      error
	cfg      Config
}

func NewBridge() *Bridge {
	return &Bridge{state: StateIdle}
}

func (b *Bridge) Start(cfg Config) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == StateRunning || b.state == StateConnecting {
		return errors.New("bridge is already running")
	}

	b.cfg = cfg
	b.err = nil
	b.state = StateConnecting

	go b.run()
	return nil
}

func (b *Bridge) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == StateIdle {
		return nil
	}

	if b.httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = b.httpSrv.Shutdown(ctx)
		b.httpSrv = nil
	}
	if b.listener != nil {
		_ = b.listener.Close()
		b.listener = nil
	}
	if b.srv != nil {
		_ = b.srv.Close()
		b.srv = nil
	}

	b.state = StateIdle
	return nil
}

func (b *Bridge) Status() StatusInfo {
	b.mu.Lock()
	defer b.mu.Unlock()

	info := StatusInfo{State: b.state}
	if b.err != nil {
		info.Error = b.err.Error()
	}
	if b.srv != nil && b.state == StateRunning {
		st, err := b.srv.Up(context.Background())
		if err == nil {
			info.Hostname = st.Self.DNSName
			if len(st.Self.TailscaleIPs) > 0 {
				info.IP = st.Self.TailscaleIPs[0].String()
			}
		}
	}
	if len(b.cfg.Targets) > 0 {
		info.Targets = make(map[string]string)
		for _, t := range b.cfg.Targets {
			name := t.Name
			if name == "" {
				name = "default"
			}
			info.Targets[name] = t.Address
		}
	}
	return info
}

func (b *Bridge) run() {
	cfg := b.cfg

	authkey := cfg.AuthKey
	if authkey == "" {
		authkey = os.Getenv("TS_AUTHKEY")
	}
	if authkey == "" {
		b.fail(errors.New("no authkey provided"))
		return
	}

	stateDir := cfg.StateDir
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".tsnet-bridge", "state")
	}

	ephemeral := true
	if cfg.Ephemeral != nil {
		ephemeral = *cfg.Ephemeral
	}

	srv := &tsnet.Server{
		Hostname:  cfg.Hostname,
		AuthKey:   authkey,
		Dir:       stateDir,
		Ephemeral: ephemeral,
		Logf:      func(format string, args ...any) { log.Printf("[tsnet] "+format, args...) },
	}

	b.mu.Lock()
	b.srv = srv
	b.mu.Unlock()

	if err := b.joinWithRetry(srv); err != nil {
		b.fail(err)
		return
	}

	handler := b.buildHandler(cfg)

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		b.fail(fmt.Errorf("listen %s: %w", cfg.Listen, err))
		return
	}

	b.mu.Lock()
	b.listener = listener
	b.state = StateRunning
	b.mu.Unlock()

	httpSrv := &http.Server{Handler: handler}
	b.mu.Lock()
	b.httpSrv = httpSrv
	b.mu.Unlock()

	log.Printf("bridge listening on %s", cfg.Listen)
	if err := httpSrv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		b.fail(fmt.Errorf("http serve: %w", err))
	}
}

func (b *Bridge) joinWithRetry(srv *tsnet.Server) error {
	const maxRetries = 3
	backoff := 2 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Printf("joining tailnet (attempt %d/%d)...", attempt, maxRetries)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		status, err := srv.Up(ctx)
		cancel()
		if err == nil {
			log.Printf("joined tailnet as %s (%v)", status.Self.DNSName, status.Self.TailscaleIPs)
			return nil
		}
		log.Printf("join attempt %d failed: %v", attempt, err)
		if attempt < maxRetries {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return errors.New("failed to join tailnet after retries")
}

func (b *Bridge) buildHandler(cfg Config) http.Handler {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/tsnet-bridge/health", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		srv := b.srv
		b.mu.Unlock()

		if srv == nil {
			http.Error(w, `{"status":"error","error":"not running"}`, http.StatusServiceUnavailable)
			return
		}
		st, err := srv.Up(context.Background())
		if err != nil {
			http.Error(w, `{"status":"error","error":"Tailscale connection failed"}`, http.StatusServiceUnavailable)
			return
		}
		ip := ""
		if len(st.Self.TailscaleIPs) > 0 {
			ip = st.Self.TailscaleIPs[0].String()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"hostname": st.Self.DNSName,
			"ip":       ip,
		})
	})

	// buildProxy creates a reverse proxy for a single target.
	buildProxy := func(t Target) *httputil.ReverseProxy {
		targetURL, _ := url.Parse(t.targetScheme() + "://" + t.Address)
		proxy := httputil.NewSingleHostReverseProxy(targetURL)
		proxy.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				b.mu.Lock()
				srv := b.srv
				b.mu.Unlock()
				if srv == nil {
					return nil, errors.New("bridge not running")
				}
				return srv.Dial(ctx, "tcp", t.Address)
			},
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ForceAttemptHTTP2:     false,
		}
		origDirector := proxy.Director
		apiKey := t.APIKey
		proxy.Director = func(req *http.Request) {
			clientHost := req.Host
			origDirector(req)
			if apiKey != "" {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}
			req.Header.Set("X-Forwarded-For", req.RemoteAddr)
			req.Header.Set("X-Forwarded-Host", clientHost)
			req.Header.Set("X-Forwarded-Proto", "http")
			req.Host = clientHost
		}
		return proxy
	}

	// Single target → all paths go to the same backend.
	if len(cfg.Targets) == 1 {
		t := cfg.Targets[0]
		proxy := buildProxy(t)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			log.Printf("%s %s → %s", r.Method, r.URL.Path, t.Address)
			if isWebSocketUpgrade(r) {
				b.handleWebSocketTunnel(w, r, t)
				return
			}
			proxy.ServeHTTP(w, r)
		})
		return mux
	}

	// Multiple targets → serve at /<name>/...
	var defaultProxy *httputil.ReverseProxy
	defaultT := findDefaultTarget(cfg)
	if defaultT != nil {
		defaultProxy = buildProxy(*defaultT)
	}

	for _, t := range cfg.Targets {
		name := t.Name
		prefix := "/" + name + "/"
		addr := t.Address
		target := t
		targetProxy := buildProxy(t)

		mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = r.URL.Path[len("/"+name):]
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
			log.Printf("%s %s → %s", r.Method, r.URL.Path, addr)
			if isWebSocketUpgrade(r) {
				b.handleWebSocketTunnel(w, r, target)
				return
			}
			targetProxy.ServeHTTP(w, r)
		})
	}

	// /v1/ route forwards to default target for OpenAI-compatible API calls.
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s → default target", r.Method, r.URL.Path)
		if isWebSocketUpgrade(r) {
			if defaultT != nil {
				b.handleWebSocketTunnel(w, r, *defaultT)
			} else {
				http.Error(w, `{"error":"default target not configured"}`, http.StatusBadGateway)
			}
			return
		}
		if defaultProxy != nil {
			defaultProxy.ServeHTTP(w, r)
		} else {
			http.Error(w, `{"error":"default target not configured"}`, http.StatusBadGateway)
		}
	})

	// Root handler: WebSocket fallback + service info.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketUpgrade(r) {
			log.Printf("websocket upgrade: %s %s (Origin: %s, Referer: %s)",
				r.Method, r.URL.RequestURI(), r.Header.Get("Origin"), r.Header.Get("Referer"))

			if target := b.resolveTargetFromPath(r.URL.Path, cfg); target != nil {
				b.handleWebSocketTunnel(w, r, *target)
				return
			}
			if target := b.resolveTargetFromReferer(r, cfg); target != nil {
				b.handleWebSocketTunnel(w, r, *target)
				return
			}
			if defaultT != nil {
				log.Printf("websocket upgrade: using default target %s for path: %s",
					defaultT.Name, r.URL.Path)
				b.handleWebSocketTunnel(w, r, *defaultT)
				return
			}
			log.Printf("websocket upgrade: no matching target found for path: %s", r.URL.Path)
			http.Error(w, `{"error":"no matching target for websocket"}`, http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		b.mu.Lock()
		targets := make(map[string]string)
		for _, t := range b.cfg.Targets {
			targets[t.Name] = t.Address
		}
		b.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "tsnet-bridge",
			"targets": targets,
		})
	})

	return mux
}

// resolveTargetFromPath checks the request path to find which target
// a request belongs to.
func (b *Bridge) resolveTargetFromPath(path string, cfg Config) *Target {
	for _, t := range cfg.Targets {
		prefix := "/" + t.Name + "/"
		if strings.HasPrefix(path, prefix) {
			return &t
		}
	}
	return nil
}

// resolveTargetFromReferer checks the Referer header to find which target
// a request belongs to.
func (b *Bridge) resolveTargetFromReferer(r *http.Request, cfg Config) *Target {
	referer := r.Header.Get("Referer")
	if referer == "" {
		return nil
	}
	for _, t := range cfg.Targets {
		prefix := "/" + t.Name + "/"
		if strings.Contains(referer, prefix) {
			return &t
		}
	}
	return nil
}

func (b *Bridge) fail(err error) {
	b.mu.Lock()
	b.state = StateError
	b.err = err
	if b.srv != nil {
		_ = b.srv.Close()
		b.srv = nil
	}
	if b.listener != nil {
		_ = b.listener.Close()
		b.listener = nil
	}
	b.mu.Unlock()
	log.Printf("bridge error: %v", err)
}

// handleWebSocketTunnel creates a raw TCP tunnel for WebSocket connections
// through the Tailscale network.
func (b *Bridge) handleWebSocketTunnel(w http.ResponseWriter, r *http.Request, target Target) {
	log.Printf("websocket tunnel: upgrading %s %s for target %s (address=%s, scheme=%s)",
		r.Method, r.URL.RequestURI(), target.Name, target.Address, target.targetScheme())

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "WebSocket not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	b.mu.Lock()
	srv := b.srv
	b.mu.Unlock()
	if srv == nil {
		writeRawError(clientConn, http.StatusBadGateway, "bridge not running")
		return
	}

	backendConn, err := srv.Dial(r.Context(), "tcp", target.Address)
	if err != nil {
		log.Printf("websocket dial %s: %v", target.Address, err)
		writeRawError(clientConn, http.StatusBadGateway, "backend dial failed")
		return
	}
	defer backendConn.Close()

	// If the target uses HTTPS, do a TLS handshake over the Tailscale connection.
	// Use a single bufio.Reader so buffered bytes are not lost after ReadResponse.
	var rawBackend net.Conn = backendConn
	if target.targetScheme() == "https" {
		host := target.Address
		if idx := strings.LastIndex(host, ":"); idx >= 0 {
			host = host[:idx]
		}
		tlsConn := tls.Client(backendConn, &tls.Config{
			ServerName:         host,
			InsecureSkipVerify: true, // Tailscale cert not in system trust store; WireGuard already encrypts
		})
		if err := tlsConn.Handshake(); err != nil {
			log.Printf("websocket tls handshake %s: %v", target.Address, err)
			writeRawError(clientConn, http.StatusBadGateway, "TLS handshake failed")
			return
		}
		rawBackend = tlsConn
		defer tlsConn.Close()
		log.Printf("websocket tls handshake OK: %s (SNI=%s)", target.Address, host)
	}

	// Forward headers
	r.Header.Set("X-Forwarded-For", r.RemoteAddr)
	r.Header.Set("X-Forwarded-Host", r.Host)
	r.Header.Set("X-Forwarded-Proto", "http")

	// Forward the HTTP upgrade request to the backend.
	if err := r.Write(rawBackend); err != nil {
		log.Printf("websocket write request: %v", err)
		writeRawError(clientConn, http.StatusBadGateway, "failed to write request to backend")
		return
	}

	// Read the backend's response using a bufio.Reader.
	// CRITICAL: after ReadResponse, the bufio.Reader may have buffered bytes
	// that arrived after the 101 headers. We must flush those to the client
	// before switching to raw io.Copy, otherwise data is lost (causing 1006).
	bufReader := bufio.NewReader(rawBackend)
	resp, err := http.ReadResponse(bufReader, r)
	if err != nil {
		log.Printf("websocket read response: %v", err)
		writeRawError(clientConn, http.StatusBadGateway, "failed to read backend response")
		return
	}
	log.Printf("websocket upgrade response: %d %s", resp.StatusCode, resp.Status)

	// Write the response status line + headers to the client.
	if err := resp.Write(clientConn); err != nil {
		log.Printf("websocket write response: %v", err)
		return
	}

	// Flush any bytes the bufio.Reader already consumed past the headers.
	if n := bufReader.Buffered(); n > 0 {
		bufBytes, _ := bufReader.Peek(n)
		if _, err := clientConn.Write(bufBytes); err != nil {
			log.Printf("websocket flush buffered: %v", err)
			return
		}
	}

	// Tunnel bidirectional data. After flushing the bufio buffer,
	// read remaining data from bufReader (not rawBackend) to avoid losing
	// any data the bufio.Reader already pulled from the socket.
	log.Printf("websocket tunnel established: %s → %s", r.Host, target.Address)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(rawBackend, clientConn)
	}()
	go func() {
		defer wg.Done()
		io.Copy(clientConn, bufReader)
	}()
	wg.Wait()
	log.Printf("websocket tunnel closed: %s", target.Address)
}

// writeRawError writes a minimal HTTP error response to a hijacked connection.
// Used after Hijack() when http.Error can no longer be called.
func writeRawError(w net.Conn, code int, msg string) {
	body := fmt.Sprintf(`{"error":"%s"}`, msg)
	fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		code, http.StatusText(code), len(body), body)
}

// isWebSocketUpgrade returns true if the request is a WebSocket upgrade.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}
