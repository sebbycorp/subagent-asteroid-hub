package kagent

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/api/client"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/model"
	"github.com/sebbycorp/subagent-asteroid-hub/internal/traffic"
)

// Options selects the kagent environment to talk to.
type Options struct {
	API       string // e.g. http://host.docker.internal:8083
	Gateway   string // A2A gateway; "" means API
	UserID    string
	Token     string // optional bearer token
	CAFile    string // optional CA for TLS endpoints
	Namespace string // where agents are listed and demo agents created
}

// slowEvery is how often pools, workers and the session→agent map refresh;
// actors are listed on every Snapshot call.
const slowEvery = 2 * time.Second

// Client is a Source for the hub and a Client for the traffic generator.
type Client struct {
	opts     Options
	api      *client.APIClientSet
	gw       *client.APIClientSet // a connection to the A2A gateway
	a2a      a2aclient.Transport
	system   apiv1.SystemServiceClient
	sessions apiv1.SessionServiceClient
	agents   apiv1.AgentServiceClient
	harness  apiv1.HarnessServiceClient

	checkpoints apiv1.CheckpointServiceClient
	modelSvc    apiv1.ModelServiceClient

	mu        sync.Mutex
	slowAt    time.Time
	pools     []model.Pool
	sessAgent map[string]string
	models    []string
}

type bearer string

func (b bearer) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + string(b)}, nil
}
func (bearer) RequireTransportSecurity() bool { return false }

// Dial connects to a kagent environment.
func Dial(o Options) (*Client, error) {
	if o.Gateway == "" {
		o.Gateway = o.API
	}
	if o.Namespace == "" {
		o.Namespace = "kagent"
	}
	var copts []client.ClientOption
	if o.UserID != "" {
		copts = append(copts, client.WithUserID(o.UserID))
	}
	if o.Token != "" {
		copts = append(copts, client.WithGRPCDialOptions(grpc.WithPerRPCCredentials(bearer(o.Token))))
	}
	if o.CAFile != "" {
		copts = append(copts, client.WithGRPCTLS(client.GRPCTLSConfig{CAFile: o.CAFile}))
	}
	api, err := client.NewAPI(o.API, copts...)
	if err != nil {
		return nil, fmt.Errorf("kagent api %s: %w", o.API, err)
	}
	gw, err := client.NewAPI(o.Gateway, copts...)
	if err != nil {
		api.Close()
		return nil, fmt.Errorf("kagent gateway %s: %w", o.Gateway, err)
	}
	conn, err := api.Conn()
	if err != nil {
		api.Close()
		gw.Close()
		return nil, err
	}
	gwConn, err := gw.Conn()
	if err != nil {
		api.Close()
		gw.Close()
		return nil, err
	}
	return &Client{
		opts: o, api: api, gw: gw,
		a2a:      a2agrpc.NewGRPCTransportFromClient(a2apb.NewA2AServiceClient(gwConn)),
		system:   apiv1.NewSystemServiceClient(conn),
		sessions: apiv1.NewSessionServiceClient(conn),
		agents:   apiv1.NewAgentServiceClient(conn),
		harness:  apiv1.NewHarnessServiceClient(conn),

		checkpoints: apiv1.NewCheckpointServiceClient(conn),
		modelSvc:    apiv1.NewModelServiceClient(conn),
	}, nil
}

// Close releases the connections.
func (c *Client) Close() error {
	return errors.Join(c.api.Close(), c.gw.Close())
}

// Env is a short name for the connected environment (the API host).
func (c *Client) Env() string {
	if u, err := url.Parse(c.opts.API); err == nil && u.Host != "" {
		return u.Host
	}
	return c.opts.API
}

func callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 8*time.Second)
}

// Snapshot reads the cluster: actors every call, the rest at most every 2 s.
func (c *Client) Snapshot(ctx context.Context) (model.Snapshot, error) {
	if err := c.refreshSlow(ctx); err != nil {
		return model.Snapshot{}, err
	}
	c.mu.Lock()
	pools, sessAgent, models := c.pools, c.sessAgent, c.models
	c.mu.Unlock()

	actors := map[string]model.Actor{}
	token := ""
	for {
		cctx, cancel := callCtx(ctx)
		resp, err := c.system.ListSubstrateActors(cctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{PageToken: token}})
		cancel()
		if err != nil {
			return model.Snapshot{}, fmt.Errorf("list actors: %w", err)
		}
		if e := resp.GetAteApiError(); e != "" {
			return model.Snapshot{}, fmt.Errorf("substrate: %s", e)
		}
		for _, a := range resp.GetActors() {
			if a.GetMetadata().GetAtespace() == "ate-golden" {
				continue
			}
			m := ActorFromProto(a, sessAgent)
			actors[m.UID] = m
		}
		if token = resp.GetPage().GetNextPageToken(); token == "" {
			break
		}
	}
	return model.Snapshot{TakenAt: time.Now(), Pools: pools, Actors: actors, Models: models}, nil
}

