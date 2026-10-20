//go:build live

package kagent

import (
	"context"
	"fmt"
	"os"
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
	if err := c.Chat(ctx, id, "Reply with just the word pong."); err != nil {
		t.Fatal(err)
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
