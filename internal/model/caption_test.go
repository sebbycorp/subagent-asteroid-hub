package model

import "testing"

func ms(n int64) *int64 { return &n }

func TestCaptionSuspendWithLatency(t *testing.T) {
	got := Caption(Event{Kind: EventSuspend, Agent: "hello-substrate", SuspendMS: ms(45)})
	want := "TURN DONE. SUBSTRATE FROZE HELLO-SUBSTRATE TO STORAGE 45 MS AFTER THE TURN. WORKER FREED."
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestCaptionSuspendWithoutLatency(t *testing.T) {
	got := Caption(Event{Kind: EventSuspend, Agent: "hello-substrate"})
	want := "TURN DONE. SUBSTRATE FROZE HELLO-SUBSTRATE TO STORAGE. WORKER FREED."
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestCaptionRunningWithRestore(t *testing.T) {
	got := Caption(Event{Kind: EventRunning, Agent: "coder", Worker: "kagent-default-abc", RestoreMS: ms(130)})
	want := "CODER RUNNING ON KAGENT-DEFAULT-ABC. RESTORED IN 130 MS."
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestCaptionOtherKinds(t *testing.T) {
	cases := map[EventKind]string{
		EventCreate: "NEW SESSION FOR CODER. SUBSTRATE STARTS A FRESH SANDBOX ON POOL-A.",
		EventResume: "RESUME. CODER'S SNAPSHOT COMES BACK FROM STORAGE ONTO A WORKER.",
		EventFail:   "CODER CRASHED. SUBSTRATE REPORTS THE SANDBOX FAILED.",
		EventGone:   "CODER SESSION DELETED.",
	}
	for k, want := range cases {
		if got := Caption(Event{Kind: k, Agent: "coder", Pool: "pool-a"}); got != want {
			t.Errorf("%s: got %q want %q", k, got, want)
		}
	}
}
