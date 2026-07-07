package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
}

type Config struct {
	AuthKey   string   `yaml:"authkey"    json:"authkey"`
	Hostname  string   `yaml:"hostname"   json:"hostname"`
	Ephemeral *bool    `yaml:"ephemeral"  json:"ephemeral"`
	Listen    string   `yaml:"listen"     json:"listen"`
	StateDir  string   `yaml:"state-dir"  json:"stateDir"`
	Targets   []Target `yaml:"targets"    json:"targets"`
	AutoStart bool     `yaml:"autostart"  json:"autostart"`
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
	// Extract port from current listen
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
	// Try cwd first
	if data, err := os.ReadFile("tsnet-bridge.yaml"); err == nil {
		return cfg, yamlUnmarshal(data, &cfg)
	}
	// Try home dir
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

// -------------------------------------------------------
// Bridge
// -------------------------------------------------------

type StatusInfo struct {
	State    State          `json:"state"`
	Error    string         `json:"error,omitempty"`
	IP       string         `json:"ip,omitempty"`
	Hostname string         `json:"hostname,omitempty"`
	Targets  map[string]string `json:"targets,omitempty"`
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

	// Resolve authkey
	authkey := cfg.AuthKey
	if authkey == "" {
		authkey = os.Getenv("TS_AUTHKEY")
	}
	if authkey == "" {
		b.fail(errors.New("no authkey provided"))
		return
	}

	// Resolve state dir
	stateDir := cfg.StateDir
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".tsnet-bridge", "state")
	}

	ephemeral := true
	if cfg.Ephemeral != nil {
		ephemeral = *cfg.Ephemeral
	}

	// Create tsnet server
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

	// Join tailnet with retry
	if err := b.joinWithRetry(srv); err != nil {
		b.fail(err)
		return
	}

	// Build HTTP handler
	handler := b.buildHandler(cfg)

	// Start listening
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
			http.Error(w, fmt.Sprintf(`{"status":"error","error":"%s"}`, err.Error()), http.StatusServiceUnavailable)
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

	// Single target → serve at /v1/...
	if len(cfg.Targets) == 1 {
		t := cfg.Targets[0]
		targetURL, _ := url.Parse("http://" + t.Address)
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
		}
		// Inject API key if configured
		origDirector := proxy.Director
		apiKey := t.APIKey
		proxy.Director = func(req *http.Request) {
			origDirector(req)
			if apiKey != "" {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}
		}

		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			log.Printf("%s %s → %s", r.Method, r.URL.Path, t.Address)
			proxy.ServeHTTP(w, r)
		})
		return mux
	}

	// Multiple targets → serve at /<name>/v1/...
	for _, t := range cfg.Targets {
		name := t.Name
		targetURL, _ := url.Parse("http://" + t.Address)
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
		}
		origDirector := proxy.Director
		apiKey := t.APIKey
		proxy.Director = func(req *http.Request) {
			origDirector(req)
			if apiKey != "" {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}
		}

		prefix := "/" + name + "/"
		addr := t.Address
		mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = r.URL.Path[len("/"+name):]
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
			log.Printf("%s %s → %s", r.Method, r.URL.Path, addr)
			proxy.ServeHTTP(w, r)
		})
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
