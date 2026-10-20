# Substrate Asteroids — design

Date: 2026-10-20
Status: for approval

## Intent

A self-running, live show that teaches how **kagent Agent Substrate** works, styled as
classic 1979 vector *Asteroids*. Sebastian puts it on screen and narrates; nobody plays.
Everything that moves on screen is a real object or a real state change in a real
cluster. The audience should leave understanding three ideas:

1. An agent session is an **actor** that runs in a gVisor sandbox on a **worker** from
   a **worker pool**.
2. When a turn finishes the actor is **suspended**: its memory and disk are
   checkpointed to the snapshot store and the worker is freed. A suspended actor costs
   no compute. Substrate does this by itself, tens of milliseconds after the turn ends.
3. When it's needed again it is **resumed**: the snapshot is restored onto a worker and
   it carries on with its state intact.

It's a sibling to [substrate-hud](https://github.com/sebbycorp/substrate-hud) (a Tauri
macOS overlay that only watches one cluster by scraping Substrate's Postgres and
api-server logs). This app differs in three ways: it **generates real traffic** so
there is always something to show, it runs as a **Docker container** viewed in a
browser, and it talks only to the **public kagent 1.0 API**, so it can be pointed at
any kagent / Agent Substrate environment.

## Goals

- Real and live: every on-screen event is backed by a real state change read from the
  API or a real call the app made. There is no simulated or replay mode.
- Teach: every event is labelled and explained in plain English, with real numbers
  (restore ms, time-to-suspend ms, busy workers).
- Self-running: once started, it keeps itself interesting with no one at the keyboard.
- Portable: connect to any kagent 1.0 environment by URL (local kind via
  port-forward, or a remote cluster).
- One command to run: `docker run …`, then open a browser.

## Non-goals

