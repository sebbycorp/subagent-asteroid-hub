# Substrate Asteroids Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Docker-packaged, self-running vector-Asteroids show that visualises a live kagent / Agent Substrate cluster and explains actor create, resume and suspend in plain English.

**Architecture:** A Go backend polls the kagent 1.0 gRPC API (actors every 250 ms, pools/workers/sessions every 2 s), diffs successive snapshots into lifecycle events, and streams state and events to the browser over SSE. A traffic generator sends real A2A chats in waves so there is always something to show. A single canvas page draws the vector stage and plays events in order with a short presentation delay.

**Tech Stack:** Go 1.27; `github.com/kagent-dev/kagent/go` at commit `1f7dd92d3002` (tag v1.0.0-alpha16) for API clients and generated types; A2A via `github.com/a2aproject/a2a-go/v2`; vanilla JS + `<canvas>` (no build step), logic tested with `node --test`; Docker multi-stage build.

**Spec:** `docs/superpowers/specs/2026-10-20-substrate-asteroids-design.md`

## Global Constraints

- Real only: every event sent to the browser comes from a diff of two real snapshots. No simulated or replay mode, and the frontend never invents events.
- The ship never calls `SuspendSession` / `ResumeSession`. Suspends are Substrate's; captions credit Substrate.
- The app only creates, chats on or deletes sessions and agents it created: session names start `asteroids-`, demo agent names start `asteroids-demo-`.
- No kubeconfig and no Kubernetes API; only the kagent API (gRPC, default `http://host.docker.internal:8083`), user id header `x-user-id`, default `admin@kagent.dev`.
- Look: black background, white 1.5 px strokes, uppercase monospace text with letter spacing, a fixed 1280×720 stage scaled to fit the window.
- Sessions are listed with `all_creators: true`.
- Go module needs `replace go.ate.dev/substrate => github.com/kagent-dev/substrate v0.6.0-alpha3` (verified: without it `go mod tidy` fails).
- HTTP listens on `:7790`.

## Review Focus

1. **A whole resume → run → suspend cycle happens between two actor polls** (a fast turn). Expected: show a resume and a suspend, not nothing. Detected by the snapshot generation increasing while the state reads suspended both times. Test: `TestDiffMissedCycle` (Task 1).
2. **The kagent API goes away mid-show** (port-forward dies). Expected: a visible "LINK LOST" state, then automatic recovery with no burst of fake events on reconnect. Tests: `TestHubDisconnectThenReconnectNoReplay` (Task 3) and the `connected:false` banner (Task 6).
3. **The app restarts with sessions from a previous run still in the cluster.** Expected: it adopts its own `asteroids-` sessions into the cap and cleanup, and never touches others. Test: `TestGeneratorAdoptsOwnSessionsOnly` (Task 4).
4. **A burst of events, such as a wave of 4 agents, arrives at once.** Expected: each event is still shown and captioned readably, in order, none dropped. Test: `schedule spaces bursts` (Task 6).
5. **A cluster with no agents, or agents that aren't Ready.** Expected: waves do nothing and the screen says why ("NO READY AGENTS. PRESS D FOR DEMO AGENTS"), with no errors thrown. Test: `TestWaveNoReadyAgents` (Task 4).

---

## File Structure

```
go.mod / go.sum
cmd/asteroids/main.go            wiring: config → kagent client → hub, generator, server
internal/model/model.go          Snapshot, Actor, Pool, Event types (JSON for the browser)
internal/model/diff.go           Diff(prev, cur) → []Event   (pure, the core logic)
internal/model/caption.go        Caption(Event) string        (plain-English copy)
internal/kagent/convert.go       proto → model mapping (pure)
internal/kagent/client.go        gRPC Source + traffic.Client implementation
internal/hub/hub.go              poll loop, latency marks, stats, SSE fan-out
internal/traffic/traffic.go      waves, own-session tracking, caps, demo agents
internal/config/config.go        env → Config
internal/server/server.go        HTTP routes, SSE, embedded web assets
web/embed.go                     //go:embed of the static files
web/index.html, web/style.css
web/show.mjs                     pure presentation logic (queue/pacing, asteroid shapes, ship aim)
web/render.mjs                   canvas vector drawing
web/main.mjs                     SSE wiring, keyboard, connect overlay
web/show.test.mjs                node --test
Dockerfile, .dockerignore, Makefile, README.md, .gitignore
```

