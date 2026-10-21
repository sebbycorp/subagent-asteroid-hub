// Wires the live event stream to the stage. The backend owns the truth:
// rocks, capsules and explosions only start from real, displayed events, and
// each state message reconciles anything no event is about to change.
import {
  newQueue, schedule, due, pending, nextSuspend, rockShape, aimAngle, wrapLines, settled,
  newCaptions, pushCaption, currentCaption, newAnims, beginAnim, endAnim, animating, liveLine,
  logLine, portFor, attribute, growScale, fxDuration, fxLabel, llmTiming, DELAY_MS,
} from './show.mjs';
import { setup, draw, saucerSpots, vaultSlot, activeField, gatewayPos, GUNNER, cannonAim, muzzleAt, NEW, RESUMED, GW, ALIEN } from './render.mjs';

const ACTIVE = new Set(['resuming', 'running', 'suspending']);
const canvas = document.getElementById('stage');
const ctx = setup(canvas);

const world = {
  state: null,
  rocks: new Map(), // uid → rock
  vault: new Set(), // uids shown as stored
  fragments: [],
  bullets: [],
  capsules: [],
  flashes: [],
  ship: { x: GUNNER.x, y: GUNNER.y, a: -0.8, flash: 0 }, // the kagent robot's rail cannon
  captionLines: [],
  idleCaption: 'WAITING FOR SUBSTRATE…',
  help: '',
  toast: null,
  tour: null, // the guided tour's card (from the server)
  tourLive: '',
  focusUids: [],
  splits: [],
  gwFx: [], // agentgateway requests being drawn
  gwPending: [], // agentgateway requests waiting for their display time
  log: [], // lines under the snapshot store
  modelLabel: '',
  tourGw: { chapter: 0, llm: 0, tool: 0 },
  shipFlash: -1e9,
  approvals: new Map(), // sessionId → turn waiting for a person
  approvalByUid: new Map(),
  beamAt: {}, // pool → when its saucer last launched an asteroid (tractor beam)
};
const queue = newQueue();
const captions = newCaptions();
const anims = newAnims(); // per-actor animation tokens
window.__show = { world, queue }; // for debugging in the browser console

// ── helpers ────────────────────────────────────────────────────────────────

const pools = () => world.state?.snapshot?.pools || [];
const actors = () => world.state?.snapshot?.actors || {};

function saucerFor(pool) {
  const spots = saucerSpots(pools(), performance.now());
  return spots.find((s) => s.pool.name === pool) || spots[0] || { x: 500, y: 88 };
}

function shortWorker(w) {
  return w ? w.split('-').pop() : '';
}

function subLabel(a) {
  if (!a) return '';
  if (a.state === 'resuming') return 'RESUMING';
  if (a.state === 'suspending') return 'SUSPENDING';
  return a.worker ? `RUNNING ON WORKER ${shortWorker(a.worker)}` : 'RUNNING';
}

function spawnRock(uid, info, from) {
  if (from && info.pool !== undefined) world.beamAt[info.pool || (pools()[0] || {}).name] = performance.now();
  else if (from) world.beamAt[(pools()[0] || {}).name] = performance.now();
  const angle = Math.PI / 2 + (Math.random() - 0.5) * 1.6;
  const speed = 22 + Math.random() * 22;
  const F = activeField(!!world.tour);
  const r = {
    uid,
    born: performance.now(),
    runSince: performance.now(), // active from launch: it grows until frozen
    x: from ? from.x : F.x0 + Math.random() * (F.x1 - F.x0),
    y: from ? from.y + 30 : F.y0 + Math.random() * (F.y1 - F.y0),
    vx: Math.cos(angle) * speed,
    vy: Math.sin(angle) * speed,
    rot: 0,
    spin: (Math.random() - 0.5) * 0.6,
    shape: rockShape(uid),
    agent: info.agent || 'agent',
    worker: info.worker || '',
    sub: subLabel(info),
  };
  world.rocks.set(uid, r);
  return r;
}

function fly(from, to, dur, done) {
  const c = { x: from.x, y: from.y, from, to, t0: performance.now(), dur, done };
  world.capsules.push(c);
}

function vaultPos() {
  return vaultSlot(world.vault.size);
}

