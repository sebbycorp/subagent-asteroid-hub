// Vector drawing for the 1280×720 stage. Everything is white strokes on black.

import { streamDots } from './show.mjs';

export const W = 1280;
export const H = 720;
export const FIELD = { x0: 20, x1: 990, y0: 150, y1: 610 };
export const VAULT = { x: 1010, y: 150, w: 250, h: 450 };
// Agentgateway is a planet floating through the field inside one shield;
// every call into or out of a sandbox (LLM, MCP, A2A) goes through it.
export const PLANET_R = 62;
export const SHIELD_R = 88;

// gatewayPos is where the planet drifts to at time t: a slow loop through the
// field, kept right of the tour card while the tour is open.
export function gatewayPos(t, tourOpen) {
  const s = t / 1000;
  const [cx, ax] = tourOpen ? [685, 115] : [465, 330];
  return { x: cx + ax * Math.sin(s * 0.05), y: 362 + 118 * Math.sin(s * 0.09 + 1) };
}

// edgeToward is the point on the shield facing p.
export function edgeToward(c, p) {
  const a = Math.atan2(p.y - c.y, p.x - c.x);
  return { x: c.x + Math.cos(a) * SHIELD_R, y: c.y + Math.sin(a) * SHIELD_R };
}

const FONT = 'ui-monospace, "SF Mono", Menlo, Consolas, monospace';
const INK = '#fff';
const DIM = 'rgba(255,255,255,0.45)';
const RED = '#ff5a4f';
export const NEW = '#4fd8ff'; // a new session, cloned from the golden snapshot
export const RESUMED = '#ffbf47'; // a session restored from its own snapshot
export const GW = '#7dffb0'; // agentgateway

function originColor(r) {
  return r.origin === 'new' ? NEW : r.origin === 'resumed' ? RESUMED : INK;
}

export function setup(canvas) {
  const ctx = canvas.getContext('2d');
  function fit() {
    const dpr = window.devicePixelRatio || 1;
    canvas.width = Math.round(window.innerWidth * dpr);
    canvas.height = Math.round(window.innerHeight * dpr);
    const s = Math.min(canvas.width / W, canvas.height / H);
    const ox = (canvas.width - W * s) / 2;
    const oy = (canvas.height - H * s) / 2;
    ctx.setTransform(s, 0, 0, s, ox, oy);
  }
  window.addEventListener('resize', fit);
  fit();
  return ctx;
}

function text(ctx, str, x, y, { size = 14, align = 'left', color = INK, spacing = 2 } = {}) {
  ctx.font = `${size}px ${FONT}`;
  ctx.letterSpacing = `${spacing}px`;
  ctx.textAlign = align;
  ctx.fillStyle = color;
  ctx.fillText(String(str).toUpperCase(), x, y);
}

function stroke(ctx, color = INK, width = 1.5) {
  ctx.strokeStyle = color;
  ctx.lineWidth = width;
  ctx.stroke();
}

export function poly(ctx, pts, x, y, rot = 0, scale = 1) {
  ctx.beginPath();
  pts.forEach(([px, py], i) => {
    const c = Math.cos(rot), s = Math.sin(rot);
    const X = x + (px * c - py * s) * scale;
    const Y = y + (px * s + py * c) * scale;
    i ? ctx.lineTo(X, Y) : ctx.moveTo(X, Y);
  });
  ctx.closePath();
}

// The kagent worker-pool ship is drawn this much bigger than its base size.
export const SAUCER_SCALE = 1.5;

// saucerSpots places each worker pool's ship along the top; each glides side
// to side over time.
export function saucerSpots(pools, t = 0) {
  const n = Math.max(pools.length, 1);
  const span = FIELD.x1 - FIELD.x0;
  const half = 70 * SAUCER_SCALE;
  const amp = Math.max(0, Math.min(330, span / (2 * n) - half - 4));
  return pools.map((p, i) => ({
    pool: p,
    x: FIELD.x0 + (span * (i + 1)) / (n + 1) + amp * Math.sin((t / 1000) * 0.12 + i * 1.7),
    y: 96,
  }));
}