func (c *Client) refreshSlow(ctx context.Context) error {
	c.mu.Lock()
	fresh := time.Since(c.slowAt) < slowEvery
	c.mu.Unlock()
	if fresh {
		return nil
	}
	cctx, cancel := callCtx(ctx)
	defer cancel()
	sum, err := c.system.GetSubstrateSummary(cctx, &apiv1.GetSubstrateSummaryRequest{})
	if err != nil {
		return fmt.Errorf("substrate summary: %w", err)
	}
	if e := sum.GetAteApiError(); e != "" {
		return fmt.Errorf("substrate: %s", e)
	}
	workers := map[string][]string{}
	token := ""
	for {
		resp, err := c.system.ListSubstrateWorkers(cctx, &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{PageToken: token}})
		if err != nil {
			return fmt.Errorf("list workers: %w", err)
		}
		for _, w := range resp.GetWorkers() {
			workers[w.GetWorkerPool()] = append(workers[w.GetWorkerPool()], w.GetWorkerPod())
		}
		if token = resp.GetPage().GetNextPageToken(); token == "" {
			break
		}
	}
	var pools []model.Pool
	for _, p := range sum.GetWorkerPools() {
		ws := workers[p.GetRef().GetName()]
		sort.Strings(ws)
		pools = append(pools, model.Pool{Namespace: p.GetRef().GetNamespace(), Name: p.GetRef().GetName(), Workers: ws})
	}
	sort.Slice(pools, func(i, j int) bool { return pools[i].Name < pools[j].Name })

	sessions, err := c.listSessions(cctx)
	if err != nil {
		return err
	}
	sessAgent := map[string]string{}
	for _, s := range sessions {
		sessAgent[s.ID] = s.Agent.Name
	}
	var models []string
	if mcs, err := c.modelSvc.ListModelConfigs(cctx, &apiv1.ListModelConfigsRequest{}); err == nil {
		models = ModelsFromConfigs(mcs.GetModelConfigs())
	}
	c.mu.Lock()
	c.pools, c.sessAgent, c.models, c.slowAt = pools, sessAgent, models, time.Now()
	c.mu.Unlock()
	return nil
}

func (c *Client) listSessions(ctx context.Context) ([]traffic.SessionInfo, error) {
	var out []traffic.SessionInfo
	token := ""
	for {
		resp, err := c.sessions.ListSessions(ctx, &apiv1.ListSessionsRequest{AllCreators: true, Page: &apiv1.PageRequest{PageToken: token}})
		if err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		for _, s := range resp.GetSessions() {
			info := traffic.SessionInfo{
				ID: s.GetId(), Name: s.GetName(),
				Agent: traffic.AgentRef{Namespace: s.GetAgent().GetNamespace(), Name: s.GetAgent().GetName()},
			}
			if ts := s.GetCreatedAt(); ts != nil {
				info.CreatedAt = ts.AsTime()
			}
			out = append(out, info)
		}
		if token = resp.GetPage().GetNextPageToken(); token == "" {
			return out, nil
		}
	}
}

// ListSessions lists every user's sessions.
func (c *Client) ListSessions(ctx context.Context) ([]traffic.SessionInfo, error) {
	cctx, cancel := callCtx(ctx)
	defer cancel()
	return c.listSessions(cctx)
}

