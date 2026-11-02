/* sisflow — event emitter (docs/03 §4). Handlers run synchronously in
 * subscription order. The emitter also owns the re-entrancy guard: while a
 * commit (batch/load/undo/redo) is in flight, graph mutations requested from
 * inside handlers are queued and replayed after the commit; a synchronous
 * mutation from a handler outside any commit throws ReentrancyError. */

export class ReentrancyError extends Error {
  constructor(msg) {
    super(msg);
    this.name = 'ReentrancyError';
  }
}

/** Create an emitter. `on/off/once/emit` as usual; `queue` + guard helpers
 * implement the docs/03 §4 re-entrancy rule for the graph store. */
export function createEmitter() {
  const handlers = new Map(); // event -> fn[]
  let dispatching = 0; // >0 while emit() runs handlers
  const queued = []; // mutations deferred until after the current commit

  function on(event, fn) {
    if (!handlers.has(event)) handlers.set(event, []);
    handlers.get(event).push(fn);
    return () => off(event, fn);
  }

  function off(event, fn) {
    const list = handlers.get(event);
    if (!list) return;
    const i = list.indexOf(fn);
    if (i >= 0) list.splice(i, 1);
  }

  function once(event, fn) {
    const unsub = on(event, (payload) => {
      unsub();
      fn(payload);
    });
    return unsub;
  }

  function emit(event, payload) {
    const list = handlers.get(event);
    if (!list || list.length === 0) return;
    dispatching += 1;
    try {
      // copy: handlers may unsubscribe themselves mid-dispatch
      for (const fn of list.slice()) fn(payload);
    } finally {
      dispatching -= 1;
    }
  }

  /** True while any handler chain is running. */
  function isDispatching() {
    return dispatching > 0;
  }

  /** Defer a mutation thunk until the current commit finishes (docs/03 §4). */
  function queue(fn) {
    queued.push(fn);
  }

  /** Drain deferred mutations; each runs as its own history entry. */
  function drainQueue() {
    while (queued.length > 0) {
      const fn = queued.shift();
      fn();
    }
  }

  function dispose() {
    handlers.clear();
    queued.length = 0;
  }

  return { on, off, once, emit, queue, drainQueue, isDispatching, dispose };
}