// The kagent mark, drawn in the worker-pool ship's dome.
const KAGENT = typeof Image === 'undefined' ? {} : Object.assign(new Image(), { src: 'kagent-mark.png' });

// drawSaucer draws a worker pool as kagent's domed mothership: the kagent
// robot in the dome, chasing rim lights, one porthole per worker (lit while
// hosting an actor), and a tractor beam when it launches an asteroid.
function drawSaucer(ctx, s, busyWorkers, now, beamAt) {
  const { x, y } = s;
  const S = SAUCER_SCALE;
  ctx.save();
  ctx.translate(x, y);
  ctx.scale(S, S);
  // tractor beam
  const bk = (now - (beamAt ?? -1e9)) / 800;
  if (bk >= 0 && bk < 1) {
    const prev = ctx.globalAlpha;
    ctx.globalAlpha = prev * (1 - bk) * 0.7;
    ctx.beginPath();
    ctx.moveTo(-16, 20); ctx.lineTo(-44, 70);
    ctx.moveTo(16, 20); ctx.lineTo(44, 70);
    for (let i = 1; i <= 5; i++) {
      const t = ((i / 5 + bk * 2) % 1), yy = 20 + t * 50, w = 16 + t * 28;
      ctx.moveTo(-w, yy); ctx.lineTo(w, yy);
    }
    stroke(ctx, NEW, 1 / S);
    ctx.globalAlpha = prev;
  }
  // dome with the kagent robot inside
  ctx.beginPath();
  ctx.ellipse(0, -9, 22, 19, 0, Math.PI, 0);
  stroke(ctx, INK, 1.4 / S);
  if (KAGENT.complete && KAGENT.naturalWidth) ctx.drawImage(KAGENT, -11, -27, 22, 22);
  // upper body, rim, skirt
  ctx.beginPath();
  ctx.ellipse(0, -4, 44, 9, 0, Math.PI, 0);
  stroke(ctx, INK, 1.4 / S);
  ctx.beginPath();
  ctx.ellipse(0, 0, 68, 11, 0, 0, Math.PI * 2);
  stroke(ctx, INK, 1.6 / S);
  ctx.beginPath();
  ctx.moveTo(-48, 8); ctx.lineTo(-30, 19); ctx.lineTo(30, 19); ctx.lineTo(48, 8);
  stroke(ctx, INK, 1.4 / S);
  // chasing rim lights (front half bright)
  for (let i = 0; i < 16; i++) {
    const a = (i / 16) * Math.PI * 2 + now / 900;
    ctx.beginPath();
    ctx.arc(Math.cos(a) * 58, Math.sin(a) * 6, Math.sin(a) > 0 ? 1.4 : 0.9, 0, Math.PI * 2);
    ctx.fillStyle = Math.sin(a) > 0 ? INK : DIM;
    ctx.fill();
  }
  // portholes: one per worker
  const workers = s.pool.workers || [];
  const gap = 13;
  workers.forEach((w, i) => {
    const px = -((workers.length - 1) * gap) / 2 + i * gap;
    ctx.beginPath();
    ctx.arc(px, 13.5, 3.4, 0, Math.PI * 2);
    if (busyWorkers.has(w)) { ctx.fillStyle = INK; ctx.fill(); }
    stroke(ctx, INK, 1 / S);
  });
  ctx.restore();
  text(ctx, s.pool.name, x, y + 42, { size: 10, align: 'center', spacing: 2 });
  text(ctx, `KAGENT WORKER POOL · ${workers.length} WORKER${workers.length === 1 ? '' : 'S'}`, x, y + 54, { size: 7, align: 'center', color: DIM, spacing: 1 });
}

