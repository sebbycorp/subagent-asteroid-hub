package model

import (
	"fmt"
	"strings"
)

// InstantMS is the time-to-suspend below which the caption says "the
// instant the turn ended": below it the number is within clock noise.
const InstantMS = 10

// Caption is the plain-English, upper-case line shown for an event.
func Caption(e Event) string {
	a := strings.ToUpper(e.Agent)
	var s string
	switch e.Kind {
	case EventCreate:
		s = fmt.Sprintf("NEW SESSION FOR %s. SUBSTRATE STARTS A FRESH SANDBOX", a)
		if e.Pool != "" {
			s += " ON " + strings.ToUpper(e.Pool)
		}
		s += "."
	case EventResume:
		s = fmt.Sprintf("RESUME. %s'S SNAPSHOT COMES BACK FROM STORAGE ONTO A WORKER.", a)
	case EventRunning:
		s = fmt.Sprintf("%s RUNNING ON %s", a, strings.ToUpper(e.Worker))
		if e.RestoreMS != nil {
			s += fmt.Sprintf(". RESTORED IN %d MS", *e.RestoreMS)
		}
		s += "."
	case EventSuspend:
		s = fmt.Sprintf("TURN DONE. SUBSTRATE FROZE %s TO STORAGE", a)
		switch {
		case e.SuspendMS == nil:
		case *e.SuspendMS <= InstantMS:
			s += " THE INSTANT THE TURN ENDED"
		default:
			s += fmt.Sprintf(" %d MS AFTER THE TURN", *e.SuspendMS)
		}
		s += ". WORKER FREED."
	case EventFail:
		s = fmt.Sprintf("%s CRASHED. SUBSTRATE REPORTS THE SANDBOX FAILED.", a)
	case EventGone:
		s = fmt.Sprintf("%s SESSION DELETED.", a)
	}
	return s
}
