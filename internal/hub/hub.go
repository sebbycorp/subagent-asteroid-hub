// Package hub polls a Source, turns snapshot diffs into captioned events,
// measures latencies for the app's own chats, and fans everything out to
// browser subscribers.
package hub

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/gateway"
	"github.com/sebbycorp/subagent-asteroid-hub/internal/model"
)

// Source reads one snapshot of the cluster.
type Source interface {
	Snapshot(ctx context.Context) (model.Snapshot, error)
}

// Stats are running totals since the hub started.
type Stats struct {
	Creates       int64  `json:"creates"`
	Resumes       int64  `json:"resumes"`
	Suspends      int64  `json:"suspends"`
	Fails         int64  `json:"fails"`
	LastRestoreMS *int64 `json:"lastRestoreMs,omitempty"`
	LastSuspendMS *int64 `json:"lastSuspendMs,omitempty"`
}

// WaveStatus mirrors the traffic generator for the screen.
type WaveStatus struct {
	Auto     bool   `json:"auto"`
	InFlight int    `json:"inFlight"`
	Note     string `json:"note,omitempty"`
}

// GatewayStats counts requests agentgateway reported.
type GatewayStats struct {
	Seen  bool  `json:"seen"` // agentgateway has reported at least once
	LLM   int64 `json:"llm"`
	Tools int64 `json:"tools"`
	Chats int64 `json:"chats"`
}

// State is what the browser draws.
type State struct {
	Env       string         `json:"env"`
	Connected bool           `json:"connected"`
	Error     string         `json:"error,omitempty"`
	Snapshot  model.Snapshot `json:"snapshot"`
	Stats     Stats          `json:"stats"`
	Waves     WaveStatus     `json:"waves"`
	Tour      any            `json:"tour,omitempty"` // the guided tour's card, when set
	Gateway   GatewayStats   `json:"gateway"`
}

const subBuffer = 256

// holdFor is how long a suspend is held waiting for the chat's turn-end mark:
// Substrate often suspends within milliseconds of the reply stream ending.
const holdFor = time.Second

type heldSuspend struct {
	e        model.Event
	session  string
	deadline time.Time
}

// Hub owns the current state. All methods are safe for concurrent use.
type Hub struct {
	mu        sync.Mutex
	src       Source
	state     State
	prev      model.Snapshot // zero after an error or a source switch
	seq       uint64
	sent      map[string]time.Time
	turnEnded map[string]time.Time
	chatting  map[string]bool // sent, turn not yet ended
	held      map[string]heldSuspend
	lastKind  map[string]model.EventKind // per actor UID
	subs      map[chan []byte]struct{}
	now       func() time.Time
}

// New creates a hub reading from src.
func New(src Source, env string) *Hub {
	return &Hub{
		src:       src,
		state:     State{Env: env, Snapshot: model.Snapshot{Actors: map[string]model.Actor{}}},
		sent:      map[string]time.Time{},
		turnEnded: map[string]time.Time{},
		chatting:  map[string]bool{},
		held:      map[string]heldSuspend{},
		lastKind:  map[string]model.EventKind{},
		subs:      map[chan []byte]struct{}{},
		now:       time.Now,
	}
}

// SetSource switches environment. The next tick only seeds state.
func (h *Hub) SetSource(src Source, env string) {
	h.mu.Lock()
	h.src, h.prev = src, model.Snapshot{}
	h.state = State{Env: env, Snapshot: model.Snapshot{Actors: map[string]model.Actor{}}, Waves: h.state.Waves, Tour: h.state.Tour}
	h.sent, h.turnEnded = map[string]time.Time{}, map[string]time.Time{}
	h.chatting, h.held = map[string]bool{}, map[string]heldSuspend{}
	h.mu.Unlock()
}

// MarkSent records when the app sent a chat to a session.
func (h *Hub) MarkSent(sessionID string, at time.Time) {
	h.mu.Lock()
	h.sent[sessionID] = at
	h.chatting[sessionID] = true
	h.mu.Unlock()
}

// MarkTurnEnded records when a chat's reply stream ended.
func (h *Hub) MarkTurnEnded(sessionID string, at time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.turnEnded[sessionID] = at
	delete(h.chatting, sessionID)
	if hs, ok := h.held[sessionID]; ok {
		delete(h.held, sessionID)
		h.releaseLocked([]model.Event{hs.e}, h.state.Snapshot)
	}
}

// SetWaves updates the traffic status shown on screen.
func (h *Hub) SetWaves(w WaveStatus) {
	h.mu.Lock()
	h.state.Waves = w
	msg := h.stateMsgLocked()
	h.broadcastLocked(msg)
	h.mu.Unlock()
}

// SetTour updates the guided-tour card shown on screen.
func (h *Hub) SetTour(t any) {
	h.mu.Lock()
	h.state.Tour = t
	h.broadcastLocked(h.stateMsgLocked())
	h.mu.Unlock()
}

