import test from 'node:test';
import assert from 'node:assert/strict';
import { activeField, FIELD, CARD } from './render.mjs';

test('the field steps aside for the tour card', () => {
  assert.deepEqual(activeField(false), FIELD);
  const f = activeField(true);
  assert.ok(f.x0 >= CARD.x + CARD.w, `field x0 ${f.x0} overlaps the card`);
  assert.equal(f.x1, FIELD.x1);
  assert.ok(f.x1 - f.x0 >= 300, 'field still wide enough to show rocks');
});

test('the agentgateway planet floats, inside one shield, within the field', async () => {
  const { gatewayPos, edgeToward, FIELD, CARD, VAULT, SHIELD_R, PLANET_R } = await import('./render.mjs');
  assert.equal(PLANET_R, 31, "planet is half its old 62px");
  const a = gatewayPos(0, false), b = gatewayPos(20000, false);
  assert.ok(Math.hypot(a.x - b.x, a.y - b.y) > 20, 'it moves');
  for (let t = 0; t < 600000; t += 997) {
    for (const tour of [false, true]) {
      const p = gatewayPos(t, tour);
      assert.ok(p.x - SHIELD_R - 20 >= FIELD.x0 && p.x + SHIELD_R + 120 <= VAULT.x, `x ${p.x} out of bounds`);
      assert.ok(p.y - SHIELD_R - 24 >= 130 && p.y + SHIELD_R + 40 <= FIELD.y1, `y ${p.y} out of bounds`);
      if (tour) assert.ok(p.x - SHIELD_R >= CARD.x + CARD.w, `planet over the tour card at ${p.x}`);
    }
  }
  const e = edgeToward({ x: 400, y: 300 }, { x: 600, y: 300 });
  assert.equal(Math.round(e.x), 400 + SHIELD_R);
  assert.equal(FIELD.x0, 20);
});

test('the kagent ship glides side to side along the top, staying in the field', async () => {
  const { saucerSpots, FIELD, SAUCER_SCALE } = await import('./render.mjs');
  assert.ok(SAUCER_SCALE >= 1.4, 'bigger ship');
  const pools = [{ name: 'kagent-default', workers: ['a', 'b', 'c'] }];
  const xs = [];
  for (let t = 0; t < 120000; t += 500) {
    const [s] = saucerSpots(pools, t);
    xs.push(s.x);
    assert.ok(s.x - 70 * SAUCER_SCALE >= FIELD.x0 && s.x + 70 * SAUCER_SCALE <= FIELD.x1, `x ${s.x}`);
    assert.ok(s.y < FIELD.y0, 'stays on top');
  }
  assert.ok(Math.max(...xs) - Math.min(...xs) > 300, 'moves side to side');
});

test('the fighter flies opposite the agentgateway planet and never comes close', async () => {
  const { gatewayPos, fighterPos, SHIELD_R, CARD, FIELD } = await import('./render.mjs');
  let minD = Infinity;
  for (let t = 0; t < 900000; t += 250) {
    for (const tour of [false, true]) {
      const g = gatewayPos(t, tour), f = fighterPos(t, tour);
      minD = Math.min(minD, Math.hypot(g.x - f.x, g.y - f.y));
      assert.ok(f.x >= FIELD.x0 + 20 && f.x <= FIELD.x1 - 20 && f.y >= FIELD.y0 + 20 && f.y <= FIELD.y1 - 20, `fighter out of field ${f.x},${f.y}`);
      if (tour) assert.ok(f.x >= CARD.x + CARD.w + 20, `fighter over the tour card at ${f.x}`);
    }
  }
  assert.ok(minD >= SHIELD_R + 60, `closest approach ${minD.toFixed(0)}px`);
  // moving the opposite way: when the planet heads right, the fighter heads left
  const g0 = gatewayPos(1000, false), g1 = gatewayPos(2000, false);
  const f0 = fighterPos(1000, false), f1 = fighterPos(2000, false);
  assert.ok(Math.sign(g1.x - g0.x) === -Math.sign(f1.x - f0.x));
});

test('the moon hides when it passes behind the planet', async () => {
  const { moonAt, PLANET_R } = await import('./render.mjs');
  const c = { x: 500, y: 300 };
  const behind = moonAt(c, -Math.PI / 2, -0.32); // back of the orbit, over the disc
  assert.equal(behind.visible, false, 'behind the planet');
  assert.ok(Math.hypot(behind.x - c.x, behind.y - c.y) < PLANET_R);
  assert.equal(moonAt(c, Math.PI / 2, -0.32).visible, true, 'in front of the planet');
  assert.equal(moonAt(c, Math.PI, -0.32).visible, true, 'off to the side');
  for (let a = 0; a < Math.PI * 2; a += 0.01) {
    const m = moonAt(c, a, -0.32 + 0.12 * Math.sin(a * 3));
    if (Math.sin(a) < 0 && Math.hypot(m.x - c.x, m.y - c.y) < PLANET_R + 4) assert.equal(m.visible, false, `peeks through at ${a}`);
  }
});

test('the fighter glows, but its glow never leaks into the rest of the scene', async () => {
  const { drawShip } = await import('./render.mjs');
  const calls = [];
  let depth = 0, glowed = false;
  const ctx = new Proxy({ shadowBlur: 0, globalAlpha: 1 }, {
    get(t, k) {
      if (k in t) return t[k];
      return (...a) => {
        calls.push(k);
        if (k === 'save') depth++;
        if (k === 'restore') { depth--; t.shadowBlur = 0; }
      };
    },
    set(t, k, v) { if (k === 'shadowBlur' && v > 0) glowed = true; t[k] = v; return true; },
  });
  drawShip(ctx, { x: 100, y: 100, a: 0.5, flash: 1 });
  assert.equal(depth, 0, 'every save is restored');
  assert.ok(glowed, 'the fighter has a neon glow');
  assert.ok(calls.filter((c) => c === 'stroke').length >= 8, 'a detailed hull, not a sketch');
});