function explode(r, now) {
  const sc = r.scale || 1, c = Math.cos(r.rot), sn = Math.sin(r.rot);
  const at = ([px, py]) => [(px * c - py * sn) * sc, (px * sn + py * c) * sc]; // hull in the ship's heading
  for (let i = 0; i < r.shape.length; i++) {
    const [ax, ay] = at(r.shape[i]);
    const [bx, by] = at(r.shape[(i + 1) % r.shape.length]);
    const a = Math.atan2(by - ay, bx - ax);
    const out = Math.atan2(ay + by, ax + bx);
    world.fragments.push({
      x: r.x + ax, y: r.y + ay, a, len: Math.hypot(bx - ax, by - ay),
      vx: Math.cos(out) * (30 + Math.random() * 40), vy: Math.sin(out) * (30 + Math.random() * 40), born: now,
    });
  }
}

// ── events (already paced by the queue) ────────────────────────────────────

// touch marks an instant event as the actor's latest, making any animation
// still running for an older event stale.
function touch(uid) {
  endAnim(anims, uid, beginAnim(anims, uid));
}

const LOG_MAX = 60;

function logPush(item, color) {
  world.log.push({ text: logLine(item), color });
  if (world.log.length > LOG_MAX) world.log.splice(0, world.log.length - LOG_MAX);
}

// tag colours a rock by where its state came from: a new session is cloned
// from the agent's golden snapshot, a resumed one from its own snapshot.
function setOrigin(r, origin, gen) {
  r.origin = origin;
  r.tag = origin === 'new' ? 'NEW · FROM GOLDEN SNAPSHOT' : `RESUMED · SNAPSHOT GEN ${gen ?? '?'}`;
}

function show(ev, now) {
  pushCaption(captions, ev, now);
  logPush({ src: 'event', ...ev }, ev.kind === 'create' ? NEW : ev.kind === 'resume' ? RESUMED : undefined);
  const info = { ...actors()[ev.actorUid], agent: ev.agent, worker: ev.worker };
  const uid = ev.actorUid;
  switch (ev.kind) {
    case 'create': {
      touch(uid);
      const parent = forkParent(uid);
      world.vault.delete(uid);
      if (parent) {
        // the fork splits off its parent, like an asteroid breaking in two
        const r = spawnRock(uid, { ...info, state: 'resuming' }, null);
        r.x = parent.x; r.y = parent.y;
        const a = Math.random() * Math.PI * 2;
        r.vx = Math.cos(a) * 40; r.vy = Math.sin(a) * 40;
        if (parent.rock) { parent.rock.vx = -r.vx; parent.rock.vy = -r.vy; }
        r.sub = 'FORKED FROM CHECKPOINT';
        setOrigin(r, 'resumed', actors()[uid]?.snapshotGen);
        world.splits.push({ x: parent.x, y: parent.y, born: now });
        break;
      }
      const r = spawnRock(uid, { ...info, state: 'resuming' }, saucerFor(ev.pool));
      r.sub = 'NEW SANDBOX';
      setOrigin(r, 'new');
      break;
    }
    case 'resume': {
      if (world.rocks.has(uid)) {
        touch(uid);
        thaw(world.rocks.get(uid), now); // a person decided: it wakes up
        break;
      }
      const from = world.vault.has(uid) ? vaultSlot([...world.vault].indexOf(uid)) : vaultPos();
      world.vault.delete(uid);
      const tok = beginAnim(anims, uid);
      const s = saucerFor(ev.pool);
      fly(from, s, 650, () => {
        if (!endAnim(anims, uid, tok)) return;
        const r = spawnRock(uid, { ...info, state: 'resuming' }, s);
        r.sub = 'RESUMING';
        setOrigin(r, 'resumed', actors()[uid]?.snapshotGen);
      });
      break;
    }
    case 'running': {
      touch(uid);
      let r = world.rocks.get(uid);
      if (!r) {
        r = spawnRock(uid, info, saucerFor(ev.pool));
        setOrigin(r, ev.startMs != null ? 'new' : 'resumed', info.snapshotGen);
      }
      r.worker = ev.worker;
      r.sub = subLabel({ ...info, state: 'running' });
      r.runSince ??= now; // it grows while it works (from launch, if it launched)
      break;
    }
    case 'suspend': {
      if (approvalFor(uid)) {
        // waiting for a human: Substrate froze it, but it stays in the field
        // with its pilot out holding up the request (see freeze)
        touch(uid);
        freeze(world.rocks.get(uid) || spawnRock(uid, info, null));
        break;
      }
      const r = world.rocks.get(uid);
      const tok = beginAnim(anims, uid);
      const store = () => { if (endAnim(anims, uid, tok)) world.vault.add(uid); };
      if (!r) {
        fly(saucerFor(ev.pool), vaultPos(), 800, store);
        break;
      }
      r.dying = true;
      const { x: sx, y: sy } = muzzleAt(world.ship);
      world.ship.flash = 1;
      const dur = 150 + Math.hypot(r.x - sx, r.y - sy) * 0.35; // a cannonball takes a moment to get there
      world.bullets.push({ x: sx, y: sy, from: { x: sx, y: sy }, target: r, t0: now, dur, hit: () => {
        explode(r, performance.now());
        world.rocks.delete(uid);
        fly({ x: r.x, y: r.y }, vaultPos(), 900, store);
      } });
      break;
    }
    case 'fail': {
      touch(uid);
      const r = world.rocks.get(uid);
      if (r) {
        r.failed = true;
        world.flashes.push({ x: r.x, y: r.y, born: now });
      }
      break;
    }
    case 'gone': {
      touch(uid);
      world.rocks.delete(uid);
      world.vault.delete(uid);
      break;
    }
  }
}

