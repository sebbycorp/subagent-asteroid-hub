// Vector drawing for the 1280×720 stage. Everything is white strokes on black.

import { streamDots } from './show.mjs';

export const W = 1280;
export const H = 720;
export const FIELD = { x0: 20, x1: 990, y0: 150, y1: 610 };
export const VAULT = { x: 1010, y: 150, w: 250, h: 450 };
// Agentgateway is a planet floating through the field inside one shield;
// every call into or out of a sandbox (LLM, MCP, A2A) goes through it.
export const PLANET_R = 31;
export const SHIELD_R = 44;

// gatewayPos is where the planet drifts to at time t: a slow loop through the
// field, kept right of the tour card while the tour is open.
export function gatewayPos(t, tourOpen) {
  const a = orbitPhase(t);
  const [cx, ax] = tourOpen ? [685, 115] : [465, 330];
  return { x: cx + ax * Math.sin(a), y: 362 + 118 * Math.cos(a) };
}

// The planet and the fighter share one slow orbit, on opposite sides of it:
// when one swings right the other swings left, when one is high the other is
// low, so they never come near each other.
function orbitPhase(t) {
  return (t / 1000) * 0.05;
}

// fighterPos is where the fighter patrols: opposite the agentgateway planet
// (and, while the tour card is open, mirrored within the field beside it).
export function fighterPos(t, tourOpen) {
  const a = orbitPhase(t);
  const y = 400 - 110 * Math.cos(a);
  if (tourOpen) return { x: 1370 - gatewayPos(t, true).x, y }; // mirrored within the planet's tour lane
  return { x: 505 - 300 * Math.sin(a), y };
}