function drawRock(ctx, r) {
  const sc = r.scale || 1;
  poly(ctx, r.shape, r.x, r.y, r.rot, sc);
  stroke(ctx, r.failed ? RED : originColor(r));
  // dashed leader to the label
  const lx = r.x + 34 * sc, ly = r.y - 22 * sc;
  ctx.setLineDash([2, 3]);
  ctx.beginPath();
  ctx.moveTo(r.x + 20 * sc, r.y - 10 * sc);
  ctx.lineTo(lx - 4, ly + 4);
  stroke(ctx, DIM, 1);
  ctx.setLineDash([]);
  text(ctx, r.agent, lx, ly, { size: 10, spacing: 1 });
  text(ctx, r.sub, lx, ly + 13, { size: 9, spacing: 1, color: DIM });
  if (r.tag) text(ctx, r.tag, lx, ly + 25, { size: 8, spacing: 1, color: originColor(r) });
}

export function drawShip(ctx, ship) {
  const pts = [[16, 0], [-11, -9], [-6, 0], [-11, 9]];
  poly(ctx, pts, ship.x, ship.y, ship.a);
  stroke(ctx);
  // engine flame
  const f = 7 + Math.random() * 7;
  poly(ctx, [[-7, -3.5], [-7 - f, 0], [-7, 3.5]], ship.x, ship.y, ship.a);
  stroke(ctx, RESUMED, 1);
  if (ship.flash > 0) {
    ctx.beginPath();
    ctx.moveTo(ship.x - Math.cos(ship.a) * 8, ship.y - Math.sin(ship.a) * 8);
    ctx.lineTo(ship.x - Math.cos(ship.a) * 18, ship.y - Math.sin(ship.a) * 18);
    stroke(ctx, INK, 1);
  }
}

export function vaultSlot(i) {
  const col = i % 10, row = Math.floor(i / 10);
  return { x: VAULT.x + 18 + col * 22, y: VAULT.y + 64 + row * 16 };
}

function capsule(ctx, x, y) {
  ctx.beginPath();
  ctx.roundRect(x - 8, y - 4, 16, 8, 4);
  stroke(ctx, INK, 1.2);
}

// fit trims a line to a pixel width at the current font.
function fit(ctx, str, width) {
  if (ctx.measureText(str).width <= width) return str;
  while (str.length > 1 && ctx.measureText(str + '…').width > width) str = str.slice(0, -1);
  return str + '…';
}

const LOG_TOP = VAULT.y + 172;

function drawLog(ctx, lines) {
  ctx.beginPath();
  ctx.moveTo(VAULT.x + 10, LOG_TOP - 14);
  ctx.lineTo(VAULT.x + VAULT.w - 10, LOG_TOP - 14);
  stroke(ctx, DIM, 1);
  text(ctx, 'LOG', VAULT.x + 12, LOG_TOP, { size: 10, spacing: 2 });
  const rows = Math.floor((VAULT.y + VAULT.h - 62 - (LOG_TOP + 14)) / 12);
  const shown = lines.slice(-rows);
  ctx.font = `8px ${FONT}`;
  ctx.letterSpacing = '0.5px';
  shown.forEach((l, i) => {
    const y = LOG_TOP + 16 + i * 12;
    ctx.fillStyle = l.color || DIM;
    ctx.textAlign = 'left';
    ctx.fillText(fit(ctx, l.text, VAULT.w - 24), VAULT.x + 12, y);
  });
}

function drawVault(ctx, count) {
  ctx.beginPath();
  ctx.rect(VAULT.x, VAULT.y, VAULT.w, VAULT.h);
  stroke(ctx);
  text(ctx, 'SNAPSHOT STORE', VAULT.x + VAULT.w / 2, VAULT.y + 24, { size: 12, align: 'center' });
  text(ctx, 'S3 · SUSPENDED ACTORS', VAULT.x + VAULT.w / 2, VAULT.y + 42, { size: 9, align: 'center', color: DIM });
  const shown = Math.min(count, 200);
  for (let i = 0; i < shown; i++) {
    const s = vaultSlot(i);
    if (s.y > LOG_TOP - 24) break; // capsules above, the log below
    capsule(ctx, s.x, s.y);
  }
  text(ctx, `${count} ACTOR${count === 1 ? '' : 'S'} STORED`, VAULT.x + VAULT.w / 2, VAULT.y + VAULT.h - 34, { size: 12, align: 'center' });
  text(ctx, '0 CPU · 0 MEMORY', VAULT.x + VAULT.w / 2, VAULT.y + VAULT.h - 16, { size: 10, align: 'center', color: DIM });
}