---

### Task 1: Module, model types and the snapshot diff

**Files:**
- Create: `go.mod`, `.gitignore`, `internal/model/model.go`, `internal/model/diff.go`, `internal/model/caption.go`
- Test: `internal/model/diff_test.go`, `internal/model/caption_test.go`

**Interfaces:**
- Produces (package `model`):
  ```go
  type ActorState string // "resuming" "running" "suspending" "suspended" "crashed" "deleting" "reverting" "unknown"
  type Actor struct {
      UID string `json:"uid"`; Name string `json:"name"`; SessionID string `json:"sessionId"`
      Agent string `json:"agent"`; State ActorState `json:"state"`
      Pool string `json:"pool"`; Worker string `json:"worker"`           // worker pod; "" when not assigned
      SnapshotGen int64 `json:"snapshotGen"`                          // max status.snapshots[].generation
      UpdatedAt time.Time `json:"updatedAt"`                           // actor metadata updateTime (server clock)
  }
  type Pool struct { Namespace string `json:"namespace"`; Name string `json:"name"`; Workers []string `json:"workers"` }
  type Snapshot struct { TakenAt time.Time `json:"takenAt"`; Pools []Pool `json:"pools"`; Actors map[string]Actor `json:"actors"` } // keyed by UID
  type EventKind string // EventCreate "create", EventResume "resume", EventRunning "running", EventSuspend "suspend", EventFail "fail", EventGone "gone"
  type Event struct {
      Seq uint64 `json:"seq"`; At time.Time `json:"at"`; Kind EventKind `json:"kind"`
      ActorUID string `json:"actorUid"`; Agent string `json:"agent"`; Pool string `json:"pool"`; Worker string `json:"worker"`
      RestoreMS *int64 `json:"restoreMs,omitempty"`; SuspendMS *int64 `json:"suspendMs,omitempty"`
      Caption string `json:"caption"`
  }
  func Diff(prev, cur Snapshot) []Event   // Seq and Caption left zero; the hub fills them
  func Caption(e Event) string
  ```

- [ ] **Step 1: Create `go.mod`**

```bash
go mod init github.com/sebbycorp/subagent-asteroid-hub
go mod edit -go=1.27 -replace go.ate.dev/substrate=github.com/kagent-dev/substrate@v0.6.0-alpha3
printf '.superpowers/\n/bin/\n' > .gitignore
```

- [ ] **Step 2: Write failing tests in `internal/model/diff_test.go`**

The rules the tests pin (`prev` → `cur`, per actor UID):

| prev | cur | events |
|------|-----|--------|
| zero `Snapshot` (no `TakenAt`) | anything | none (first poll only seeds state) |
| absent | resuming/running | `create` |
| absent | suspended | `create`, then `suspend` |
| suspended/suspending | resuming | `resume` |
| suspended/suspending | running | `resume`, then `running` |
| resuming | running | `running` |
| resuming/running | suspending/suspended | `suspend` |
| suspended | suspended, `SnapshotGen` increased | `resume`, then `suspend` (missed cycle) |
| any | crashed (not crashed before) | `fail` |
| present | absent | `gone` |
| same state, same gen | | none |

Each event's `At` is `cur.Actors[uid].UpdatedAt` (the server's time, so latencies are true), and `Agent`, `Pool` and `Worker` are copied from cur, or from prev for `gone` and for a `suspend` whose cur `Worker` is empty. Output order: by `At`, then by `UID`; events for one actor keep table order.

Tests: `TestDiffFirstPollSeedsOnly`, `TestDiffCreate`, `TestDiffCreateAlreadySuspended`, `TestDiffResumeToRunning`, `TestDiffResumingThenRunning`, `TestDiffSuspend` (asserts `Worker` comes from prev), `TestDiffMissedCycle` (prev suspended gen 2 → cur suspended gen 3 ⇒ `[resume, suspend]`), `TestDiffCrash`, `TestDiffGone`, `TestDiffNoChange`, `TestDiffOrdering` (two actors, later `UpdatedAt` second).

