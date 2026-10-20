// Package model is the app's view of an Agent Substrate cluster: pools,
// actors, and the lifecycle events derived by diffing two snapshots.
package model

import "time"

// ActorState is a Substrate actor state, lower-cased without the proto prefix.
type ActorState string

const (
	StateResuming   ActorState = "resuming"
	StateRunning    ActorState = "running"
	StateSuspending ActorState = "suspending"
	StateSuspended  ActorState = "suspended"
	StateCrashed    ActorState = "crashed"
	StateDeleting   ActorState = "deleting"
	StateReverting  ActorState = "reverting"
	StateUnknown    ActorState = "unknown"
)

// Actor is one Substrate actor (one agent session's sandbox).
type Actor struct {
	UID       string     `json:"uid"`
	Name      string     `json:"name"`
	SessionID string     `json:"sessionId"`
	Agent     string     `json:"agent"`
	State     ActorState `json:"state"`
	Pool      string     `json:"pool"`
	// Worker is the worker pod hosting the actor; empty when not assigned.
	Worker string `json:"worker"`
	// SnapshotGen is the highest snapshot generation; it grows on every checkpoint.
	SnapshotGen int64 `json:"snapshotGen"`
	// UpdatedAt is the actor's metadata update time (server clock).
	UpdatedAt time.Time `json:"updatedAt"`
}

// Pool is a WorkerPool and its worker pods.
type Pool struct {
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Workers   []string `json:"workers"`
}

// Snapshot is the cluster as seen by one poll. A zero TakenAt means "no
// previous snapshot".
type Snapshot struct {
	TakenAt time.Time        `json:"takenAt"`
	Pools   []Pool           `json:"pools"`
	Actors  map[string]Actor `json:"actors"`
}

// EventKind is a lifecycle event shown on screen.
type EventKind string

const (
	EventCreate  EventKind = "create"
	EventResume  EventKind = "resume"
	EventRunning EventKind = "running"
	EventSuspend EventKind = "suspend"
	EventFail    EventKind = "fail"
	EventGone    EventKind = "gone"
)

// Event is one real lifecycle change of one actor.
type Event struct {
	Seq       uint64    `json:"seq"`
	At        time.Time `json:"at"`
	Kind      EventKind `json:"kind"`
	ActorUID  string    `json:"actorUid"`
	Agent     string    `json:"agent"`
	Pool      string    `json:"pool"`
	Worker    string    `json:"worker"`
	RestoreMS *int64    `json:"restoreMs,omitempty"`
	SuspendMS *int64    `json:"suspendMs,omitempty"`
	Caption   string    `json:"caption"`
}
