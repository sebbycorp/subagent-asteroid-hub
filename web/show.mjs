// Pure presentation logic: event pacing, rock shapes, ship aim. No DOM.

// Real lifecycles take ~1–2 s; events are shown after a short delay and at
// least GAP_MS apart so each one can be read. Order is never changed.
export const DELAY_MS = 600;
export const GAP_MS = 900;

export function newQueue() {
  return { items: [], last: -Infinity };
}

// schedule adds an event and returns when it will be displayed.
export function schedule(queue, ev, nowMs) {
  const displayAt = Math.max(nowMs + DELAY_MS, queue.last + GAP_MS);
  queue.last = displayAt;
  queue.items.push({ ev, displayAt });
  return displayAt;
}

// due removes and returns the events whose time has come, in seq order.
export function due(queue, nowMs) {
  queue.items.sort((a, b) => a.ev.seq - b.ev.seq);
  const out = [];
  while (queue.items.length && queue.items[0].displayAt <= nowMs) {
    out.push(queue.items.shift().ev);
  }
  // an earlier-seq item still waiting blocks later ones that are ready
  return out;
}

// pending reports whether any queued event concerns this actor.
export function pending(queue, uid) {
  return queue.items.some((i) => i.ev.actorUid === uid);
}

// nextSuspend is the actor whose suspend will be shown next, if any.
export function nextSuspend(queue) {
  const it = [...queue.items].sort((a, b) => a.ev.seq - b.ev.seq).find((i) => i.ev.kind === 'suspend');
  return it ? it.ev.actorUid : null;
}

function seeded(str) {
  let h = 2166136261;
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return () => {
    h = (h + 0x6d2b79f5) | 0;
    let t = Math.imul(h ^ (h >>> 15), 1 | h);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// rockShape is a jagged polygon, the same for the same actor every time.
export function rockShape(uid) {
  const rand = seeded(uid);
  const n = 8 + Math.floor(rand() * 4);
  const radius = 18 + rand() * 10;
  const pts = [];
  for (let i = 0; i < n; i++) {
    const a = (i / n) * Math.PI * 2;
    const r = radius * (0.7 + 0.3 * rand());
    pts.push([Math.cos(a) * r, Math.sin(a) * r]);
  }
  return pts;
}

export function aimAngle(ship, target) {
  return Math.atan2(target.y - ship.y, target.x - ship.x);
}

// wrapLines splits text into at most maxLines lines of maxChars.
export function wrapLines(text, maxChars, maxLines = 2) {
  const words = (text || '').split(' ');
  const lines = [''];
  for (const w of words) {
    const cur = lines[lines.length - 1];
    if (!cur) lines[lines.length - 1] = w;
    else if ((cur + ' ' + w).length <= maxChars) lines[lines.length - 1] = cur + ' ' + w;
    else lines.push(w);
  }
  return lines.slice(0, maxLines);
}

// Reconciliation waits this long before correcting the stage to match the
// state: events for a change (e.g. a suspend held for its turn-end time) may
// still be on their way, and the stage must not pre-empt them.
export const GRACE_MS = 1500;

// settled records when the stage and the state started to disagree about an
// actor and reports whether that disagreement has lasted GRACE_MS.
export function settled(seen, uid, mismatch, nowMs) {
  if (!mismatch) {
    seen.delete(uid);
    return false;
  }
  if (!seen.has(uid)) seen.set(uid, nowMs);
  return nowMs - seen.get(uid) >= GRACE_MS;
}

// Captions get their own queue: each stays up at least CAPTION_MS so it can
// be read, even when animations run faster. If a burst backs up more than
// MAX_WAITING, "running" captions go first (the rock's label already says
// it), then creates/resumes; suspend captions — the point of the show — stay.
export const CAPTION_MS = 2400;
export const MAX_WAITING = 3;

export function newCaptions() {
  return { current: null, since: -Infinity, waiting: [] };
}

export function pushCaption(cq, ev, nowMs) {
  if (cq.current === null) {
    cq.current = ev.caption;
    cq.since = nowMs;
    return;
  }
  cq.waiting.push(ev);
  while (cq.waiting.length > MAX_WAITING) {
    let i = cq.waiting.findIndex((e) => e.kind === 'running');
    if (i < 0) i = cq.waiting.findIndex((e) => e.kind !== 'suspend');
    cq.waiting.splice(i < 0 ? 0 : i, 1);
  }
}

export function currentCaption(cq, nowMs) {
  if (cq.waiting.length && nowMs - cq.since >= CAPTION_MS) {
    cq.current = cq.waiting.shift().caption;
    cq.since = nowMs;
  }
  return cq.current;
}

// Per-actor animation bookkeeping. beginAnim returns a token; endAnim reports
// whether that token is still the actor's latest, so an animation overtaken
// by a newer event for the same actor (e.g. a suspend capsule still landing
// when the resume starts) doesn't apply its stale result.
export function newAnims() {
  return { latest: new Map(), count: new Map(), next: 1 };
}

export function beginAnim(an, uid) {
  const tok = an.next++;
  an.latest.set(uid, tok);
  an.count.set(uid, (an.count.get(uid) || 0) + 1);
  return tok;
}

export function endAnim(an, uid, tok) {
  const n = (an.count.get(uid) || 1) - 1;
  if (n > 0) an.count.set(uid, n);
  else an.count.delete(uid);
  return an.latest.get(uid) === tok;
}

export function animating(an, uid) {
  return an.count.has(uid);
}
