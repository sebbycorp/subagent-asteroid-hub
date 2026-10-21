//go:build live

package kagent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/model"
	"github.com/sebbycorp/subagent-asteroid-hub/internal/traffic"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	api := os.Getenv("KAGENT_API")
	if api == "" {
		api = "http://localhost:8083"
	}
	c, err := Dial(Options{API: api, UserID: "admin@kagent.dev", Namespace: "kagent"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestLiveSnapshot(t *testing.T) {
	s, err := liveClient(t).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Pools) < 1 {
		t.Fatalf("no pools: %+v", s)
	}
	t.Logf("pools=%+v actors=%d", s.Pools, len(s.Actors))
}

func TestLiveChatCycle(t *testing.T) {
	ctx := context.Background()
	c := liveClient(t)
	id, err := c.CreateSession(ctx, traffic.AgentRef{Namespace: "kagent", Name: "hello-substrate"}, fmt.Sprintf("asteroids-it-%d", time.Now().Unix()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.DeleteSession(ctx, id) })
	res, err := c.Chat(ctx, id, "Reply with just the word pong.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(res.Reply), "pong") || res.TaskID == "" {
		t.Fatalf("chat result %+v", res)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		s, err := c.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range s.Actors {
			if a.SessionID == id && a.State == model.StateSuspended && a.SnapshotGen >= 1 {
				t.Logf("actor %+v", a)
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("actor never reached suspended")
}

func TestLiveCheckpointFork(t *testing.T) {
	ctx := context.Background()
	c := liveClient(t)
	id, err := c.CreateSession(ctx, traffic.AgentRef{Namespace: "kagent", Name: "hello-substrate"}, fmt.Sprintf("asteroids-it-%d", time.Now().Unix()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.DeleteSession(ctx, id) })
	res, err := c.Chat(ctx, id, "Remember: the code word is ORION. Reply only OK.")
	if err != nil {
		t.Fatal(err)
	}
	ck, err := c.Checkpoint(ctx, id, res.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	fork, err := c.Fork(ctx, ck, "asteroids-it-fork")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.DeleteSession(ctx, fork) })
	ans, err := c.Chat(ctx, fork, "What was the code word? Reply with just the word.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(ans.Reply), "ORION") {
		t.Fatalf("fork forgot: %+v", ans)
	}
}

func TestLiveHumanApproval(t *testing.T) {
	ctx := context.Background()
	c := liveClient(t)
	id, err := c.CreateSession(ctx, traffic.AgentRef{Namespace: "kagent", Name: "asteroids-demo-ops"}, fmt.Sprintf("asteroids-it-%d", time.Now().Unix()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.DeleteSession(ctx, id) })
	res, err := c.Chat(ctx, id, "How many pods are running in the kagent namespace?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Approval == nil || len(res.Approval.Tools) == 0 || res.Approval.TaskID == "" {
		t.Fatalf("expected a pending tool approval, got %+v", res)
	}
	t.Logf("waiting on a human for %s %v", res.Approval.Tools[0].Name, res.Approval.Tools[0].Args)
	// the actor is suspended while it waits
	deadline := time.Now().Add(30 * time.Second)
	for {
		s, err := c.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var st model.ActorState
		for _, a := range s.Actors {
			if a.SessionID == id {
				st = a.State
			}
		}
		if st == model.StateSuspended {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("actor not suspended while waiting (state %q)", st)
		}
		time.Sleep(250 * time.Millisecond)
	}
	done, err := c.Decide(ctx, id, res.Approval, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if done.Approval != nil || !strings.ContainsAny(done.Reply, "0123456789") {
		t.Fatalf("after approval: %+v", done)
	}
	t.Logf("answer after approval: %q", done.Reply)
}

// k8s-operator (deploy/agentgateway/45-k8s-operator.yaml): reads run freely,
// changes wait for a human. The change is rejected, so nothing is touched.
func TestLiveOperatorApprovesOnlyChanges(t *testing.T) {
	ctx := context.Background()
	c := liveClient(t)
	id, err := c.CreateSession(ctx, traffic.AgentRef{Namespace: "kagent", Name: "k8s-operator"}, fmt.Sprintf("asteroids-it-%d", time.Now().Unix()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.DeleteSession(ctx, id) })
	read, err := c.Chat(ctx, id, "How many deployments are in the kagent namespace?")
	if err != nil {
		t.Fatal(err)
	}
	if read.Approval != nil || !strings.ContainsAny(read.Reply, "0123456789") {
		t.Fatalf("a read should answer without approval, got %+v", read)
	}
	t.Logf("read: %q", read.Reply)
	write, err := c.Chat(ctx, id, "Scale the deployment web in namespace playground to 2 replicas.")
	if err != nil {
		t.Fatal(err)
	}
	if write.Approval == nil || write.Approval.Tools[0].Name != "k8s_scale" {
		t.Fatalf("a change should wait for approval of k8s_scale, got %+v", write)
	}
	t.Logf("waiting on a human for %s %v", write.Approval.Tools[0].Name, write.Approval.Tools[0].Args)
	done, err := c.Decide(ctx, id, write.Approval, false, "a human rejected this tool call")
	if err != nil {
		t.Fatal(err)
	}
	if done.Approval != nil {
		t.Fatalf("after rejection it asked again: %+v", done)
	}
	t.Logf("after rejection: %q", done.Reply)
}
