import test from 'node:test';
import assert from 'node:assert/strict';
import { DELAY_MS, GAP_MS, newQueue, schedule, due, rockShape, aimAngle } from './show.mjs';

test('schedule delays first event by DELAY_MS', () => {
  const q = newQueue();
  assert.equal(schedule(q, { seq: 1 }, 1000), 1000 + DELAY_MS);
  assert.deepEqual(due(q, 1000 + DELAY_MS - 1), []);
  assert.deepEqual(due(q, 1000 + DELAY_MS).map((e) => e.seq), [1]);
});

test('schedule spaces bursts', () => {
  const q = newQueue();
  const at = [1, 2, 3, 4].map((seq) => schedule(q, { seq }, 0));
  assert.deepEqual(at, [600, 1500, 2400, 3300]);
  assert.equal(GAP_MS, 900);
  assert.deepEqual(due(q, 600).map((e) => e.seq), [1]);
  assert.deepEqual(due(q, 1499).map((e) => e.seq), []);
  assert.deepEqual(due(q, 1500).map((e) => e.seq), [2]);
  assert.deepEqual(due(q, 5000).map((e) => e.seq), [3, 4]);
});

test('schedule keeps seq order when events arrive out of order', () => {
  const q = newQueue();
  schedule(q, { seq: 5 }, 0);
  schedule(q, { seq: 4 }, 0);
  assert.deepEqual(due(q, 10000).map((e) => e.seq), [4, 5]);
});

test('schedule after a quiet spell is not penalised by an old gap', () => {
  const q = newQueue();
  schedule(q, { seq: 1 }, 0);
  due(q, 600);
  assert.equal(schedule(q, { seq: 2 }, 60000), 60000 + DELAY_MS);
});

test('rockShape deterministic and bounded', () => {
  const a = rockShape('cbcec519-0f94');
  assert.deepEqual(a, rockShape('cbcec519-0f94'));
  assert.notDeepEqual(a, rockShape('d6221e2d-aceb'));
  assert.ok(a.length >= 8 && a.length <= 11, `points ${a.length}`);
  for (const [x, y] of a) {
    const r = Math.hypot(x, y);
    assert.ok(r >= 18 * 0.7 - 1e-9 && r <= 28 + 1e-9, `radius ${r}`);
  }
});

test('aimAngle points at target', () => {
  assert.ok(Math.abs(aimAngle({ x: 0, y: 0 }, { x: 0, y: 10 }) - Math.PI / 2) < 1e-9);
  assert.ok(Math.abs(aimAngle({ x: 0, y: 0 }, { x: -5, y: 0 }) - Math.PI) < 1e-9);
});

test('settled only after a mismatch lasts GRACE_MS', async () => {
  const { settled, GRACE_MS } = await import('./show.mjs');
  const seen = new Map();
  assert.equal(settled(seen, 'a', true, 0), false);
  assert.equal(settled(seen, 'a', true, GRACE_MS - 1), false);
  assert.equal(settled(seen, 'a', true, GRACE_MS), true);
  assert.equal(settled(seen, 'a', false, GRACE_MS + 10), false);
  assert.equal(seen.has('a'), false, 'agreement clears the timer');
  assert.equal(settled(seen, 'a', true, 5000), false, 'a new mismatch starts over');
});
