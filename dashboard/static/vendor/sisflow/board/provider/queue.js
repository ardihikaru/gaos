import { clone } from '../core/shapes.js';

const defaultClock = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (id) => clearTimeout(id),
};

const idOf = (op) => op.payload?.id ?? op.payload?.card?.id ?? op.payload?.column?.id ?? op.payload?.row?.id;
const familyOf = (op) => op.op.split('-')[0];

function wait(clock, ms) {
  return new Promise((resolve) => clock.setTimeout(resolve, ms));
}

function mergeOp(a, b) {
  if (!a || familyOf(a) !== familyOf(b) || idOf(a) !== idOf(b)) return false;
  if (a.op.endsWith('-update') && b.op === a.op) {
    const prev = clone(a.payload.prev);
    Object.assign(a.payload.patch, clone(b.payload.patch));
    Object.assign(a.payload.prev, clone(b.payload.prev), prev);
    return true;
  }
  if (a.op.endsWith('-move') && b.op === a.op) {
    a.payload.to = clone(b.payload.to);
    return true;
  }
  if (a.op.endsWith('-add') && b.op.endsWith('-update')) {
    Object.assign(a.payload[familyOf(a)], clone(b.payload.patch));
    return true;
  }
  if (a.op.endsWith('-add') && b.op.endsWith('-remove')) return 'drop';
  if ((a.op.endsWith('-update') || a.op.endsWith('-move')) && b.op.endsWith('-remove')) return clone(b);
  return false;
}

function compactOps(items) {
  const out = [];
  for (const item of items) {
    for (const op of item.ops || []) {
      const next = clone(op);
      const at = out.findIndex((old) => familyOf(old) === familyOf(next) && idOf(old) === idOf(next));
      const merged = at >= 0 ? mergeOp(out[at], next) : false;
      if (merged === true) continue;
      if (merged === 'drop') out.splice(at, 1);
      else if (merged) out[at] = merged;
      else out.push(next);
    }
  }
  return out;
}

export function createSaveQueue({ debounceMs = 300, retry = { max: 2, baseMs: 500 }, clock = defaultClock, send }) {
  if (typeof send !== 'function') throw new Error('sisflow board: queue send required');
  let buffer = [], timer = null, inFlight = null;
  const errors = [];
  const max = retry?.max ?? 2, baseMs = retry?.baseMs ?? 500;

  function arm() {
    if (timer != null) clock.clearTimeout(timer);
    timer = clock.setTimeout(() => { timer = null; flush(); }, debounceMs);
  }

  async function drain(items) {
    let attempt = 0;
    const batch = items.map((item) => ({ ...item, ops: compactOps([item]) }));
    const wire = compactOps(items);
    while (true) {
      try {
        await send(batch, wire);
        return;
      } catch (err) {
        if (attempt >= max) {
          const failed = err.item ? [err.item] : items;
          for (const item of failed) for (const fn of errors.slice()) fn(err, item);
          return;
        }
        await wait(clock, baseMs * 2 ** attempt);
        attempt += 1;
      }
    }
  }

  function flush() {
    if (timer != null) { clock.clearTimeout(timer); timer = null; }
    if (inFlight) return inFlight;
    if (buffer.length === 0) return Promise.resolve();
    const items = buffer;
    buffer = [];
    inFlight = drain(items).finally(() => {
      inFlight = null;
      if (buffer.length > 0) flush();
    });
    return inFlight;
  }

  function rebind(from, to) {
    for (const item of buffer) rewriteItem(item, from, to);
  }

  return {
    push(item) {
      buffer.push({ ...item, ops: clone(item.ops || []) });
      arm();
    },
    flush,
    pending: () => buffer.length + (inFlight ? 1 : 0),
    onError(fn) { errors.push(fn); return () => { const i = errors.indexOf(fn); if (i >= 0) errors.splice(i, 1); }; },
    rebind,
  };
}

export function rewriteItem(item, from, to) {
  const rewrite = (v) => {
    if (Array.isArray(v)) v.forEach(rewrite);
    else if (v && typeof v === 'object') for (const [k, val] of Object.entries(v)) {
      if ((k === 'id' || k === 'from') && val === from) v[k] = to;
      else rewrite(val);
    }
  };
  rewrite(item.ops);
  return item;
}
