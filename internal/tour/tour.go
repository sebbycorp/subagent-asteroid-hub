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
	Now      func() time.Time
}

var ErrBusy = errors.New("the tour is still running this chapter")

const (
	codeWord = "ORION"
	teach    = "Remember: the code word is ORION. Reply only OK."
	ask      = "What was the code word? Reply with just the word."
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
	t.session, t.lastTask, t.fork = "", "", ""
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
			s.Busy, s.Doing = false, ""
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

func (t *Tour) chat(ctx context.Context, id, text string) (traffic.ChatResult, error) {
	t.m.MarkSent(id, t.o.Now())
	res, err := t.client().Chat(ctx, id, text)
	t.m.MarkTurnEnded(id, t.o.Now())
	return res, err
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
	a := agents[0]
	t.doing("CREATING A SESSION ON " + strings.ToUpper(a.Name) + "…")
	id, err := c.CreateSession(ctx, a, fmt.Sprintf("asteroids-tour-%d", t.o.Now().UnixMilli()%1_000_000))
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.session = id
	t.mu.Unlock()
	t.update(func(s *Status) { s.Focus = []string{id}; s.Doing = `SENDING: "REMEMBER: THE CODE WORD IS ORION."` })
	res, err := t.chat(ctx, id, teach)
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
	res, err := t.chat(ctx, id, ask)
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
	res, err := t.chat(ctx, fork, ask)
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

func (t *Tour) runDensity(ctx context.Context) error {
	t.doing("SENDING A WAVE…")
	n, err := t.w.Wave(ctx)
	if errors.Is(err, traffic.ErrWaveRunning) {
		return errors.New("a wave is already running; watch it")
	}
	if err != nil {
		return err
	}
	t.update(func(s *Status) {
		s.Result = fmt.Sprintf("%d CHATS SENT. WATCH THE WORKERS STAY BUSY ONLY WHILE AGENTS THINK.", n)
	})
	return nil
}
