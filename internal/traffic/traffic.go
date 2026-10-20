// Package traffic sends real chats to agents in waves so the show always has
// actors creating, resuming and suspending. It only ever touches sessions it
// created (named "asteroids-…").
package traffic

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Client is the slice of the kagent API the generator needs.
type Client interface {
	ListReadyAgents(ctx context.Context) ([]AgentRef, error)
	ListSessions(ctx context.Context) ([]SessionInfo, error)
	CreateSession(ctx context.Context, a AgentRef, name string) (string, error)
	DeleteSession(ctx context.Context, id string) error
	Chat(ctx context.Context, sessionID, text string) (ChatResult, error)
	ListHarnesses(ctx context.Context) ([]string, error)
	CreateAgent(ctx context.Context, a AgentRef, harness, modelConfig, systemPrompt string) error
}

// Marker receives chat timing so latencies can be measured.
type Marker interface {
	MarkSent(id string, at time.Time)
	MarkTurnEnded(id string, at time.Time)
}

// Options tune the generator. Zero values take the defaults.
type Options struct {
	Namespace     string        // default "kagent"
	AgentsPerWave int           // default 4
	MaxSessions   int           // default 12
	Interval      time.Duration // auto-wave period; default 20s
	Now           func() time.Time
	// OnStatus, if set, is called whenever auto, in-flight or the note changes.
	OnStatus func(auto bool, inFlight int, note string)
}

var (
	ErrWaveRunning = errors.New("a wave is still running")
	ErrNoAgents    = errors.New("no ready agents")
	// ErrSessionGone is returned (wrapped) by Client.Chat when the session no
	// longer exists, e.g. someone deleted it.
	ErrSessionGone = errors.New("session no longer exists")
)

const (
	sessionPrefix  = "asteroids-"
	perAgentTarget = 2
	noAgentsNote   = "NO READY AGENTS. PRESS D FOR DEMO AGENTS"
)

var prompts = []string{
	"In one sentence, what is a sandbox?",
	"Name one planet and one fact about it, in one sentence.",
	"Reply with a single haiku about clouds.",
	"In one sentence, why do computers use snapshots?",
	"Give one tip for writing clear code, in one sentence.",
	"In one sentence, what does a load balancer do?",
}

var demoAgents = []struct{ name, prompt string }{
	{"asteroids-demo-scout", "You are Scout. Answer in one short, friendly sentence."},
	{"asteroids-demo-coder", "You are Coder, a terse programmer. Answer in one sentence."},
	{"asteroids-demo-poet", "You are Poet. Answer in a single short line of verse."},
}

type session struct {
	info     SessionInfo
	born     int64 // creation order; adopted sessions sort first
	lastUsed int64 // 0 = never chatted by this run
}

// Generator runs waves of real chats.
type Generator struct {
	o  Options
	wg sync.WaitGroup

	mu       sync.Mutex
	c        Client
	m        Marker
	sessions map[string]*session
	adopted  bool
	tick     int64
	rot      int
	prompt   int
	inFlight int
	auto     bool
	note     string
}

// New creates a generator. Auto waves start on.
func New(c Client, m Marker, o Options) *Generator {
	if o.Namespace == "" {
		o.Namespace = "kagent"
	}
	if o.AgentsPerWave <= 0 {
		o.AgentsPerWave = 4
	}
	if o.MaxSessions <= 0 {
		o.MaxSessions = 12
	}
	if o.Interval <= 0 {
		o.Interval = 20 * time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Generator{o: o, c: c, m: m, sessions: map[string]*session{}, auto: true}
}

// SetClient switches environment; owned sessions are re-adopted on the next wave.
func (g *Generator) SetClient(c Client) {
	g.mu.Lock()
	g.c, g.sessions, g.adopted, g.rot = c, map[string]*session{}, false, 0
	g.mu.Unlock()
}

// InFlight is the number of chats still running.
func (g *Generator) InFlight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return max(g.inFlight, 0)
}

// SetAuto turns automatic waves on or off.
func (g *Generator) SetAuto(on bool) {
	g.mu.Lock()
	g.auto = on
	g.mu.Unlock()
	g.status()
}

// Auto reports whether automatic waves are on.
func (g *Generator) Auto() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.auto
}

func (g *Generator) owned() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.sessions)
}

func (g *Generator) status() {
	if g.o.OnStatus == nil {
		return
	}
	g.mu.Lock()
	auto, n, note := g.auto, g.inFlight, g.note
	g.mu.Unlock()
	g.o.OnStatus(auto, n, note)
}

func (g *Generator) setNote(note string) {
	g.mu.Lock()
	g.note = note
	g.mu.Unlock()
	g.status()
}