// forkParent says where a forked actor splits off from: its parent's rock, or
// the parent's capsule in the store.
function forkParent(uid) {
  const t = world.tour;
  if (!t?.active || t.kind !== 'fork' || (t.focus || []).length < 2) return null;
  const as = actors();
  if (as[uid]?.sessionId !== t.focus[1]) return null;
  const p = Object.values(as).find((a) => a.sessionId === t.focus[0]);
  if (!p) return null;
  const rock = world.rocks.get(p.uid);
  if (rock) return { x: rock.x, y: rock.y, rock };
  if (world.vault.has(p.uid)) return vaultSlot([...world.vault].indexOf(p.uid));
  return null;
}

function updateTour() {
  const t = world.state?.tour;
  world.tour = t && t.active ? t : null;
  const ids = new Set(world.tour?.focus || []);
  world.focusUids = Object.values(actors()).filter((a) => ids.has(a.sessionId)).map((a) => a.uid);
  if (world.tour && world.tour.chapter !== world.tourGw.chapter) {
    world.tourGw = { chapter: world.tour.chapter, llm: 0, tool: 0 };
  }
  world.tourLive = world.tour ? liveLine(world.tour, world.state, world.tourGw) : '';
}

// tourAgent is the agent behind the tour's main session, if any.
function tourAgent() {
  const sid = world.tour?.focus?.[0];
  return Object.values(actors()).find((a) => a.sessionId === sid)?.agent;
}

// showGateway draws a request agentgateway handled: a shield around the
// calling agent's asteroid and a beam through the gate to where it went.
function showGateway(ev, now) {
  const port = portFor(ev);
  if (ev.kind === 'llm') world.modelLabel = port.label;
  const rock = ev.direction === 'egress' ? attribute(ev, [...world.rocks.values()]) : null;
  const to = ev.kind === 'chat' ? saucerFor((pools()[0] || {}).name) : null;
  // labels for the same planet stack instead of overlapping
  const slot = world.gwFx.length % 6;
  const timing = ev.kind === 'llm' ? llmTiming(ev) : null;
  world.gwFx.push({
    rock, port: port.key, kind: ev.kind, label: fxLabel(ev), slot, born: now, timing,
    dur: timing ? timing.total + timing.travel + 150 : fxDuration(ev.durationMs),
    ingress: ev.direction === 'ingress', to: to && { x: to.x, y: to.y + 20 },
  });
  world.shipFlash = now;
  logPush({ src: 'gateway', ...ev, agent: ev.agent || rock?.agent }, GW);
  if (world.tour?.kind === 'gateway' && (ev.agent || rock?.agent) === tourAgent()) {
    if (ev.kind === 'llm') world.tourGw.llm++;
    if (ev.kind === 'tool') world.tourGw.tool++;
    world.tourLive = liveLine(world.tour, world.state, world.tourGw);
  }
}

// ── state reconciliation ───────────────────────────────────────────────────

// ── human in the loop ──────────────────────────────────────────────────────