- [ ] **Step 3: Run `go test ./internal/model/` and confirm it fails to compile (types missing)**

- [ ] **Step 4: Implement `model.go` and `Diff` in `diff.go`**

- [ ] **Step 5: Write `caption_test.go`, then implement `Caption`**

The copy (uppercase; `{A}` agent, `{P}` pool, `{W}` worker; the ms clause only when the pointer is set):

- create: `NEW SESSION FOR {A}. SUBSTRATE STARTS A FRESH SANDBOX ON {P}.`
- resume: `RESUME. {A}'S SNAPSHOT COMES BACK FROM STORAGE ONTO A WORKER.`
- running: `{A} RUNNING ON {W}[. RESTORED IN {n} MS].`
- suspend: `TURN DONE. SUBSTRATE FROZE {A} TO STORAGE[ {n} MS AFTER THE TURN]. WORKER FREED.`
- fail: `{A} CRASHED. SUBSTRATE REPORTS THE SANDBOX FAILED.`
- gone: `{A} SESSION DELETED.`

Tests assert the exact string for suspend with and without `SuspendMS`, and for running with `RestoreMS`.

- [ ] **Step 6: Run `go test ./internal/model/` and confirm it passes**

- [ ] **Step 7: Commit** — `git add go.mod .gitignore internal/model && git commit -m "feat(model): snapshot diff into actor lifecycle events"`

---

### Task 2: kagent API adapter

**Files:**
- Create: `internal/kagent/convert.go`, `internal/kagent/client.go`
- Test: `internal/kagent/convert_test.go`, `internal/kagent/live_test.go` (build tag `live`)

**Interfaces:**
- Consumes: `model.Snapshot`, `model.Actor`, `model.Pool` (Task 1).
- Produces (package `kagent`):
  ```go
  type Options struct { API, Gateway, UserID, Token, CAFile, Namespace string } // Gateway "" ⇒ API
  type Client struct { /* … */ }
  func Dial(o Options) (*Client, error)
  func (c *Client) Close() error
  func (c *Client) Env() string                                   // host part of API, e.g. "host.docker.internal:8083"
  func (c *Client) Snapshot(ctx context.Context) (model.Snapshot, error)   // satisfies hub.Source
  // satisfies traffic.Client (Task 4):
  func (c *Client) ListReadyAgents(ctx context.Context) ([]traffic.AgentRef, error)
  func (c *Client) ListSessions(ctx context.Context) ([]traffic.SessionInfo, error)
  func (c *Client) CreateSession(ctx context.Context, a traffic.AgentRef, name string) (string, error)
  func (c *Client) DeleteSession(ctx context.Context, id string) error
  func (c *Client) Chat(ctx context.Context, sessionID, text string) error   // returns when the A2A stream ends
  func (c *Client) ListHarnesses(ctx context.Context) ([]string, error)
  func (c *Client) CreateAgent(ctx context.Context, a traffic.AgentRef, harness, modelConfig, systemPrompt string) error
  // pure mapping:
  func ActorFromProto(a *ateapipb.Actor, sessionAgents map[string]string) model.Actor
  func AgentFromTemplate(template string) string   // "hello-substrate-a62adabe1f4f" → "hello-substrate"
  ```
- To avoid an import cycle, `traffic.AgentRef` and `traffic.SessionInfo` live in `traffic` (Task 4 defines them); write Task 4's type declarations first, or define them here and move them. Implementer's choice, as long as the names match.

