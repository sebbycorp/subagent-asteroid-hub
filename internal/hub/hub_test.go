package hub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/gateway"
	"github.com/sebbycorp/subagent-asteroid-hub/internal/model"
)

var t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// fakeSource returns the queued results in order, then repeats the last one.
type fakeSource struct {
	mu  sync.Mutex
	out []result
}

type result struct {
	s   model.Snapshot
	err error
}

func (f *fakeSource) Snapshot(context.Context) (model.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.out[0]
	if len(f.out) > 1 {
		f.out = f.out[1:]
	}
	return r.s, r.err
}

func src(rs ...result) *fakeSource { return &fakeSource{out: rs} }

func ok(at time.Duration, actors ...model.Actor) result {
	m := map[string]model.Actor{}
	for _, a := range actors {
		m[a.UID] = a
	}
	return result{s: model.Snapshot{TakenAt: t0.Add(at), Actors: m}}
}

func fail(msg string) result { return result{err: errors.New(msg)} }

func act(uid string, st model.ActorState, gen int64, at time.Duration) model.Actor {
	return model.Actor{UID: uid, SessionID: "s-" + uid, Agent: "hello-substrate", State: st, Worker: "w1", SnapshotGen: gen, UpdatedAt: t0.Add(at)}
}

type msg struct {
	Type  string      `json:"type"`
	State State       `json:"state"`
	Event model.Event `json:"event"`
}

func drain(ch <-chan []byte) []msg {
	var out []msg
	for {
		select {
		case b := <-ch:
			var m msg
			if err := json.Unmarshal(b, &m); err != nil {
				panic(err)
			}
			out = append(out, m)
		default:
			return out
		}
	}
}

func events(ms []msg) []model.Event {
	var out []model.Event
	for _, m := range ms {
		if m.Type == "event" {
			out = append(out, m.Event)
		}
	}
	return out
}

func TestHubFirstTickNoEvents(t *testing.T) {
	h := New(src(ok(0, act("a", model.StateRunning, 1, 0))), "env")
	ch, cancel := h.Subscribe()
	defer cancel()
	h.Tick(context.Background())
	ms := drain(ch)
	if len(events(ms)) != 0 {
		t.Fatalf("events on first tick: %+v", ms)
	}
	if !h.State().Connected || len(h.State().Snapshot.Actors) != 1 {
		t.Fatalf("state %+v", h.State())
	}
}

func TestHubEmitsCaptionedEventsWithSeq(t *testing.T) {
	h := New(src(ok(0), ok(time.Second, act("a", model.StateResuming, 0, time.Second), act("b", model.StateResuming, 0, 2*time.Second))), "env")
	ch, cancel := h.Subscribe()
	defer cancel()
	h.Tick(context.Background())
	h.Tick(context.Background())
	ms := drain(ch)
	evs := events(ms)
	if len(evs) != 2 || evs[0].Seq != 1 || evs[1].Seq != 2 {
		t.Fatalf("events %+v", evs)
	}
	if !strings.HasPrefix(evs[0].Caption, "NEW SESSION FOR HELLO-SUBSTRATE") {
		t.Fatalf("caption %q", evs[0].Caption)
	}
	// a tick's events go out before its state, so a browser reconciling on
	// the state already has the events queued and doesn't pre-empt them
	if ms[len(ms)-3].Type != "event" || ms[len(ms)-2].Type != "event" || ms[len(ms)-1].Type != "state" {
		t.Fatalf("expected events then state, got %+v", ms)
	}
}

