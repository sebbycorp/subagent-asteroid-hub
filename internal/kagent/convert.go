// Package kagent adapts the kagent 1.0 gRPC API to the app's model and
// traffic interfaces.
package kagent

import (
	"strings"

	"go.ate.dev/substrate/pkg/proto/ateapipb"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/model"
)

var actorStates = map[ateapipb.ActorState]model.ActorState{
	ateapipb.ActorState_ACTOR_STATE_RESUMING:   model.StateResuming,
	ateapipb.ActorState_ACTOR_STATE_RUNNING:    model.StateRunning,
	ateapipb.ActorState_ACTOR_STATE_SUSPENDING: model.StateSuspending,
	ateapipb.ActorState_ACTOR_STATE_SUSPENDED:  model.StateSuspended,
	ateapipb.ActorState_ACTOR_STATE_CRASHED:    model.StateCrashed,
	ateapipb.ActorState_ACTOR_STATE_DELETING:   model.StateDeleting,
	ateapipb.ActorState_ACTOR_STATE_REVERTING:  model.StateReverting,
}

// ActorFromProto maps a Substrate actor. sessionAgents maps session ID to
// agent name; actors without an entry fall back to their template name.
func ActorFromProto(a *ateapipb.Actor, sessionAgents map[string]string) model.Actor {
	md, st := a.GetMetadata(), a.GetStatus()
	sessionID := strings.TrimPrefix(md.GetName(), "session-")
	agent, ok := sessionAgents[sessionID]
	if !ok {
		agent = AgentFromTemplate(a.GetActorTemplate().GetName())
	}
	state, ok := actorStates[st.GetState()]
	if !ok {
		state = model.StateUnknown
	}
	var gen int64
	for _, s := range st.GetSnapshots() {
		gen = max(gen, int64(s.GetGeneration()))
	}
	out := model.Actor{
		UID:         md.GetUid(),
		Name:        md.GetName(),
		SessionID:   sessionID,
		Agent:       agent,
		State:       state,
		Pool:        st.GetWorkerAssignment().GetWorkerPool(),
		Worker:      st.GetWorkerAssignment().GetWorkerPod(),
		SnapshotGen: gen,
	}
	if ts := md.GetUpdateTime(); ts != nil {
		out.UpdatedAt = ts.AsTime()
	}
	return out
}

// AgentFromTemplate strips the "-<12 hex>" revision suffix from an actor
// template name: "hello-substrate-a62adabe1f4f" → "hello-substrate".
func AgentFromTemplate(template string) string {
	i := strings.LastIndexByte(template, '-')
	if i < 0 || len(template)-i-1 != 12 {
		return template
	}
	for _, c := range template[i+1:] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return template
		}
	}
	return template[:i]
}
