// Package tour runs a guided, five-chapter lesson on how Agent Substrate
// works. Each chapter does something real on the cluster (create, ask, fork,
// wave) while a card floating over the live show explains it.
package tour

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/traffic"
)

// Client is the slice of the kagent API the tour needs.
type Client interface {
	ListReadyAgents(ctx context.Context) ([]traffic.AgentRef, error)
	CreateSession(ctx context.Context, a traffic.AgentRef, name string) (string, error)
	Chat(ctx context.Context, sessionID, text string) (traffic.ChatResult, error)
	Checkpoint(ctx context.Context, sessionID, headTaskID string) (string, error)
	Fork(ctx context.Context, checkpointID, name string) (string, error)
}

// Waver sends a wave of chats (the traffic generator).
type Waver interface {
	Wave(ctx context.Context) (int, error)
}

// Status is what the floating card shows.
type Status struct {
	Active  bool     `json:"active"`
	Chapter int      `json:"chapter"`
	Total   int      `json:"total"`
	Title   string   `json:"title"`
	Body    []string `json:"body"`
	Doing   string   `json:"doing,omitempty"`
	Result  string   `json:"result,omitempty"`
	Error   string   `json:"error,omitempty"`
	Busy    bool     `json:"busy"`
	Focus   []string `json:"focus"` // session IDs to highlight; [parent, fork] in chapter 4
	Kind    string   `json:"kind"`  // create | suspend | resume | fork | density
}

// Options hooks the tour into the app.
type Options struct {
	OnChange func(Status)
	OnActive func(active bool) // e.g. pause auto waves during the tour
	// OnApproval receives a turn that paused for a human (the HITL chapter).
	OnApproval func(sessionID, agent string, ap *traffic.Approval)
	Now        func() time.Time
}

var ErrBusy = errors.New("the tour is still running this chapter")

const (
	codeWord = "ORION"
	teach    = "Remember: the code word is ORION. Reply only OK."
	ask      = "What was the code word? Reply with just the word."

	gatewayQuestion = "How many pods are running in the kagent namespace? Use your Kubernetes tools, then answer in one short sentence."
	hitlQuestion    = "How many pods are running in the kagent namespace?"

	// HITLAgent needs a person's approval before any tool call
	// (deploy/agentgateway/40-agents.yaml).
	HITLAgent = "asteroids-demo-ops"
)

type chapter struct {
	kind  string
	title string
	body  []string
	run   func(t *Tour, ctx context.Context) error
}

var chapters = []chapter{
	{"create", "A NEW AGENT SESSION", []string{
		"WE START A FRESH SESSION ON AN AGENT.",
		"SUBSTRATE CLONES THE AGENT'S GOLDEN SNAPSHOT INTO A NEW",
		"GVISOR SANDBOX AND PLACES IT ON A WORKER FROM THE POOL.",
		"WE TEACH IT A SECRET: THE CODE WORD IS ORION.",
	}, (*Tour).runCreate},
	{"suspend", "SUSPEND: FROZEN TO STORAGE", []string{
		"THE TURN IS OVER, SO SUBSTRATE SUSPENDED THE SANDBOX",
		"THE MOMENT THE REPLY ENDED. ITS MEMORY AND DISK ARE NOW",
		"A SNAPSHOT IN S3: THE CAPSULE IN THE SNAPSHOT STORE.",
		"THE WORKER IS FREE. THIS SESSION COSTS 0 CPU, 0 MEMORY.",
	}, nil},
	{"resume", "RESUME: SAME MIND, NEW MOMENT", []string{
		"WE ASK THE FROZEN SESSION A QUESTION.",
		"SUBSTRATE PULLS ITS SNAPSHOT FROM S3 AND RESTORES IT",
		"ONTO A FREE WORKER IN ABOUT A TENTH OF A SECOND.",
		"IF ITS STATE SURVIVED, IT KNOWS THE CODE WORD.",
	}, (*Tour).runResume},
	{"fork", "FORK: ONE MIND, TWO FUTURES", []string{
		"WE CHECKPOINT THE SESSION (A NAMED POINT BUILT ON ITS",
		"SUSPEND SNAPSHOT) AND FORK A BRAND-NEW SESSION FROM IT,",
		"LIKE THE ASTEROID SPLITTING IN TWO. NOBODY TOLD THE",
		"FORK THE CODE WORD. DOES IT KNOW?",
	}, (*Tour).runFork},
	{"gateway", "AGENTGATEWAY: THE AGENT HOLDS NO KEYS", []string{
		"WE ASK THE AGENT ABOUT THE CLUSTER. TO ANSWER, IT CALLS ITS",
		"MODEL AND A KUBERNETES TOOL. BOTH CALLS LEAVE THE SANDBOX",
		"THROUGH AGENTGATEWAY: SUBSTRATE ONLY LETS IT REACH THE",
		"GATEWAY, AND THE GATEWAY HOLDS THE CREDENTIALS. WATCH THE SHIELD.",
	}, (*Tour).runGateway},
	{"hitl", "HUMAN IN THE LOOP: IT WAITS FOR YOU", []string{
		"WE ASK OPS, AN AGENT THAT NEEDS A PERSON'S OK BEFORE ANY TOOL CALL.",
		"IT ASKS TO RUN A TOOL AND PAUSES. SUBSTRATE FREEZES IT WHILE IT",
		"WAITS: 0 CPU, WORKER FREED, FOR SECONDS OR DAYS. ITS PILOT HOLDS",
		"UP THE REQUEST. PRESS Y TO APPROVE OR X TO REJECT.",
	}, (*Tour).runHITL},
	{"density", "DENSITY: MANY AGENTS, FEW MACHINES", []string{
		"NOW A WAVE OF CHATS ACROSS EVERY AGENT.",
		"A SESSION HOLDS A WORKER ONLY WHILE IT IS THINKING;",
		"THE REST WAIT IN S3 AT ZERO COST. THAT IS HOW SUBSTRATE",
		"RUNS THOUSANDS OF AGENTS ON A HANDFUL OF WORKERS.",
	}, (*Tour).runDensity},
}