// approvalFor is the request an actor's session is waiting on, if any.
function approvalFor(uid) {
  const sid = actors()[uid]?.sessionId;
  return sid ? world.approvals.get(sid) : undefined;
}

function freeze(r) {
  if (!r || r.frozen) return;
  r.frozen = true;
  r.saved = { vx: r.vx, vy: r.vy };
  r.vx = r.vy = 0;
  r.sub = 'FROZEN · WAITING FOR YOU · 0 CPU';
}

function thaw(r, now) {
  if (!r || !r.frozen) return;
  r.frozen = false;
  ({ vx: r.vx, vy: r.vy } = r.saved || { vx: 20, vy: 20 });
  r.runSince = now;
  r.sub = 'RESUMING';
}

// syncApprovals tracks the board from state; a new request gets a caption
// and a log line, and its ship freezes.
function syncApprovals(list) {
  const next = new Map((list || []).map((p) => [p.sessionId, p]));
  for (const [sid, p] of next) {
    if (world.approvals.has(sid)) continue;
    const tool = (p.tools?.[0]?.name || 'a tool').toUpperCase();
    const who = (p.agent || 'agent').toUpperCase();
    pushCaption(captions, { kind: 'hitl', caption: `${who} WANTS TO RUN ${tool}. SUBSTRATE FROZE IT WHILE IT WAITS FOR YOU: 0 CPU. PRESS Y OR X.` }, performance.now());
    world.log.push({ text: `${new Date().toISOString().slice(11, 19)} ⏸ ${who.replace(/^ASTEROIDS-DEMO-/, '')} ASKS: ${tool}`, color: ALIEN });
  }
  world.approvals = next;
  world.approvalByUid = new Map();
  for (const a of Object.values(actors())) {
    const p = next.get(a.sessionId);
    if (p) {
      world.approvalByUid.set(a.uid, p);
      if (a.state === 'suspended') freeze(world.rocks.get(a.uid));
    }
  }
}

const mismatchSince = new Map();
let seeded = false;

// reconcile corrects the stage to the state for actors no event is about to
// change — immediately on the first state, otherwise only once the two have
// disagreed for GRACE_MS (so a slow or held event is never pre-empted).
function reconcile() {
  const as = actors();
  const now = performance.now();
  const uids = new Set([...Object.keys(as), ...world.rocks.keys(), ...world.vault]);
  for (const uid of uids) {
    if (pending(queue, uid) || animating(anims, uid)) {
      mismatchSince.delete(uid);
      continue;
    }
    const a = as[uid];
    const waiting = a && a.state === 'suspended' && world.approvals.has(a.sessionId);
    const want = !a ? 'none' : ACTIVE.has(a.state) || waiting ? 'rock' : a.state === 'suspended' ? 'vault' : 'keep';
    const have = world.rocks.has(uid) ? 'rock' : world.vault.has(uid) ? 'vault' : 'none';
    if (want === 'keep') continue;
    if (want === have) {
      settled(mismatchSince, uid, false, now);
      if (want === 'rock') world.vault.delete(uid); // never both at once
      if (want === 'rock') {
        const r = world.rocks.get(uid);
        if (!r.dying && !r.frozen) { r.agent = a.agent; r.worker = a.worker; r.sub = subLabel(a); }
      }
      continue;
    }
    if (seeded && !settled(mismatchSince, uid, true, now)) continue;
    mismatchSince.delete(uid);
    if (want === 'rock') {
      world.vault.delete(uid);
      let r = world.rocks.get(uid);
      if (!r) {
        r = spawnRock(uid, a, null);
        setOrigin(r, a.snapshotGen > 0 ? 'resumed' : 'new', a.snapshotGen);
      }
      r.agent = a.agent;
      r.worker = a.worker;
      r.sub = subLabel(a);
      if (!r.runSince) r.runSince = performance.now();
    } else if (want === 'vault') {
      world.rocks.delete(uid);
      world.vault.add(uid);
    } else {
      world.rocks.delete(uid);
      world.vault.delete(uid);
    }
  }
  if (world.state?.connected) seeded = true;
}

// ── live stream ────────────────────────────────────────────────────────────