Facts the implementer needs (all verified against alpha16):
- `client.NewAPI(url, client.WithUserID(id))` → `cs.Conn()` gives a `grpc.ClientConnInterface` for `apiv1.NewSystemServiceClient`, `apiv1.NewSessionServiceClient`, `apiv1.NewAgentServiceClient`, `apiv1.NewHarnessServiceClient` (imports `github.com/kagent-dev/kagent/go/api/client`, `apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"`, `go.ate.dev/substrate/pkg/proto/ateapipb`).
- Chat: `client.NewGateway(url, client.WithUserID(id))` → `gw.A2A.ForSession(ctx, id)` → `SendStreamingMessage(ctx, &a2atype.SendMessageRequest{Message: …})` and drain the iterator. Use `go/core/cli/internal/commands/session/invoke.go` (`newInvokeRequest`, `consumeA2AStream`) at that commit as the reference.
- Token: `client.WithGRPCDialOptions(grpc.WithPerRPCCredentials(bearer))` where `bearer` returns `authorization: Bearer <token>` and `RequireTransportSecurity() == false`. CA: `client.WithGRPCTLS(...)`.
- Actor → model: `State` maps `ACTOR_STATE_RESUMING` to `"resuming"` and so on; `UNSPECIFIED` maps to `"unknown"`. `Worker` = `status.workerAssignment.workerPod`, `Pool` = `status.workerAssignment.workerPool`. `SessionID` = name without the `session-` prefix. `Agent` = `sessionAgents[SessionID]`, else `AgentFromTemplate(actorTemplate.name)`. `SnapshotGen` = max of `status.snapshots[].generation`. `UpdatedAt` = `metadata.updateTime`.
- Pools: `GetSubstrateSummary().WorkerPools[].Ref` (namespace/name). Workers: `ListSubstrateWorkers().Workers[]` grouped by `GetWorkerPool()`, using `GetWorkerPod()` as the worker name. Skip actors in atespace `ate-golden`.
- `Snapshot` refreshes pools, workers and the session→agent map at most every 2 s (cached) and calls `ListSubstrateActors` every time. Page through `ListSubstrateActors` / `ListSessions` until the page token is empty.
- `ListReadyAgents`: `ListAgents{namespace}` and keep agents whose `resource.value.status.conditions[type=Ready].status == "True"`.
- `CreateAgent`: a `StructuredObject{apiVersion:"api.kagent.dev/v1alpha3", kind:"Agent", value:<Struct>}` with the same shape as the spec's `hello-substrate` (`harnessRef`, `template.inline.{description, modelConfig.name, systemPrompt}`).

- [ ] **Step 1: Write failing `convert_test.go`**: `TestActorFromProtoRunning` (worker, pool and agent come from the session map), `TestActorFromProtoSuspendedNoWorker`, `TestActorFromProtoSnapshotGenIsMax`, `TestAgentFromTemplate` (strips a `-<12 hex>` suffix only; `"plain"` → `"plain"`).
- [ ] **Step 2: Run `go test ./internal/kagent/` and confirm it fails.**
- [ ] **Step 3: Implement `convert.go`, then `client.go`.** Run `go mod tidy`.
- [ ] **Step 4: Run `go test ./internal/kagent/` and confirm it passes.**
- [ ] **Step 5: Write `live_test.go`** (`//go:build live`): `TestLiveSnapshot` dials `KAGENT_API` (default `http://localhost:8083`) and asserts at least 1 pool and no error. `TestLiveChatCycle` creates session `asteroids-it-<unix>` on `hello-substrate`, `Chat`s "Reply with just the word pong.", polls `Snapshot` until that actor reads `suspended` with `SnapshotGen ≥ 1` (timeout 60 s), then deletes the session.
- [ ] **Step 6: Run `go test -tags live ./internal/kagent/ -run Live -v`** with the port-forward up. Expected: PASS.
- [ ] **Step 7: Commit** — `feat(kagent): gRPC adapter for actors, pools, sessions, chat`

---

### Task 3: Hub (polling, latencies, stats, fan-out)

**Files:**
- Create: `internal/hub/hub.go`
- Test: `internal/hub/hub_test.go`

