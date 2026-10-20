package kagent

import (
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

// replyCollector assembles an agent's reply text and task ID from an A2A
// event stream.
type replyCollector struct {
	taskID string
	order  []a2atype.ArtifactID
	parts  map[a2atype.ArtifactID]string
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
	case *a2atype.TaskArtifactUpdateEvent:
		if e.Artifact != nil {
			rc.set(e.Artifact.ID, partsText(e.Artifact.Parts), e.Append)
		}
	case *a2atype.Task:
		rc.taskID = string(e.ID)
		for _, a := range e.Artifacts {
			rc.set(a.ID, partsText(a.Parts), false)
		}
	}
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
