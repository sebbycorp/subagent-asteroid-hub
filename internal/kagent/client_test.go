package kagent

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/traffic"
)

func TestSessionErrMapsNotFound(t *testing.T) {
	err := sessionErr("a2a client", status.Error(codes.NotFound, "session x not found"))
	if !errors.Is(err, traffic.ErrSessionGone) {
		t.Fatalf("NotFound not mapped: %v", err)
	}
	err = sessionErr("a2a client", status.Error(codes.Unavailable, "down"))
	if errors.Is(err, traffic.ErrSessionGone) || err == nil {
		t.Fatalf("Unavailable mapped wrongly: %v", err)
	}
	if sessionErr("x", nil) != nil {
		t.Fatal("nil error not nil")
	}
}
