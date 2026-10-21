// Package gateway turns agentgateway's OTLP access logs into the requests the
// show draws: each agent's calls out of its sandbox (to the model, to tools)
// and chats coming in, all of which cross agentgateway.
package gateway

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

// Event is one request agentgateway handled.
type Event struct {
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"`      // llm | tool | chat
	Direction  string    `json:"direction"` // egress (out of a sandbox) | ingress (into an agent)
	Agent      string    `json:"agent,omitempty"`
	Target     string    `json:"target"` // the route's backend, e.g. qwen, kagent-tools, kagent-a2a
	Model      string    `json:"model,omitempty"`
	Tool       string    `json:"tool,omitempty"`
	Status     int       `json:"status"`
	DurationMS int64     `json:"durationMs"`
	Tokens     int64     `json:"tokens,omitempty"`
}

// AgentAttribute is the access-log attribute carrying the calling agent
// (added by the EnterpriseAgentgatewayPolicy from the x-asteroids-agent header).
const AgentAttribute = "asteroids.agent"

func attrs(kvs []*commonpb.KeyValue) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		v := kv.GetValue()
		switch {
		case v.GetStringValue() != "":
			m[kv.GetKey()] = v.GetStringValue()
		case v.GetIntValue() != 0:
			m[kv.GetKey()] = strconv.FormatInt(v.GetIntValue(), 10)
		}
	}
	return m
}

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// FromLogs extracts the requests worth showing. MCP plumbing (initialize,
// tools/list, notifications, stream GETs) and kagent's pre-chat lookups are
// dropped; model calls, tool calls and incoming chats are kept. Records carry
// no timestamp, so At is the receive time.
func FromLogs(req *collogs.ExportLogsServiceRequest, now time.Time) []Event {
	var out []Event
	for _, rl := range req.GetResourceLogs() {
		for _, sl := range rl.GetScopeLogs() {
			for _, lr := range sl.GetLogRecords() {
				a := attrs(lr.GetAttributes())
				e := Event{
					At:         now,
					Agent:      a[AgentAttribute],
					Target:     a["route"][strings.LastIndexByte(a["route"], '/')+1:],
					Status:     int(atoi(a["http.response.status_code"])),
					DurationMS: atoi(strings.TrimSuffix(a["duration"], "ms")),
				}
				switch {
				case a["protocol"] == "llm":
					e.Kind, e.Direction = "llm", "egress"
					e.Model = a["gen_ai.request.model"]
					e.Tokens = atoi(a["gen_ai.usage.input_tokens"]) + atoi(a["gen_ai.usage.output_tokens"])
				case a["mcp.method.name"] == "tools/call":
					e.Kind, e.Direction = "tool", "egress"
					e.Tool = a["gen_ai.tool.name"]
					if t := a["mcp.target"]; t != "" {
						e.Target = t
					}
				case strings.Contains(a["url.path"], "A2AService/SendStreamingMessage"),
					strings.Contains(a["url.path"], "A2AService/SendMessage"):
					e.Kind, e.Direction = "chat", "ingress"
				default:
					continue
				}
				out = append(out, e)
			}
		}
	}
	return out
}

// Handler is an OTLP/HTTP logs receiver (protobuf) that hands each batch's
// events to sink.
func Handler(sink func([]Event), now func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req collogs.ExportLogsServiceRequest
		if err := proto.Unmarshal(b, &req); err != nil {
			http.Error(w, "expected an OTLP ExportLogsServiceRequest (protobuf)", http.StatusBadRequest)
			return
		}
		if evs := FromLogs(&req, now()); len(evs) > 0 {
			sink(evs)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		resp, _ := proto.Marshal(&collogs.ExportLogsServiceResponse{})
		w.Write(resp)
	})
}