// Run fires a wave every Interval while auto waves are on.
func (g *Generator) Run(ctx context.Context) {
	t := time.NewTicker(g.o.Interval)
	defer t.Stop()
	for {
		if g.Auto() {
			g.Wave(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (g *Generator) adopt(ctx context.Context, c Client) error {
	all, err := c.ListSessions(ctx)
	if err != nil {
		return err
	}
	var mine []SessionInfo
	for _, s := range all {
		if strings.HasPrefix(s.Name, sessionPrefix) {
			mine = append(mine, s)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].CreatedAt.Before(mine[j].CreatedAt) })
	g.mu.Lock()
	for i, s := range mine {
		g.sessions[s.ID] = &session{info: s, born: int64(i - len(mine))}
	}
	g.adopted = true
	g.mu.Unlock()
	return nil
}

// Wave starts one chat per agent (up to AgentsPerWave) and returns how many
// started. Chats run in the background.
func (g *Generator) Wave(ctx context.Context) (int, error) {
	g.mu.Lock()
	if g.inFlight != 0 { // > 0 running, -1 preparing
		g.mu.Unlock()
		return 0, ErrWaveRunning
	}
	g.inFlight = -1 // reserve while preparing
	c, adopted := g.c, g.adopted
	g.mu.Unlock()
	n, err := g.prepareAndStart(ctx, c, adopted)
	if err != nil {
		g.mu.Lock()
		g.inFlight = 0
		g.mu.Unlock()
		if errors.Is(err, ErrNoAgents) {
			g.setNote(noAgentsNote)
		}
		return 0, err
	}
	g.setNote("")
	return n, nil
}

func (g *Generator) prepareAndStart(ctx context.Context, c Client, adopted bool) (int, error) {
	if !adopted {
		if err := g.adopt(ctx, c); err != nil {
			return 0, err
		}
	}
	agents, err := c.ListReadyAgents(ctx)
	if err != nil {
		return 0, err
	}
	if len(agents) == 0 {
		return 0, ErrNoAgents
	}
	k := min(g.o.AgentsPerWave, len(agents))
	var targets []string
	picked := map[string]bool{}
	for i := 0; i < k; i++ {
		g.mu.Lock()
		a := agents[(g.rot+i)%len(agents)]
		g.mu.Unlock()
		id, err := g.pickSession(ctx, c, a, picked)
		if err != nil {
			return 0, err
		}
		picked[id] = true
		targets = append(targets, id)
	}
	g.mu.Lock()
	g.rot = (g.rot + k) % len(agents)
	g.inFlight = len(targets)
	g.mu.Unlock()
	g.status()
	for _, id := range targets {
		g.mu.Lock()
		text := prompts[g.prompt%len(prompts)]
		g.prompt++
		g.mu.Unlock()
		g.wg.Add(1)
		go g.chat(ctx, c, id, text)
	}
	return len(targets), nil
}

// pickSession returns the session to chat on for agent a: a new one while the
// agent has fewer than 2, otherwise the least recently used. Sessions already
// picked this wave are never deleted to make room.
func (g *Generator) pickSession(ctx context.Context, c Client, a AgentRef, picked map[string]bool) (string, error) {
	g.mu.Lock()
	var mine []*session
	for _, s := range g.sessions {
		if s.info.Agent == a {
			mine = append(mine, s)
		}
	}
	g.tick++
	tick := g.tick
	if len(mine) >= perAgentTarget {
		sort.Slice(mine, func(i, j int) bool {
			if mine[i].lastUsed != mine[j].lastUsed {
				return mine[i].lastUsed < mine[j].lastUsed
			}
			return mine[i].born < mine[j].born
		})
		mine[0].lastUsed = tick
		id := mine[0].info.ID
		g.mu.Unlock()
		return id, nil
	}
	var oldest *session
	if len(g.sessions) >= g.o.MaxSessions {
		for _, s := range g.sessions {
			if !picked[s.info.ID] && (oldest == nil || s.born < oldest.born) {
				oldest = s
			}
		}
		if oldest != nil {
			delete(g.sessions, oldest.info.ID)
		}
	}
	g.mu.Unlock()

	if oldest != nil {
		if err := c.DeleteSession(ctx, oldest.info.ID); err != nil {
			return "", fmt.Errorf("delete oldest session: %w", err)
		}
	}
	name := fmt.Sprintf("%s%s-%d", sessionPrefix, a.Name, g.o.Now().UnixMilli()%1_000_000)
	id, err := c.CreateSession(ctx, a, name)
	if err != nil {
		return "", err
	}
	g.mu.Lock()
	g.sessions[id] = &session{info: SessionInfo{ID: id, Name: name, Agent: a, CreatedAt: g.o.Now()}, born: tick, lastUsed: tick}
	g.mu.Unlock()
	return id, nil
}

func (g *Generator) chat(ctx context.Context, c Client, id, text string) {
	defer g.wg.Done()
	g.m.MarkSent(id, g.o.Now())
	_, err := c.Chat(ctx, id, text)
	g.m.MarkTurnEnded(id, g.o.Now())
	g.mu.Lock()
	g.inFlight--
	if errors.Is(err, ErrSessionGone) {
		delete(g.sessions, id) // next wave starts a fresh one
	}
	if err != nil {
		msg := strings.ToUpper(err.Error())
		if len(msg) > 90 {
			msg = msg[:90] + "…"
		}
		g.note = "CHATS FAILING: " + msg
	}
	g.mu.Unlock()
	g.status()
}

// EnsureDemoAgents creates three small demo agents when the namespace has no
// ready agents, and returns the names it created (nil if none were needed).
func (g *Generator) EnsureDemoAgents(ctx context.Context) ([]string, error) {
	g.mu.Lock()
	c := g.c
	g.mu.Unlock()
	agents, err := c.ListReadyAgents(ctx)
	if err != nil {
		return nil, err
	}
	if len(agents) > 0 {
		return nil, nil
	}
	harnesses, err := c.ListHarnesses(ctx)
	if err != nil {
		return nil, err
	}
	if len(harnesses) == 0 {
		return nil, errors.New("no Harness in namespace " + g.o.Namespace)
	}
	var names []string
	for _, d := range demoAgents {
		if err := c.CreateAgent(ctx, AgentRef{Namespace: g.o.Namespace, Name: d.name}, harnesses[0], "default-model-config", d.prompt); err != nil {
			return names, err
		}
		names = append(names, d.name)
	}
	return names, nil
}