function connectStream() {
  const es = new EventSource('/events');
  es.onmessage = (m) => {
    const msg = JSON.parse(m.data);
    if (msg.type === 'state') {
      world.state = msg.state;
      world.idleCaption = msg.state.connected
        ? 'CONNECTED. EVERY ROCK IS A LIVE SANDBOX · EVERY CAPSULE A SNAPSHOT IN STORAGE.'
        : 'WAITING FOR SUBSTRATE…';
      syncApprovals(msg.state.approvals);
      reconcile();
      updateTour();
      updateHelp();
    } else if (msg.type === 'event') {
      schedule(queue, msg.event, performance.now());
    } else if (msg.type === 'gateway') {
      // shown on the same delay as lifecycle events so it lines up with them
      world.gwPending.push({ ev: msg.gateway, at: performance.now() + DELAY_MS });
    }
  };
  es.onerror = () => {
    if (world.state) world.state = { ...world.state, connected: false, error: 'app server unreachable' };
  };
}

// ── controls ───────────────────────────────────────────────────────────────

function toast(text) {
  world.toast = { text, at: performance.now() };
}

async function post(path, body = {}) {
  const r = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  let data = {};
  try { data = await r.json(); } catch {}
  return { ok: r.ok, data };
}

function updateHelp() {
  const w = world.state?.waves || {};
  const flight = w.inFlight ? `  ·  ${w.inFlight} CHAT${w.inFlight === 1 ? '' : 'S'} IN FLIGHT` : '';
  world.help = `N TOUR  ·  Y/X APPROVE  ·  T WAVE  ·  A AUTO WAVES ${w.auto ? 'ON' : 'OFF'}  ·  D DEMO AGENTS  ·  C SETTINGS  ·  F FULLSCREEN${flight}`;
}

const overlay = document.getElementById('connect');
const form = document.getElementById('connect-form');

async function openConnect() {
  try {
    const c = await (await fetch('/api/config')).json();
    for (const k of ['api', 'gateway', 'userId', 'namespace']) form.elements[k].value = c[k] || '';
    form.elements.token.value = '';
  } catch {}
  document.getElementById('connect-error').textContent = '';
  overlay.hidden = false;
  form.elements.api.focus();
}

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  const body = Object.fromEntries(new FormData(form).entries());
  const { ok, data } = await post('/api/config', body);
  if (ok) {
    overlay.hidden = true;
    world.rocks.clear();
    world.vault.clear();
    seeded = false;
    toast(`CONNECTED TO ${data.api}`);
  } else {
    document.getElementById('connect-error').textContent = data.error || 'connect failed';
  }
});
document.getElementById('connect-cancel').addEventListener('click', () => { overlay.hidden = true; });
document.getElementById('settings-btn').addEventListener('click', openConnect);
document.getElementById('tour-btn').addEventListener('click', async () => {
  const { ok, data } = await post('/api/tour', { action: world.tour ? 'next' : 'start' });
  if (!ok) toast(data.error || 'tour failed');
});

window.addEventListener('keydown', async (e) => {
  if (!overlay.hidden) {
    if (e.key === 'Escape') overlay.hidden = true;
    return;
  }
  const k = e.key.toLowerCase();
  if (k === 'n') {
    const { ok, data } = await post('/api/tour', { action: world.tour ? 'next' : 'start' });
    if (!ok) toast(data.error || 'tour failed');
  } else if (k === 'escape' && world.tour) {
    await post('/api/tour', { action: 'stop' });
  } else if (k === 'y' || k === 'x') {
    // answer the request a pilot is holding up (the oldest on screen)
    const p = [...world.approvals.values()][0];
    if (!p) { toast('NOBODY IS WAITING FOR A DECISION'); return; }
    const { ok, data } = await post('/api/approval', { sessionId: p.sessionId, taskId: p.taskId, approved: k === 'y' });
    toast(ok ? (k === 'y' ? 'APPROVED: IT RESUMES' : 'REJECTED: IT RESUMES AND IS TOLD NO') : data.error || 'decision failed');
  } else if (k === 't') {
    const { ok, data } = await post('/api/wave');
    toast(ok ? `WAVE: ${data.sent} CHAT${data.sent === 1 ? '' : 'S'} SENT` : (data.error || 'wave failed'));
  } else if (k === 'a') {
    const { data } = await post('/api/auto', { on: !world.state?.waves?.auto });
    toast(`AUTO WAVES ${data.auto ? 'ON' : 'OFF'}`);
  } else if (k === 'd') {
    toast('CREATING DEMO AGENTS…');
    const { ok, data } = await post('/api/demo-agents');
    toast(!ok ? data.error : data.created.length ? `CREATED ${data.created.join(', ')}` : 'AGENTS ALREADY READY');
  } else if (k === 'c') {
    openConnect();
  } else if (k === 'f') {
    document.fullscreenElement ? document.exitFullscreen() : document.documentElement.requestFullscreen();
  }
});

