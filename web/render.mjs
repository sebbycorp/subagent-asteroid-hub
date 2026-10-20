// Vector drawing for the 1280×720 stage. Everything is white strokes on black.

export const W = 1280;
export const H = 720;
export const FIELD = { x0: 20, x1: 990, y0: 150, y1: 610 };
export const VAULT = { x: 1010, y: 150, w: 250, h: 450 };
const FONT = 'ui-monospace, "SF Mono", Menlo, Consolas, monospace';
const INK = '#fff';
const DIM = 'rgba(255,255,255,0.45)';
const RED = '#ff5a4f';

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

export function saucerSpots(pools) {
  const n = Math.max(pools.length, 1);
  const span = FIELD.x1 - FIELD.x0;
  return pools.map((p, i) => ({ pool: p, x: FIELD.x0 + (span * (i + 1)) / (n + 1), y: 88 }));
}

function drawSaucer(ctx, s, busyWorkers) {
  const { x, y } = s;
  ctx.beginPath();
  ctx.moveTo(x - 46, y);
  ctx.lineTo(x - 20, y - 12);
  ctx.lineTo(x + 20, y - 12);
  ctx.lineTo(x + 46, y);
  ctx.lineTo(x + 20, y + 11);
  ctx.lineTo(x - 20, y + 11);
  ctx.closePath();
  ctx.moveTo(x - 46, y);
  ctx.lineTo(x + 46, y);
  ctx.moveTo(x - 13, y - 12);
  ctx.lineTo(x - 7, y - 23);
  ctx.lineTo(x + 7, y - 23);
  ctx.lineTo(x + 13, y - 12);
  stroke(ctx);
  text(ctx, s.pool.name, x, y - 32, { size: 11, align: 'center' });
  const workers = s.pool.workers || [];
  const bw = 14, gap = 6;
  const total = workers.length * bw + (workers.length - 1) * gap;
  workers.forEach((w, i) => {
    const bx = x - total / 2 + i * (bw + gap);
    ctx.beginPath();
    ctx.rect(bx, y + 18, bw, 7);
    if (busyWorkers.has(w)) {
      ctx.fillStyle = INK;
      ctx.fill();
    }
    stroke(ctx, INK, 1);
  });
  text(ctx, `${workers.length} WORKER${workers.length === 1 ? '' : 'S'}`, x, y + 40, { size: 9, align: 'center', color: DIM });
}

function drawRock(ctx, r) {
  poly(ctx, r.shape, r.x, r.y, r.rot);
  stroke(ctx, r.failed ? RED : INK);
  // dashed leader to the label
  const lx = r.x + 34, ly = r.y - 22;
  ctx.setLineDash([2, 3]);
  ctx.beginPath();
  ctx.moveTo(r.x + 20, r.y - 10);
  ctx.lineTo(lx - 4, ly + 4);
  stroke(ctx, DIM, 1);
  ctx.setLineDash([]);
  text(ctx, r.agent, lx, ly, { size: 10, spacing: 1 });
  text(ctx, r.sub, lx, ly + 13, { size: 9, spacing: 1, color: DIM });
}

export function drawShip(ctx, ship) {
  const pts = [[16, 0], [-11, -9], [-6, 0], [-11, 9]];
  poly(ctx, pts, ship.x, ship.y, ship.a);
  stroke(ctx);
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

function drawVault(ctx, count) {
  ctx.beginPath();
  ctx.rect(VAULT.x, VAULT.y, VAULT.w, VAULT.h);
  stroke(ctx);
  text(ctx, 'SNAPSHOT STORE', VAULT.x + VAULT.w / 2, VAULT.y + 24, { size: 12, align: 'center' });
  text(ctx, 'S3 · SUSPENDED ACTORS', VAULT.x + VAULT.w / 2, VAULT.y + 42, { size: 9, align: 'center', color: DIM });
  const shown = Math.min(count, 200);
  for (let i = 0; i < shown; i++) {
    const s = vaultSlot(i);
    if (s.y > VAULT.y + VAULT.h - 60) break;
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
  for (const s of saucerSpots(st.snapshot?.pools || [])) drawSaucer(ctx, s, busy);
  drawVault(ctx, world.vault.size);
  for (const r of world.rocks.values()) drawRock(ctx, r);
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

export const CARD = { x: 30, y: 150, w: 540 };

function drawTourCard(ctx, tour, live, now) {
  const lines = [];
  const body = tour.body || [];
  const h = 64 + body.length * 16 + 22 * 4;
  ctx.save();
  ctx.fillStyle = 'rgba(0,0,0,0.86)';
  ctx.fillRect(CARD.x, CARD.y, CARD.w, h);
  ctx.beginPath();
  ctx.rect(CARD.x, CARD.y, CARD.w, h);
  stroke(ctx, INK, 1.5);
  ctx.restore();
  let y = CARD.y + 26;
  const x = CARD.x + 18;
  text(ctx, `TOUR ${tour.chapter}/${tour.total} · ${tour.title}`, x, y, { size: 13, spacing: 2 });
  y += 26;
  for (const l of body) { text(ctx, l, x, y, { size: 10, spacing: 1, color: DIM }); y += 16; }
  y += 10;
  if (tour.doing) {
    const dot = Math.floor(now / 400) % 2 ? '▸' : ' ';
    text(ctx, `${dot} ${tour.doing}`, x, y, { size: 11, spacing: 1 });
    y += 22;
  }
  if (tour.result) { text(ctx, tour.result, x, y, { size: 11, spacing: 1 }); y += 22; }
  if (tour.error) { text(ctx, tour.error.slice(0, 80), x, y, { size: 11, spacing: 1, color: RED }); y += 22; }
  if (live) { text(ctx, live, x, y, { size: 10, spacing: 1, color: DIM }); y += 22; }
  text(ctx, tour.busy ? 'WORKING ON THE CLUSTER…' : tour.chapter >= tour.total ? 'N FINISH · ESC END TOUR' : 'N NEXT CHAPTER · ESC END TOUR',
    CARD.x + CARD.w - 18, CARD.y + h - 12, { size: 9, align: 'right', color: DIM });
}

// activeField is where rocks may fly: while the tour card is open the field
// steps aside so the card never hides the action.
export function activeField(tourOpen) {
  return tourOpen ? { ...FIELD, x0: CARD.x + CARD.w + 30 } : FIELD;
}
