// Wires the live event stream to the stage. The backend owns the truth:
// rocks, capsules and explosions only start from real, displayed events, and
// each state message reconciles anything no event is about to change.
import {
  newQueue, schedule, due, pending, nextSuspend, rockShape, aimAngle, wrapLines, settled,
  newCaptions, pushCaption, currentCaption, newAnims, beginAnim, endAnim, animating,
} from './show.mjs';
import { setup, draw, saucerSpots, vaultSlot, FIELD } from './render.mjs';

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
  ship: { x: 500, y: 470, a: -Math.PI / 2, flash: 0 },
  captionLines: [],
  idleCaption: 'WAITING FOR SUBSTRATE…',
  help: '',
  toast: null,
};
const queue = newQueue();
const captions = newCaptions();
const anims = newAnims(); // per-actor animation tokens
window.__show = { world, queue }; // for debugging in the browser console

// ── helpers ────────────────────────────────────────────────────────────────

const pools = () => world.state?.snapshot?.pools || [];
const actors = () => world.state?.snapshot?.actors || {};

function saucerFor(pool) {
  const spots = saucerSpots(pools());
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
  const angle = Math.PI / 2 + (Math.random() - 0.5) * 1.6;
  const speed = 22 + Math.random() * 22;
  const r = {
    uid,
    x: from ? from.x : FIELD.x0 + Math.random() * (FIELD.x1 - FIELD.x0),
    y: from ? from.y + 30 : FIELD.y0 + Math.random() * (FIELD.y1 - FIELD.y0),
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
  for (let i = 0; i < r.shape.length; i++) {
    const [ax, ay] = r.shape[i];
    const [bx, by] = r.shape[(i + 1) % r.shape.length];
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

function show(ev, now) {
  pushCaption(captions, ev, now);
  const info = { ...actors()[ev.actorUid], agent: ev.agent, worker: ev.worker };
  const uid = ev.actorUid;
  switch (ev.kind) {
    case 'create': {
      touch(uid);
      world.vault.delete(uid);
      const r = spawnRock(uid, { ...info, state: 'resuming' }, saucerFor(ev.pool));
      r.sub = 'NEW SANDBOX';
      break;
    }
    case 'resume': {
      if (world.rocks.has(uid)) { touch(uid); break; }
      const from = world.vault.has(uid) ? vaultSlot([...world.vault].indexOf(uid)) : vaultPos();
      world.vault.delete(uid);
      const tok = beginAnim(anims, uid);
      const s = saucerFor(ev.pool);
      fly(from, s, 650, () => {
        if (!endAnim(anims, uid, tok)) return;
        const r = spawnRock(uid, { ...info, state: 'resuming' }, s);
        r.sub = 'RESUMING';
      });
      break;
    }
    case 'running': {
      touch(uid);
      const r = world.rocks.get(uid) || spawnRock(uid, info, saucerFor(ev.pool));
      r.worker = ev.worker;
      r.sub = subLabel({ ...info, state: 'running' });
      break;
    }
    case 'suspend': {
      const r = world.rocks.get(uid);
      const tok = beginAnim(anims, uid);
      const store = () => { if (endAnim(anims, uid, tok)) world.vault.add(uid); };
      if (!r) {
        fly(saucerFor(ev.pool), vaultPos(), 800, store);
        break;
      }
      r.dying = true;
      const sx = world.ship.x + Math.cos(world.ship.a) * 16;
      const sy = world.ship.y + Math.sin(world.ship.a) * 16;
      world.ship.flash = 1;
      world.bullets.push({ x: sx, y: sy, from: { x: sx, y: sy }, target: r, t0: now, dur: 220, hit: () => {
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

// ── state reconciliation ───────────────────────────────────────────────────

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
    const want = !a ? 'none' : ACTIVE.has(a.state) ? 'rock' : a.state === 'suspended' ? 'vault' : 'keep';
    const have = world.rocks.has(uid) ? 'rock' : world.vault.has(uid) ? 'vault' : 'none';
    if (want === 'keep') continue;
    if (want === have) {
      settled(mismatchSince, uid, false, now);
      if (want === 'rock') world.vault.delete(uid); // never both at once
      if (want === 'rock') {
        const r = world.rocks.get(uid);
        if (!r.dying) { r.agent = a.agent; r.worker = a.worker; r.sub = subLabel(a); }
      }
      continue;
    }
    if (seeded && !settled(mismatchSince, uid, true, now)) continue;
    mismatchSince.delete(uid);
    if (want === 'rock') {
      world.vault.delete(uid);
      const r = world.rocks.get(uid) || spawnRock(uid, a, null);
      r.agent = a.agent;
      r.worker = a.worker;
      r.sub = subLabel(a);
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
      reconcile();
      updateHelp();
    } else if (msg.type === 'event') {
      schedule(queue, msg.event, performance.now());
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
  world.help = `T WAVE  ·  A AUTO WAVES ${w.auto ? 'ON' : 'OFF'}  ·  D DEMO AGENTS  ·  C CONNECT  ·  F FULLSCREEN${flight}`;
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

window.addEventListener('keydown', async (e) => {
  if (!overlay.hidden) {
    if (e.key === 'Escape') overlay.hidden = true;
    return;
  }
  const k = e.key.toLowerCase();
  if (k === 't') {
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
  world.captionLines = wrapLines(currentCaption(captions, now) ?? world.idleCaption, 96);

  for (const r of world.rocks.values()) {
    r.x += r.vx * dt;
    r.y += r.vy * dt;
    r.rot += r.spin * dt;
    const w = FIELD.x1 - FIELD.x0, h = FIELD.y1 - FIELD.y0;
    if (r.x < FIELD.x0) r.x += w;
    if (r.x > FIELD.x1) r.x -= w;
    if (r.y < FIELD.y0) r.y += h;
    if (r.y > FIELD.y1) r.y -= h;
  }
  world.fragments = world.fragments.filter((f) => now - f.born < 1200);
  for (const f of world.fragments) { f.x += f.vx * dt; f.y += f.vy * dt; f.a += dt; }
  world.flashes = world.flashes.filter((f) => now - f.born < 600);

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

  // autopilot: drift on a slow figure-eight, turn toward the next suspend
  const t = now / 1000;
  const ship = world.ship;
  ship.x = 505 + 300 * Math.sin(t * 0.09);
  ship.y = 480 + 70 * Math.sin(t * 0.18);
  const targetUid = nextSuspend(queue);
  let target = targetUid && world.rocks.get(targetUid);
  if (!target) {
    let best = Infinity;
    for (const r of world.rocks.values()) {
      const d = Math.hypot(r.x - ship.x, r.y - ship.y);
      if (d < best && !r.dying) { best = d; target = r; }
    }
  }
  if (target) {
    let diff = aimAngle(ship, target) - ship.a;
    diff = Math.atan2(Math.sin(diff), Math.cos(diff));
    ship.a += Math.max(-3 * dt, Math.min(3 * dt, diff));
  }
  ship.flash = Math.max(0, ship.flash - dt * 4);

  draw(ctx, world, now);
  requestAnimationFrame(frame);
}

updateHelp();
connectStream();
requestAnimationFrame(frame);
