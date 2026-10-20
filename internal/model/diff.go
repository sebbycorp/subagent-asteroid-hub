package model

import "sort"

func active(s ActorState) bool    { return s == StateResuming || s == StateRunning }
func suspended(s ActorState) bool { return s == StateSuspended || s == StateSuspending }

// Diff turns two successive snapshots into lifecycle events. The first poll
// (zero prev) only seeds state. Seq and Caption are left for the caller.
func Diff(prev, cur Snapshot) []Event {
	evs := []Event{}
	if prev.TakenAt.IsZero() {
		return evs
	}
	type group struct {
		at  int64
		uid string
		evs []Event
	}
	var groups []group
	for uid, c := range cur.Actors {
		p, existed := prev.Actors[uid]
		var kinds []EventKind
		switch {
		case !existed && c.State == StateRunning:
			kinds = []EventKind{EventCreate, EventRunning}
		case !existed && c.State == StateResuming:
			kinds = []EventKind{EventCreate}
		case !existed && suspended(c.State):
			kinds = []EventKind{EventCreate, EventSuspend}
		case !existed:
		case suspended(p.State) && c.State == StateResuming:
			kinds = []EventKind{EventResume}
		case suspended(p.State) && c.State == StateRunning:
			kinds = []EventKind{EventResume, EventRunning}
		case p.State == StateResuming && c.State == StateRunning:
			kinds = []EventKind{EventRunning}
		case active(p.State) && suspended(c.State):
			kinds = []EventKind{EventSuspend}
		case p.State == StateSuspended && c.State == StateSuspended && c.SnapshotGen > p.SnapshotGen:
			// A whole resume → run → suspend cycle happened between polls.
			kinds = []EventKind{EventResume, EventSuspend}
		case c.State == StateCrashed && p.State != StateCrashed:
			kinds = []EventKind{EventFail}
		}
		if len(kinds) == 0 {
			continue
		}
		g := group{at: c.UpdatedAt.UnixNano(), uid: uid}
		for _, k := range kinds {
			e := Event{At: c.UpdatedAt, Kind: k, ActorUID: uid, Agent: c.Agent, Pool: c.Pool, Worker: c.Worker}
			if k == EventSuspend && e.Worker == "" && existed {
				e.Worker = p.Worker
			}
			g.evs = append(g.evs, e)
		}
		groups = append(groups, g)
	}
	for uid, p := range prev.Actors {
		if _, ok := cur.Actors[uid]; ok {
			continue
		}
		at := cur.TakenAt
		groups = append(groups, group{at: at.UnixNano(), uid: uid, evs: []Event{{
			At: at, Kind: EventGone, ActorUID: uid, Agent: p.Agent, Pool: p.Pool, Worker: p.Worker,
		}}})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].at != groups[j].at {
			return groups[i].at < groups[j].at
		}
		return groups[i].uid < groups[j].uid
	})
	for _, g := range groups {
		evs = append(evs, g.evs...)
	}
	return evs
}
