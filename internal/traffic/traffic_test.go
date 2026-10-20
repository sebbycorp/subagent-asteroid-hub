package traffic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClient struct {
	mu        sync.Mutex
	agents    []AgentRef
	sessions  []SessionInfo
	harnesses []string
	created   []string // session names
	deleted   []string // session IDs
	chats     []string // session IDs
	newAgents []string
	chatGate  chan struct{} // if set, Chat blocks until closed
	listGate  chan struct{} // if set, ListReadyAgents blocks until closed
	listing   chan struct{} // if set, signalled when ListReadyAgents is entered
	n         int
}

func (f *fakeClient) ListReadyAgents(context.Context) ([]AgentRef, error) {
	if f.listing != nil {
		f.listing <- struct{}{}
	}
	if f.listGate != nil {
		<-f.listGate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]AgentRef(nil), f.agents...), nil
}
func (f *fakeClient) ListSessions(context.Context) ([]SessionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SessionInfo(nil), f.sessions...), nil
}
func (f *fakeClient) CreateSession(_ context.Context, a AgentRef, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	id := fmt.Sprintf("id-%d", f.n)
	f.created = append(f.created, name)
	f.sessions = append(f.sessions, SessionInfo{ID: id, Name: name, Agent: a})
	return id, nil
}
func (f *fakeClient) DeleteSession(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeClient) Chat(_ context.Context, id, _ string) error {
	if f.chatGate != nil {
		<-f.chatGate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chats = append(f.chats, id)
	return nil
}
func (f *fakeClient) ListHarnesses(context.Context) ([]string, error) { return f.harnesses, nil }
func (f *fakeClient) CreateAgent(_ context.Context, a AgentRef, harness, model, prompt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.newAgents = append(f.newAgents, a.Name+"@"+harness+"/"+model)
	return nil
}

type fakeMarker struct {
	mu    sync.Mutex
	sent  map[string]time.Time
	ended map[string]time.Time
}

func newMarker() *fakeMarker {
	return &fakeMarker{sent: map[string]time.Time{}, ended: map[string]time.Time{}}
}
func (m *fakeMarker) MarkSent(id string, at time.Time) {
	m.mu.Lock()
	m.sent[id] = at
	m.mu.Unlock()
}
func (m *fakeMarker) MarkTurnEnded(id string, at time.Time) {
	m.mu.Lock()
	m.ended[id] = at
	m.mu.Unlock()
}

func agents(names ...string) []AgentRef {
	var out []AgentRef
	for _, n := range names {
		out = append(out, AgentRef{Namespace: "kagent", Name: n})
	}
	return out
}

func wave(t *testing.T, g *Generator) int {
	t.Helper()
	n, err := g.Wave(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	g.wg.Wait()
	return n
}

func TestWaveCreatesThenResumes(t *testing.T) {
	c := &fakeClient{agents: agents("hello")}
	g := New(c, newMarker(), Options{})
	wave(t, g)
	wave(t, g)
	if len(c.created) != 2 {
		t.Fatalf("created %v, want 2 sessions", c.created)
	}
	for _, name := range c.created {
		if !strings.HasPrefix(name, "asteroids-hello-") {
			t.Fatalf("session name %q", name)
		}
	}
	wave(t, g) // now owns 2 for hello: chat on the least recently used (id-1)
	if len(c.created) != 2 {
		t.Fatalf("third wave created a session: %v", c.created)
	}
	if last := c.chats[len(c.chats)-1]; last != "id-1" {
		t.Fatalf("third wave chatted %q, want LRU id-1 (chats %v)", last, c.chats)
	}
	wave(t, g)
	if last := c.chats[len(c.chats)-1]; last != "id-2" {
		t.Fatalf("fourth wave chatted %q, want id-2", last)
	}
}

func TestWaveCapDeletesOldestOwned(t *testing.T) {
	c := &fakeClient{agents: agents("a", "b", "c")}
	g := New(c, newMarker(), Options{MaxSessions: 3})
	wave(t, g) // creates a1 b1 c1 (3, at cap)
	wave(t, g) // each agent has 1 < 2: creating needs room, oldest deleted first
	if len(c.deleted) != 3 || c.deleted[0] != "id-1" {
		t.Fatalf("deleted %v, want oldest first starting id-1", c.deleted)
	}
	if g.owned() > 3 {
		t.Fatalf("owned %d > cap", g.owned())
	}
}

func TestGeneratorAdoptsOwnSessionsOnly(t *testing.T) {
	c := &fakeClient{
		agents: agents("hello"),
		sessions: []SessionInfo{
			{ID: "old-1", Name: "asteroids-hello-1", Agent: agents("hello")[0], CreatedAt: time.Unix(1, 0)},
			{ID: "old-2", Name: "asteroids-hello-2", Agent: agents("hello")[0], CreatedAt: time.Unix(2, 0)},
			{ID: "theirs", Name: "other", Agent: agents("hello")[0], CreatedAt: time.Unix(0, 0)},
		},
	}
	g := New(c, newMarker(), Options{MaxSessions: 2})
	wave(t, g)
	if len(c.created) != 0 {
		t.Fatalf("created %v although 2 adopted sessions exist", c.created)
	}
	for _, id := range append(append([]string{}, c.chats...), c.deleted...) {
		if id == "theirs" {
			t.Fatal("touched a session the app does not own")
		}
	}
	if c.chats[0] != "old-1" {
		t.Fatalf("chatted %v, want adopted old-1", c.chats)
	}
}

func TestWaveMarksSentAndTurnEnded(t *testing.T) {
	c := &fakeClient{agents: agents("hello")}
	m := newMarker()
	g := New(c, m, Options{})
	wave(t, g)
	if _, ok := m.sent["id-1"]; !ok {
		t.Fatal("MarkSent not called")
	}
	if !m.ended["id-1"].After(m.sent["id-1"]) && !m.ended["id-1"].Equal(m.sent["id-1"]) {
		t.Fatal("MarkTurnEnded missing or before MarkSent")
	}
}

func TestWaveRejectsWhileRunning(t *testing.T) {
	c := &fakeClient{agents: agents("hello"), chatGate: make(chan struct{})}
	g := New(c, newMarker(), Options{})
	if _, err := g.Wave(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.InFlight() != 1 {
		t.Fatalf("in flight %d", g.InFlight())
	}
	if _, err := g.Wave(context.Background()); !errors.Is(err, ErrWaveRunning) {
		t.Fatalf("err = %v, want ErrWaveRunning", err)
	}
	close(c.chatGate)
	g.wg.Wait()
	if g.InFlight() != 0 {
		t.Fatalf("in flight %d after finish", g.InFlight())
	}
}

func TestWaveNoReadyAgents(t *testing.T) {
	c := &fakeClient{}
	g := New(c, newMarker(), Options{})
	if _, err := g.Wave(context.Background()); !errors.Is(err, ErrNoAgents) {
		t.Fatalf("err = %v, want ErrNoAgents", err)
	}
	if len(c.created)+len(c.chats) != 0 {
		t.Fatal("made calls with no agents")
	}
}

func TestWaveRotatesAgents(t *testing.T) {
	c := &fakeClient{agents: agents("a", "b", "c")}
	g := New(c, newMarker(), Options{AgentsPerWave: 2})
	wave(t, g)
	wave(t, g)
	got := map[string]int{}
	for _, s := range c.sessions {
		got[s.Agent.Name]++
	}
	if got["c"] == 0 {
		t.Fatalf("agent c never used: %v", got)
	}
}

func TestEnsureDemoAgentsOnlyWhenEmpty(t *testing.T) {
	c := &fakeClient{harnesses: []string{"kagent-default"}}
	g := New(c, newMarker(), Options{})
	names, err := g.EnsureDemoAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"asteroids-demo-scout", "asteroids-demo-coder", "asteroids-demo-poet"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("names %v", names)
	}
	if c.newAgents[0] != "asteroids-demo-scout@kagent-default/default-model-config" {
		t.Fatalf("agent %q", c.newAgents[0])
	}
	c2 := &fakeClient{agents: agents("hello"), harnesses: []string{"h"}}
	names, err = New(c2, newMarker(), Options{}).EnsureDemoAgents(context.Background())
	if err != nil || names != nil || len(c2.newAgents) != 0 {
		t.Fatalf("created demo agents although agents exist: %v %v", names, c2.newAgents)
	}
}

func TestWaveRejectsWhilePreparing(t *testing.T) {
	c := &fakeClient{agents: agents("hello"), listGate: make(chan struct{}), listing: make(chan struct{}, 1)}
	g := New(c, newMarker(), Options{})
	done := make(chan error)
	go func() { _, err := g.Wave(context.Background()); done <- err }()
	<-c.listing
	if _, err := g.Wave(context.Background()); !errors.Is(err, ErrWaveRunning) {
		t.Fatalf("second wave while first prepares: err = %v, want ErrWaveRunning", err)
	}
	close(c.listGate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	g.wg.Wait()
}