- A playable game. No player controls, no score-chasing, no lives.
- A general kagent admin UI or chat client.
- Supporting kagent 0.10.x (no v1alpha3 API, no SessionService).
- Kubernetes access. The app never needs a kubeconfig.
- An offline / recorded fallback. *(My call: first showings are assumed to be on the
  laptop's local kind cluster. Revisit if a venue or remote cluster is planned.)*
- Standalone sandboxes (SandboxService). *(Considered as a way to make the ship the
  real cause of suspends; dropped. Creating a SandboxTemplate on the alpha16 kind
  install failed with "sandbox workload and guest images must be pinned by sha256
  digest" even with a digest-pinned image.)*

## Anti-goals (would be a failure even if it works)

- Showing things that didn't happen: asteroids or explosions without a matching real
  event.
- Claiming the ship caused a suspend. Substrate does it; captions must say so.
- Slick but unreadable: if the audience can't tell what a suspend or resume *is* from
  the screen and captions, it failed.
- Departing from the vector look (filled sprites, gradients, colour-heavy UI).
- Harming the target cluster: unbounded session creation, or deleting or touching
  anything the app didn't create.

## The look (decided)

Classic vector *Asteroids*: black background, thin white strokes, polygon asteroids, a
triangle ship, monospace/vector-style uppercase lettering with letter spacing. White is
the main colour. *(Builder's call: any sparing accent, e.g. dim green for running, red
for failures, as long as it still reads as vector-arcade.)*

Explanations use **labels in the sky plus a caption line**. The action never pauses
or freezes.

- **Top bar** (arcade-score style): environment name, pools, busy workers / total,
  running actors, suspended actors, last restore ms, last time-to-suspend ms, total
  suspends.
- **Labels on asteroids**: agent name and state (`RUNNING ON WORKER 2`, `RESUMING`,
  `CREATING`), with a thin dashed leader line.
- **Caption line** at the bottom: one or two lines of plain English for the most recent
  event, e.g. "TURN DONE. SUBSTRATE FROZE IT TO S3 IN 45 MS. WORKER FREED." Captions
  queue so a burst of events doesn't flicker past unread.

Reference mockup: `.superpowers/brainstorm/*/content/vector-look.html`, card A
(`core-loop.html` shows the earlier, non-vector layout of the same elements).

## Mapping: Substrate → screen (decided)

| Substrate | On screen |
|-----------|-----------|
| WorkerPool | A vector UFO saucer near the top. One saucer per pool, labelled. |
| Worker | A small bay under its pool's saucer. Lit while hosting an actor. |
| Actor created (new session) | A new asteroid launched from the pool's saucer. |
| Actor resumed | A capsule rises from the vault to the saucer, which relaunches *the same* asteroid (same shape, same label). |
| Actor running | The asteroid drifts in the field, labelled. |
| Actor suspended | The autopilot ship fires and the asteroid breaks into line fragments; a capsule (the checkpoint) drops into the vault. |
| Suspended actors | Capsules in the **snapshot vault** at the bottom, with a count ("N ACTORS STORED · 0 CPU"). |
| Actor failed | Builder's call, in the classic idiom (e.g. the small red UFO). |

Asteroids keep a stable shape per actor (seeded from the actor UID) so a resumed actor
is recognisably the same rock. The "pool full / actors waiting" visual is only built if
the API exposes a waiting state; otherwise it's dropped rather than invented.

## The ship and the suspend (decided: option A)

Observed on the local install: an actor resumes ~130 ms after a chat arrives, runs for
the turn (~1.4 s for a one-word reply), and Substrate suspends it **~45 ms after the
turn ends**. Nothing outside Substrate can beat that, so:

- **The ship is a narrator, not the cause.** It flies on autopilot, tracks the actor
  that's working, and fires the moment the real suspend is observed. The ship makes no
  suspend API call.
- Captions credit Substrate ("SUBSTRATE SUSPENDED IT 45 MS AFTER THE TURN"). That speed
  is a headline teaching point.
- **Pacing (my call):** real lifecycles are ~1–2 s, too fast to follow. The frontend
  plays the true event sequence in order with a short, fixed presentation delay and
  slowed animation, and always shows the true measured numbers. It never reorders,
  skips or invents events.

## Traffic: where asteroids come from (decided)

The app generates real traffic itself, because a quiet cluster is an empty sky.

- **Waves**: a wave sends one short, real chat to each of several agents over A2A. It
  runs automatically on an interval and can be triggered by a key or HTTP endpoint
  (like substrate-hud's `T` and `GET /traffic`).
- **Create and resume**: a wave both starts new sessions (`CreateSession`, then chat)
  and chats on earlier sessions the app created. A chat on a suspended session resumes
  it implicitly (observed), so that's the resume path, with no explicit `ResumeSession`
  call needed.
- **Demo agents (my call)**: if the cluster has no usable agents, a button on the
  connect screen, or a startup flag, creates a few small demo agents through
  `AgentService/CreateAgent`, using the cluster's default ModelConfig and an existing
  Harness. Everything the app creates is named/labelled so it can find it again.
- **Bounds and cleanup (my call)**: caps on concurrent chats and on total sessions the
  app keeps; when the cap is hit, the app deletes its own oldest sessions. It never
  deletes or touches anything it didn't create.
- **Watch-only**: waves can be switched off for busy clusters; the app then just shows
  real traffic.
- **Visibility (my call)**: list sessions with `all_creators: true` so actors from all
  users can be matched to agents for labels (verified: without it, another user id
  sees no sessions).

## Connecting to an environment (my calls)

- Target is the kagent controller's API: gRPC on port 8083 in the default install, also
  serving A2A. Default address `host.docker.internal:8083`, which reaches a
  `kubectl port-forward` on the laptop.
- Config by env vars / flags, plus a small connect screen in the browser to switch
  environment: API address, optional separate A2A gateway address, optional CA file
  (mounted), optional bearer token, user id (default `admin@kagent.dev`, as the CLI).
- Connection health is always visible; the show must never silently freeze.

## Architecture (my calls)

- **One container**: a Go backend serving the page and a live event stream (SSE or
  WebSocket) to the browser.
- **Actors are the source of truth for lifecycle.** The session API reports `READY`
  throughout suspend/resume (observed), so lifecycle must come from
  `SystemService/ListSubstrateActors` (`ACTOR_STATE_RUNNING` / `ACTOR_STATE_SUSPENDED`
  / …), joined to sessions by name (`session-<session id>`).
- **Polling**: there is no watch/stream RPC for actors in alpha16. Poll actors fast
  enough to catch a ~1.4 s running window (≈200–250 ms; builder tunes it), and pools /
  workers / sessions more slowly. Diff successive polls into events with timestamps.
  The app's own A2A stream (start, end) sharpens timing for its own chats.
- **Latencies**: restore ms = chat sent → actor running; time-to-suspend = turn ended
  (A2A stream end) → actor suspended. Shown only where measured.
- **gRPC stubs**: generate from kagent's protos pinned to the target version, or use
  server reflection; builder's call.
- **Frontend**: a single HTML page with a canvas and vector drawing. A fixed 16:9 stage
  that scales to the window, so it also works as an OBS browser source. The backend
  owns truth; the frontend animates and never invents events.

## Useful facts about the API (kagent 1.0.0-alpha16, observed)

- Services: `kagent.api.v1.{Agent,AgentTemplate,Checkpoint,Harness,Model,Sandbox,
  SandboxTemplate,ScheduledRun,Session,System,Tool}Service`, `lf.a2a.v1.A2AService`
  (incl. `SendStreamingMessage`). Server reflection is on in the test install
  (`config.server.grpcReflection: true`).
- `SystemService`: `GetSubstrateSummary` (worker pools), `ListSubstrateWorkers`
  (`workerPool`, `workerPod`, `nodeName`, `sandboxClass: gvisor`),
  `ListSubstrateActors` (name `session-<id>`, `actorTemplate`, `status.state`,
  `status.snapshots[]` with `s3://…` URIs; optional `atespace` filter).
- `SessionService`: `CreateSession`, `ListSessions` (`all_creators`, `agent`, `page`),
  `GetSession`, `DeleteSession`, `SuspendSession` / `ResumeSession` (`{session_id}`).
  `Session.state` stays `RUNTIME_STATE_READY` across actor suspend/resume.
- `AgentService`: `CreateAgent`, `ListAgents`, `GetAgent`, `UpdateAgent`, `DeleteAgent`.
- The CLI (`kagent agent session create --agent X`, `kagent agent invoke --session ID
  --task …`) is a working reference for the create-then-chat flow.

## Test environment

Local kind cluster `kind-kagent` (Kubernetes 1.37), kagent 1.0.0-alpha16, Substrate
0.6.0-alpha3, one pool `kagent/kagent-default` (1 worker, gVisor), OpenAI
`gpt-4.1-mini` as `default-model-config`, Harness `kagent-default`
(snapshots `s3://ate-snapshots/kagent`), agent `hello-substrate`. API on
`localhost:8083` via `kubectl port-forward -n kagent svc/kagent-controller 8083:8083`.

## Left to the builder

- Ship autopilot behaviour, asteroid physics, wrap-around, animation timing.
- Exact caption wording (plain English, short, uppercase).
- Wave interval, prompts, caps, demo agent definitions.
- Failure visuals; empty-state screen when there are no pools or actors.
- How to show many pools (more than ~3 saucers).