// ── animation loop ─────────────────────────────────────────────────────────

let last = performance.now();
function frame(now) {
  const dt = Math.min((now - last) / 1000, 0.1);
  last = now;
  for (const ev of due(queue, now)) show(ev, now);
  while (world.gwPending.length && world.gwPending[0].at <= now) showGateway(world.gwPending.shift().ev, now);
  world.gwFx = world.gwFx.filter((fx) => now - fx.born < fx.dur);
  world.captionLines = wrapLines(currentCaption(captions, now) ?? world.idleCaption, 96);

  const F = activeField(!!world.tour);
  for (const r of world.rocks.values()) {
    r.x += r.vx * dt;
    r.y += r.vy * dt;
    if (!r.frozen) r.rot += r.spin * dt; // rocks tumble; a frozen one holds still
    if (r.runSince && !r.dying && !r.frozen) r.scale = growScale(now - r.runSince);
    const w = F.x1 - F.x0, h = F.y1 - F.y0;
    if (r.x < F.x0) {
      if (world.tour) {
        // the card owns this area: glide out of it and bounce off its edge
        r.x += Math.min(F.x0 - r.x, Math.max(120 * dt, (F.x0 - r.x) * 4 * dt));
        r.vx = Math.abs(r.vx);
      } else {
        r.x += w;
      }
    }
    if (r.x > F.x1) r.x -= w;
    if (r.y < F.y0) r.y += h;
    if (r.y > F.y1) r.y -= h;
  }
  world.fragments = world.fragments.filter((f) => now - f.born < 1200);
  for (const f of world.fragments) { f.x += f.vx * dt; f.y += f.vy * dt; f.a += dt; }
  world.flashes = world.flashes.filter((f) => now - f.born < 600);
  world.splits = world.splits.filter((sp) => now - sp.born < 700);

  world.bullets = world.bullets.filter((b) => {
    const k = (now - b.t0) / b.dur;
    if (k >= 1) { b.hit(); return false; }
    b.x = b.from.x + (b.target.x - b.from.x) * k;
    b.y = b.from.y + (b.target.y - b.from.y) * k;
    return true;
  });
  world.capsules = world.capsules.filter((c) => {
    const k = Math.min((now - c.t0) / c.dur, 1);
    const e = k < 0.5 ? 2 * k * k : 1 - Math.pow(-2 * k + 2, 2) / 2;
    c.x = c.from.x + (c.to.x - c.from.x) * e;
    c.y = c.from.y + (c.to.y - c.from.y) * e - Math.sin(k * Math.PI) * 40;
    if (k >= 1) { c.done(); return false; }
    return true;
  });

  // the agentgateway planet drifts; it eases to its tour-time lane and back
  const goal = gatewayPos(now, !!world.tour);
  if (!world.gwPos) world.gwPos = goal;
  const e = Math.min(1, dt * 1.2);
  world.gwPos = { x: world.gwPos.x + (goal.x - world.gwPos.x) * e, y: world.gwPos.y + (goal.y - world.gwPos.y) * e };

  // the kagent robot swings its cannon toward the next suspend (or the nearest rock)
  const t = now / 1000;
  const ship = world.ship;
  const targetUid = nextSuspend(queue);
  let target = targetUid && world.rocks.get(targetUid);
  if (!target) {
    let best = Infinity;
    for (const r of world.rocks.values()) {
      const d = Math.hypot(r.x - ship.x, r.y - ship.y);
      if (d < best && !r.dying) { best = d; target = r; }
    }
  }
  const want = target ? cannonAim(ship, target) : -0.8 + 0.25 * Math.sin(t * 0.6); // idle: a slow sweep
  ship.target = target ? { x: target.x, y: target.y } : null; // the targeting laser
  ship.a += Math.max(-3 * dt, Math.min(3 * dt, want - ship.a));
  ship.flash = Math.max(0, ship.flash - dt * 4);

  draw(ctx, world, now);
  requestAnimationFrame(frame);
}

updateHelp();
connectStream();
requestAnimationFrame(frame);