// Tour is the guided lesson. Safe for concurrent use.
type Tour struct {
	o  Options
	m  traffic.Marker
	w  Waver
	wg sync.WaitGroup

	mu       sync.Mutex
	c        Client
	st       Status
	session  string // the tour's main session
	lastTask string // its latest turn, the checkpoint head
	fork     string
	hitl     string // the HITL chapter's session, while it waits for a person
}

// New creates an inactive tour.
func New(c Client, w Waver, m traffic.Marker, o Options) *Tour {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Tour{o: o, c: c, w: w, m: m, st: Status{Total: len(chapters), Focus: []string{}}}
}

// SetClient switches environment and ends any running tour.
func (t *Tour) SetClient(c Client) {
	t.Stop()
	t.mu.Lock()
	t.c = c
	t.mu.Unlock()
}

// Status returns the card's current content.
func (t *Tour) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.st
	s.Body = append([]string(nil), s.Body...)
	s.Focus = append([]string{}, s.Focus...)
	return s
}

// Start begins the tour at chapter 1 (restarting it if it was running).
func (t *Tour) Start(ctx context.Context) error {
	t.mu.Lock()
	if t.st.Busy {
		t.mu.Unlock()
		return ErrBusy
	}
	wasActive := t.st.Active
	t.session, t.lastTask, t.fork, t.hitl = "", "", "", ""
	t.mu.Unlock()
	if !wasActive && t.o.OnActive != nil {
		t.o.OnActive(true)
	}
	return t.enter(ctx, 0)
}

// Next moves to the next chapter; after the last one the tour ends.
func (t *Tour) Next(ctx context.Context) error {
	t.mu.Lock()
	if t.st.Busy {
		t.mu.Unlock()
		return ErrBusy
	}
	active, n := t.st.Active, t.st.Chapter
	t.mu.Unlock()
	if !active {
		return t.Start(ctx)
	}
	if n >= len(chapters) {
		t.Stop()
		return nil
	}
	return t.enter(ctx, n)
}

// Stop ends the tour. Running chapter actions finish in the background.
func (t *Tour) Stop() {
	t.mu.Lock()
	if !t.st.Active {
		t.mu.Unlock()
		return
	}
	t.st = Status{Total: len(chapters), Focus: []string{}}
	t.mu.Unlock()
	t.changed()
	if t.o.OnActive != nil {
		t.o.OnActive(false)
	}
}

func (t *Tour) changed() {
	if t.o.OnChange != nil {
		t.o.OnChange(t.Status())
	}
}

