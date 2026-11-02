import { createSaveQueue } from './queue.js';
import { cloneEntry, rewriteRefs, sendOpsMode, sendRestMode } from './rest-wire.js';

const defaultFetch = (...args) => globalThis.fetch(...args);
const joinQuery = (params) => '?' + new URLSearchParams(Object.entries(params).filter(([, v]) => v != null)).toString();

/** @typedef {import('../../../types/board.d.ts').RestProviderOptions} RestProviderOptions */

async function readJson(res) {
  if (!res.ok) throw Object.assign(new Error('sisflow board: load failed'), { status: res.status });
  const text = await res.text?.();
  return text ? JSON.parse(text) : {};
}

/** @param {RestProviderOptions} [opts] */
export function createRestProvider({ base = '/api', fetch = defaultFetch, mode = 'rest', debounceMs = 300, retry } = {}) {
  const rebinders = [];
  let keepalive = false;
  const rebind = (from, to) => {
    queue.rebind(from, to);
    for (const fn of rebinders.slice()) fn({ from, to });
  };
  const send = (batch) => mode === 'ops'
    ? sendOpsMode({ base, fetch, batch, keepalive })
    : sendRestMode({ base, fetch, batch, keepalive, rebind });
  const queue = createSaveQueue({ debounceMs, retry, send });

  return {
    async load() {
      return readJson(await fetch(base + '/board', { method: 'GET' }));
    },
    async loadPage({ column, row, offset, limit }) {
      return readJson(await fetch(base + '/cards' + joinQuery({ column, row, offset, limit }), { method: 'GET' }));
    },
    push(entry) {
      queue.push(cloneEntry(entry));
    },
    flush(opts = {}) {
      keepalive = !!opts.keepalive;
      return queue.flush().finally(() => { keepalive = false; });
    },
    pending: () => queue.pending(),
    onError: (fn) => queue.onError(fn),
    onRebind(fn) { rebinders.push(fn); return () => { const i = rebinders.indexOf(fn); if (i >= 0) rebinders.splice(i, 1); }; },
    rebind(from, to) { queue.rebind(from, to); },
  };
}

export { rewriteRefs };