// moonAt is where the moon riding the planet's tilted ring is drawn at angle
// a, and whether it shows: on the back half of its orbit it is hidden while
// it passes behind the planet's disc.
export function moonAt(c, a, tilt) {
  const mx = Math.cos(a) * PLANET_R * 1.5, my = Math.sin(a) * PLANET_R * 0.3;
  const x = c.x + mx * Math.cos(tilt) - my * Math.sin(tilt);
  const y = c.y + mx * Math.sin(tilt) + my * Math.cos(tilt);
  const behind = Math.sin(a) < 0 && Math.hypot(x - c.x, y - c.y) < PLANET_R + 4;
  return { x, y, visible: !behind };
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
// to side over time. face (-1..1) is its heading: its sign is the direction
// of travel, and it passes through 0 as the ship turns at each end.
export function saucerSpots(pools, t = 0) {
  const n = Math.max(pools.length, 1);
  const span = FIELD.x1 - FIELD.x0;
  const half = 70 * SAUCER_SCALE;
  const amp = Math.max(0, Math.min(330, span / (2 * n) - half - 4));
  return pools.map((p, i) => {
    const phase = (t / 1000) * 0.12 + i * 1.7;
    return {
      pool: p,
      x: FIELD.x0 + (span * (i + 1)) / (n + 1) + amp * Math.sin(phase),
      y: 96,
      face: amp > 0 ? Math.max(-1, Math.min(1, Math.cos(phase) * 3)) : 1,
    };
  });
}

// The kagent mark, drawn in the worker-pool ship's dome.
const KAGENT = typeof Image === 'undefined' ? {} : Object.assign(new Image(), { src: 'kagent-mark.png' });

// vmetal is a top-lit vertical metal gradient between y0 (lit) and y1 (dark).
function vmetal(ctx, y0, y1, lit, dark) {
  const g = ctx.createLinearGradient(0, y0, 0, y1);
  g.addColorStop(0, lit);
  g.addColorStop(1, dark);
  return g;
}

// sphere fills a small lit ball (a shield dome, a sensor).
function sphere(ctx, x, y, r) {
  const g = ctx.createRadialGradient(x - r * 0.4, y - r * 0.45, r * 0.1, x, y, r);
  g.addColorStop(0, '#f2f5f8');
  g.addColorStop(0.6, '#7d8794');
  g.addColorStop(1, '#151a20');
  ctx.beginPath();
  ctx.arc(x, y, r, 0, Math.PI * 2);
  ctx.fillStyle = g;
  ctx.fill();
}

// drawSaucer draws a worker pool as kagent's capital ship: a wedge-hulled
// Republic destroyer in side profile, shaded like a 3D model (a sunlit upper
// deck over a dark lower hull), with a stepped superstructure, a bridge
// tower with lit shield domes, red Republic markings, rows of lit windows,
// three engine bells with exhaust plumes, and the kagent emblem on its
// flank. One hangar light per worker (lit while hosting an actor), and a
// tractor beam from the hangar when it launches a ship. It flies nose first.
export function drawSaucer(ctx, s, busyWorkers, now, beamAt) {
  const { x, y } = s;
  const S = SAUCER_SCALE;
  const face = s.face ?? 1;
  const glow = (color, blur) => { ctx.shadowColor = color; ctx.shadowBlur = blur; };
  const fillShape = (pts, fill, edge, w = 0.9) => {
    shape(ctx, pts);
    ctx.fillStyle = fill;
    ctx.fill();
    if (edge) stroke(ctx, edge, w / S);
  };
  ctx.save();
  ctx.translate(x, y + 4);
  // tractor beam from the hangar (not mirrored, so it always drops straight)
  const bk = (now - (beamAt ?? -1e9)) / 800;
  if (bk >= 0 && bk < 1) {
    const prev = ctx.globalAlpha;
    ctx.globalAlpha = prev * (1 - bk) * 0.7;
    ctx.beginPath();
    const hx = 26 * S * face;
    ctx.moveTo(hx - 10, 16); ctx.lineTo(hx - 40, 70);
    ctx.moveTo(hx + 10, 16); ctx.lineTo(hx + 40, 70);
    for (let i = 1; i <= 5; i++) {
      const t = ((i / 5 + bk * 2) % 1), yy = 16 + t * 54, w = 10 + t * 30;
      ctx.moveTo(hx - w, yy); ctx.lineTo(hx + w, yy);
    }
    stroke(ctx, NEW, 1);
    ctx.globalAlpha = prev;
  }
  ctx.scale(S * Math.sign(face) * Math.max(Math.abs(face), 0.15), S);
  // engine plumes behind the stern
  for (const ey of [-6, 1, 7]) {
    const f = 12 + Math.random() * 6;
    const g = ctx.createLinearGradient(-66, 0, -66 - f, 0);
    g.addColorStop(0, 'rgba(79,216,255,0.85)');
    g.addColorStop(1, 'rgba(79,216,255,0)');
    shape(ctx, [[-66, ey - 2.6], [-66 - f, ey], [-66, ey + 2.6]]);
    ctx.fillStyle = g;
    ctx.fill();
  }
  // the hull: a sunlit upper deck over a dark lower hull, split at the trench
  const upper = [[74, 3], [-28, -8], [-34, -12], [-60, -12], [-66, -8], [-66, 1]];
  const lower = [[74, 3], [-66, 1], [-66, 9], [-40, 12], [20, 9]];
  fillShape(lower, vmetal(ctx, 1, 12, '#5d6671', '#0e1217'));
  fillShape(upper, vmetal(ctx, -12, 3, '#e6ebf1', '#77818e'));
  // stepped superstructure
  fillShape([[-30, -10.4], [-36, -15], [-58, -15], [-62, -11.5], [-60, -12], [-34, -12]], vmetal(ctx, -15, -10, '#f2f5f8', '#8e98a5'), 'rgba(255,255,255,0.5)');
  // red Republic markings: deck stripe, prow, dorsal chevrons
  ctx.beginPath();
  ctx.moveTo(70, 1.4); ctx.lineTo(-24, -6.8);
  stroke(ctx, RED, 1.8 / S);
  fillShape([[74, 3], [60, 1.6], [60, 4.4]], RED);
  ctx.beginPath();
  for (const cx of [44, 38]) { ctx.moveTo(cx, -0.6); ctx.lineTo(cx - 4, -2.2); ctx.lineTo(cx - 8, -0.9); }
  stroke(ctx, RED, 1.1 / S);
  // deck plating and the trench
  ctx.beginPath();
  for (let px = 56; px > -26; px -= 10) {
    const top = 3 - ((74 - px) / 102) * 11;
    ctx.moveTo(px, top + 1.2); ctx.lineTo(px - 5, top + 2.8);
  }
  stroke(ctx, 'rgba(0,0,0,0.3)', 0.8 / S);
  ctx.beginPath();
  ctx.moveTo(72, 3); ctx.lineTo(-66, 1);
  stroke(ctx, 'rgba(0,0,0,0.7)', 1.4 / S);
  // rows of lit windows along the lower hull, slowly changing
  for (let i = 0; i < 40; i++) {
    const px = 56 - i * 3, row = i % 2;
    const keel = px > 20 ? 9 - ((px - 20) / 54) * 6 : 9 + ((20 - px) / 60) * 3;
    const py = 3.2 + row * 2 + (keel - 9) * 0.2;
    if (py > keel - 1.2) continue;
    const on = ((i * 7 + Math.floor(now / 1500)) % 5) !== 0;
    ctx.fillStyle = on ? (i % 9 === 0 ? 'rgba(255,191,71,0.9)' : 'rgba(220,240,255,0.75)') : 'rgba(255,255,255,0.15)';
    ctx.fillRect(px, py, 1, 0.6);
  }
  // the whole hull outlined, with a faint glow
  shape(ctx, [[74, 3], [-28, -8], [-34, -12], [-60, -12], [-66, -8], [-66, 9], [-40, 12], [20, 9]]);
  glow(NEW, 4);
  stroke(ctx, 'rgba(255,255,255,0.6)', 1 / S);
  ctx.shadowBlur = 0;
  // bridge tower: neck, command bar, lit shield domes on stalks
  fillShape([[-42, -15], [-45, -21], [-51, -21], [-54, -15]], vmetal(ctx, -21, -15, '#c9d1da', '#4c5560'), 'rgba(255,255,255,0.5)');
  fillShape([[-60, -26], [-36, -26], [-36, -21], [-60, -21]], vmetal(ctx, -26, -21, '#f2f5f8', '#6f7a87'), 'rgba(255,255,255,0.6)');
  ctx.beginPath();
  ctx.moveTo(-58, -23.5); ctx.lineTo(-38, -23.5);
  glow(NEW, 5);
  stroke(ctx, NEW, 1 / S);
  ctx.shadowBlur = 0;
  for (const dx of [-55, -41]) {
    ctx.beginPath();
    ctx.moveTo(dx, -26); ctx.lineTo(dx, -28.6);
    stroke(ctx, 'rgba(255,255,255,0.7)', 0.9 / S);
    sphere(ctx, dx, -31, 2.6);
  }
  // engine bells: three glowing thrusters at the stern
  for (const ey of [-6, 1, 7]) {
    const g = ctx.createRadialGradient(-66, ey, 0.3, -66, ey, 3.8);
    g.addColorStop(0, '#ffffff');
    g.addColorStop(0.4, NEW);
    g.addColorStop(1, 'rgba(79,216,255,0)');
    ctx.beginPath();
    ctx.arc(-66, ey, 3.8, 0, Math.PI * 2);
    glow(NEW, 10 + Math.random() * 6);
    ctx.fillStyle = g;
    ctx.fill();
  }
  ctx.shadowBlur = 0;
  // running lights chasing along the trench toward the prow
  for (let i = 0; i < 10; i++) {
    const k = ((i / 10) + now / 4000) % 1;
    const px = -64 + k * 132;
    ctx.beginPath();
    ctx.arc(px, 2 - (px + 66) * 0.015, 0.8, 0, Math.PI * 2);
    ctx.fillStyle = k > 0.85 ? INK : DIM;
    ctx.fill();
  }
  // hangar lights on the keel: one per worker
  const workers = s.pool.workers || [];
  const gap = 9;
  workers.forEach((w, i) => {
    const px = 26 - ((workers.length - 1) * gap) / 2 + i * gap; // forward of the emblem
    ctx.beginPath();
    ctx.rect(px - 2.6, 5, 5.2, 2.2);
    ctx.fillStyle = busyWorkers.has(w) ? RESUMED : '#0b0e12';
    if (busyWorkers.has(w)) glow(RESUMED, 6);
    ctx.fill();
    ctx.shadowBlur = 0;
    stroke(ctx, 'rgba(255,255,255,0.7)', 0.9 / S);
  });
  // the kagent emblem on the flank: a dark disc, a glowing purple ring with a
  // slowly turning outer ring, the mark kept upright when facing left
  ctx.beginPath();
  ctx.arc(-8, 2, 12.5, 0, Math.PI * 2);
  ctx.fillStyle = '#05070a';
  ctx.fill();
  glow(KAGENT_PURPLE, 10);
  stroke(ctx, KAGENT_PURPLE, 1.4 / S);
  ctx.shadowBlur = 0;
  ctx.save();
  ctx.translate(-8, 2);
  ctx.rotate(now / 3000);
  ctx.setLineDash([2.2, 2.2]);
  ctx.beginPath();
  ctx.arc(0, 0, 14.5, 0, Math.PI * 2);
  stroke(ctx, KAGENT_PURPLE, 0.9 / S);
  ctx.setLineDash([]);
  ctx.restore();
  if (KAGENT.complete && KAGENT.naturalWidth) {
    ctx.save();
    ctx.translate(-8, 2);
    ctx.scale(Math.sign(face), 1);
    ctx.drawImage(KAGENT, -11, -11, 22, 22);
    ctx.restore();
  }
  ctx.restore();
  text(ctx, s.pool.name, x, y + 42, { size: 10, align: 'center', spacing: 2 });
  text(ctx, `KAGENT WORKER POOL · ${workers.length} WORKER${workers.length === 1 ? '' : 'S'}`, x, y + 54, { size: 7, align: 'center', color: DIM, spacing: 1 });
}

export const ALIEN = '#9dff6b'; // the human-in-the-loop request
const KAGENT_PURPLE = '#9b4dff'; // the kagent robot pilot

// LIGHT is where the sun sits (upper left of the screen): every fighter is
// shaded from it whatever way it's flying, which is what makes them read as 3D.
const LIGHT = { x: -0.6, y: -0.8 };

// metal is a hull gradient for a ship rotated by rot: bright on the side
// facing the light, dark away from it.
function localLight(rot) {
  const c = Math.cos(-rot), s = Math.sin(-rot);
  return [c * LIGHT.x - s * LIGHT.y, s * LIGHT.x + c * LIGHT.y];
}

function metal(ctx, rot, reach, lit = '#d7dee8', dark = '#1b2129') {
  const [lx, ly] = localLight(rot);
  const g = ctx.createLinearGradient(lx * reach, ly * reach, -lx * reach, -ly * reach);
  g.addColorStop(0, lit);
  g.addColorStop(0.55, '#6b7584');
  g.addColorStop(1, dark);
  return g;
}

function shape(ctx, pts) {
  ctx.beginPath();
  pts.forEach(([px, py], i) => (i ? ctx.lineTo(px, py) : ctx.moveTo(px, py)));
  ctx.closePath();
}

// drawScout draws a session's sandbox as a Rebel ship (X-, Y- or A-wing, or
// the Millennium Falcon) launched from the Republic destroyer, shaded like a
// 3D model, flying nose first; it powers up (grows)
// while working. Its trim and engines carry its origin colour (new or
// resumed). A frozen fighter (waiting for a human) goes dark and dashed,
// with its pilot out on the hull holding up the request.
export function drawScout(ctx, r, now, approval) {
  const sc = r.scale || 1;
  const color = r.frozen ? DIM : r.failed ? RED : originColor(r);
  const live = !r.frozen;
  ctx.save();
  ctx.translate(r.x, r.y);
  ctx.rotate(r.rot);
  ctx.scale(sc, sc);
  const lw = 1.2 / sc;
  if (r.frozen) ctx.setLineDash([3 / sc, 3 / sc]);
  const hull = (pts, reach = 16) => {
    shape(ctx, pts);
    ctx.fillStyle = live ? metal(ctx, r.rot, reach) : 'rgba(255,255,255,0.05)';
    ctx.fill();
    stroke(ctx, live ? 'rgba(255,255,255,0.55)' : DIM, lw);
  };
  const engine = (ex, ey, er) => {
    ctx.beginPath();
    ctx.arc(ex, ey, er, 0, Math.PI * 2);
    if (live) {
      ctx.shadowColor = color;
      ctx.shadowBlur = 8 + Math.random() * 4;
      ctx.fillStyle = color;
      ctx.fill();
      ctx.shadowBlur = 0;
    }
    stroke(ctx, color, lw * 0.8);
  };
  const canopy = (cx, cy, rx, ry) => {
    ctx.beginPath();
    ctx.ellipse(cx, cy, rx, ry, 0, 0, Math.PI * 2);
    ctx.fillStyle = live ? '#0b1a2a' : 'transparent';
    ctx.fill();
    stroke(ctx, live ? color : DIM, lw * 0.8);
    ctx.beginPath();
    ctx.ellipse(cx + rx * 0.2, cy - ry * 0.35, rx * 0.45, ry * 0.25, 0, 0, Math.PI * 2);
    ctx.fillStyle = live ? 'rgba(255,255,255,0.6)' : 'transparent';
    ctx.fill();
  };
  const trim = (pts) => {
    ctx.beginPath();
    pts.forEach(([x1, y1, x2, y2]) => { ctx.moveTo(x1, y1); ctx.lineTo(x2, y2); });
    stroke(ctx, color, lw * 1.3);
  };
  if (r.kind === 'ywing') {
    for (const sy of [-8, 8]) { // engine nacelles
      hull([[2, sy - 2.2], [-14, sy - 2.2], [-14, sy + 2.2], [2, sy + 2.2]], 6);
      engine(-14, sy, 2);
    }
    ctx.beginPath(); ctx.moveTo(-2, -6); ctx.lineTo(-2, 6); stroke(ctx, DIM, lw); // the strut
    hull([[16, 0], [12, -4], [-6, -4], [-8, 0], [-6, 4], [12, 4]], 8); // cockpit pod
    trim([[4, -4, 4, 4], [-3, -10.2, -3, -5.8], [-3, 5.8, -3, 10.2]]);
    canopy(9, 0, 3.5, 2.2);
  } else if (r.kind === 'awing') {
    const L = [[16, 0], [-8, -13], [-12, -13], [-10, 0]], R = [[16, 0], [-10, 0], [-12, 13], [-8, 13]];
    shape(ctx, L); ctx.fillStyle = live ? metal(ctx, r.rot, 14, '#e3e9f1', '#3a424e') : 'transparent'; ctx.fill();
    shape(ctx, R); ctx.fillStyle = live ? metal(ctx, r.rot, 14, '#8e98a6', '#11161c') : 'transparent'; ctx.fill();
    shape(ctx, [[16, 0], [-8, -13], [-12, -13], [-10, 0], [-12, 13], [-8, 13]]);
    stroke(ctx, live ? 'rgba(255,255,255,0.55)' : DIM, lw);
    ctx.beginPath(); ctx.moveTo(16, 0); ctx.lineTo(-10, 0); stroke(ctx, 'rgba(255,255,255,0.35)', lw * 0.7); // the ridge
    trim([[2, -6.5, -6, -11], [2, 6.5, -6, 11]]);
    engine(-11, -9, 1.8); engine(-11, 9, 1.8);
    canopy(4, 0, 4, 2.4);
  } else if (r.kind === 'falcon') {
    for (const m of [1, -1]) hull([[18, 7 * m], [18, 2.5 * m], [9, 2.5 * m], [9, 7 * m]], 8); // mandibles
    ctx.beginPath(); // the disc, lit like a shallow dome
    ctx.arc(0, 0, 13, 0, Math.PI * 2);
    if (live) {
      const [gx, gy] = localLight(r.rot);
      const g = ctx.createRadialGradient(gx * 6, gy * 6, 1, 0, 0, 13.5);
      g.addColorStop(0, '#e6ebf1');
      g.addColorStop(0.6, '#7d8794');
      g.addColorStop(1, '#232931');
      ctx.fillStyle = g;
      ctx.fill();
    }
    stroke(ctx, live ? 'rgba(255,255,255,0.6)' : DIM, lw);
    ctx.beginPath(); // hull plating: spokes from the hub
    for (let i = 0; i < 10; i++) {
      const a = (i / 10) * Math.PI * 2 + 0.3;
      ctx.moveTo(Math.cos(a) * 4.5, Math.sin(a) * 4.5); ctx.lineTo(Math.cos(a) * 12, Math.sin(a) * 12);
    }
    stroke(ctx, live ? 'rgba(0,0,0,0.35)' : DIM, lw * 0.6);
    ctx.beginPath(); ctx.arc(0, 0, 4.5, 0, Math.PI * 2); stroke(ctx, color, lw); // the hub
    ctx.beginPath(); ctx.ellipse(-2, -6.5, 2.6, 1.6, 0.4, 0, Math.PI * 2); stroke(ctx, INK, lw * 0.8); // radar dish
    hull([[13, 9.5], [13, 13], [4, 13], [4, 9.5]], 6); // the cockpit tube, off to starboard
    canopy(13.6, 11.2, 2.3, 2);
    ctx.beginPath(); // the engine strip glowing along the stern
    ctx.arc(0, 0, 12.4, Math.PI - 0.9, Math.PI + 0.9);
    if (live) { ctx.shadowColor = color; ctx.shadowBlur = 10 + Math.random() * 4; }
    stroke(ctx, color, lw * 2);
    ctx.shadowBlur = 0;
  } else { // xwing
    for (const sy of [-1, 1]) { // the X-foils: an upper wing (lit) and a lower one peeking out
      hull([[-1, 2.5 * sy], [-5, 15 * sy], [-10, 15 * sy], [-10, 2.5 * sy]], 16);
      ctx.beginPath(); ctx.moveTo(-5, 15 * sy); ctx.lineTo(7, 15 * sy); stroke(ctx, INK, lw * 0.8); // wingtip cannon
      trim([[-4, 8 * sy, -8.5, 8 * sy]]);
      engine(-10.5, 4 * sy, 1.9);
    }
    hull([[18, 0], [6, -2.2], [-10, -3], [-12, 0], [-10, 3], [6, 2.2]], 10); // fuselage
    ctx.beginPath(); ctx.arc(-4, 0, 1.6, 0, Math.PI * 2); stroke(ctx, color, lw * 0.8); // the droid
    canopy(3, 0, 3.6, 1.8);
  }
  ctx.restore();
  ctx.setLineDash([]);
  // dashed leader to the label
  const lx = r.x + 30 * sc, ly = r.y - 22 * sc;
  ctx.setLineDash([2, 3]);
  ctx.beginPath();
  ctx.moveTo(r.x + 16 * sc, r.y - 10 * sc);
  ctx.lineTo(lx - 4, ly + 4);
  stroke(ctx, DIM, 1);
  ctx.setLineDash([]);
  text(ctx, r.agent, lx, ly, { size: 10, spacing: 1 });
  text(ctx, r.sub, lx, ly + 13, { size: 9, spacing: 1, color: r.frozen ? ALIEN : DIM });
  if (r.tag) text(ctx, r.tag, lx, ly + 25, { size: 8, spacing: 1, color: originColor(r) });
  if (r.frozen && approval) drawPilot(ctx, r.x, r.y - 26 * sc, now, approval);
}

// drawPilot draws the little kagent alien (kagent's robot mark with antennae
// and legs) who climbs out of a frozen UFO and holds up the tool call it
// wants a human to approve.
function drawPilot(ctx, x, y, now, ap) {
  const bob = Math.sin(now / 300) * 1.5;
  y += bob;
  const S = 28; // the kagent robot, head and body in one
  // legs
  ctx.beginPath();
  ctx.moveTo(x - 4, y + S / 2 - 2); ctx.lineTo(x - 5, y + S / 2 + 4);
  ctx.moveTo(x + 4, y + S / 2 - 2); ctx.lineTo(x + 5, y + S / 2 + 4);
  stroke(ctx, KAGENT_PURPLE, 1.4);
  // antennae
  ctx.beginPath();
  ctx.moveTo(x - 4, y - S / 2 + 3); ctx.lineTo(x - 7, y - S / 2 - 4);
  ctx.moveTo(x + 4, y - S / 2 + 3); ctx.lineTo(x + 7, y - S / 2 - 4);
  stroke(ctx, KAGENT_PURPLE, 1.2);
  for (const ax of [-7, 7]) {
    ctx.beginPath(); ctx.arc(x + ax, y - S / 2 - 4.5, 1.6, 0, Math.PI * 2);
    ctx.fillStyle = Math.floor(now / 400) % 2 ? ALIEN : KAGENT_PURPLE; ctx.fill();
  }
  if (KAGENT.complete && KAGENT.naturalWidth) ctx.drawImage(KAGENT, x - S / 2, y - S / 2, S, S);
  else { ctx.beginPath(); ctx.rect(x - 7, y - 6, 14, 12); stroke(ctx, KAGENT_PURPLE, 1.4); }
  // waving arm up to the sign
  const wave = Math.sin(now / 180) * 0.25;
  const hx = x + 18 + Math.cos(-1 + wave) * 2, hy = y - 18 + Math.sin(-1 + wave) * 2;
  ctx.beginPath();
  ctx.moveTo(x + 12, y + 1); ctx.lineTo(hx, hy);
  stroke(ctx, KAGENT_PURPLE, 1.3);
  // the sign: what it wants to run, and how to answer
  const tool = (ap.tools?.[0]?.name || 'A TOOL').toUpperCase();
  const lines = [`RUN ${tool}?`, 'Y APPROVE · X REJECT'];
  ctx.font = `9px ${FONT}`;
  ctx.letterSpacing = '1px';
  const w = Math.max(...lines.map((l) => ctx.measureText(l).width)) + 14;
  const sx = hx - 6, sy = hy - 34;
  ctx.fillStyle = 'rgba(0,0,0,0.85)';
  ctx.fillRect(sx, sy, w, 28);
  ctx.beginPath();
  ctx.rect(sx, sy, w, 28);
  stroke(ctx, Math.floor(now / 450) % 2 ? ALIEN : INK, 1.3);
  ctx.beginPath(); ctx.moveTo(hx, hy); ctx.lineTo(hx, sy + 28); stroke(ctx, ALIEN, 1);
  text(ctx, lines[0], sx + 7, sy + 12, { size: 9, spacing: 1, color: ALIEN });
  text(ctx, lines[1], sx + 7, sy + 23, { size: 8, spacing: 1, color: INK });
}

// drawShip draws our ship, the one that marks each freeze: Vader's TIE
// Advanced seen from above, a ball cockpit lit like a sphere between bent
// solar-panel wings, with a rear hull, red Sith trim, glowing ion engines,
// and twin green laser bolts when it fires.
const SITH = '#ff3b3b';
const TIE_LASER = '#6dff6d';
export function drawShip(ctx, ship) {
  const glow = (color, blur) => { ctx.shadowColor = color; ctx.shadowBlur = blur; };
  const [lx, ly] = localLight(ship.a);
  const lw = 1;
  ctx.save();
  ctx.translate(ship.x, ship.y);
  ctx.rotate(ship.a);
  ctx.scale(1.5, 1.5);
  const bar = (pts, reach = 8) => {
    shape(ctx, pts);
    ctx.fillStyle = metal(ctx, ship.a, reach, '#c4ccd6', '#14181e');
    ctx.fill();
    stroke(ctx, 'rgba(255,255,255,0.6)', lw * 0.8);
  };
  // ion exhaust out the back of the rear hull
  for (const ey of [-1.6, 1.6]) {
    const f = 4 + Math.random() * 5;
    ctx.beginPath();
    ctx.moveTo(-11, ey - 0.9); ctx.lineTo(-11 - f, ey); ctx.lineTo(-11, ey + 0.9);
    glow(SITH, 8);
    stroke(ctx, SITH, 1);
  }
  ctx.shadowBlur = 0;
  bar([[-3, -4], [-11, -3], [-11, 3], [-3, 4]]); // the rear hull
  for (const m of [1, -1]) bar([[1.4, 3 * m], [1.4, 7 * m], [-1.4, 7 * m], [-1.4, 3 * m]]); // struts
  // the bent wings: dark solar cells, metal frame, spokes, red trim
  for (const m of [1, -1]) {
    const p = [[-5, 6], [5, 6], [8, 11], [4, 18], [-4, 18], [-8, 11]].map(([px, py]) => [px, py * m]);
    const cx = 0, cy = 12.3 * m;
    shape(ctx, p);
    ctx.fillStyle = metal(ctx, ship.a, 20, '#3f4754', '#05070a');
    ctx.fill();
    stroke(ctx, 'rgba(255,255,255,0.65)', lw);
    ctx.beginPath();
    for (const [px, py] of p) { ctx.moveTo(cx, cy); ctx.lineTo(px, py); }
    stroke(ctx, 'rgba(255,255,255,0.16)', lw * 0.6);
    shape(ctx, p.map(([px, py]) => [cx + (px - cx) * 0.72, cy + (py - cy) * 0.72]));
    stroke(ctx, SITH, lw * 0.7);
  }
  // the ball cockpit, lit like a sphere, with its octagonal window up front
  const br = 5.6;
  ctx.beginPath();
  ctx.arc(0, 0, br, 0, Math.PI * 2);
  const g = ctx.createRadialGradient(lx * br * 0.5, ly * br * 0.5, br * 0.1, 0, 0, br);
  g.addColorStop(0, '#e2e7ed');
  g.addColorStop(0.6, '#5f6874');
  g.addColorStop(1, '#0d1014');
  ctx.fillStyle = g;
  ctx.fill();
  stroke(ctx, 'rgba(255,255,255,0.6)', lw);
  const wx = br * 0.42, wr = br * 0.5;
  ctx.beginPath();
  for (let i = 0; i < 8; i++) {
    const a = (i / 8) * Math.PI * 2 + Math.PI / 8;
    i ? ctx.lineTo(wx + Math.cos(a) * wr, Math.sin(a) * wr) : ctx.moveTo(wx + Math.cos(a) * wr, Math.sin(a) * wr);
  }
  ctx.closePath();
  ctx.fillStyle = '#12060a';
  ctx.fill();
  glow(SITH, 6);
  stroke(ctx, SITH, 0.9);
  ctx.shadowBlur = 0;
  ctx.beginPath();
  for (let i = 0; i < 4; i++) {
    const a = (i / 4) * Math.PI;
    ctx.moveTo(wx - Math.cos(a) * wr * 0.9, -Math.sin(a) * wr * 0.9);
    ctx.lineTo(wx + Math.cos(a) * wr * 0.9, Math.sin(a) * wr * 0.9);
  }
  stroke(ctx, 'rgba(255,255,255,0.25)', 0.5);
  // firing: twin green bolts from the cannons under the cockpit
  if (ship.flash > 0) {
    glow(TIE_LASER, 10);
    ctx.beginPath();
    for (const py of [-2.2, 2.2]) { ctx.moveTo(6, py); ctx.lineTo(24, py); }
    stroke(ctx, TIE_LASER, 1.6);
  }
  ctx.shadowBlur = 0;
  ctx.restore();
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
  for (const r of world.rocks.values()) drawScout(ctx, r, now, world.approvalByUid?.get(r.uid));
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
export function drawGatewayPlanet(ctx, world, now) {
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
  // the Death Star: a sphere lit from the upper left, its equatorial trench,
  // surface plating, and the superlaser dish holding the agentgateway mark
  const R = PLANET_R;
  const body = ctx.createRadialGradient(x - R * 0.4, y - R * 0.45, R * 0.08, x, y, R * 1.02);
  body.addColorStop(0, '#d9dfe6');
  body.addColorStop(0.5, '#6e7883');
  body.addColorStop(1, '#101419');
  ctx.save();
  ctx.beginPath();
  ctx.arc(x, y, R, 0, Math.PI * 2);
  ctx.fillStyle = body;
  ctx.fill();
  ctx.clip();
  ctx.beginPath(); // plating: latitude seams
  for (const k of [-0.6, -0.3, 0.3, 0.6]) {
    const yy = y + k * R, rx = Math.sqrt(1 - k * k) * R;
    ctx.ellipse(x, yy, rx, R * 0.08, 0, 0, Math.PI);
  }
  stroke(ctx, 'rgba(0,0,0,0.35)', 0.8);
  ctx.beginPath(); // the trench: a dark band with a lit lower lip
  ctx.ellipse(x, y + R * 0.06, R, R * 0.1, 0, 0, Math.PI);
  stroke(ctx, 'rgba(0,0,0,0.75)', 2.2);
  ctx.beginPath();
  ctx.ellipse(x, y + R * 0.06 + 1.6, R, R * 0.1, 0, 0.1 * Math.PI, 0.9 * Math.PI);
  stroke(ctx, 'rgba(255,255,255,0.35)', 0.7);
  for (let i = 0; i < 14; i++) { // city lights on the dark side
    const a2 = i * 2.39, rr = R * (0.35 + ((i * 37) % 60) / 100);
    const px = x + Math.cos(a2) * rr, py = y + Math.sin(a2) * rr;
    if (px - x + py - y < R * 0.2) continue;
    ctx.fillStyle = (Math.floor(now / 700) + i) % 5 ? 'rgba(255,255,255,0.55)' : GW;
    ctx.fillRect(px, py, 1, 1);
  }
  ctx.restore();
  ctx.beginPath(); // the limb
  ctx.arc(x, y, R, 0, Math.PI * 2);
  stroke(ctx, 'rgba(255,255,255,0.5)', 1);
  const dx = x - R * 0.36, dy = y - R * 0.4, dr = R * 0.34; // the dish: concave, so lit low-right
  const dish = ctx.createRadialGradient(dx + dr * 0.35, dy + dr * 0.35, dr * 0.1, dx, dy, dr);
  dish.addColorStop(0, '#9aa4b0');
  dish.addColorStop(1, '#1a1f26');
  ctx.beginPath();
  ctx.arc(dx, dy, dr, 0, Math.PI * 2);
  ctx.fillStyle = dish;
  ctx.fill();
  ctx.shadowColor = GW;
  ctx.shadowBlur = 6 + flare * 10;
  stroke(ctx, GW, 1 + flare);
  ctx.shadowBlur = 0;
  // the ring slowly precesses, with a moon riding it
  const tilt = -0.32 + 0.12 * Math.sin(now / 4000);
  ctx.beginPath();
  ctx.ellipse(x, y, PLANET_R * 1.5, PLANET_R * 0.3, tilt, 0.12 * Math.PI, 0.88 * Math.PI, true);
  stroke(ctx, DIM, 1);
  const moon = moonAt({ x, y }, now / 1600, tilt);
  if (moon.visible) {
    ctx.beginPath();
    ctx.arc(moon.x, moon.y, 4, 0, Math.PI * 2);
    stroke(ctx, INK, 1.2);
  }
  if (LOGO.complete && LOGO.naturalWidth) {
    const size = dr * 1.6; // the agentgateway mark in the superlaser dish
    ctx.drawImage(LOGO, dx - size / 2, dy - size / 2, size, size);
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
      ctx.setLineDash([2, 4]); // dotted, so it reads as a field around the fighter
      hexagon(ctx, fx.rock.x, fx.rock.y, 34 * sc + Math.sin(k * Math.PI * 6) * 2);
      stroke(ctx, GW, 1.8);
      ctx.setLineDash([]);
    }
    // a running feed of calls beside the planet (on whichever side has room)
    const right = gw.x < 600;
    text(ctx, fx.label, gw.x + (right ? SHIELD_R + 16 : -SHIELD_R - 16), gw.y - 30 + (fx.slot || 0) * 12,
      { size: 8, spacing: 1, color: GW, align: right ? 'left' : 'right' });
    ctx.restore();
  }
  ctx.globalAlpha = base;
}
