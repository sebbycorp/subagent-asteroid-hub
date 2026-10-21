// Package hitl holds agent turns paused for a human (kagent's human in the
// loop) and answers them. While a turn waits here, Substrate has the agent
// suspended: waiting for a person costs no compute.
package hitl

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/traffic"
)

// Decider sends a human's decision and returns the resumed turn.
type Decider interface {
	Decide(ctx context.Context, sessionID string, ap *traffic.Approval, approved bool, reason string) (traffic.ChatResult, error)
}

// Pending is one paused turn on the board, as the screen shows it.
type Pending struct {
	SessionID string                 `json:"sessionId"`
	Agent     string                 `json:"agent"`
	TaskID    string                 `json:"taskId"`
	Tools     []traffic.ApprovalTool `json:"tools"`
	Since     time.Time              `json:"since"`
}

// Options hooks the board into the app.
type Options struct {
	OnChange func([]Pending)                                                          // the board changed
	OnResult func(sessionID string, approved bool, res traffic.ChatResult, err error) // a decided turn finished
	Now      func() time.Time
}

var ErrNotPending = errors.New("no turn is waiting for that decision")

// RejectionReason is what the agent is told when a person says no.
const RejectionReason = "a human rejected this tool call"

// Board is safe for concurrent use.
type Board struct {
	o  Options
	m  traffic.Marker
	wg sync.WaitGroup

	mu      sync.Mutex
	d       Decider
	pending []Pending
	approvs map[string]*traffic.Approval
}

// New creates an empty board.
func New(d Decider, m traffic.Marker, o Options) *Board {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Board{o: o, m: m, d: d, approvs: map[string]*traffic.Approval{}}
}

// SetDecider switches environment; anything pending belonged to the old one.
func (b *Board) SetDecider(d Decider) {
	b.mu.Lock()
	b.d, b.pending, b.approvs = d, nil, map[string]*traffic.Approval{}
	b.mu.Unlock()
	b.changed()
}

// List returns the waiting turns, oldest first.
func (b *Board) List() []Pending {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Pending(nil), b.pending...)
}

func (b *Board) changed() {
	if b.o.OnChange != nil {
		b.o.OnChange(b.List())
	}
}

// Add puts a paused turn on the board (replacing an older pause of the same
// session).
func (b *Board) Add(sessionID, agent string, ap *traffic.Approval) {
	b.mu.Lock()
	b.removeLocked(sessionID)
	b.pending = append(b.pending, Pending{SessionID: sessionID, Agent: agent, TaskID: ap.TaskID, Tools: ap.Tools, Since: b.o.Now()})
	b.approvs[sessionID] = ap
	b.mu.Unlock()
	b.changed()
}

func (b *Board) removeLocked(sessionID string) (Pending, bool) {
	for i, p := range b.pending {
		if p.SessionID == sessionID {
			b.pending = append(b.pending[:i], b.pending[i+1:]...)
			delete(b.approvs, sessionID)
			return p, true
		}
	}
	return Pending{}, false
}

// Forget drops a session's pending request (the session was deleted).
func (b *Board) Forget(sessionID string) {
	b.mu.Lock()
	_, ok := b.removeLocked(sessionID)
	b.mu.Unlock()
	if ok {
		b.changed()
	}
}

// Decide answers a waiting turn in the background; the agent resumes and
// finishes its turn. The decision must name the exact turn (session and
// task) the person was shown, so it can't land on a request they never saw.
func (b *Board) Decide(ctx context.Context, sessionID, taskID string, approved bool) error {
	b.mu.Lock()
	ap := b.approvs[sessionID]
	if sessionID == "" || taskID == "" || ap == nil || ap.TaskID != taskID {
		b.mu.Unlock()
		return ErrNotPending
	}
	p, ok := b.removeLocked(sessionID)
	d := b.d
	b.mu.Unlock()
	if !ok {
		return ErrNotPending
	}
	b.changed()
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ctx := context.WithoutCancel(ctx)
		b.m.MarkSent(sessionID, b.o.Now())
		res, err := d.Decide(ctx, sessionID, ap, approved, RejectionReason)
		if err == nil && res.Approval != nil {
			// the resumed turn wants another tool: back on the board, then
			// end the turn (the hub holds the suspend until the board is set)
			b.Add(sessionID, p.Agent, res.Approval)
		}
		b.m.MarkTurnEnded(sessionID, b.o.Now())
		if b.o.OnResult != nil {
			b.o.OnResult(sessionID, approved, res, err)
		}
	}()
	return nil
}
