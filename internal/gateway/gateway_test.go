package gateway

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var now = time.Date(2026, 10, 21, 1, 30, 0, 0, time.UTC)

func fixture(t *testing.T) *collogs.ExportLogsServiceRequest {
	t.Helper()
	b, err := os.ReadFile("testdata/agentgateway-logs.json")
	if err != nil {
		t.Fatal(err)
	}
	var req collogs.ExportLogsServiceRequest
	if err := protojson.Unmarshal(b, &req); err != nil {
		t.Fatal(err)
	}
	return &req
}

// The fixture is one real agent turn: an ingress health check, MCP
// handshakes, an LLM call to qwen, a tool call, and the stream's GET.
// Only the requests that tell the story become events.
func TestFromLogsKeepsTheStory(t *testing.T) {
	evs := FromLogs(fixture(t), now)
	var kinds []string
	for _, e := range evs {
		kinds = append(kinds, e.Kind)
	}
	want := map[string]int{"llm": 0, "tool": 0}
	for _, k := range kinds {
		want[k]++
	}
	if want["llm"] < 1 || want["tool"] != 1 {
		t.Fatalf("kinds %v", kinds)
	}
	found := false
	for _, e := range evs {
		if e.Kind == "llm" && e.Tokens == 608 && e.DurationMS == 1775 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the 571+37 token, 1775ms qwen call is missing: %+v", evs)
	}
	for _, e := range evs {
		if e.Kind == "tool" {
			if e.Agent != "asteroids-demo-poet" || e.Tool != "k8s_get_resources" || e.Target != "kagent-tools" || e.Direction != "egress" || e.Status != 200 || e.DurationMS != 54 {
				t.Fatalf("tool event %+v", e)
			}
		}
		if e.Kind == "llm" && (e.Agent != "asteroids-demo-poet" || e.Target != "qwen" || e.Model != "qwen3:8b") {
			t.Fatalf("llm event %+v", e)
		}
		if !e.At.Equal(now) {
			t.Fatalf("At %v, want receive time (records carry no timestamp)", e.At)
		}
	}
}

func TestFromLogsIngressChat(t *testing.T) {
	req := &collogs.ExportLogsServiceRequest{}
	b := []byte(`{"resourceLogs":[{"scopeLogs":[{"logRecords":[
	 {"attributes":[{"key":"route","value":{"stringValue":"agentgateway-system/kagent-a2a"}},
	  {"key":"url.path","value":{"stringValue":"/lf.a2a.v1.A2AService/SendStreamingMessage"}},
	  {"key":"http.response.status_code","value":{"stringValue":"200"}},
	  {"key":"duration","value":{"stringValue":"4273ms"}}]},
	 {"attributes":[{"key":"route","value":{"stringValue":"agentgateway-system/kagent-a2a"}},
	  {"key":"url.path","value":{"stringValue":"/kagent.api.v1.SessionService/GetSession"}}]}]}]}]}`)
	if err := protojson.Unmarshal(b, req); err != nil {
		t.Fatal(err)
	}
	evs := FromLogs(req, now)
	if len(evs) != 1 || evs[0].Kind != "chat" || evs[0].Direction != "ingress" || evs[0].DurationMS != 4273 {
		t.Fatalf("events %+v", evs)
	}
}

func TestHandlerDecodesProtobuf(t *testing.T) {
	body, _ := proto.Marshal(fixture(t))
	var got []Event
	h := Handler(func(evs []Event) { got = append(got, evs...) }, func() time.Time { return now })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/logs", bytes.NewReader(body)))
	if rec.Code != http.StatusOK || len(got) == 0 {
		t.Fatalf("code %d events %d", rec.Code, len(got))
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/logs", bytes.NewReader([]byte("not protobuf"))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("garbage accepted: %d", rec.Code)
	}
}
