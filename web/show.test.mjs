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

test('captions hold CAPTION_MS each and queue in order', async () => {
  const { newCaptions, pushCaption, currentCaption, CAPTION_MS } = await import('./show.mjs');
  const cq = newCaptions();
  pushCaption(cq, { kind: 'resume', caption: 'A' }, 0);
  pushCaption(cq, { kind: 'suspend', caption: 'B' }, 100);
  assert.equal(currentCaption(cq, 100), 'A');
  assert.equal(currentCaption(cq, CAPTION_MS - 1), 'A');
  assert.equal(currentCaption(cq, CAPTION_MS), 'B');
  assert.equal(currentCaption(cq, 10 * CAPTION_MS), 'B', 'last caption stays up');
});

test('caption backlog drops running captions first, never suspends', async () => {
  const { newCaptions, pushCaption, currentCaption, CAPTION_MS, MAX_WAITING } = await import('./show.mjs');
  const cq = newCaptions();
  pushCaption(cq, { kind: 'resume', caption: 'r1' }, 0);
  const kinds = ['running', 'suspend', 'resume', 'running', 'suspend', 'resume', 'running', 'suspend'];
  kinds.forEach((k, i) => pushCaption(cq, { kind: k, caption: `${k}${i}` }, 0));
  assert.ok(cq.waiting.length <= MAX_WAITING, `waiting ${cq.waiting.length}`);
  const shown = [];
  for (let t = 0; t < 20 * CAPTION_MS; t += CAPTION_MS) {
    const c = currentCaption(cq, t);
    if (shown[shown.length - 1] !== c) shown.push(c);
  }
  for (const s of ['suspend1', 'suspend4', 'suspend7']) assert.ok(shown.includes(s), `${s} missing from ${shown}`);
  assert.ok(!shown.includes('running0'), 'oldest running caption should be dropped first');
});

test('a newer event makes an older animation stale; busy counts overlap', async () => {
  const { newAnims, beginAnim, endAnim, animating } = await import('./show.mjs');
  const an = newAnims();
  const suspendTok = beginAnim(an, 'a'); // suspend capsule still flying…
  const resumeTok = beginAnim(an, 'a'); // …when the same actor's resume starts
  assert.equal(animating(an, 'a'), true);
  assert.equal(endAnim(an, 'a', suspendTok), false, 'stale suspend must not touch the vault');
  assert.equal(animating(an, 'a'), true, 'resume still in flight');
  assert.equal(endAnim(an, 'a', resumeTok), true);
  assert.equal(animating(an, 'a'), false);
});

test('liveLine describes the focused actor and density from state', async () => {
  const { liveLine } = await import('./show.mjs');
  const state = {
    snapshot: {
      pools: [{ name: 'p', workers: ['w1'] }],
      actors: {
        u1: { uid: 'u1', sessionId: 's1', state: 'suspended', snapshotGen: 3 },
        u2: { uid: 'u2', sessionId: 's2', state: 'running', worker: 'kagent-default-abc-jxg28' },
      },
    },
  };
  assert.equal(liveLine({ kind: 'suspend', focus: ['s1'] }, state), 'LIVE: SUSPENDED · SNAPSHOT GEN 3 · 0 CPU');
  assert.equal(liveLine({ kind: 'resume', focus: ['s2'] }, state), 'LIVE: RUNNING ON WORKER JXG28');
  assert.equal(liveLine({ kind: 'density', focus: [] }, state), 'LIVE: 2 SESSIONS · 1 WORKER · 1 RUNNING NOW');
  assert.equal(liveLine({ kind: 'create', focus: ['missing'] }, state), 'LIVE: WAITING FOR THE ACTOR…');
});

test('log lines: short, real, one per event', async () => {
  const { logLine } = await import('./show.mjs');
  const at = '2026-10-21T01:23:45.678Z';
  assert.equal(logLine({ src: 'event', kind: 'resume', agent: 'asteroids-demo-poet', at }), '01:23:45 RESUME   POET');
  assert.equal(logLine({ src: 'event', kind: 'running', agent: 'asteroids-demo-poet', restoreMs: 125, at }), '01:23:45 RUNNING  POET 125MS');
  assert.equal(logLine({ src: 'event', kind: 'running', agent: 'hello-substrate', startMs: 300, at }), '01:23:45 STARTED  HELLO-SUBSTRATE 300MS');
  assert.equal(logLine({ src: 'event', kind: 'suspend', agent: 'asteroids-demo-poet', suspendMs: 4, at }), '01:23:45 SUSPEND  POET INSTANT');
  assert.equal(logLine({ src: 'gateway', kind: 'llm', agent: 'asteroids-demo-poet', model: 'qwen3:8b', durationMs: 1775, at }), '01:23:45 ⬡ POET → QWEN3:8B 1.8S');
  assert.equal(logLine({ src: 'gateway', kind: 'tool', agent: 'asteroids-demo-poet', tool: 'k8s_get_resources', durationMs: 54, at }), '01:23:45 ⬡ POET → K8S_GET_RESOURCES 54MS');
  assert.equal(logLine({ src: 'gateway', kind: 'chat', durationMs: 4273, at }), '01:23:45 ⬡ CHAT IN → AGENT 4.3S');
});

test('gateway requests map to the outside world they reach', async () => {
  const { planetFor } = await import('./show.mjs');
  assert.equal(planetFor({ kind: 'llm', target: 'qwen', model: 'qwen3:8b' }).key, 'model');
  assert.equal(planetFor({ kind: 'llm', target: 'qwen', model: 'qwen3:8b' }).label, 'QWEN3:8B');
  assert.equal(planetFor({ kind: 'tool', target: 'kagent-tools' }).key, 'tools');
  assert.equal(planetFor({ kind: 'chat', target: 'kagent-a2a' }).key, 'people');
});

test('gateway chapter live line counts this agent\'s gateway calls', async () => {
  const { liveLine } = await import('./show.mjs');
  const line = liveLine({ kind: 'gateway', focus: ['s1'] }, { snapshot: { actors: {} } }, { llm: 2, tool: 1 });
  assert.equal(line, 'LIVE: AGENTGATEWAY HANDLED 2 MODEL CALLS · 1 TOOL CALL');
});

test('a gateway call is pinned on an asteroid only when it is unambiguous', async () => {
  const { attribute } = await import('./show.mjs');
  const a = { agent: 'poet', born: 1 }, b = { agent: 'coder', born: 2 }, dying = { agent: 'scout', born: 3, dying: true };
  assert.equal(attribute({ agent: 'coder' }, [a, b]), b, 'tagged call: its agent');
  assert.equal(attribute({}, [a, dying]), a, 'untagged call, one busy asteroid: that one');
  assert.equal(attribute({}, [a, b]), null, 'untagged call, several busy: none');
  assert.equal(attribute({ agent: 'ghost' }, [a]), null, 'tagged call for an agent not on screen: none');
});

test('a call the backend pinned to an actor goes to that actor\'s rock', async () => {
  const { attribute } = await import('./show.mjs');
  const a = { uid: 'u1', agent: 'poet', born: 1 }, b = { uid: 'u2', agent: 'poet', born: 2 };
  assert.equal(attribute({ agent: 'poet', actorUid: 'u1' }, [a, b]), a);
});
