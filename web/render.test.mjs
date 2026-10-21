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

test('the alien gunner holds the bottom-left corner, clear of the planet', async () => {
  const { gatewayPos, GUNNER, GUNNER_SCALE, muzzleAt, SHIELD_R, FIELD } = await import('./render.mjs');
  assert.ok(GUNNER.x <= FIELD.x0 + 120 && GUNNER.y >= FIELD.y1 - 90, `gunner at ${GUNNER.x},${GUNNER.y}`);
  assert.ok(GUNNER_SCALE >= 1.5, 'a big cannon');
  assert.ok(GUNNER.y + 34 * GUNNER_SCALE <= FIELD.y1 - 10, 'its base sits inside the field, above the legend');
  assert.ok(GUNNER.x - 50 * GUNNER_SCALE >= 0, 'its ground stays on screen');
  const m = muzzleAt({ ...GUNNER, a: 0, flash: 0 });
  assert.ok(Math.abs(m.x - (GUNNER.x + 50 * GUNNER_SCALE)) < 1e-9, 'shots leave from the emitter at the end of the rails');
  let minD = Infinity;
  for (let t = 0; t < 900000; t += 250) {
    for (const tour of [false, true]) {
      const g = gatewayPos(t, tour);
      minD = Math.min(minD, Math.hypot(g.x - GUNNER.x, g.y - GUNNER.y));
    }
  }
  assert.ok(minD >= SHIELD_R + 60, `closest approach ${minD.toFixed(0)}px`);
});

test('the cannon swings from straight up to just below level, never through the ground', async () => {
  const { cannonAim, CANNON_MIN, CANNON_MAX } = await import('./render.mjs');
  const g = { x: 100, y: 500 };
  assert.ok(Math.abs(cannonAim(g, { x: 100, y: 100 }) + Math.PI / 2) < 1e-9, 'straight up');
  assert.ok(Math.abs(cannonAim(g, { x: 500, y: 100 }) + Math.PI / 4) < 1e-9, 'up and right');
  assert.equal(cannonAim(g, { x: 500, y: 700 }), CANNON_MAX, 'low right: as low as it goes');
  assert.equal(cannonAim(g, { x: 20, y: 600 }), CANNON_MIN, 'behind it, low: back to its highest');
  assert.equal(cannonAim(g, { x: 20, y: 100 }), CANNON_MIN, 'up and behind: its highest');
});

test('the kagent robot and its rail cannon draw without leaking canvas state, firing or not', async () => {
  const { drawGunner } = await import('./render.mjs');
  for (const flash of [0, 1]) {
    let depth = 0, strokes = 0;
    const ctx = new Proxy({ globalAlpha: 1 }, {
      get(t, k) {
        if (k in t) return t[k];
        return () => { if (k === 'save') depth++; if (k === 'restore') depth--; if (k === 'stroke') strokes++; };
      },
      set(t, k, v) { t[k] = v; return true; },
    });
    drawGunner(ctx, { x: 80, y: 560, a: -0.8, flash, target: { x: 400, y: 200 } }, 1000);
    assert.equal(depth, 0);
    assert.ok(strokes >= 14, 'the kagent robot, a rail cannon, its turret');
  }
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
