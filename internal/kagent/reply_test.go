package kagent

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

func TestReplyCollectorStreamedArtifact(t *testing.T) {
	var rc replyCollector
	rc.apply(&a2atype.TaskStatusUpdateEvent{TaskID: "t1"})
	rc.apply(&a2atype.TaskArtifactUpdateEvent{TaskID: "t1", Artifact: &a2atype.Artifact{ID: "a1", Parts: a2atype.ContentParts{a2atype.NewTextPart("ORI")}}})
	rc.apply(&a2atype.TaskArtifactUpdateEvent{TaskID: "t1", Append: true, Artifact: &a2atype.Artifact{ID: "a1", Parts: a2atype.ContentParts{a2atype.NewTextPart("ON")}}})
	if rc.taskID != "t1" || rc.text() != "ORION" {
		t.Fatalf("got %q %q", rc.taskID, rc.text())
	}
}

func TestReplyCollectorFinalTask(t *testing.T) {
	var rc replyCollector
	rc.apply(&a2atype.Task{ID: "t2", Artifacts: []*a2atype.Artifact{{ID: "a", Parts: a2atype.ContentParts{a2atype.NewTextPart("OK")}}}})
	if rc.taskID != "t2" || rc.text() != "OK" {
		t.Fatalf("got %q %q", rc.taskID, rc.text())
	}
}

func TestReplyCollectorReplaceWithoutAppend(t *testing.T) {
	var rc replyCollector
	rc.apply(&a2atype.TaskArtifactUpdateEvent{TaskID: "t", Artifact: &a2atype.Artifact{ID: "a", Parts: a2atype.ContentParts{a2atype.NewTextPart("draft")}}})
	rc.apply(&a2atype.TaskArtifactUpdateEvent{TaskID: "t", Artifact: &a2atype.Artifact{ID: "a", Parts: a2atype.ContentParts{a2atype.NewTextPart("final")}}})
	if rc.text() != "final" {
		t.Fatalf("got %q", rc.text())
	}
}