function drawTopBar(ctx, st, busy, running, stored) {
  const pools = st.snapshot?.pools || [];
  const workers = pools.reduce((n, p) => n + (p.workers || []).length, 0);
  const s = st.stats || {};
  text(ctx, `${st.env || '—'} · POOLS ${pools.length} · BUSY ${busy}/${workers} · RUNNING ${running} · STORED ${stored}`, 20, 30, { size: 12, spacing: 1.5 });
  const restore = s.lastRestoreMs != null ? `${s.lastRestoreMs} MS` : '—';
  const suspend = s.lastSuspendMs == null ? '—' : s.lastSuspendMs <= 10 ? 'INSTANT' : `${s.lastSuspendMs} MS`;
  text(ctx, `RESTORE ${restore} · TO SUSPEND ${suspend} · SUSPENDS ${s.suspends || 0}`, W - 20, 30, { size: 12, align: 'right', spacing: 1.5 });
  ctx.beginPath();
  ctx.moveTo(0, 44);
  ctx.lineTo(W, 44);
  stroke(ctx, DIM, 1);
}

function drawCaption(ctx, lines, note, help) {
  ctx.beginPath();
  ctx.moveTo(0, 622);
  ctx.lineTo(W, 622);
  stroke(ctx, DIM, 1);
  lines.forEach((l, i) => text(ctx, l, W / 2, 652 + i * 26, { size: 16, align: 'center', spacing: 3 }));
  if (note) text(ctx, note, W / 2, 616, { size: 11, align: 'center', color: RED });
  text(ctx, help, W / 2, 712, { size: 9, align: 'center', color: DIM });
}

export function draw(ctx, world, now) {
  ctx.save();
  ctx.fillStyle = '#000';
  ctx.fillRect(-W, -H, W * 3, H * 3);
  ctx.restore();
  const st = world.state || {};
  const dim = st.connected === false;
  ctx.globalAlpha = dim ? 0.35 : 1;

  const busy = new Set();
  for (const r of world.rocks.values()) if (r.worker && !r.dying) busy.add(r.worker);
  drawTopBar(ctx, st, busy.size, world.rocks.size, world.vault.size);
  for (const s of saucerSpots(st.snapshot?.pools || [], now)) drawSaucer(ctx, s, busy, now, world.beamAt?.[s.pool.name]);
  drawVault(ctx, world.vault.size);
  drawLog(ctx, world.log || []);
  drawGatewayPlanet(ctx, world, now);
  drawLegend(ctx);
  for (const r of world.rocks.values()) drawRock(ctx, r);
  drawGatewayFx(ctx, world, now);
  for (const f of world.fragments) {
    ctx.globalAlpha = Math.max(0, 1 - (now - f.born) / 1200) * (dim ? 0.35 : 1);
    ctx.beginPath();
    ctx.moveTo(f.x, f.y);
    ctx.lineTo(f.x + Math.cos(f.a) * f.len, f.y + Math.sin(f.a) * f.len);
    stroke(ctx);
  }
  ctx.globalAlpha = dim ? 0.35 : 1;
  for (const b of world.bullets) {
    ctx.beginPath();
    ctx.arc(b.x, b.y, 1.8, 0, Math.PI * 2);
    ctx.fillStyle = INK;
    ctx.fill();
  }
  for (const c of world.capsules) capsule(ctx, c.x, c.y);
  for (const f of world.flashes) {
    const k = (now - f.born) / 600;
    ctx.beginPath();
    ctx.arc(f.x, f.y, 10 + k * 40, 0, Math.PI * 2);
    ctx.globalAlpha = Math.max(0, 1 - k);
    stroke(ctx, RED, 2);
    ctx.globalAlpha = dim ? 0.35 : 1;
  }
  drawFocus(ctx, world, now);
  drawShip(ctx, world.ship);
  drawCaption(ctx, world.captionLines, st.waves?.note, world.help);
  ctx.globalAlpha = 1;

  if (world.tour?.active) drawTourCard(ctx, world.tour, world.tourLive, now);
  if (dim) {
    text(ctx, 'LINK LOST · RETRYING', W / 2, H / 2, { size: 28, align: 'center', spacing: 6 });
    if (st.error) text(ctx, st.error.slice(0, 110), W / 2, H / 2 + 30, { size: 11, align: 'center', color: DIM, spacing: 1 });
  }
  if (world.toast && now - world.toast.at < 2500) {
    text(ctx, world.toast.text, W / 2, 140, { size: 12, align: 'center' });
  }
}

