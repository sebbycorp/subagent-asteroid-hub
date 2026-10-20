package kagent

import (
	"testing"
	"time"

	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/model"
)

var t0 = time.Date(2026, 10, 20, 21, 57, 6, 997_000_000, time.UTC)

func protoActor(state ateapipb.ActorState, wa *ateapipb.WorkerAssignment, gens ...int32) *ateapipb.Actor {
	var snaps []*ateapipb.Snapshot
	for _, g := range gens {
		snaps = append(snaps, &ateapipb.Snapshot{Generation: g})
	}
	return &ateapipb.Actor{
		Metadata: &ateapipb.ResourceMetadata{
			Atespace: "kagent", Name: "session-01a15ae0-26a9-7812-904e-7534c48a7052",
			Uid: "cbcec519", UpdateTime: timestamppb.New(t0),
		},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: "kagent", Name: "hello-substrate-a62adabe1f4f"},
		Status:        &ateapipb.ActorStatus{State: state, WorkerAssignment: wa, Snapshots: snaps},
	}
}

func TestActorFromProtoRunning(t *testing.T) {
	wa := &ateapipb.WorkerAssignment{WorkerPool: "kagent-default", WorkerPod: "kagent-default-67d8cc74bd-jxg28"}
	a := ActorFromProto(protoActor(ateapipb.ActorState_ACTOR_STATE_RUNNING, wa, 1),
		map[string]string{"01a15ae0-26a9-7812-904e-7534c48a7052": "from-session-map"})
	want := model.Actor{
		UID: "cbcec519", Name: "session-01a15ae0-26a9-7812-904e-7534c48a7052",
		SessionID: "01a15ae0-26a9-7812-904e-7534c48a7052", Agent: "from-session-map",
		State: model.StateRunning, Pool: "kagent-default", Worker: "kagent-default-67d8cc74bd-jxg28",
		SnapshotGen: 1, UpdatedAt: t0,
	}
	if a != want {
		t.Fatalf("got  %+v\nwant %+v", a, want)
	}
}

func TestActorFromProtoSuspendedNoWorker(t *testing.T) {
	a := ActorFromProto(protoActor(ateapipb.ActorState_ACTOR_STATE_SUSPENDED, nil, 2), nil)
	if a.State != model.StateSuspended || a.Worker != "" || a.Pool != "" {
		t.Fatalf("got %+v", a)
	}
	if a.Agent != "hello-substrate" {
		t.Fatalf("agent fallback = %q, want hello-substrate", a.Agent)
	}
}

func TestActorFromProtoUnspecifiedIsUnknown(t *testing.T) {
	a := ActorFromProto(protoActor(ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED, nil), nil)
	if a.State != model.StateUnknown {
		t.Fatalf("state = %q", a.State)
	}
}

func TestActorFromProtoSnapshotGenIsMax(t *testing.T) {
	a := ActorFromProto(protoActor(ateapipb.ActorState_ACTOR_STATE_SUSPENDED, nil, 2, 5, 3), nil)
	if a.SnapshotGen != 5 {
		t.Fatalf("gen = %d, want 5", a.SnapshotGen)
	}
}

func TestAgentFromTemplate(t *testing.T) {
	cases := map[string]string{
		"hello-substrate-a62adabe1f4f": "hello-substrate",
		"plain":                        "plain",
		"agent-notahexsuffix":          "agent-notahexsuffix",
		"":                             "",
	}
	for in, want := range cases {
		if got := AgentFromTemplate(in); got != want {
			t.Errorf("AgentFromTemplate(%q) = %q, want %q", in, got, want)
		}
	}
}
