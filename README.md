# Substrate Asteroids

A self-running, live show that teaches how **kagent Agent Substrate** works,
drawn as 1979 vector *Asteroids*. Nobody plays: you put it on a screen and
narrate. Every rock, shot and capsule is a real actor or a real state change in
a real cluster, read from the kagent 1.0 API. The app also sends real chats to
your agents in waves, so there is always something happening.

## What you're looking at

| On screen | In Agent Substrate |
|-----------|--------------------|
| UFO saucer | a **WorkerPool**; the small bays under it are its **workers** (lit while hosting an actor) |
| Rock launched from a saucer | an actor being **created** (a new session gets a fresh gVisor sandbox) |
| Capsule rising from the store, then the same rock relaunching | a **resume**: the actor's snapshot is restored onto a worker |
| Rock drifting, labelled `RUNNING ON WORKER …` | an actor **running** a turn |
| Ship fires, rock breaks up, capsule drops into the store | a **suspend**: memory and disk are checkpointed to S3 and the worker is freed |
| Snapshot store | every suspended actor, stored at 0 CPU and 0 memory |

The ship doesn't cause the suspend. Substrate suspends an actor by itself, the
moment its turn ends. On a local kind cluster that's within a few milliseconds
of the reply finishing, and the captions say so. The ship just marks the moment.

The top bar shows live numbers: busy workers, running and stored actors, the
last restore time (chat sent → actor running), the last time-to-suspend, and
the total number of suspends.

## Quick start (local kind cluster)

You need kagent 1.0 (tested with `v1.0.0-alpha16` and Substrate `0.6.0-alpha3`)
and Docker.

```bash
make pf        # terminal 1: port-forward the kagent API to localhost:8083
make image run # terminal 2: build and run the container
open http://localhost:7790
```

`make run` publishes the app on `127.0.0.1` only, because its control API
spends your model budget. If the container can't reach the port-forward,
start the port-forward with
`kubectl port-forward --address 0.0.0.0 -n kagent svc/kagent-controller 8083:8083`.

Without Docker, run `make dev`.

## Keys

| Key | Action |
|-----|--------|
| `T` | send a wave of chats now |
| `A` | toggle automatic waves |
| `D` | create three small demo agents (only if the namespace has no ready agents) |
| `C` | connect to another kagent environment |
| `F` | fullscreen |

A Stream Deck or script can trigger a wave with
`curl -X POST -H 'Content-Type: application/json' localhost:7790/traffic`.

## Configuration

| Env var | Default | Meaning |
|---------|---------|---------|
| `KAGENT_API` | `http://host.docker.internal:8083` | kagent controller API (gRPC) |
| `KAGENT_GATEWAY` | same as API | A2A gateway, if separate |
| `KAGENT_USER_ID` | `admin@kagent.dev` | sent as `x-user-id` |
| `KAGENT_TOKEN` | — | optional bearer token |
| `KAGENT_CA_FILE` | — | CA file for TLS endpoints (mount it into the container) |
| `KAGENT_NAMESPACE` | `kagent` | where agents are listed and demo agents are created |
| `WAVE_INTERVAL` | `20s` | time between automatic waves |
| `AUTO_WAVES` | `true` | start with automatic waves on |
| `DEMO_AGENTS` | `false` | create demo agents at start if none are ready |
| `LISTEN_ADDR` | `:7790` | HTTP listen address |

## Connecting to any environment

The app only talks to the public kagent 1.0 API. It needs no kubeconfig and
doesn't scrape Substrate's internals. Point `KAGENT_API` at another cluster's
controller, or press `C` while the show is running. Waves only create, chat on,
or delete sessions named `asteroids-…`. Other users' sessions are shown but
never touched.

Time measurements compare this machine's clock with the cluster's. On a remote
cluster with clock skew, readings within 50 ms count as instant.

## Development

```bash
make test   # Go tests (race detector) + node tests for the show logic
go test -tags live ./internal/kagent/ -run Live -v   # against a live cluster on :8083
```

Design: `docs/superpowers/specs/2026-10-20-substrate-asteroids-design.md`.