**Interfaces:**
- Consumes: `model.Diff`, `model.Caption`, `model.Snapshot`.
- Produces (package `hub`):
  ```go
  type Source interface { Snapshot(ctx context.Context) (model.Snapshot, error) }
  type Stats struct { Creates, Resumes, Suspends, Fails int64; LastRestoreMS, LastSuspendMS *int64 } // json camelCase
  type State struct {
      Env string `json:"env"`; Connected bool `json:"connected"`; Error string `json:"error,omitempty"`
      Snapshot model.Snapshot `json:"snapshot"`; Stats Stats `json:"stats"`
      Waves WaveStatus `json:"waves"`
  }
  type WaveStatus struct { Auto bool `json:"auto"`; InFlight int `json:"inFlight"`; Note string `json:"note,omitempty"` }
  type Hub struct { /* … */ }
  func New(src Source, env string) *Hub
  func (h *Hub) SetSource(src Source, env string)     // reconnect: resets prev snapshot to zero so nothing replays
  func (h *Hub) Tick(ctx context.Context)              // one poll + diff + broadcast
  func (h *Hub) Run(ctx context.Context, every time.Duration)   // Tick loop; main passes 250ms
  func (h *Hub) MarkSent(sessionID string, at time.Time)
  func (h *Hub) MarkTurnEnded(sessionID string, at time.Time)
  func (h *Hub) SetWaves(w WaveStatus)
  func (h *Hub) State() State
  func (h *Hub) Subscribe() (<-chan []byte, func())   // JSON messages; buffered 256; a slow subscriber is dropped, never blocks
  ```
- Message shapes: `{"type":"state","state":State}`, sent on every Tick and on subscribe, and `{"type":"event","event":model.Event}`, one per event, after the state for that tick.

Rules:
- A Tick error sets `Connected=false` and `Error`, keeps the last snapshot and emits no events. The next successful Tick after an error diffs against a **zero** snapshot, so the reconnect only seeds state.
- Latency: on a `running` event whose session has a `MarkSent` time, `RestoreMS = event.At − sent`. On a `suspend` event with a `MarkTurnEnded` time, `SuspendMS = event.At − turnEnded`. Clear each mark once used. Ignore negative values.
- `Seq` increases monotonically per hub. `Caption` is set with `model.Caption` after the latencies are filled.

- [ ] **Step 1: Write failing tests with a fake `Source`** (a slice of snapshots or errors returned in order): `TestHubFirstTickNoEvents`, `TestHubEmitsCaptionedEventsWithSeq`, `TestHubRestoreAndSuspendLatency` (sent at T, running `UpdatedAt` T+130ms ⇒ `RestoreMS=130`; turn ended at T2, suspended at T2+45ms ⇒ `SuspendMS=45` and caption contains `45 MS AFTER THE TURN`), `TestHubDisconnectThenReconnectNoReplay`, `TestHubSlowSubscriberDropped`, `TestHubStatsCount`.
- [ ] **Step 2: Run `go test ./internal/hub/` and confirm it fails.**
- [ ] **Step 3: Implement `hub.go`.**
- [ ] **Step 4: Run `go test -race ./internal/hub/` and confirm it passes.**
- [ ] **Step 5: Commit** — `feat(hub): poll, diff, latency and SSE fan-out`

---

### Task 4: Traffic generator