func TestHubRestoreAndSuspendLatency(t *testing.T) {
	sent := t0.Add(10 * time.Second)
	turnEnd := t0.Add(12 * time.Second)
	h := New(src(
		ok(0, act("a", model.StateSuspended, 1, 0)),
		ok(11*time.Second, act("a", model.StateRunning, 1, 10*time.Second+130*time.Millisecond)),
		ok(13*time.Second, act("a", model.StateSuspended, 2, 12*time.Second+45*time.Millisecond)),
	), "env")
	ch, cancel := h.Subscribe()
	defer cancel()
	h.MarkSent("s-a", sent)
	h.Tick(context.Background())
	h.Tick(context.Background())
	h.MarkTurnEnded("s-a", turnEnd)
	h.Tick(context.Background())
	evs := events(drain(ch))
	var running, suspend *model.Event
	for i := range evs {
		switch evs[i].Kind {
		case model.EventRunning:
			running = &evs[i]
		case model.EventSuspend:
			suspend = &evs[i]
		}
	}
	if running == nil || running.RestoreMS == nil || *running.RestoreMS != 130 {
		t.Fatalf("running %+v", running)
	}
	if suspend == nil || suspend.SuspendMS == nil || *suspend.SuspendMS != 45 {
		t.Fatalf("suspend %+v", suspend)
	}
	if !strings.Contains(suspend.Caption, "45 MS AFTER THE TURN") {
		t.Fatalf("caption %q", suspend.Caption)
	}
	st := h.State().Stats
	if st.LastRestoreMS == nil || *st.LastRestoreMS != 130 || st.LastSuspendMS == nil || *st.LastSuspendMS != 45 {
		t.Fatalf("stats %+v", st)
	}
}

func TestHubDisconnectThenReconnectNoReplay(t *testing.T) {
	h := New(src(
		ok(0, act("a", model.StateRunning, 1, 0)),
		fail("connection refused"),
		ok(5*time.Second, act("a", model.StateSuspended, 2, 4*time.Second), act("b", model.StateRunning, 0, 4*time.Second)),
		ok(6*time.Second, act("a", model.StateSuspended, 2, 4*time.Second), act("b", model.StateSuspended, 1, 5*time.Second)),
	), "env")
	ch, cancel := h.Subscribe()
	defer cancel()
	h.Tick(context.Background())
	h.Tick(context.Background())
	if st := h.State(); st.Connected || st.Error == "" || len(st.Snapshot.Actors) != 1 {
		t.Fatalf("after error: %+v", st)
	}
	h.Tick(context.Background())
	if evs := events(drain(ch)); len(evs) != 0 {
		t.Fatalf("reconnect replayed events: %+v", evs)
	}
	if !h.State().Connected {
		t.Fatal("not reconnected")
	}
	h.Tick(context.Background())
	evs := events(drain(ch))
	if len(evs) != 1 || evs[0].ActorUID != "b" || evs[0].Kind != model.EventSuspend {
		t.Fatalf("after reconnect: %+v", evs)
	}
}

func TestHubSlowSubscriberDropped(t *testing.T) {
	h := New(src(ok(0)), "env")
	_, cancel := h.Subscribe() // never read
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 300; i++ {
			h.Tick(context.Background())
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Tick blocked on a slow subscriber")
	}
	if n := h.subscribers(); n != 0 {
		t.Fatalf("slow subscriber still attached (%d)", n)
	}
}

func TestHubStatsCount(t *testing.T) {
	h := New(src(
		ok(0),
		ok(time.Second, act("a", model.StateSuspended, 1, time.Second)),
		ok(2*time.Second, act("a", model.StateSuspended, 2, 2*time.Second)),
	), "env")
	for i := 0; i < 3; i++ {
		h.Tick(context.Background())
	}
	st := h.State().Stats
	if st.Creates != 1 || st.Resumes != 1 || st.Suspends != 2 {
		t.Fatalf("stats %+v", st)
	}
}

func TestHubSetSourceResetsDiff(t *testing.T) {
	h := New(src(ok(0, act("a", model.StateRunning, 1, 0))), "one")
	h.Tick(context.Background())
	ch, cancel := h.Subscribe()
	defer cancel()
	h.SetSource(src(ok(time.Second, act("z", model.StateRunning, 1, time.Second))), "two")
	h.Tick(context.Background())
	if evs := events(drain(ch)); len(evs) != 0 {
		t.Fatalf("switching env produced events: %+v", evs)
	}
	if h.State().Env != "two" {
		t.Fatalf("env %q", h.State().Env)
	}
}

