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
  assert.ok(PLANET_R >= 60, `planet radius ${PLANET_R}: make it big`);
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
