package kagent

import (
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/traffic"
)

// replyCollector assembles an agent's reply text and task ID from an A2A
// event stream.
type replyCollector struct {
	taskID string
	order  []a2atype.ArtifactID
	parts  map[a2atype.ArtifactID]string
	state  a2atype.TaskState
	status *a2atype.Message // the latest status message (carries HITL requests)
}

func partsText(ps a2atype.ContentParts) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.Text())
	}
	return b.String()
}

func (rc *replyCollector) set(id a2atype.ArtifactID, text string, appendText bool) {
	if rc.parts == nil {
		rc.parts = map[a2atype.ArtifactID]string{}
	}
	if _, ok := rc.parts[id]; !ok {
		rc.order = append(rc.order, id)
	}
	if appendText {
		text = rc.parts[id] + text
	}
	rc.parts[id] = text
}

func (rc *replyCollector) apply(ev a2atype.Event) {
	if id := ev.TaskInfo().TaskID; id != "" {
		rc.taskID = string(id)
	}
	switch e := ev.(type) {
	case *a2atype.TaskStatusUpdateEvent:
		rc.state, rc.status = e.Status.State, e.Status.Message
	case *a2atype.TaskArtifactUpdateEvent:
		if e.Artifact != nil {
			rc.set(e.Artifact.ID, partsText(e.Artifact.Parts), e.Append)
		}
	case *a2atype.Task:
		rc.taskID = string(e.ID)
		rc.state, rc.status = e.Status.State, e.Status.Message
		for _, a := range e.Artifacts {
			rc.set(a.ID, partsText(a.Parts), false)
		}
	}
}

// approval is the pending human decision, if the turn paused for one.
func (rc *replyCollector) approval() *traffic.Approval {
	if rc.state != a2atype.TaskStateInputRequired || rc.status == nil {
		return nil
	}
	req, err := kagenta2a.ParseToolApprovalRequest(rc.status)
	if err != nil || req == nil || len(req.Tools) == 0 {
		return nil
	}
	ap := &traffic.Approval{TaskID: rc.taskID}
	for _, t := range req.Tools {
		ap.Tools = append(ap.Tools, traffic.ApprovalTool{ID: t.ID, Name: t.Name, Args: t.Args})
	}
	return ap
}

func (rc *replyCollector) text() string {
	var out []string
	for _, id := range rc.order {
		if t := strings.TrimSpace(rc.parts[id]); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, " ")
}