func (t *Tour) update(f func(s *Status)) {
	t.mu.Lock()
	f(&t.st)
	t.mu.Unlock()
	t.changed()
}

// enter shows chapter i (0-based) and runs its action in the background.
func (t *Tour) enter(ctx context.Context, i int) error {
	ch := chapters[i]
	t.mu.Lock()
	focus := []string{}
	if t.session != "" {
		focus = append(focus, t.session)
	}
	if ch.kind == "fork" && t.fork != "" {
		focus = append(focus, t.fork)
	}
	t.st = Status{
		Active: true, Chapter: i + 1, Total: len(chapters), Kind: ch.kind,
		Title: ch.title, Body: ch.body, Focus: focus, Busy: ch.run != nil,
	}
	t.mu.Unlock()
	t.changed()
	if ch.run == nil {
		return nil
	}
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		err := ch.run(t, context.WithoutCancel(ctx))
		t.update(func(s *Status) {
			if s.Chapter != i+1 || !s.Active {
				return // moved on or stopped meanwhile
			}
			s.Busy = false
			if !(s.Kind == "hitl" && t.hitlWaiting()) {
				s.Doing = ""
			}
			if err != nil {
				s.Error = strings.ToUpper(err.Error())
			}
		})
	}()
	return nil
}

func (t *Tour) doing(msg string) {
	t.update(func(s *Status) { s.Doing = msg })
}

func (t *Tour) client() Client {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.c
}

func (t *Tour) chat(ctx context.Context, id, text, agent string) (traffic.ChatResult, error) {
	t.m.MarkSent(id, t.o.Now())
	res, err := t.client().Chat(ctx, id, text)
	if err == nil && res.Approval != nil && t.o.OnApproval != nil {
		t.o.OnApproval(id, agent, res.Approval) // before the turn ends
	}
	t.m.MarkTurnEnded(id, t.o.Now())
	return res, err
}

// hitlWaiting reports whether the HITL chapter is waiting for a person.
// Callers must not hold t.mu… except enter's update, which runs under it.
func (t *Tour) hitlWaiting() bool { return t.hitl != "" }

func (t *Tour) runHITL(ctx context.Context) error {
	c := t.client()
	agents, err := c.ListReadyAgents(ctx)
	if err != nil {
		return err
	}
	var ops *traffic.AgentRef
	for i := range agents {
		if agents[i].Name == HITLAgent {
			ops = &agents[i]
		}
	}
	if ops == nil {
		return errors.New("no human-in-the-loop agent: apply " + HITLAgent + " (deploy/agentgateway)")
	}
	t.doing("ASKING OPS: \"HOW MANY PODS RUN IN THE KAGENT NAMESPACE?\"")
	id, err := c.CreateSession(ctx, *ops, fmt.Sprintf("asteroids-tour-hitl-%d", t.o.Now().UnixMilli()%1_000_000))
	if err != nil {
		return err
	}
	t.update(func(s *Status) { s.Focus = []string{id} })
	res, err := t.chat(ctx, id, hitlQuestion, ops.Name)
	if err != nil {
		return err
	}
	if res.Approval == nil {
		t.update(func(s *Status) { s.Result = "IT ANSWERED WITHOUT A TOOL: " + quote(res.Reply) })
		return nil
	}
	tool := strings.ToUpper(res.Approval.Tools[0].Name)
	t.update(func(s *Status) {
		t.hitl = id
		s.Doing = "WAITING FOR YOU · PRESS Y TO APPROVE, X TO REJECT " + tool
	})
	return nil
}

// Resolved reports a person's decision on a paused turn and how it ended.
func (t *Tour) Resolved(sessionID string, approved bool, res traffic.ChatResult, err error) {
	t.update(func(s *Status) {
		if !s.Active || s.Kind != "hitl" || t.hitl != sessionID {
			return
		}
		t.hitl, s.Doing = "", ""
		switch {
		case err != nil:
			s.Error = strings.ToUpper(err.Error())
		case approved:
			s.Result = "APPROVED. IT ANSWERED: " + quote(res.Reply)
		default:
			s.Result = "REJECTED. IT REPLIED: " + quote(res.Reply)
		}
	})
}

func quote(reply string) string {
	reply = strings.TrimSpace(reply)
	if len(reply) > 60 {
		reply = reply[:60] + "…"
	}
	return `"` + strings.ToUpper(reply) + `"`
}

