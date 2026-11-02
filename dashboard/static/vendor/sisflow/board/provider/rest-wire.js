import { clone } from '../core/shapes.js';

const entity = (op) => op.op.split('-')[0];
const urlId = (id) => encodeURIComponent(String(id));
const body = (v) => ({ headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(v) });
const json = async (res) => {
  if (res.status === 204) return {};
  const text = await res.text?.();
  return text ? JSON.parse(text) : {};
};

function resourcePayload(op, dir = 'do') {
  const p = op.payload, undo = dir === 'undo', type = entity(op);
  if (op.op.endsWith('-add')) return undo
    ? { method: 'DELETE', path: `/${type}s/${urlId(p[type].id)}` }
    : { method: 'POST', path: `/${type}s`, body: { [type]: p[type], at: p.at ?? { index: p.index } } };
  if (op.op.endsWith('-remove')) return undo
    ? { method: 'POST', path: `/${type}s`, body: { [type]: p[type] ?? p.card, at: p.at ?? { index: p.index } } }
    : { method: 'DELETE', path: `/${type}s/${urlId(p.id ?? p[type]?.id)}` };
  if (op.op.endsWith('-update')) return {
    method: 'PUT', path: `/${type}s/${urlId(p.id)}`, body: { patch: undo ? p.prev : p.patch },
  };
  if (op.op.endsWith('-move')) return {
    method: 'PUT', path: `/${type}s/${urlId(p.id)}/move`, body: undo ? p.from : p.to,
  };
  throw new Error('sisflow board: unsupported provider op ' + op.op);
}

export async function sendOpsMode({ base, fetch, batch, keepalive }) {
  const res = await fetch(base + '/board/ops', {
    method: 'POST',
    ...body(batch.map((item) => ({ dir: item.dir || 'do', ops: item.ops }))),
    keepalive,
  });
  if (!res.ok) throw Object.assign(new Error('sisflow board: save failed'), { status: res.status });
  return json(res);
}

export async function sendRestMode({ base, fetch, batch, keepalive, rebind }) {
  for (const item of batch) {
    const dir = item.dir || 'do';
    for (const op of item.ops) {
      const req = resourcePayload(op, dir);
      const res = await fetch(base + req.path, { method: req.method, ...(req.body ? body(req.body) : {}), keepalive });
      if (!res.ok) throw Object.assign(new Error('sisflow board: save failed'), { status: res.status, item });
      const data = await json(res);
      if (dir === 'do' && op.op === 'card-add' && data.id != null && data.id !== op.payload.card.id) {
        const from = op.payload.card.id, to = String(data.id);
        rebind(from, to);
        rewriteRefs(batch, from, to);
      }
    }
  }
}

export function rewriteRefs(target, from, to) {
  const visit = (v, key) => {
    if (Array.isArray(v)) v.forEach((item, i) => { if (item === from) v[i] = to; else visit(item, key); });
    else if (v && typeof v === 'object') for (const [k, val] of Object.entries(v)) {
      if ((k === 'id' || k === 'from' || k === 'to') && val === from) v[k] = to;
      else visit(val, k);
    }
  };
  visit(target);
  return target;
}

export const cloneEntry = (entry) => ({ ...entry, ops: clone(entry.ops || []) });