// ListReadyAgents lists agents in the namespace whose Ready condition is True.
func (c *Client) ListReadyAgents(ctx context.Context) ([]traffic.AgentRef, error) {
	cctx, cancel := callCtx(ctx)
	defer cancel()
	var out []traffic.AgentRef
	token := ""
	for {
		resp, err := c.agents.ListAgents(cctx, &apiv1.ListAgentsRequest{Namespace: c.opts.Namespace, Page: &apiv1.PageRequest{PageToken: token}})
		if err != nil {
			return nil, fmt.Errorf("list agents: %w", err)
		}
		for _, a := range resp.GetAgents() {
			if ready(a.GetResource().GetValue()) {
				out = append(out, traffic.AgentRef{Namespace: a.GetRef().GetNamespace(), Name: a.GetRef().GetName()})
			}
		}
		if token = resp.GetPage().GetNextPageToken(); token == "" {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func ready(obj *structpb.Struct) bool {
	status := obj.GetFields()["status"].GetStructValue()
	for _, cv := range status.GetFields()["conditions"].GetListValue().GetValues() {
		f := cv.GetStructValue().GetFields()
		if f["type"].GetStringValue() == "Ready" {
			return f["status"].GetStringValue() == "True"
		}
	}
	return false
}

// CreateSession starts a session on an agent and returns its ID.
func (c *Client) CreateSession(ctx context.Context, a traffic.AgentRef, name string) (string, error) {
	cctx, cancel := callCtx(ctx)
	defer cancel()
	resp, err := c.sessions.CreateSession(cctx, &apiv1.CreateSessionRequest{
		RequestId: uuid.NewString(), Name: name,
		Agent: &apiv1.ResourceReference{Namespace: a.Namespace, Name: a.Name},
	})
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return resp.GetSession().GetId(), nil
}

// DeleteSession deletes a session.
func (c *Client) DeleteSession(ctx context.Context, id string) error {
	cctx, cancel := callCtx(ctx)
	defer cancel()
	_, err := c.sessions.DeleteSession(cctx, &apiv1.DeleteSessionRequest{SessionId: id})
	return err
}

// Chat sends one message to a session over A2A and returns when the reply
// stream ends (the agent's turn is over), with the reply text and task ID.
func (c *Client) Chat(ctx context.Context, sessionID, text string) (traffic.ChatResult, error) {
	return c.send(ctx, sessionID, a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(text)))
}

// Decide answers a paused turn's tool approval and returns the resumed turn.
func (c *Client) Decide(ctx context.Context, sessionID string, ap *traffic.Approval, approved bool, reason string) (traffic.ChatResult, error) {
	text := "Approved"
	if !approved {
		text = "Denied"
	}
	msg := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(text))
	msg.TaskID, msg.ContextID = a2atype.TaskID(ap.TaskID), sessionID
	resp := kagenta2a.ToolApprovalResponse{Type: kagenta2a.HITLTypeToolApprovalResponse}
	for _, t := range ap.Tools {
		d := kagenta2a.ToolApproval{ID: t.ID, Approved: approved}
		if !approved {
			d.RejectionReason = reason
		}
		resp.Approvals = append(resp.Approvals, d)
	}
	if err := kagenta2a.AttachHITL(msg, resp); err != nil {
		return traffic.ChatResult{}, err
	}
	return c.send(ctx, sessionID, msg)
}

// send streams one message to a session, declaring kagent's human-in-the-loop
// extension so a turn can pause for approval (and be answered).
func (c *Client) send(ctx context.Context, sessionID string, msg *a2atype.Message) (traffic.ChatResult, error) {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	// A2A routes by tenant (the agent) and continues the session by context.
	sess, err := c.sessions.GetSession(cctx, &apiv1.GetSessionRequest{SessionId: sessionID})
	if err != nil {
		return traffic.ChatResult{}, sessionErr("a2a session", err)
	}
	agent := sess.GetSession().GetAgent()
	if msg.ContextID == "" {
		msg.ContextID = sessionID
	}
	// Service params become the request's gRPC metadata (the transport
	// replaces any set on the context), so the HITL extension goes here.
	params := a2aclient.ServiceParams{strings.ToLower(a2atype.SvcParamExtensions): {kagenta2a.HITLExtensionURI}}
	if c.opts.UserID != "" {
		params["x-user-id"] = []string{c.opts.UserID}
	}
	var rc replyCollector
	result := func() traffic.ChatResult {
		return traffic.ChatResult{Reply: rc.text(), TaskID: rc.taskID, Approval: rc.approval()}
	}
	req := &a2atype.SendMessageRequest{Tenant: agent.GetNamespace() + "/" + agent.GetName(), Message: msg}
	for ev, err := range c.a2a.SendStreamingMessage(cctx, params, req) {
		if err != nil {
			return result(), sessionErr("a2a stream", err)
		}
		if ev != nil {
			rc.apply(ev)
		}
	}
	return result(), nil
}

// Checkpoint saves a named point in a session's history (at the given turn)
// and waits until it is ready to fork from. It returns the checkpoint ID.
func (c *Client) Checkpoint(ctx context.Context, sessionID, headTaskID string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// A checkpoint is built on the turn's suspend snapshot, so until Substrate
	// has written it the API answers FailedPrecondition: retry until then.
	req := &apiv1.CreateCheckpointRequest{SessionId: sessionID, RequestId: uuid.NewString(), ExpectedHeadTaskId: headTaskID}
	var resp *apiv1.CreateCheckpointResponse
	var err error
	for {
		resp, err = c.checkpoints.CreateCheckpoint(cctx, req)
		if status.Code(err) != codes.FailedPrecondition {
			break
		}
		select {
		case <-cctx.Done():
			return "", fmt.Errorf("create checkpoint: %w", err)
		case <-time.After(250 * time.Millisecond):
		}
	}
	if err != nil {
		return "", fmt.Errorf("create checkpoint: %w", err)
	}
	ck := resp.GetCheckpoint()
	for ck.GetState() == apiv1.CheckpointState_CHECKPOINT_STATE_CREATING {
		select {
		case <-cctx.Done():
			return "", fmt.Errorf("checkpoint %s not ready: %w", ck.GetId(), cctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
		list, err := c.checkpoints.ListCheckpoints(cctx, &apiv1.ListCheckpointsRequest{SessionId: sessionID})
		if err != nil {
			return "", fmt.Errorf("list checkpoints: %w", err)
		}
		for _, x := range list.GetCheckpoints() {
			if x.GetId() == ck.GetId() {
				ck = x
			}
		}
	}
	if ck.GetState() != apiv1.CheckpointState_CHECKPOINT_STATE_READY {
		return "", fmt.Errorf("checkpoint %s is %s", ck.GetId(), ck.GetState())
	}
	return ck.GetId(), nil
}

// Fork starts a new session from a checkpoint and names it.
func (c *Client) Fork(ctx context.Context, checkpointID, name string) (string, error) {
	cctx, cancel := callCtx(ctx)
	defer cancel()
	resp, err := c.checkpoints.ForkSession(cctx, &apiv1.ForkSessionRequest{CheckpointId: checkpointID, RequestId: uuid.NewString()})
	if err != nil {
		return "", fmt.Errorf("fork session: %w", err)
	}
	id := resp.GetSession().GetId()
	if _, err := c.sessions.UpdateSessionName(cctx, &apiv1.UpdateSessionNameRequest{SessionId: id, Name: name}); err != nil {
		return id, fmt.Errorf("name forked session: %w", err)
	}
	return id, nil
}

// sessionErr wraps a chat error, marking a missing session as
// traffic.ErrSessionGone so the generator stops using it.
func sessionErr(op string, err error) error {
	if err == nil {
		return nil
	}
	if status.Code(err) == codes.NotFound {
		return fmt.Errorf("%s: %w: %w", op, traffic.ErrSessionGone, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// ListHarnesses lists Harness names in the namespace, ready ones first.
func (c *Client) ListHarnesses(ctx context.Context) ([]string, error) {
	cctx, cancel := callCtx(ctx)
	defer cancel()
	resp, err := c.harness.ListHarnesses(cctx, &apiv1.ListHarnessesRequest{Namespace: c.opts.Namespace})
	if err != nil {
		return nil, fmt.Errorf("list harnesses: %w", err)
	}
	hs := resp.GetHarnesses()
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].GetReady() && !hs[j].GetReady() })
	var out []string
	for _, h := range hs {
		out = append(out, h.GetRef().GetName())
	}
	return out, nil
}

// CreateAgent creates a small declarative Agent.
func (c *Client) CreateAgent(ctx context.Context, a traffic.AgentRef, harness, modelConfig, systemPrompt string) error {
	value, err := structpb.NewStruct(map[string]any{
		"apiVersion": "api.kagent.dev/v1alpha3",
		"kind":       "Agent",
		"metadata":   map[string]any{"name": a.Name, "namespace": a.Namespace},
		"spec": map[string]any{
			"harnessRef": map[string]any{"name": harness},
			"template": map[string]any{"inline": map[string]any{
				"description":  "Substrate Asteroids demo agent",
				"modelConfig":  map[string]any{"name": modelConfig},
				"systemPrompt": systemPrompt,
			}},
		},
	})
	if err != nil {
		return err
	}
	cctx, cancel := callCtx(ctx)
	defer cancel()
	_, err = c.agents.CreateAgent(cctx, &apiv1.CreateAgentRequest{
		Ref:      &apiv1.ResourceReference{Namespace: a.Namespace, Name: a.Name},
		Resource: &apiv1.StructuredObject{ApiVersion: "api.kagent.dev/v1alpha3", Kind: "Agent", Value: value},
	})
	if err != nil {
		return fmt.Errorf("create agent %s: %w", a.Name, err)
	}
	return nil
}