// brackets marks something the tour is talking about.
function brackets(ctx, x, y, r, now) {
  const k = 4 + Math.sin(now / 180) * 2;
  const R = r + k, L = 8;
  ctx.beginPath();
  for (const [sx, sy] of [[-1, -1], [1, -1], [1, 1], [-1, 1]]) {
    ctx.moveTo(x + sx * R, y + sy * (R - L));
    ctx.lineTo(x + sx * R, y + sy * R);
    ctx.lineTo(x + sx * (R - L), y + sy * R);
  }
  stroke(ctx, INK, 1.5);
}

function drawFocus(ctx, world, now) {
  for (const uid of world.focusUids || []) {
    const r = world.rocks.get(uid);
    if (r) brackets(ctx, r.x, r.y, 28, now);
    else if (world.vault.has(uid)) {
      const s = vaultSlot([...world.vault].indexOf(uid));
      brackets(ctx, s.x, s.y, 8, now);
    }
  }
  for (const sp of world.splits || []) {
    const k = (now - sp.born) / 700;
    if (k > 1) continue;
    ctx.globalAlpha = 1 - k;
    ctx.beginPath();
    ctx.arc(sp.x, sp.y, 12 + k * 46, 0, Math.PI * 2);
    stroke(ctx, INK, 1.2);
    ctx.globalAlpha = 1;
  }
}

export const CARD = { x: 30, y: 150, w: 450 };

// wrapPx splits text into lines no wider than width at the given style.
function wrapPx(ctx, str, width, size, spacing) {
  ctx.font = `${size}px ${FONT}`;
  ctx.letterSpacing = `${spacing}px`;
  const out = [];
  let line = '';
  for (const word of String(str).toUpperCase().split(/\s+/)) {
    const next = line ? `${line} ${word}` : word;
    if (line && ctx.measureText(next).width > width) {
      out.push(line);
      line = word;
    } else line = next;
  }
  if (line) out.push(line);
  return out;
}

function drawTourCard(ctx, tour, live, now) {
  const x = CARD.x + 18;
  const inner = CARD.w - 36;
  // lay out first (wrapped to the card), then size the card to fit
  const rows = [];
  const add = (str, size, spacing, color, gapAfter = 0) => {
    for (const l of wrapPx(ctx, str, inner, size, spacing)) rows.push({ l, size, spacing, color, h: size + 6 });
    if (gapAfter) rows.push({ l: '', h: gapAfter });
  };
  add(`TOUR ${tour.chapter}/${tour.total} · ${tour.title}`, 12, 2, INK, 8);
  add((tour.body || []).join(' '), 10, 1, DIM, 10);
  if (tour.doing) add(`${Math.floor(now / 400) % 2 ? '▸' : ' '} ${tour.doing}`, 11, 1, INK, 4);
  if (tour.result) add(tour.result, 11, 1, INK, 4);
  if (tour.error) add(tour.error, 11, 1, RED, 4);
  if (live) add(live, 10, 1, GW, 4);
  const h = 20 + rows.reduce((n, r) => n + r.h, 0) + 26;
  ctx.save();
  ctx.fillStyle = 'rgba(0,0,0,0.86)';
  ctx.fillRect(CARD.x, CARD.y, CARD.w, h);
  ctx.beginPath();
  ctx.rect(CARD.x, CARD.y, CARD.w, h);
  stroke(ctx, INK, 1.5);
  ctx.restore();
  let y = CARD.y + 24;
  for (const r of rows) {
    if (r.l) text(ctx, r.l, x, y, { size: r.size, spacing: r.spacing, color: r.color });
    y += r.h;
  }
  text(ctx, tour.busy ? 'WORKING ON THE CLUSTER…' : tour.chapter >= tour.total ? 'N FINISH · ESC END TOUR' : 'N NEXT CHAPTER · ESC END TOUR',
    CARD.x + CARD.w - 18, CARD.y + h - 12, { size: 9, align: 'right', color: DIM });
}

