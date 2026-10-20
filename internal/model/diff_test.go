package model

import (
	"reflect"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func snap(actors ...Actor) Snapshot {
	m := map[string]Actor{}
	for _, a := range actors {
		m[a.UID] = a
	}
	return Snapshot{TakenAt: t0, Actors: m}
}

func actor(uid string, st ActorState, gen int64, worker string, at time.Duration) Actor {
	return Actor{UID: uid, Name: "session-" + uid, Agent: "agent-" + uid, State: st, Pool: "pool", Worker: worker, SnapshotGen: gen, UpdatedAt: t0.Add(at)}
}

func kinds(evs []Event) []EventKind {
	out := []EventKind{}
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

func assertKinds(t *testing.T, evs []Event, want ...EventKind) {
	t.Helper()
	if want == nil {
		want = []EventKind{}
	}
	if got := kinds(evs); !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
}

func TestDiffFirstPollSeedsOnly(t *testing.T) {
	evs := Diff(Snapshot{}, snap(actor("a", StateRunning, 1, "w1", 0)))
	assertKinds(t, evs)
}

func TestDiffCreate(t *testing.T) {
	evs := Diff(snap(), snap(actor("a", StateResuming, 0, "w1", time.Second)))
	assertKinds(t, evs, EventCreate)
	e := evs[0]
	if e.ActorUID != "a" || e.Agent != "agent-a" || e.Pool != "pool" || e.Worker != "w1" || !e.At.Equal(t0.Add(time.Second)) {
		t.Fatalf("bad event %+v", e)
	}
}

func TestDiffCreateAlreadySuspended(t *testing.T) {
	evs := Diff(snap(), snap(actor("a", StateSuspended, 1, "", 0)))
	assertKinds(t, evs, EventCreate, EventSuspend)
}

func TestDiffResumeToRunning(t *testing.T) {
	evs := Diff(snap(actor("a", StateSuspended, 1, "", 0)), snap(actor("a", StateRunning, 1, "w1", time.Second)))
	assertKinds(t, evs, EventResume, EventRunning)
}

func TestDiffResumeToResuming(t *testing.T) {
	evs := Diff(snap(actor("a", StateSuspending, 1, "", 0)), snap(actor("a", StateResuming, 1, "w1", time.Second)))
	assertKinds(t, evs, EventResume)
}

func TestDiffResumingThenRunning(t *testing.T) {
	evs := Diff(snap(actor("a", StateResuming, 1, "w1", 0)), snap(actor("a", StateRunning, 1, "w1", time.Second)))
	assertKinds(t, evs, EventRunning)
}

func TestDiffSuspend(t *testing.T) {
	evs := Diff(snap(actor("a", StateRunning, 1, "w1", 0)), snap(actor("a", StateSuspended, 2, "", time.Second)))
	assertKinds(t, evs, EventSuspend)
	if evs[0].Worker != "w1" {
		t.Fatalf("suspend worker = %q, want w1 from prev", evs[0].Worker)
	}
}

func TestDiffMissedCycle(t *testing.T) {
	evs := Diff(snap(actor("a", StateSuspended, 2, "", 0)), snap(actor("a", StateSuspended, 3, "", time.Second)))
	assertKinds(t, evs, EventResume, EventSuspend)
}

func TestDiffCrash(t *testing.T) {
	evs := Diff(snap(actor("a", StateRunning, 1, "w1", 0)), snap(actor("a", StateCrashed, 1, "w1", time.Second)))
	assertKinds(t, evs, EventFail)
	evs = Diff(snap(actor("a", StateCrashed, 1, "w1", 0)), snap(actor("a", StateCrashed, 1, "w1", time.Second)))
	assertKinds(t, evs)
}

func TestDiffGone(t *testing.T) {
	evs := Diff(snap(actor("a", StateSuspended, 1, "", 0)), snap())
	assertKinds(t, evs, EventGone)
	if evs[0].Agent != "agent-a" {
		t.Fatalf("gone agent = %q", evs[0].Agent)
	}
}

func TestDiffNoChange(t *testing.T) {
	a := actor("a", StateRunning, 1, "w1", 0)
	assertKinds(t, Diff(snap(a), snap(a)))
}

func TestDiffOrdering(t *testing.T) {
	prev := snap()
	cur := snap(actor("late", StateRunning, 0, "w1", 2*time.Second), actor("early", StateRunning, 0, "w2", time.Second))
	evs := Diff(prev, cur)
	if len(evs) != 2 || evs[0].ActorUID != "early" || evs[1].ActorUID != "late" {
		t.Fatalf("order = %+v", evs)
	}
}