**Files:**
- Create: `internal/traffic/traffic.go`
- Test: `internal/traffic/traffic_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks. The `kagent.Client` satisfies `Client`, and `*hub.Hub` satisfies `Marker`.
- Produces (package `traffic`):
  ```go
  type AgentRef struct { Namespace, Name string }
  type SessionInfo struct { ID, Name string; Agent AgentRef; CreatedAt time.Time }
  type Client interface {
      ListReadyAgents(ctx context.Context) ([]AgentRef, error)
      ListSessions(ctx context.Context) ([]SessionInfo, error)
      CreateSession(ctx context.Context, a AgentRef, name string) (string, error)
      DeleteSession(ctx context.Context, id string) error
      Chat(ctx context.Context, sessionID, text string) error
      ListHarnesses(ctx context.Context) ([]string, error)
      CreateAgent(ctx context.Context, a AgentRef, harness, modelConfig, systemPrompt string) error
  }
  type Marker interface { MarkSent(id string, at time.Time); MarkTurnEnded(id string, at time.Time) }
  type Options struct { Namespace string; AgentsPerWave, MaxSessions int; Interval time.Duration; Now func() time.Time }
  // defaults: AgentsPerWave 4, MaxSessions 12, Interval 20s
  var ErrWaveRunning = errors.New("a wave is still running")
  var ErrNoAgents = errors.New("no ready agents")
  type Generator struct { /* … */ }
  func New(c Client, m Marker, o Options) *Generator
  func (g *Generator) SetClient(c Client)                 // reconnect; forgets adopted sessions and re-adopts on next wave
  func (g *Generator) Wave(ctx context.Context) (int, error)   // returns chats started; chats run in goroutines
  func (g *Generator) InFlight() int
  func (g *Generator) SetAuto(on bool); func (g *Generator) Auto() bool
  func (g *Generator) Run(ctx context.Context)              // auto waves every Interval while Auto()
  func (g *Generator) EnsureDemoAgents(ctx context.Context) ([]string, error)
  ```

Rules:
- Before the first wave after a (re)connect, adopt existing sessions whose `Name` starts with `asteroids-`. Never list, chat on or delete any other session.
- Per wave, pick up to `AgentsPerWave` ready agents, rotating through the list. For each agent: if the app owns fewer than 2 sessions for it, create `asteroids-<agent>-<n>`; otherwise chat on its least recently used session (the resume path).
- Before creating, if owned sessions are at or above `MaxSessions`, delete the oldest-created owned session.
- Around each chat: `MarkSent(id, now)` before sending and `MarkTurnEnded(id, now)` when `Chat` returns.
- Prompts: a fixed list of short, harmless questions with one-sentence answers (e.g. "In one sentence, what is a sandbox?"), rotated.
- `EnsureDemoAgents`: if `ListReadyAgents` is non-empty, return nil. Otherwise create `asteroids-demo-scout`, `asteroids-demo-coder` and `asteroids-demo-poet` with the first harness from `ListHarnesses`, model config `default-model-config`, and one-line persona prompts. Return the names created.

- [ ] **Step 1: Write failing tests with a fake `Client`** (records calls, configurable agents and sessions) and a fake `Marker`: `TestWaveCreatesThenResumes` (wave 1 creates; once 2 are owned per agent, the next wave chats on the LRU one, with no `CreateSession`), `TestWaveCapDeletesOldestOwned`, `TestGeneratorAdoptsOwnSessionsOnly` (a session named `other` is never deleted or chatted), `TestWaveMarksSentAndTurnEnded`, `TestWaveRejectsWhileRunning` (`ErrWaveRunning`), `TestWaveNoReadyAgents` (`ErrNoAgents`, no calls), `TestEnsureDemoAgentsOnlyWhenEmpty`.
- [ ] **Step 2: Run `go test ./internal/traffic/` and confirm it fails.**
- [ ] **Step 3: Implement `traffic.go`.**
- [ ] **Step 4: Run `go test -race ./internal/traffic/` and confirm it passes.**
- [ ] **Step 5: Commit** — `feat(traffic): real chat waves with own-session caps`

---

### Task 5: Config, HTTP server, main

**Files:**
- Create: `internal/config/config.go`, `internal/server/server.go`, `web/embed.go`, a placeholder `web/index.html`, `cmd/asteroids/main.go`
- Test: `internal/config/config_test.go`, `internal/server/server_test.go`

**Interfaces:**
- Consumes: `hub.Hub`, `traffic.Generator`, `kagent.Dial`.
- Produces:
  ```go
  // config
  type Config struct { API, Gateway, UserID, Token, CAFile, Namespace, Listen string; WaveInterval time.Duration; AutoWaves, DemoAgents bool }
  func FromEnv(lookup func(string) (string, bool)) (Config, error)
  // env: KAGENT_API (http://host.docker.internal:8083), KAGENT_GATEWAY (""), KAGENT_USER_ID (admin@kagent.dev),
  //      KAGENT_TOKEN, KAGENT_CA_FILE, KAGENT_NAMESPACE (kagent), LISTEN_ADDR (:7790),
  //      WAVE_INTERVAL (20s), AUTO_WAVES (true), DEMO_AGENTS (false)
  // web
  var Files embed.FS
  // server
  type Connector func(api, gateway, userID, token, caFile string) error   // main implements: Dial, then hub.SetSource + gen.SetClient
  func New(h *hub.Hub, g *traffic.Generator, connect Connector, assets fs.FS) http.Handler
  ```
- Routes:
  - `GET /` static assets.
  - `GET /events` SSE: `data: <json>\n\n` for each hub message, with a `: ping` comment every 15 s.
  - `POST /api/wave` and `GET /traffic` (Stream Deck compatible): 202 `{"sent":n}`, or 409 `{"error":…}` for `ErrWaveRunning` / `ErrNoAgents`.
  - `POST /api/auto {"on":bool}`.
  - `POST /api/demo-agents` returns `{"created":[…]}`.
  - `GET /api/config` returns the current API, gateway, user id and namespace (never the token).
  - `POST /api/config {"api","gateway","userId","token","caFile"}` calls `connect`; returns 200, or 400 with the error.
  - `GET /healthz`.
- main: start `hub.Run(ctx, 250*time.Millisecond)` and `gen.Run(ctx)`. After each wave or auto change, call `hub.SetWaves`. If `DemoAgents` is set, call `EnsureDemoAgents` once. Shut down cleanly on SIGINT/SIGTERM.

- [ ] **Step 1: Write failing tests:** `TestFromEnvDefaults` (every default above), `TestFromEnvOverrides`, `TestFromEnvBadDuration`; `TestServerWave202`, `TestServerWaveConflict409`, `TestServerSSEFirstMessageIsState` (read the first `data:` line within 2 s), `TestServerConfigHidesToken`, `TestServerConfigReconnectCallsConnector`.
- [ ] **Step 2: Run `go test ./internal/config/ ./internal/server/` and confirm it fails.**
- [ ] **Step 3: Implement config, server, embed and main.**
- [ ] **Step 4: Run `go test -race ./...` and confirm it passes. Then `go run ./cmd/asteroids` with `KAGENT_API=http://localhost:8083`; `curl -sN localhost:7790/events | head -c 400` should show a state with `"connected":true` and the `kagent-default` pool; `curl -X POST localhost:7790/api/wave` should return 202, and `event` messages should follow on `/events`.**
- [ ] **Step 5: Commit** — `feat(server): config, SSE stream, wave and connect endpoints`

