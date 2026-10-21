package kagent

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
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

func TestReplyCollectorPendingApproval(t *testing.T) {
	msg := a2atype.NewMessage(a2atype.MessageRoleAgent)
	if err := kagenta2a.AttachHITL(msg, kagenta2a.ToolApprovalRequest{
		Type:  kagenta2a.HITLTypeToolApprovalRequest,
		Tools: []kagenta2a.HITLTool{{ID: "call-1", Name: "k8s_get_resources", Args: map[string]any{"resource_type": "pods"}}},
	}); err != nil {
		t.Fatal(err)
	}
	var rc replyCollector
	rc.apply(&a2atype.TaskStatusUpdateEvent{TaskID: "t9", Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired, Message: msg}})
	ap := rc.approval()
	if ap == nil || ap.TaskID != "t9" || len(ap.Tools) != 1 || ap.Tools[0].ID != "call-1" || ap.Tools[0].Name != "k8s_get_resources" || ap.Tools[0].Args["resource_type"] != "pods" {
		t.Fatalf("approval %+v", ap)
	}
	var done replyCollector
	done.apply(&a2atype.TaskStatusUpdateEvent{TaskID: "t9", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}})
	if done.approval() != nil {
		t.Fatal("completed task reported as waiting")
	}
}