// Gateway relays requests agentgateway handled, then the updated state.
func (h *Hub) Gateway(evs []gateway.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	g := &h.state.Gateway
	g.Seen = true
	// Model calls share one model config and carry no agent name; when only
	// one actor is running, the call is its. Otherwise leave it unnamed.
	var running []model.Actor
	for _, a := range h.state.Snapshot.Actors {
		if a.State == model.StateRunning || a.State == model.StateResuming {
			running = append(running, a)
		}
	}
	for i := range evs {
		e := &evs[i]
		if e.Direction != "egress" {
			continue
		}
		for _, a := range running {
			if e.Agent == "" && len(running) == 1 || e.Agent != "" && a.Agent == e.Agent {
				e.Agent, e.ActorUID = a.Agent, a.UID
				break
			}
		}
	}
	for _, e := range evs {
		switch e.Kind {
		case "llm":
			g.LLM++
		case "tool":
			g.Tools++
		case "chat":
			g.Chats++
		}
		b, _ := json.Marshal(map[string]any{"type": "gateway", "gateway": e})
		h.broadcastLocked(b)
	}
	h.broadcastLocked(h.stateMsgLocked())
}

// State returns a copy of the current state.
func (h *Hub) State() State {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state
}

// Run ticks until ctx ends.
func (h *Hub) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		h.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick polls once, diffs, and broadcasts the state followed by its events.
func (h *Hub) Tick(ctx context.Context) {
	h.mu.Lock()
	src := h.src
	h.mu.Unlock()
	snap, err := src.Snapshot(ctx)

	h.mu.Lock()
	defer h.mu.Unlock()
	if src != h.src { // environment switched mid-poll
		return
	}
	if err != nil {
		h.state.Connected, h.state.Error = false, err.Error()
		h.prev = model.Snapshot{}
		h.broadcastLocked(h.stateMsgLocked())
		return
	}
	now := h.now()
	var out []model.Event
	for session, hs := range h.held {
		if now.After(hs.deadline) {
			delete(h.held, session)
			out = append(out, hs.e)
		}
	}
	for _, e := range model.Diff(h.prev, snap) {
		session := h.sessionLocked(e.ActorUID, snap)
		if hs, ok := h.held[session]; ok { // newer news for this actor: release first
			delete(h.held, session)
			out = append(out, hs.e)
		}
		if e.Kind == model.EventSuspend && h.chatting[session] {
			h.held[session] = heldSuspend{e: e, session: session, deadline: now.Add(holdFor)}
			continue
		}
		out = append(out, e)
	}
	h.prev = snap
	h.state.Connected, h.state.Error, h.state.Snapshot = true, "", snap
	h.releaseLocked(out, snap)
}

func (h *Hub) sessionLocked(uid string, snap model.Snapshot) string {
	if s := snap.Actors[uid].SessionID; s != "" {
		return s
	}
	return h.prev.Actors[uid].SessionID
}

// releaseLocked numbers, measures and captions events, then broadcasts the
// events followed by the state, so a browser reconciling on the state
// already has those events queued.
func (h *Hub) releaseLocked(evs []model.Event, snap model.Snapshot) {
	for i := range evs {
		h.enrichLocked(&evs[i], snap)
	}
	for _, e := range evs {
		b, _ := json.Marshal(map[string]any{"type": "event", "event": e})
		h.broadcastLocked(b)
	}
	h.broadcastLocked(h.stateMsgLocked())
}

// skewMS is how far the cluster's clock may trail the app's; readings
// within it count as zero rather than unknown.
const skewMS = 50

func msBetween(from, to time.Time) *int64 {
	d := to.Sub(from).Milliseconds()
	if d < -skewMS {
		return nil
	}
	d = max(d, 0)
	return &d
}

func (h *Hub) enrichLocked(e *model.Event, snap model.Snapshot) {
	h.seq++
	e.Seq = h.seq
	session := h.sessionLocked(e.ActorUID, snap)
	st := &h.state.Stats
	switch e.Kind {
	case model.EventCreate:
		st.Creates++
	case model.EventResume:
		st.Resumes++
	case model.EventFail:
		st.Fails++
	case model.EventRunning:
		if at, ok := h.sent[session]; ok {
			delete(h.sent, session)
			if h.lastKind[e.ActorUID] == model.EventCreate {
				e.StartMS = msBetween(at, e.At) // cold start, not a restore
			} else if e.RestoreMS = msBetween(at, e.At); e.RestoreMS != nil {
				st.LastRestoreMS = e.RestoreMS
			}
		}
	case model.EventSuspend:
		st.Suspends++
		if at, ok := h.turnEnded[session]; ok {
			delete(h.turnEnded, session)
			if e.SuspendMS = msBetween(at, e.At); e.SuspendMS != nil {
				st.LastSuspendMS = e.SuspendMS
			}
		}
	}
	h.lastKind[e.ActorUID] = e.Kind
	if e.Kind == model.EventGone {
		delete(h.lastKind, e.ActorUID)
	}
	e.Caption = model.Caption(*e)
}

func (h *Hub) stateMsgLocked() []byte {
	b, _ := json.Marshal(map[string]any{"type": "state", "state": h.state})
	return b
}

func (h *Hub) broadcastLocked(b []byte) {
	for ch := range h.subs {
		select {
		case ch <- b:
		default: // too slow: drop the subscriber rather than block the show
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// Subscribe returns a channel of JSON messages, starting with the current
// state, and a cancel func. A subscriber that falls 256 messages behind is
// dropped (its channel is closed).
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, subBuffer)
	h.mu.Lock()
	ch <- h.stateMsgLocked()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
