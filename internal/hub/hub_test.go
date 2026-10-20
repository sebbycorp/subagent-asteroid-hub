package hub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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
	h := New(src(ok(0), ok(time.Second, act("a", model.StateRunning, 0, time.Second), act("b", model.StateRunning, 0, 2*time.Second))), "env")
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
	// the state for a tick goes out before its events
	last := ms[len(ms)-3]
	if last.Type != "state" {
		t.Fatalf("expected state before events, got %+v", ms)
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
