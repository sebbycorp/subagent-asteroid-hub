// Pure presentation logic: event pacing, rock shapes, ship aim. No DOM.

// Real lifecycles take ~1–2 s; events are shown after a short delay and at
// least GAP_MS apart so each one can be read. Order is never changed.
export const DELAY_MS = 600;
export const GAP_MS = 900;

export function newQueue() {
  return { items: [], last: -Infinity };
}

// schedule adds an event and returns when it will be displayed.
export function schedule(queue, ev, nowMs) {
  const displayAt = Math.max(nowMs + DELAY_MS, queue.last + GAP_MS);
  queue.last = displayAt;
  queue.items.push({ ev, displayAt });
  return displayAt;
}

// due removes and returns the events whose time has come, in seq order.
export function due(queue, nowMs) {
  queue.items.sort((a, b) => a.ev.seq - b.ev.seq);
  const out = [];
  while (queue.items.length && queue.items[0].displayAt <= nowMs) {
    out.push(queue.items.shift().ev);
  }
  // an earlier-seq item still waiting blocks later ones that are ready
  return out;
}

// pending reports whether any queued event concerns this actor.
export function pending(queue, uid) {
  return queue.items.some((i) => i.ev.actorUid === uid);
}

// nextSuspend is the actor whose suspend will be shown next, if any.
export function nextSuspend(queue) {
  const it = [...queue.items].sort((a, b) => a.ev.seq - b.ev.seq).find((i) => i.ev.kind === 'suspend');
  return it ? it.ev.actorUid : null;
}