// activeField is where rocks may fly: while the tour card is open the field
// steps aside so the card never hides the action.
export function activeField(tourOpen) {
  return tourOpen ? { ...FIELD, x0: CARD.x + CARD.w + 30 } : FIELD;
}

function drawLegend(ctx) {
  const items = [[NEW, '● NEW SESSION'], [RESUMED, '● RESUMED'], [GW, '⬡ AGENTGATEWAY']];
  let x = 20;
  for (const [c, label] of items) {
    text(ctx, label, x, 614, { size: 9, spacing: 1, color: c });
    ctx.font = `9px ${FONT}`;
    x += ctx.measureText(label).width + label.length + 18;
  }
}

function hexagon(ctx, x, y, r, rot = 0) {
  ctx.beginPath();
  for (let i = 0; i < 6; i++) {
    const a = Math.PI / 6 + (i * Math.PI) / 3 + rot;
    const X = x + Math.cos(a) * r, Y = y + Math.sin(a) * r;
    i ? ctx.lineTo(X, Y) : ctx.moveTo(X, Y);
  }
  ctx.closePath();
}

// The agentgateway.dev mark, drawn inside the planet.
const LOGO = typeof Image === 'undefined' ? {} : Object.assign(new Image(), { src: 'agentgateway-mark.svg' });

// drawGatewayPlanet draws agentgateway: a ringed planet inside one hexagonal
// shield that flares as calls pass, labelled with kagent's models. Slightly
// dimmed until the gateway has reported a call.
function drawGatewayPlanet(ctx, world, now) {
  const seen = world.state?.gateway?.seen;
  const prev = ctx.globalAlpha;
  ctx.globalAlpha = prev * (seen ? 1 : 0.6);
  const { x, y } = world.gwPos || gatewayPos(now, false);
  const flare = Math.max(0, 1 - (now - (world.shipFlash || -1e9)) / 700);
  // the one shield
  ctx.save();
  ctx.globalAlpha *= 0.55 + flare * 0.45;
  hexagon(ctx, x, y, SHIELD_R + flare * 5, now / 5000); // the shield turns…
  stroke(ctx, GW, 1.4 + flare * 1.6);
  ctx.globalAlpha *= 0.5;
  hexagon(ctx, x, y, SHIELD_R - 6, -now / 3500); // …its inner layer the other way
  stroke(ctx, GW, 1);
  ctx.restore();
  // the planet: body, faint bands, a tilted ring, the agentgateway mark inside
  ctx.beginPath();
  ctx.arc(x, y, PLANET_R, 0, Math.PI * 2);
  stroke(ctx, INK, 1.8);
  for (const [dy, rx] of [[-46, 41], [46, 41]]) {
    ctx.beginPath();
    ctx.ellipse(x, y + dy, rx, 5, 0, 0, Math.PI);
    stroke(ctx, DIM, 1);
  }
  // the ring slowly precesses, with a moon riding it
  const tilt = -0.32 + 0.12 * Math.sin(now / 4000);
  ctx.beginPath();
  ctx.ellipse(x, y, PLANET_R * 1.5, PLANET_R * 0.3, tilt, 0.12 * Math.PI, 0.88 * Math.PI, true);
  stroke(ctx, INK, 1.4);
  const ma = now / 1600;
  const mx = Math.cos(ma) * PLANET_R * 1.5, my = Math.sin(ma) * PLANET_R * 0.3;
  const behind = Math.sin(ma) < 0 && Math.abs(mx) < PLANET_R;
  if (!behind) {
    ctx.beginPath();
    ctx.arc(x + mx * Math.cos(tilt) - my * Math.sin(tilt), y + mx * Math.sin(tilt) + my * Math.cos(tilt), 4, 0, Math.PI * 2);
    stroke(ctx, INK, 1.2);
  }
  if (LOGO.complete && LOGO.naturalWidth) {
    const size = PLANET_R * 1.3; // small mark at the planet's core
    ctx.drawImage(LOGO, x - size / 2, y - size / 2, size, size);
  }
  text(ctx, seen ? 'AGENTGATEWAY' : 'AGENTGATEWAY · NO CALLS YET', x, y - SHIELD_R - 12, { size: 11, align: 'center', spacing: 3, color: GW });
  // the models kagent uses
  const models = world.state?.snapshot?.models || [];
  text(ctx, 'LLM · MCP · A2A', x, y + SHIELD_R + 14, { size: 8, align: 'center', spacing: 2, color: DIM });
  models.slice(0, 3).forEach((m, i) => text(ctx, m, x, y + SHIELD_R + 28 + i * 12, { size: 9, align: 'center', spacing: 1, color: INK }));
  ctx.globalAlpha = prev;
}

