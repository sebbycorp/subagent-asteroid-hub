# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Substrate Asteroids: a live, Asteroids-style visualization of kagent 1.0's Agent Substrate (actors created, running, suspended to snapshots, resumed, forked), with agentgateway in front of every agent call. Go backend, vanilla-JS canvas frontend embedded in the binary, shipped as one Docker image. It connects to any kagent 1.0 environment; nothing in the show is simulated.

## Commands

```bash
make test                                   # go test -race ./... && node --test web/*.test.mjs
go test ./internal/hub -run TestName -v     # one Go test
node --test --test-name-pattern 'cannon' web/render.test.mjs   # one JS test
go test -tags live ./internal/kagent -run TestLiveHumanApproval -v   # live tests: need a real kagent (KAGENT_API, default http://localhost:8083)

make pf          # port-forward kagent API 8083 / UI 8082 and agentgateway 8090 (sandbox) / 8091 (chat ingress); LAN_IP=… also binds them on the LAN
make image       # docker build substrate-asteroids:dev
make run         # run it on the kind network (LAN_IP=… publishes :7790 there too)
make dev         # go run, localhost only
make agentgateway  # install Solo Enterprise agentgateway, apply deploy/agentgateway/, point kagent's one model at Qwen via the gateway, narrow kagent-tools RBAC
make relay       # nginx relay so the cluster can reach Ollama on the host
```

`web/` is embedded (`web/embed.go`), so a frontend change only shows up after `make image` and restarting the container (`docker rm -f substrate-asteroids && make run LAN_IP=…`). Live tests are behind the `live` build tag and are not part of `make test`.

## Architecture

Data flows one way: kagent gRPC → hub → SSE → browser.

- `internal/kagent`: adapter over kagent's gRPC API (`:8083`): Substrate actors/workers/summary, sessions, checkpoints/forks, agents, model configs. Chats are A2A over gRPC sent through agentgateway's ingress listener.
- `internal/hub`: polls the Substrate snapshot, diffs consecutive snapshots into `model.Event`s (`internal/model/diff.go`: create/running/suspend/resume/fork/gone…), measures restore and suspend latencies, holds a suspend while a human-approval request is pending, merges agentgateway events, and broadcasts `State` plus events to SSE clients.
- `internal/gateway`: an OTLP/HTTP logs receiver for agentgateway's access logs. It turns them into `llm` / `tool` / `chat` events (model, tokens, TTFT, tool name). The calling agent comes from the `asteroids.agent` attribute, set from the `x-asteroids-agent` header each agent's RemoteMCPServer sends. Untagged egress is attributed to the only running actor.
- `internal/traffic`: waves of real chats to demo agents. Manual only by default (`AUTO_WAVES=false`); the `T` key or `POST /api/wave` sends one.
- `internal/hitl`: the board of turns paused for a human (A2A task state `INPUT_REQUIRED`). A decision must name the exact session and task (`POST /api/approval {sessionId, taskId, approved}`); a resumed turn can pause again.
- `internal/tour`: the guided tour. Seven chapters that make real API calls (create, suspend, resume, fork, gateway, hitl, density) and delete the sessions they created when stopped.
- `internal/server`: routes, the same-origin JSON guard on every POST, and `/api/config`, which switches environment at runtime.
- `cmd/asteroids/main.go` wires it all together. Config is env-only (`internal/config`: `KAGENT_API`, `KAGENT_GATEWAY`, `KAGENT_TOKEN`, `KAGENT_CA_FILE`, `LISTEN_ADDR` :7790, `OTLP_LISTEN_ADDR` :4318, `DEMO_AGENTS`, …).

Frontend (`web/`), split so most of it is unit-testable:
- `show.mjs`: pure presentation logic (event pacing queue, captions, rock shapes, call timing, log lines). No DOM.
- `render.mjs`: pure canvas drawing plus layout math (`gatewayPos`, `fighterPos`, `GUNNER`, `muzzleAt`, `moonAt`). Tests drive it with a Proxy fake 2D context.
- `main.mjs`: world state, SSE handling, turning events into animations, and reconciling against each `State` (a mismatch must persist for `GRACE_MS` before it's corrected). Not unit-tested; check changes in a browser (`window.__show` exposes the world).

What the screen means (keep the README table in sync when this changes):
- the kagent robot's rail cannon (bottom left) = a WorkerPool; its platform lights are workers
- an asteroid fired out of it = an actor (cyan = new, amber = resumed)
- the rocket shooting it down = the suspend (Substrate suspends by itself; the rocket only marks it)
- a capsule in the store = a snapshot
- the planet = agentgateway
- a grey dashed rock with the kagent robot pilot = a human-approval wait (Y/X)

The owner prefers the flat vector line-art look: no 3D shading.

## kagent / Substrate gotchas

- Depends on `github.com/kagent-dev/kagent/go` at a pinned pseudo-version and `replace go.ate.dev/substrate => github.com/kagent-dev/substrate v0.6.0-alpha3` (see `go.mod`).
- A2A over gRPC uses tenant `"<ns>/<name>"`. The HITL extension (`https://kagent.dev/extensions/hitl/v1`) must be sent as the `a2a-extensions` service param via `ServiceParams`: the a2a-go gRPC transport replaces context metadata.
- `CreateCheckpoint` needs `expected_head_task_id` and returns FailedPrecondition until the suspend snapshot exists, so retry. `ListSessions` page size is at most 100, so loop.
- A session keeps the config it was created with in its snapshot. Sessions created before a config change (for example, routing through the gateway) keep the old behaviour; delete them.
- `requireApproval: true` applies to a whole tool binding. To mix free and approval-gated tools, use two bindings to the same RemoteMCPServer (fine for the kagent harness).

## Deploy (`deploy/agentgateway/`, kind cluster `kind-kagent`)

- Every agent's model, MCP tools and incoming chats go through agentgateway (Solo Enterprise `v2026.10.0`; older CRDs fail on this Kubernetes). kagent's single `default-model-config` points at the gateway's `/qwen` route with a placeholder key; real provider keys live only in gateway Secrets. Don't print `helm get values` or Secrets unfiltered.
- Ollama binds 127.0.0.1 and checks the Host header, so the cluster reaches it through the `ollama-relay` nginx container (`deploy/agentgateway/ollama-relay.conf`).
- Chat ingress is a separate gateway listener (`:8081`) with a gRPC method allow-list (A2A, GetSession, health). OTLP logs go to the app's `:4318`, which is not published.
- kagent-tools runs read-only cluster-wide with no Secrets (`deploy/kagent-tools-rbac.yaml`); `make agentgateway` fails if it can still read Secrets. Writes are allowed only in the `playground` namespace (restricted Pod Security), via the Role in `45-k8s-operator.yaml`.
- HITL agents: `k8s-operator` (reads free, every change approved) and `asteroids-demo-ops` (all tools approved; used by the tour's hitl chapter).