function seeded(str) {
  let h = 2166136261;
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return () => {
    h = (h + 0x6d2b79f5) | 0;
    let t = Math.imul(h ^ (h >>> 15), 1 | h);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// rockShape is a jagged polygon, the same for the same actor every time.
export function rockShape(uid) {
  const rand = seeded(uid);
  const n = 8 + Math.floor(rand() * 4);
  const radius = 18 + rand() * 10;
  const pts = [];
  for (let i = 0; i < n; i++) {
    const a = (i / n) * Math.PI * 2;
    const r = radius * (0.7 + 0.3 * rand());
    pts.push([Math.cos(a) * r, Math.sin(a) * r]);
  }
  return pts;
}

export function aimAngle(ship, target) {
  return Math.atan2(target.y - ship.y, target.x - ship.x);
}

// wrapLines splits text into at most maxLines lines of maxChars.
export function wrapLines(text, maxChars, maxLines = 2) {
  const words = (text || '').split(' ');
  const lines = [''];
  for (const w of words) {
    const cur = lines[lines.length - 1];
    if (!cur) lines[lines.length - 1] = w;
    else if ((cur + ' ' + w).length <= maxChars) lines[lines.length - 1] = cur + ' ' + w;
    else lines.push(w);
  }
  return lines.slice(0, maxLines);
}

// Reconciliation waits this long before correcting the stage to match the
// state: events for a change (e.g. a suspend held for its turn-end time) may
// still be on their way, and the stage must not pre-empt them.
export const GRACE_MS = 1500;

// settled records when the stage and the state started to disagree about an
// actor and reports whether that disagreement has lasted GRACE_MS.
export function settled(seen, uid, mismatch, nowMs) {
  if (!mismatch) {
    seen.delete(uid);
    return false;
  }
  if (!seen.has(uid)) seen.set(uid, nowMs);
  return nowMs - seen.get(uid) >= GRACE_MS;
}

// Captions get their own queue: each stays up at least CAPTION_MS so it can
// be read, even when animations run faster. If a burst backs up more than
// MAX_WAITING, "running" captions go first (the rock's label already says
// it), then creates/resumes; suspend captions — the point of the show — stay.
export const CAPTION_MS = 2400;
export const MAX_WAITING = 3;

export function newCaptions() {
  return { current: null, since: -Infinity, waiting: [] };
}

export function pushCaption(cq, ev, nowMs) {
  if (cq.current === null) {
    cq.current = ev.caption;
    cq.since = nowMs;
    return;
  }
  cq.waiting.push(ev);
  while (cq.waiting.length > MAX_WAITING) {
    let i = cq.waiting.findIndex((e) => e.kind === 'running');
    if (i < 0) i = cq.waiting.findIndex((e) => e.kind !== 'suspend');
    cq.waiting.splice(i < 0 ? 0 : i, 1);
  }
}

export function currentCaption(cq, nowMs) {
  if (cq.waiting.length && nowMs - cq.since >= CAPTION_MS) {
    cq.current = cq.waiting.shift().caption;
    cq.since = nowMs;
  }
  return cq.current;
}

// Per-actor animation bookkeeping. beginAnim returns a token; endAnim reports
// whether that token is still the actor's latest, so an animation overtaken
// by a newer event for the same actor (e.g. a suspend capsule still landing
// when the resume starts) doesn't apply its stale result.
export function newAnims() {
  return { latest: new Map(), count: new Map(), next: 1 };
}

export function beginAnim(an, uid) {
  const tok = an.next++;
  an.latest.set(uid, tok);
  an.count.set(uid, (an.count.get(uid) || 0) + 1);
  return tok;
}

export function endAnim(an, uid, tok) {
  const n = (an.count.get(uid) || 1) - 1;
  if (n > 0) an.count.set(uid, n);
  else an.count.delete(uid);
  return an.latest.get(uid) === tok;
}

export function animating(an, uid) {
  return an.count.has(uid);
}

// liveLine is the tour card's last line: the real, current state of what the
// chapter is about (its actor, or the whole cluster for density).
export function liveLine(tour, state, gw = { llm: 0, tool: 0 }) {
  const actors = Object.values(state?.snapshot?.actors || {});
  if (tour.kind === 'gateway') {
    const n = (c, w) => `${c} ${w}${c === 1 ? '' : 'S'}`;
    return `LIVE: AGENTGATEWAY HANDLED ${n(gw.llm, 'MODEL CALL')} · ${n(gw.tool, 'TOOL CALL')}`;
  }
  if (tour.kind === 'density') {
    const workers = (state?.snapshot?.pools || []).reduce((n, p) => n + (p.workers || []).length, 0);
    const running = actors.filter((a) => a.state === 'running' || a.state === 'resuming').length;
    return `LIVE: ${actors.length} SESSIONS · ${workers} WORKER${workers === 1 ? '' : 'S'} · ${running} RUNNING NOW`;
  }
  const id = (tour.focus || [])[tour.kind === 'fork' ? (tour.focus.length - 1) : 0];
  const a = actors.find((x) => x.sessionId === id);
  if (!a) return 'LIVE: WAITING FOR THE ACTOR…';
  if (a.state === 'suspended') return `LIVE: SUSPENDED · SNAPSHOT GEN ${a.snapshotGen} · 0 CPU`;
  if (a.state === 'running') return `LIVE: RUNNING ON WORKER ${(a.worker || '').split('-').pop().toUpperCase()}`;
  return `LIVE: ${a.state.toUpperCase()}`;
}

// shortAgent drops the demo prefix so log lines fit the store's column.
export function shortAgent(name) {
  return (name || 'agent').replace(/^asteroids-demo-/, '').toUpperCase();
}

function clock(at) {
  return new Date(at).toISOString().slice(11, 19);
}

function dur(ms) {
  if (ms == null) return '';
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}S` : `${ms}MS`;
}

// logLine is one line of the log under the snapshot store: a lifecycle event
// (src 'event') or a request agentgateway handled (src 'gateway').
export function logLine(it) {
  const t = clock(it.at);
  if (it.src === 'gateway') {
    if (it.kind === 'chat') return `${t} ⬡ CHAT IN → AGENT ${dur(it.durationMs)}`;
    const to = it.kind === 'llm' ? (it.model || it.target) : it.tool;
    return `${t} ⬡ ${shortAgent(it.agent)} → ${String(to).toUpperCase()} ${dur(it.durationMs)}`;
  }
  const a = shortAgent(it.agent);
  switch (it.kind) {
    case 'running':
      if (it.startMs != null) return `${t} STARTED  ${a} ${dur(it.startMs)}`;
      return `${t} RUNNING  ${a}${it.restoreMs != null ? ' ' + dur(it.restoreMs) : ''}`;
    case 'suspend':
      return `${t} SUSPEND  ${a}${it.suspendMs == null ? '' : it.suspendMs <= 10 ? ' INSTANT' : ' ' + dur(it.suspendMs)}`;
    default:
      return `${t} ${it.kind.toUpperCase().padEnd(8)} ${a}`;
  }
}

// portFor names the port on the agentgateway ship a request docks at.
export function portFor(ev) {
  if (ev.kind === 'chat') return { key: 'a2a', label: 'CHATS IN' };
  if (ev.kind === 'tool') return { key: 'mcp', label: String(ev.target || 'MCP').toUpperCase() };
  return { key: 'llm', label: String(ev.model || ev.target).toUpperCase() };
}

// attribute picks the asteroid a gateway request belongs to. Tool calls carry
// their agent; model calls share one model config and don't, so they go to
// the only busy asteroid — or to none rather than a guess.
export function attribute(ev, rocks) {
  const live = rocks.filter((r) => !r.dying);
  if (ev.actorUid) {
    const pinned = live.find((r) => r.uid === ev.actorUid);
    if (pinned) return pinned;
  }
  if (ev.agent) {
    return live.filter((r) => r.agent === ev.agent).sort((x, y) => y.born - x.born)[0] || null;
  }
  return live.length === 1 ? live[0] : null;
}

// growScale is how big a running asteroid has grown: its sandbox is using
// compute and memory while it works. +25% a second, capped at 2x.
export function growScale(runningMs) {
  return Math.min(2, 1 + Math.max(0, runningMs) / 4000);
}

// fxDuration is how long a gateway call's animation plays: slower real calls
// play longer, so a 54 ms tool call and a 2 s model call look different.
export function fxDuration(realMs) {
  return Math.round(Math.min(3200, 1100 + Math.max(0, realMs) * 0.6));
}

// fxLabel is the caption that rides along with a gateway call.
export function fxLabel(ev) {
  const d = dur(ev.durationMs);
  if (ev.kind === 'chat') return `A2A CHAT IN · ${d}`;
  if (ev.kind === 'tool') return `${String(ev.target ? `${ev.target}/${ev.tool}` : ev.tool).toUpperCase()} · ${d}`;
  return `${String(ev.model || ev.target).toUpperCase()} · ${d}${ev.tokens ? ` · ${ev.tokens} TOK` : ''}`;
}
