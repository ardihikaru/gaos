/* sisflow — inverse-op undo/redo stack (docs/05). History stores plain-data
 * ops recorded by core/graph.js; applying an entry replays the ops through
 * the graph's applyOp (injected late via setApplier to keep the dependency
 * one-way). Entries are single ops or named batches; nested batches flatten
 * into the outermost. */

export function createHistory({ emitter, depth = 200, readOnly = false, trackWhileReadOnly = false } = {}) {
  let undoStack = []; // entries: { name?, ops: [op] }
  let redoStack = [];
  let cleanMark = 0; // undoStack.length at the last markClean; -1 = unreachable
  let openBatch = null; // { name, ops } while a batch fn runs
  let batchDepth = 0;
  let flight = 0; // >0 while batch/load/undo/redo commits (re-entrancy gate)
  let applier = null; // graph.applyOp(op, dir), set by index.js wiring
  const subs = []; // history 'change' listeners

  const state = (entry, reason) => ({ canUndo: undoStack.length > 0, canRedo: redoStack.length > 0, dirty: isDirty(), entry, reason });

  function notify(entry, reason) {
    for (const fn of subs.slice()) fn(state(entry, reason));
  }

  function setApplier(fn) {
    applier = fn;
  }

  const inFlight = () => flight > 0;

  /** Run fn with the re-entrancy gate up; drain queued mutations after. */
  function runInFlight(fn) {
    flight += 1;
    try {
      return fn();
    } finally {
      flight -= 1;
      if (flight === 0) emitter.drainQueue();
    }
  }

  function push(entry) {
    undoStack.push(entry);
    redoStack = []; // any new committed entry clears redo (docs/05 §5)
    if (undoStack.length > depth) {
      undoStack.shift();
      cleanMark = cleanMark === 0 ? -1 : Math.max(-1, cleanMark - 1);
    }
  }

  /* Gesture coalescing: successive per-frame ops on the same record inside a
   * batch merge into one op (drag/reshape stay O(1) per node, docs/04 §3). */
  function coalesce(ops, op) {
    const last = ops[ops.length - 1];
    if (!last || last.op !== op.op || last.payload.id !== op.payload.id) return false;
    if (op.op === 'node-move') {
      last.payload.to = op.payload.to;
      return true;
    }
    const same = (a, b) => JSON.stringify(Object.keys(a).sort()) === JSON.stringify(Object.keys(b).sort());
    if (op.op === 'edge-update' && same(last.payload.patch, op.payload.patch)) {
      last.payload.patch = op.payload.patch; // prev stays from the first frame
      return true;
    }
    return false;
  }

  /** Recorder hook called by the graph for every applied op (docs/05 §2). */
  function record(op) {
    const tracked = !readOnly || trackWhileReadOnly;
    if (openBatch) {
      if (tracked && !coalesce(openBatch.ops, op)) openBatch.ops.push(op);
      return;
    }
    if (tracked) {
      const entry = { ops: [op] };
      push(entry);
      notify(entry, 'record');
    }
    emitter.emit('change', { reason: 'op' });
  }

  /* Incremental batch API for pointer gestures (docs/04 §1.2): begin on tool
   * start, mutate per frame, commit on end / abort on Esc. The flight gate
   * stays up so handler mutations queue until the gesture settles. */
  function beginBatch(name) {
    if (openBatch) throw new Error('sisflow: batch already open');
    openBatch = { name, ops: [] };
    flight += 1;
  }

  function commitBatch() {
    const entry = openBatch;
    openBatch = null;
    flight -= 1;
    if (entry && entry.ops.length > 0) {
      if (!readOnly || trackWhileReadOnly) {
        push(entry);
        notify(entry, 'record');
      }
      emitter.emit('change', { reason: 'batch', batch: entry.name });
    }
    if (flight === 0) emitter.drainQueue();
  }

  function abortBatch() {
    const entry = openBatch;
    openBatch = null;
    flight -= 1;
    if (entry) rollback(entry.ops);
    if (flight === 0) emitter.drainQueue();
  }

  function batch(name, fn) {
    if (openBatch) {
      batchDepth += 1; // nested: flatten into the outermost entry
      try {
        return fn();
      } finally {
        batchDepth -= 1;
      }
    }
    beginBatch(name);
    let result;
    try {
      result = fn();
    } catch (err) {
      abortBatch();
      throw err;
    }
    commitBatch();
    return result;
  }

  /* Aborted batch: undo already-applied ops in reverse; a failed rollback is
   * a hard invariant break — clear the stacks, keep the document as-is. */
  function rollback(ops) {
    try {
      runInFlight(() => {
        for (let i = ops.length - 1; i >= 0; i -= 1) applier(ops[i], 'undo');
      });
    } catch (err) {
      console.error('sisflow: history rollback failed — clearing undo stack', err);
      clear();
    }
  }

  function step(fromStack, toStack, dir, reason) {
    if (readOnly || fromStack.length === 0) return false;
    const entry = fromStack.pop();
    runInFlight(() => {
      const ops = dir === 'undo' ? entry.ops.slice().reverse() : entry.ops;
      for (const op of ops) applier(op, dir === 'undo' ? 'undo' : 'do');
    });
    toStack.push(entry);
    emitter.emit('change', { reason });
    notify(entry, reason);
    return true;
  }

  const undo = () => step(undoStack, redoStack, 'undo', 'undo');
  const redo = () => step(redoStack, undoStack, 'redo', 'redo');
  const canUndo = () => undoStack.length > 0;
  const canRedo = () => redoStack.length > 0;
  const isDirty = () => cleanMark !== undoStack.length;

  function markClean() {
    cleanMark = undoStack.length;
    notify(undefined, 'markClean');
  }

  function clear() {
    undoStack = [];
    redoStack = [];
    cleanMark = 0;
    notify(undefined, 'clear');
  }

  function dropEntry(entry) {
    const index = undoStack.indexOf(entry);
    undoStack = undoStack.filter((e) => e !== entry);
    redoStack = redoStack.filter((e) => e !== entry);
    if (index >= 0) cleanMark = cleanMark > index ? Math.max(0, cleanMark - 1) : cleanMark;
    if (cleanMark > undoStack.length) cleanMark = -1;
    notify(undefined, 'drop');
  }

  function rewriteEntries(fn) {
    for (const entry of [...undoStack, ...redoStack]) fn(entry);
  }

  function on(event, fn) {
    // single channel: history 'change' (docs/05 §2)
    subs.push(fn);
    return () => {
      const i = subs.indexOf(fn);
      if (i >= 0) subs.splice(i, 1);
    };
  }

  return {
    batch, beginBatch, commitBatch, abortBatch,
    undo, redo, canUndo, canRedo, isDirty, markClean, clear, on,
    record, setApplier, inFlight, runInFlight, dropEntry, rewriteEntries,
    get readOnly() { return readOnly; },
  };
}
