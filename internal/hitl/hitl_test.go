package hitl

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/traffic"
)

type fakeDecider struct {
	mu       sync.Mutex
	calls    []string
	next     traffic.ChatResult
	gate     chan struct{}
	approved []bool
}

func (f *fakeDecider) Decide(_ context.Context, id string, ap *traffic.Approval, approved bool, reason string) (traffic.ChatResult, error) {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id+":"+ap.TaskID)
	f.approved = append(f.approved, approved)
	return f.next, nil
}

type marks struct{ sent, ended int }

func (m *marks) MarkSent(string, time.Time)      { m.sent++ }
func (m *marks) MarkTurnEnded(string, time.Time) { m.ended++ }

func approval(task string) *traffic.Approval {
	return &traffic.Approval{TaskID: task, Tools: []traffic.ApprovalTool{{ID: "c1", Name: "k8s_get_resources"}}}
}

func TestBoardHoldsPendingInOrder(t *testing.T) {
	var seen [][]Pending
	b := New(&fakeDecider{}, &marks{}, Options{OnChange: func(p []Pending) { seen = append(seen, p) }})
	b.Add("s1", "ops", approval("t1"))
	b.Add("s2", "ops", approval("t2"))
	p := b.List()
	if len(p) != 2 || p[0].SessionID != "s1" || p[1].SessionID != "s2" || p[0].Tools[0].Name != "k8s_get_resources" {
		t.Fatalf("pending %+v", p)
	}
	if len(seen) != 2 || len(seen[1]) != 2 {
		t.Fatalf("OnChange %+v", seen)
	}
}

func TestBoardDecideOldestAnswersAndClears(t *testing.T) {
	d := &fakeDecider{next: traffic.ChatResult{Reply: "8 pods"}}
	var results []string
	b := New(d, &marks{}, Options{OnResult: func(id string, approved bool, res traffic.ChatResult, err error) {
		results = append(results, id+":"+res.Reply)
	}})
	b.Add("s1", "ops", approval("t1"))
	b.Add("s2", "ops", approval("t2"))
	if err := b.Decide(context.Background(), "", true); err != nil {
		t.Fatal(err)
	}
	b.wg.Wait()
	if len(d.calls) != 1 || d.calls[0] != "s1:t1" || !d.approved[0] {
		t.Fatalf("decided %v %v", d.calls, d.approved)
	}
	if p := b.List(); len(p) != 1 || p[0].SessionID != "s2" {
		t.Fatalf("still pending %+v", p)
	}
	if len(results) != 1 || results[0] != "s1:8 pods" {
		t.Fatalf("results %v", results)
	}
}

func TestBoardDecideUnknownSession(t *testing.T) {
	b := New(&fakeDecider{}, &marks{}, Options{})
	if err := b.Decide(context.Background(), "nope", true); !errors.Is(err, ErrNotPending) {
		t.Fatalf("err %v", err)
	}
	if err := b.Decide(context.Background(), "", false); !errors.Is(err, ErrNotPending) {
		t.Fatalf("empty board: err %v", err)
	}
}

// A resumed turn can pause again for another tool: it goes back on the board.
func TestBoardResumedTurnCanPauseAgain(t *testing.T) {
	d := &fakeDecider{next: traffic.ChatResult{Approval: approval("t1b")}}
	b := New(d, &marks{}, Options{})
	b.Add("s1", "ops", approval("t1"))
	b.Decide(context.Background(), "s1", true)
	b.wg.Wait()
	if p := b.List(); len(p) != 1 || p[0].TaskID != "t1b" || p[0].Agent != "ops" {
		t.Fatalf("pending %+v", p)
	}
}

func TestBoardMarksTheResumedTurnForLatency(t *testing.T) {
	m := &marks{}
	b := New(&fakeDecider{}, m, Options{})
	b.Add("s1", "ops", approval("t1"))
	b.Decide(context.Background(), "s1", false)
	b.wg.Wait()
	if m.sent != 1 || m.ended != 1 {
		t.Fatalf("marks %+v", m)
	}
}

func TestBoardDecideTwiceIsRefusedWhileDeciding(t *testing.T) {
	d := &fakeDecider{gate: make(chan struct{})}
	b := New(d, &marks{}, Options{})
	b.Add("s1", "ops", approval("t1"))
	if err := b.Decide(context.Background(), "s1", true); err != nil {
		t.Fatal(err)
	}
	if err := b.Decide(context.Background(), "s1", true); !errors.Is(err, ErrNotPending) {
		t.Fatalf("second decision: %v", err)
	}
	close(d.gate)
	b.wg.Wait()
}