// The actor can suspend before the chat's stream end is recorded; the
// suspend is held until MarkTurnEnded so its latency is still measured.
func TestHubHoldsSuspendUntilTurnEnds(t *testing.T) {
	h := New(src(
		ok(0, act("a", model.StateSuspended, 1, 0)),
		ok(11*time.Second, act("a", model.StateRunning, 1, 10*time.Second+130*time.Millisecond)),
		ok(13*time.Second, act("a", model.StateSuspended, 2, 12*time.Second+45*time.Millisecond)),
	), "env")
	ch, cancel := h.Subscribe()
	defer cancel()
	h.MarkSent("s-a", t0.Add(10*time.Second))
	h.Tick(context.Background())
	h.Tick(context.Background())
	h.Tick(context.Background())
	for _, e := range events(drain(ch)) {
		if e.Kind == model.EventSuspend {
			t.Fatalf("suspend broadcast before turn end: %+v", e)
		}
	}
	h.MarkTurnEnded("s-a", t0.Add(12*time.Second))
	evs := events(drain(ch))
	if len(evs) != 1 || evs[0].Kind != model.EventSuspend || evs[0].SuspendMS == nil || *evs[0].SuspendMS != 45 {
		t.Fatalf("after turn end: %+v", evs)
	}
	if evs[0].Seq != 3 { // resume=1, running=2
		t.Fatalf("seq %d", evs[0].Seq)
	}
	if h.State().Stats.Suspends != 1 {
		t.Fatalf("stats %+v", h.State().Stats)
	}
}

func TestHubHeldSuspendFlushesAfterTimeout(t *testing.T) {
	now := t0
	h := New(src(
		ok(0, act("a", model.StateRunning, 1, 0)),
		ok(time.Second, act("a", model.StateSuspended, 2, time.Second)),
	), "env")
	h.now = func() time.Time { return now }
	ch, cancel := h.Subscribe()
	defer cancel()
	h.MarkSent("s-a", t0)
	h.Tick(context.Background())
	h.Tick(context.Background())
	if evs := events(drain(ch)); len(evs) != 0 {
		t.Fatalf("early: %+v", evs)
	}
	now = now.Add(1100 * time.Millisecond)
	h.Tick(context.Background())
	evs := events(drain(ch))
	if len(evs) != 1 || evs[0].Kind != model.EventSuspend || evs[0].SuspendMS != nil {
		t.Fatalf("after timeout: %+v", evs)
	}
}

// The app's clock and the cluster's can differ by a few ms; a suspend stamped
// just before the turn-end mark is still "at the turn end", not unknown.
func TestHubSuspendSkewClampedToZero(t *testing.T) {
	h := New(src(
		ok(0, act("a", model.StateRunning, 1, 0)),
		ok(time.Second, act("a", model.StateSuspended, 2, 996*time.Millisecond)),
	), "env")
	ch, cancel := h.Subscribe()
	defer cancel()
	h.Tick(context.Background())
	h.MarkTurnEnded("s-a", t0.Add(time.Second)) // 4 ms after the suspend stamp
	h.Tick(context.Background())
	evs := events(drain(ch))
	if len(evs) != 1 || evs[0].SuspendMS == nil || *evs[0].SuspendMS != 0 {
		t.Fatalf("events %+v", evs)
	}
}

func TestHubSetTourInState(t *testing.T) {
	h := New(src(ok(0)), "env")
	ch, cancel := h.Subscribe()
	defer cancel()
	drain(ch)
	h.SetTour(map[string]any{"active": true, "chapter": 2})
	ms := drain(ch)
	if len(ms) != 1 || ms[0].Type != "state" {
		t.Fatalf("messages %+v", ms)
	}
	b, _ := json.Marshal(h.State())
	if !strings.Contains(string(b), `"tour":{"active":true,"chapter":2}`) {
		t.Fatalf("state json %s", b)
	}
}

