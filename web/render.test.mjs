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

test('the agentgateway ship floats, carrying its LLM, MCP and A2A ports', async () => {
  const { shipPos, portsAt, FIELD, CARD, VAULT } = await import('./render.mjs');
  const a = shipPos(0, false), b = shipPos(20000, false);
  assert.ok(Math.hypot(a.x - b.x, a.y - b.y) > 20, 'it moves');
  for (let t = 0; t < 600000; t += 997) {
    for (const tour of [false, true]) {
      const p = shipPos(t, tour);
      assert.ok(p.x - 140 >= FIELD.x0 && p.x + 140 <= VAULT.x, `x ${p.x} out of bounds`);
      assert.ok(p.y - 60 >= 44 && p.y + 50 <= FIELD.y1, `y ${p.y} out of bounds`);
      if (tour) assert.ok(p.x - 140 >= CARD.x + CARD.w, `ship over the tour card at ${p.x}`);
    }
  }
  const ports = portsAt({ x: 400, y: 200 });
  for (const k of ['llm', 'mcp', 'a2a']) assert.ok(ports[k] && ports[k].y > 200, `port ${k} hangs below the hull`);
  assert.equal(FIELD.x0, 20, 'the field gets its full width back');
});