// fxPath is the route a call takes: from the asteroid to the shield, or for a
// chat coming in, from the shield to the worker pool. A gentle arc.
function fxPath(fx, gw) {
  const end = fx.rock ? { x: fx.rock.x, y: fx.rock.y } : fx.to || { x: gw.x + 160, y: gw.y - 60 };
  const edge = edgeToward(gw, end);
  const [a, b] = fx.ingress ? [edge, end] : [end, edge];
  const mx = (a.x + b.x) / 2, my = (a.y + b.y) / 2;
  const nx = -(b.y - a.y), ny = b.x - a.x, len = Math.hypot(nx, ny) || 1;
  return [a, { x: mx + (nx / len) * 26, y: my + (ny / len) * 26 }, b];
}

function along(pts, k) {
  const segs = [];
  let total = 0;
  for (let i = 1; i < pts.length; i++) {
    const d = Math.hypot(pts[i].x - pts[i - 1].x, pts[i].y - pts[i - 1].y);
    segs.push(d);
    total += d;
  }
  let t = k * total;
  for (let i = 0; i < segs.length; i++) {
    if (t <= segs[i]) {
      const f = segs[i] ? t / segs[i] : 0;
      return { x: pts[i].x + (pts[i + 1].x - pts[i].x) * f, y: pts[i].y + (pts[i + 1].y - pts[i].y) * f };
    }
    t -= segs[i];
  }
  return pts[pts.length - 1];
}

function dotAt(ctx, pt, r, color) {
  ctx.beginPath();
  ctx.arc(pt.x, pt.y, r, 0, Math.PI * 2);
  ctx.fillStyle = color;
  ctx.fill();
}

// lightning draws a crackling bolt along a path (fresh jitter every frame).
function lightning(ctx, pts) {
  ctx.beginPath();
  ctx.moveTo(pts[0].x, pts[0].y);
  for (let i = 1; i < pts.length; i++) {
    const a = pts[i - 1], b = pts[i];
    const n = Math.max(3, Math.round(Math.hypot(b.x - a.x, b.y - a.y) / 22));
    for (let j = 1; j <= n; j++) {
      const f = j / n, jit = j === n ? 0 : (Math.random() - 0.5) * 14;
      const nx = -(b.y - a.y), ny = b.x - a.x, len = Math.hypot(nx, ny) || 1;
      ctx.lineTo(a.x + (b.x - a.x) * f + (nx / len) * jit, a.y + (b.y - a.y) * f + (ny / len) * jit);
    }
  }
  stroke(ctx, GW, 1.6);
}