---

### Task 6: The show (vector canvas frontend)

**Files:**
- Create: `web/index.html`, `web/style.css`, `web/show.mjs`, `web/render.mjs`, `web/main.mjs`
- Test: `web/show.test.mjs`

**Interfaces:**
- Consumes: the SSE messages from Task 3 (shape above), `POST /api/wave`, `/api/auto`, `/api/demo-agents`, `/api/config`.
- Produces (`show.mjs`, pure, no DOM):
  ```js
  export const DELAY_MS = 600, GAP_MS = 900;
  export function schedule(queue, ev, nowMs)   // → displayAt; displayAt = max(nowMs + DELAY_MS, lastDisplayAt + GAP_MS); preserves seq order
  export function due(queue, nowMs)            // → events whose displayAt ≤ nowMs, removed from the queue, in seq order
  export function rockShape(uid)               // → [[x,y]…] 8–11 points, radius 18–28, deterministic per uid
  export function aimAngle(ship, target)       // → radians, for the autopilot turn
  ```

Behaviour (the spec's mapping table, made concrete):
- **Stage:** 1280×720 logical units scaled to fit with letterboxing, black background, `#fff` strokes 1.5 px, `ui-monospace` uppercase, letter spacing 2px.
- **Top bar:** `env`, number of pools, busy/total workers, running, suspended, `RESTORE {n} MS`, `TO SUSPEND {n} MS`, `SUSPENDS {n}`.
- **Saucers:** one per pool, spread across the top; label = pool name. Bays = one small rectangle per worker, filled while any running/resuming actor has that `worker`.
- **Asteroids:** one per actor in state resuming/running/suspending, drawn with `rockShape(uid)`, drifting and wrapping, with a dashed leader label `AGENT` / `RUNNING ON <worker>` (or `RESUMING`).
- **On a displayed `create`:** the rock launches from its pool's saucer.
- **On `resume`:** a capsule rises from the vault to the saucer, then the same rock launches.
- **On `suspend`:** the ship turns (`aimAngle`) during the delay window, fires a dotted shot, the rock splits into 6–8 drifting line fragments that fade over 1.2 s, and a capsule falls into the vault.
- **On `fail`:** a red-stroked flash at the rock.
- **On `gone`:** the rock or capsule fades out.
- **Vault:** bottom-right box `SNAPSHOT STORE`, one capsule per suspended actor (cap the drawn count at 40 and show the true number), text `{n} ACTORS STORED · 0 CPU`.
- **Ship:** autopilot. Between suspends it drifts slowly and points toward the nearest running rock, and it wraps around the screen.
- **Caption line:** the bottom two lines show the `caption` of the most recently displayed event, holding at least `GAP_MS`.
- **Banners:** `connected:false` shows a centred `LINK LOST · RETRYING` and dims the stage. A `waves.note` (e.g. no ready agents) shows small text above the caption.
- **Keys:** `T` wave, `A` toggle auto waves, `D` demo agents, `C` connect overlay (a form posting `/api/config`), `F` fullscreen. Nothing else is interactive.
- **Truth rule:** rocks exist only for actors in the latest state; animations only start from displayed events.

- [ ] **Step 1: Write failing `show.test.mjs`:** `schedule delays first event by DELAY_MS`, `schedule spaces bursts` (4 events at the same now get displayAt now+600, +1500, +2400, +3300, and `due` returns them one at a time in seq order), `rockShape deterministic and bounded`, `aimAngle points at target` (ship (0,0), target (0,10) ⇒ π/2).
- [ ] **Step 2: Run `node --test web/` and confirm it fails.**
- [ ] **Step 3: Implement `show.mjs` and confirm `node --test web/` passes.**
- [ ] **Step 4: Implement `render.mjs`, `main.mjs`, `index.html` and `style.css`.**
- [ ] **Step 5: Run the server against the live cluster, open `http://localhost:7790`, and press `T`.** Using the Playwright tools, take screenshots at roughly 1 s, 2.5 s and 4 s after the wave. Check that a rock launches from the saucer, a shot and explosion happen, the vault count goes up, and the caption reads `TURN DONE. SUBSTRATE FROZE … MS AFTER THE TURN. WORKER FREED.` Kill the port-forward and check that `LINK LOST · RETRYING` appears; restart it and check that the show recovers.
- [ ] **Step 6: Commit** — `feat(web): vector asteroids show`

---

### Task 7: Docker, README, live run

**Files:**
- Create: `Dockerfile`, `.dockerignore`, `Makefile`, `README.md` (replace the stub)

**Interfaces:**
- Consumes: the whole app.
- Produces: image `substrate-asteroids:dev`; `make run` runs it.

- **Dockerfile:** stage 1 `golang:1.27` runs `go mod download`, then `CGO_ENABLED=0 go build -o /asteroids ./cmd/asteroids`. Stage 2 is `gcr.io/distroless/static-debian12:nonroot`, `EXPOSE 7790`, `ENTRYPOINT ["/asteroids"]`.
- **Makefile targets:**
  - `test`: `go test -race ./... && node --test web/`
  - `image`: `docker build -t substrate-asteroids:dev .`
  - `pf`: `kubectl port-forward -n kagent svc/kagent-controller 8083:8083`
  - `run`: `docker run --rm -p 7790:7790 -e KAGENT_API=http://host.docker.internal:8083 substrate-asteroids:dev`
- **README:** what it is (one paragraph), the mapping table, quick start (port-forward, then `make image run`, open `http://localhost:7790`), env vars, keys, the "Substrate does the suspend" note, and connecting to another env.

- [ ] **Step 1: Run `make image` and confirm it builds.**
- [ ] **Step 2: Run `make pf` in the background, then `make run`, and open `http://localhost:7790`.** Expected: `connected:true`. If the container can't reach the port-forward on `host.docker.internal`, restart it with `kubectl port-forward --address 0.0.0.0 …` and document that in the README.
- [ ] **Step 3: Let auto waves run for 2 minutes and take a Playwright screenshot at the end.** Expected: the suspend count is at least 4, and `GET /api/config` shows the env. Confirm with `kagent agent session list` that only `asteroids-` sessions were created.
- [ ] **Step 4: Run `make test` and confirm everything passes.**
- [ ] **Step 5: Commit** — `feat: docker image and README`
