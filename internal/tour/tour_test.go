package tour

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/traffic"
)

type fakeClient struct {
	mu     sync.Mutex
	agents []traffic.AgentRef
	calls  []string
	n      int
	memory map[string]bool // sessions that know the code word
	ckOf   map[string]string
	gate   chan struct{}
	failOn string
}

func newFake() *fakeClient {
	return &fakeClient{
		agents: []traffic.AgentRef{{Namespace: "kagent", Name: "hello-substrate"}},
		memory: map[string]bool{}, ckOf: map[string]string{},
	}
}

func (f *fakeClient) log(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
	if f.failOn != "" && strings.HasPrefix(s, f.failOn) {
		return errors.New("boom")
	}
	return nil
}

func (f *fakeClient) ListReadyAgents(context.Context) ([]traffic.AgentRef, error) {
	return f.agents, nil
}

func (f *fakeClient) CreateSession(_ context.Context, a traffic.AgentRef, name string) (string, error) {
	f.mu.Lock()
	f.n++
	id := fmt.Sprintf("s%d", f.n)
	f.mu.Unlock()
	return id, f.log("create " + a.Name + " " + name)
}

func (f *fakeClient) Chat(_ context.Context, id, text string) (traffic.ChatResult, error) {
	if f.gate != nil {
		<-f.gate
	}
	if err := f.log("chat " + id); err != nil {
		return traffic.ChatResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Contains(text, "ORION") {
		f.memory[id] = true
		return traffic.ChatResult{Reply: "OK", TaskID: "task-" + id}, nil
	}
	if f.memory[id] {
		return traffic.ChatResult{Reply: "ORION", TaskID: "task2-" + id}, nil
	}
	return traffic.ChatResult{Reply: "I don't know", TaskID: "task3-" + id}, nil
}

func (f *fakeClient) Checkpoint(_ context.Context, id, head string) (string, error) {
	f.mu.Lock()
	f.ckOf["ck-"+id] = id
	f.mu.Unlock()
	return "ck-" + id, f.log("checkpoint " + id + " " + head)
}

func (f *fakeClient) Fork(_ context.Context, ck, name string) (string, error) {
	f.mu.Lock()
	f.n++
	id := fmt.Sprintf("s%d", f.n)
	f.memory[id] = f.memory[f.ckOf[ck]]
	f.mu.Unlock()
	return id, f.log("fork " + ck + " " + name)
}

type fakeWaves struct{ n int }

func (w *fakeWaves) Wave(context.Context) (int, error) { w.n++; return 3, nil }

type marks struct {
	mu    sync.Mutex
	sent  []string
	ended []string
}

func (m *marks) MarkSent(id string, _ time.Time) {
	m.mu.Lock()
	m.sent = append(m.sent, id)
	m.mu.Unlock()
}
func (m *marks) MarkTurnEnded(id string, _ time.Time) {
	m.mu.Lock()
	m.ended = append(m.ended, id)
	m.mu.Unlock()
}

type rig struct {
	t      *Tour
	c      *fakeClient
	w      *fakeWaves
	m      *marks
	active []bool
}

func newRig() *rig {
	r := &rig{c: newFake(), w: &fakeWaves{}, m: &marks{}}
	r.t = New(r.c, r.w, r.m, Options{OnActive: func(on bool) { r.active = append(r.active, on) }})
	return r
}

func (r *rig) step(t *testing.T, f func(context.Context) error) Status {
	t.Helper()
	if err := f(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.t.wg.Wait()
	return r.t.Status()
}

func TestTourWalksAllChapters(t *testing.T) {
	r := newRig()
	st := r.step(t, r.t.Start)
	if !st.Active || st.Chapter != 1 || st.Total != 5 || len(st.Focus) != 1 || st.Focus[0] != "s1" {
		t.Fatalf("chapter 1: %+v", st)
	}
	if !strings.Contains(st.Result, `"OK"`) {
		t.Fatalf("chapter 1 result %q", st.Result)
	}
	if !strings.HasPrefix(r.c.calls[0], "create hello-substrate asteroids-tour-") {
		t.Fatalf("calls %v", r.c.calls)
	}

	st = r.step(t, r.t.Next) // 2: suspend — narration only
	if st.Chapter != 2 || len(r.c.calls) != 2 || st.Focus[0] != "s1" {
		t.Fatalf("chapter 2: %+v calls %v", st, r.c.calls)
	}

	st = r.step(t, r.t.Next) // 3: resume — ask the code word
	if st.Chapter != 3 || !strings.Contains(st.Result, "ORION") || !strings.Contains(st.Result, "SURVIVED") {
		t.Fatalf("chapter 3: %+v", st)
	}

	st = r.step(t, r.t.Next) // 4: fork from a checkpoint at the last turn
	if st.Chapter != 4 || len(st.Focus) != 2 || st.Focus[1] != "s2" || !strings.Contains(st.Result, "ORION") {
		t.Fatalf("chapter 4: %+v calls %v", st, r.c.calls)
	}
	if !contains(r.c.calls, "checkpoint s1 task2-s1") {
		t.Fatalf("checkpoint not at the last turn: %v", r.c.calls)
	}

	st = r.step(t, r.t.Next) // 5: density wave
	if st.Chapter != 5 || r.w.n != 1 || !strings.Contains(st.Result, "3 CHATS") {
		t.Fatalf("chapter 5: %+v", st)
	}

	st = r.step(t, r.t.Next) // past the end: tour ends
	if st.Active {
		t.Fatalf("tour still active: %+v", st)
	}
	if fmt.Sprint(r.active) != "[true false]" {
		t.Fatalf("OnActive calls %v", r.active)
	}
	if len(r.m.sent) != 3 || len(r.m.ended) != 3 {
		t.Fatalf("marks sent %v ended %v", r.m.sent, r.m.ended)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func TestTourNextWhileBusy(t *testing.T) {
	r := newRig()
	r.c.gate = make(chan struct{})
	if err := r.t.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.t.Next(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if !r.t.Status().Busy {
		t.Fatal("status not busy")
	}
	close(r.c.gate)
	r.t.wg.Wait()
	if r.t.Status().Busy {
		t.Fatal("still busy")
	}
}

func TestTourActionErrorShown(t *testing.T) {
	r := newRig()
	r.c.failOn = "create"
	st := r.step(t, r.t.Start)
	if !st.Active || st.Error == "" || st.Busy {
		t.Fatalf("status %+v", st)
	}
}

func TestTourNoAgents(t *testing.T) {
	r := newRig()
	r.c.agents = nil
	st := r.step(t, r.t.Start)
	if !strings.Contains(st.Error, "NO READY AGENTS") {
		t.Fatalf("status %+v", st)
	}
}

func TestTourStop(t *testing.T) {
	r := newRig()
	r.step(t, r.t.Start)
	r.t.Stop()
	if r.t.Status().Active || fmt.Sprint(r.active) != "[true false]" {
		t.Fatalf("status %+v active %v", r.t.Status(), r.active)
	}
	r.t.Stop() // idempotent
	if fmt.Sprint(r.active) != "[true false]" {
		t.Fatalf("second stop fired OnActive: %v", r.active)
	}
}
