// Package server serves the show page, the live event stream, and the
// small control API (waves, auto, demo agents, environment switch).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/traffic"
)

// Streamer is the hub's subscription side.
type Streamer interface {
	Subscribe() (<-chan []byte, func())
}

// Waves is the traffic generator's control side.
type Waves interface {
	Wave(ctx context.Context) (int, error)
	SetAuto(on bool)
	Auto() bool
	EnsureDemoAgents(ctx context.Context) ([]string, error)
}

// ConnInfo describes a kagent environment.
type ConnInfo struct {
	API       string `json:"api"`
	Gateway   string `json:"gateway"`
	UserID    string `json:"userId"`
	Token     string `json:"token,omitempty"`
	CAFile    string `json:"caFile"`
	Namespace string `json:"namespace"`
}

// Connector switches the app to another environment.
type Connector func(ConnInfo) error

type server struct {
	hub     Streamer
	waves   Waves
	connect Connector
	mu      sync.Mutex
	conn    ConnInfo
}

// New builds the HTTP handler.
func New(h Streamer, w Waves, connect Connector, assets fs.FS, initial ConnInfo) http.Handler {
	s := &server{hub: h, waves: w, connect: connect, conn: initial}
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(assets))
	mux.HandleFunc("GET /events", s.events)
	mux.HandleFunc("POST /api/wave", sameOriginJSON(s.wave))
	mux.HandleFunc("POST /traffic", sameOriginJSON(s.wave)) // Stream Deck friendly alias
	mux.HandleFunc("POST /api/auto", sameOriginJSON(s.auto))
	mux.HandleFunc("POST /api/demo-agents", sameOriginJSON(s.demo))
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("POST /api/config", sameOriginJSON(s.setConfig))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	return mux
}

// sameOriginJSON guards state-changing calls against cross-site requests from
// other pages in the viewer's browser: the body must be declared JSON (a
// cross-origin page cannot send that without a preflight this server never
// answers) and any Origin header must match this host.
func sameOriginJSON(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "content-type must be application/json"})
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Host != r.Host {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
				return
			}
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, cancel := s.hub.Subscribe()
	defer cancel()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			w.Write([]byte(": ping\n\n"))
		case b, ok := <-ch:
			if !ok {
				return // dropped as too slow; the browser reconnects
			}
			w.Write([]byte("data: "))
			w.Write(b)
			w.Write([]byte("\n\n"))
		}
		flusher.Flush()
	}
}

func (s *server) wave(w http.ResponseWriter, r *http.Request) {
	n, err := s.waves.Wave(context.WithoutCancel(r.Context()))
	switch {
	case errors.Is(err, traffic.ErrWaveRunning), errors.Is(err, traffic.ErrNoAgents):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case err != nil:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusAccepted, map[string]int{"sent": n})
	}
}

func (s *server) auto(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.waves.SetAuto(body.On)
	writeJSON(w, http.StatusOK, map[string]bool{"auto": s.waves.Auto()})
}

func (s *server) demo(w http.ResponseWriter, r *http.Request) {
	names, err := s.waves.EnsureDemoAgents(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "created": names})
		return
	}
	if names == nil {
		names = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": names})
}

func (s *server) getConfig(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	c := s.conn
	s.mu.Unlock()
	c.Token = ""
	writeJSON(w, http.StatusOK, c)
}

func (s *server) setConfig(w http.ResponseWriter, r *http.Request) {
	var c ConnInfo
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	c.CAFile = s.conn.CAFile // a file path is container config, never request input
	// The form never sees the token; keep it only if every endpoint it is
	// sent to (API and A2A gateway) is unchanged.
	if c.Token == "" && c.API == s.conn.API && gateway(c) == gateway(s.conn) {
		c.Token = s.conn.Token
	}
	if c.Namespace == "" {
		c.Namespace = s.conn.Namespace
	}
	if c.UserID == "" {
		c.UserID = s.conn.UserID
	}
	s.mu.Unlock()
	if err := s.connect(c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	s.conn = c
	s.mu.Unlock()
	c.Token = ""
	writeJSON(w, http.StatusOK, c)
}

// gateway is where A2A traffic (and the token) goes: the API unless set.
func gateway(c ConnInfo) string {
	if c.Gateway != "" {
		return c.Gateway
	}
	return c.API
}