func (t *Tour) runCreate(ctx context.Context) error {
	c := t.client()
	agents, err := c.ListReadyAgents(ctx)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		return errors.New("no ready agents. press d to create demo agents, then n")
	}
	var a traffic.AgentRef
	for _, ag := range agents {
		if ag.Name != HITLAgent { // ops always waits for a person: not for chapter 1
			a = ag
			break
		}
	}
	if a.Name == "" {
		return errors.New("no ready agents. press d to create demo agents, then n")
	}
	t.doing("CREATING A SESSION ON " + strings.ToUpper(a.Name) + "…")
	id, err := c.CreateSession(ctx, a, fmt.Sprintf("asteroids-tour-%d", t.o.Now().UnixMilli()%1_000_000))
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.session = id
	t.mu.Unlock()
	t.update(func(s *Status) { s.Focus = []string{id}; s.Doing = `SENDING: "REMEMBER: THE CODE WORD IS ORION."` })
	res, err := t.chat(ctx, id, teach, a.Name)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.lastTask = res.TaskID
	t.mu.Unlock()
	t.update(func(s *Status) { s.Result = "AGENT REPLIED: " + quote(res.Reply) })
	return nil
}

func (t *Tour) needSession() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.session == "" {
		return "", errors.New("no tour session yet. press n to restart the tour")
	}
	return t.session, nil
}

func (t *Tour) runResume(ctx context.Context) error {
	id, err := t.needSession()
	if err != nil {
		return err
	}
	t.doing(`ASKING: "WHAT WAS THE CODE WORD?"`)
	res, err := t.chat(ctx, id, ask, "")
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.lastTask = res.TaskID
	t.mu.Unlock()
	result := "IT ANSWERED: " + quote(res.Reply)
	if strings.Contains(strings.ToUpper(res.Reply), codeWord) {
		result += ". STATE SURVIVED THE FREEZE."
	}
	t.update(func(s *Status) { s.Result = result })
	return nil
}

func (t *Tour) runFork(ctx context.Context) error {
	id, err := t.needSession()
	if err != nil {
		return err
	}
	t.mu.Lock()
	head := t.lastTask
	t.mu.Unlock()
	c := t.client()
	t.doing("CHECKPOINTING THE SESSION…")
	ck, err := c.Checkpoint(ctx, id, head)
	if err != nil {
		return err
	}
	t.doing("FORKING A NEW SESSION FROM THE CHECKPOINT…")
	fork, err := c.Fork(ctx, ck, fmt.Sprintf("asteroids-tour-fork-%d", t.o.Now().UnixMilli()%1_000_000))
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.fork = fork
	t.mu.Unlock()
	t.update(func(s *Status) { s.Focus = []string{id, fork}; s.Doing = `ASKING THE FORK: "WHAT WAS THE CODE WORD?"` })
	res, err := t.chat(ctx, fork, ask, "")
	if err != nil {
		return err
	}
	result := "THE FORK ANSWERED: " + quote(res.Reply)
	if strings.Contains(strings.ToUpper(res.Reply), codeWord) {
		result += ". IT INHERITED THE MEMORY."
	}
	t.update(func(s *Status) { s.Result = result })
	return nil
}

func (t *Tour) runGateway(ctx context.Context) error {
	id, err := t.needSession()
	if err != nil {
		return err
	}
	t.doing(`ASKING: "HOW MANY PODS RUN IN THE KAGENT NAMESPACE?"`)
	res, err := t.chat(ctx, id, gatewayQuestion, "")
	if err != nil {
		return err
	}
	t.update(func(s *Status) { s.Result = "IT ANSWERED: " + quote(res.Reply) })
	return nil
}

func (t *Tour) runDensity(ctx context.Context) error {
	t.doing("SENDING A WAVE…")
	n, err := t.w.Wave(ctx)
	if errors.Is(err, traffic.ErrWaveRunning) {
		return errors.New("a wave is already running; watch it")
	}
	if err != nil {
		return err
	}
	chats := "CHATS"
	if n == 1 {
		chats = "CHAT"
	}
	t.update(func(s *Status) {
		s.Result = fmt.Sprintf("%d %s SENT. WATCH THE WORKERS STAY BUSY ONLY WHILE AGENTS THINK.", n, chats)
	})
	return nil
}