// A brand-new session's first run is a cold start from the golden snapshot,
// not a restore: it is captioned and counted separately.
func TestHubColdStartIsNotARestore(t *testing.T) {
	h := New(src(
		ok(0),
		ok(time.Second, act("a", model.StateRunning, 0, 10*time.Second+300*time.Millisecond)),
	), "env")
	ch, cancel := h.Subscribe()
	defer cancel()
	h.MarkSent("s-a", t0.Add(10*time.Second))
	h.Tick(context.Background())
	h.Tick(context.Background())
	evs := events(drain(ch))
	if len(evs) != 2 || evs[1].Kind != model.EventRunning {
		t.Fatalf("events %+v", evs)
	}
	run := evs[1]
	if run.RestoreMS != nil || run.StartMS == nil || *run.StartMS != 300 {
		t.Fatalf("running %+v", run)
	}
	if !strings.Contains(run.Caption, "STARTED FROM THE GOLDEN SNAPSHOT IN 300 MS") {
		t.Fatalf("caption %q", run.Caption)
	}
	if h.State().Stats.LastRestoreMS != nil {
		t.Fatalf("cold start counted as restore: %+v", h.State().Stats)
	}
}

func TestHubGatewayEventsBroadcastAndCount(t *testing.T) {
	h := New(src(ok(0)), "env")
	ch, cancel := h.Subscribe()
	defer cancel()
	drain(ch)
	h.Gateway([]gateway.Event{
		{Kind: "llm", Direction: "egress", Agent: "poet", Target: "qwen"},
		{Kind: "tool", Direction: "egress", Agent: "poet", Target: "kagent-tools", Tool: "k8s_get_resources"},
		{Kind: "chat", Direction: "ingress", Target: "kagent-a2a"},
	})
	var gw int
	for b := range len(ch) {
		_ = b
		m := <-ch
		var msg struct {
			Type    string        `json:"type"`
			Gateway gateway.Event `json:"gateway"`
		}
		json.Unmarshal(m, &msg)
		if msg.Type == "gateway" {
			gw++
		}
	}
	if gw != 3 {
		t.Fatalf("gateway messages %d", gw)
	}
	g := h.State().Gateway
	if !g.Seen || g.LLM != 1 || g.Tools != 1 || g.Chats != 1 {
		t.Fatalf("gateway stats %+v", g)
	}
}

// Model calls share one model config, so they arrive without an agent name;
// the hub names the agent when exactly one actor is running at the time.
func TestHubAttributesUntaggedCallToTheOnlyRunningActor(t *testing.T) {
	h := New(src(ok(0, act("a", model.StateRunning, 1, 0), act("b", model.StateSuspended, 1, 0))), "env")
	h.Tick(context.Background())
	ch, cancel := h.Subscribe()
	defer cancel()
	drain(ch)
	h.Gateway([]gateway.Event{{Kind: "llm", Direction: "egress", Target: "qwen"}, {Kind: "chat", Direction: "ingress"}})
	var got []gateway.Event
	for range len(ch) {
		var m struct {
			Type    string        `json:"type"`
			Gateway gateway.Event `json:"gateway"`
		}
		json.Unmarshal(<-ch, &m)
		if m.Type == "gateway" {
			got = append(got, m.Gateway)
		}
	}
	if len(got) != 2 || got[0].Agent != "hello-substrate" || got[0].ActorUID != "a" {
		t.Fatalf("llm call not attributed: %+v", got)
	}
	if got[1].Agent != "" {
		t.Fatalf("ingress chat attributed: %+v", got[1])
	}
}

func TestHubLeavesUntaggedCallUnattributedWhenAmbiguous(t *testing.T) {
	h := New(src(ok(0, act("a", model.StateRunning, 1, 0), act("b", model.StateRunning, 1, 0))), "env")
	h.Tick(context.Background())
	ch, cancel := h.Subscribe()
	defer cancel()
	drain(ch)
	h.Gateway([]gateway.Event{{Kind: "llm", Direction: "egress", Target: "qwen"}})
	for range len(ch) {
		var m struct {
			Type    string        `json:"type"`
			Gateway gateway.Event `json:"gateway"`
		}
		json.Unmarshal(<-ch, &m)
		if m.Type == "gateway" && m.Gateway.Agent != "" {
			t.Fatalf("guessed an agent: %+v", m.Gateway)
		}
	}
}