function planetRing(ctx, p, k) {
  ctx.beginPath();
  ctx.arc(p.x, p.y, p.r + 3 + k * 18, 0, Math.PI * 2);
  ctx.globalAlpha *= 1 - k;
  stroke(ctx, GW, 1.6);
}

// drawGatewayFx animates each request agentgateway handled, by kind:
//  model call: a packet train to the planet, it pulses, a longer train back
//  tool call:  a crackling bolt to the planet, a result box back
//  chat in:    a comet from the planet into the worker pool
function drawGatewayFx(ctx, world, now) {
  const base = ctx.globalAlpha;
  for (const fx of world.gwFx || []) {
    const k = (now - fx.born) / fx.dur;
    if (k < 0 || k > 1) continue;
    const fade = 1 - Math.max(0, (k - 0.8) / 0.2);
    const gw = world.gwPos || gatewayPos(now, false);
    const pts = fxPath(fx, gw);
    const back = [...pts].reverse();
    const p = { x: gw.x, y: gw.y, r: PLANET_R };
    ctx.save();
    // faint guide line
    ctx.globalAlpha = base * fade * 0.22;
    ctx.setLineDash([2, 6]);
    ctx.beginPath();
    pts.forEach((pt, i) => (i ? ctx.lineTo(pt.x, pt.y) : ctx.moveTo(pt.x, pt.y)));
    stroke(ctx, GW, 1);
    ctx.setLineDash([]);
    ctx.globalAlpha = base * fade;
    if (fx.kind === 'llm' && fx.timing) {
      // a constant stream, replayed with the call's real timing: the request
      // flows in until the first token, then the answer streams back
      const tm = fx.timing, t = now - fx.born;
      for (const d of streamDots(t, 0, tm.ttft, tm.reqGap, tm.travel)) dotAt(ctx, along(pts, d), 2.4, GW);
      if (t < tm.ttft) { ctx.save(); planetRing(ctx, p, ((t / 700) % 1)); ctx.restore(); } // thinking
      for (const d of streamDots(t, tm.ttft, tm.total, tm.respGap, tm.travel)) dotAt(ctx, along(back, d), 1.8, INK);
    } else if (fx.kind === 'tool') {
      if (k < 0.35 && Math.floor(now / 60) % 3 !== 0) lightning(ctx, pts);
      if (k > 0.3 && k < 0.55) { ctx.save(); planetRing(ctx, p, (k - 0.3) / 0.25); ctx.restore(); }
      if (k > 0.45) {
        const b = along(back, Math.min(1, (k - 0.45) / 0.45));
        ctx.beginPath();
        ctx.rect(b.x - 4, b.y - 4, 8, 8);
        stroke(ctx, GW, 1.4);
      }
    } else {
      const head = Math.min(1, k / 0.75);
      for (let i = 12; i >= 0; i--) {
        const t = head - i * 0.018;
        if (t <= 0) continue;
        ctx.globalAlpha = base * fade * (1 - i / 13);
        dotAt(ctx, along(pts, t), i === 0 ? 3.4 : 2.2 - i * 0.12, i === 0 ? INK : GW);
      }
      ctx.globalAlpha = base * fade;
    }
    // the shield: the call left this sandbox only through the gateway
    if (fx.rock && !fx.ingress) {
      const sc = fx.rock.scale || 1;
      hexagon(ctx, fx.rock.x, fx.rock.y, 34 * sc + Math.sin(k * Math.PI * 6) * 2);
      stroke(ctx, GW, 1.8);
    }
    // a running feed of calls beside the planet (on whichever side has room)
    const right = gw.x < 600;
    text(ctx, fx.label, gw.x + (right ? SHIELD_R + 16 : -SHIELD_R - 16), gw.y - 30 + (fx.slot || 0) * 12,
      { size: 8, spacing: 1, color: GW, align: right ? 'left' : 'right' });
    ctx.restore();
  }
  ctx.globalAlpha = base;
}
