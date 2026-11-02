import { ReentrancyError } from '../../core/events.js';
import { clone, createIdGenerator, normalizeBoard, normalizeCard, normalizeColumn, normalizeRow } from './shapes.js';
import * as ops from './store-ops.js';
/** @typedef {import('../../../types/board.d.ts').BoardDocument} BoardDocument */
export const cellKey = (column, row = null) => String(column) + '\u0000' + (row ?? '');
export function createBoardStore({ emitter, history }) {
  const cards = new Map(), cells = new Map();
  let columns = [], rows = [], rev = 0;
  const state = () => ({ cards, columns, rows, cellOf });
  const snapshot = (v) => Object.freeze(clone(v));
  const cellOf = (column, row = null) => {
    const key = cellKey(column, row);
    if (!cells.has(key)) cells.set(key, []);
    return cells.get(key);
  };
  const idSet = () => new Set([...cards.keys(), ...columns.map((c) => c.id), ...rows.map((r) => r.id)]);
  const colExists = (id) => columns.some((c) => c.id === String(id));
  const rowExists = (id) => id == null || rows.some((r) => r.id === String(id));
  const knownCell = (column, row) => colExists(column) && rowExists(row);
  const clampIndex = (i, len) => Math.max(0, Math.min(Number.isFinite(Number(i)) ? Math.trunc(Number(i)) : len, len));
  const removeFromCell = (id, at) => {
    const cell = cellOf(at.column, at.row), i = cell.indexOf(id);
    if (i >= 0) cell.splice(i, 1);
  };
  const insertRecord = (card, at) => {
    const rec = { ...clone(card), column: at.column, row: at.row ?? null };
    cards.set(rec.id, rec);
    const cell = cellOf(rec.column, rec.row);
    cell.splice(clampIndex(at.index, cell.length), 0, rec.id);
  };
  const emit = (op, dir) => {
    rev += 1;
    emitter.emit('board:change', { op: snapshot(op), dir, revision: rev });
  };

  function submit(op) {
    if (!op) return null;
    if (emitter.isDispatching()) {
      if (history.inFlight()) {
        emitter.queue(() => submit(op));
        return op;
      }
      throw new ReentrancyError('sisflow board: mutated from an event handler outside a batch');
    }
    applyOp(op, 'do');
    history.record(op);
    return op;
  }

  function applyOp(op, dir = 'do') {
    const fwd = dir === 'do';
    const p = op.payload;
    switch (op.op) {
      case 'card-add':
      case 'card-remove': {
        const adding = (op.op === 'card-add') === fwd;
        if (adding) insertRecord(p.card, p.at);
        else { removeFromCell(p.card?.id ?? p.id, p.at); cards.delete(p.card?.id ?? p.id); }
        break;
      }
      case 'card-update':
        ops.applyPatch(cards.get(p.id), fwd ? p.patch : p.prev);
        break;
      case 'card-move': {
        const [from, to] = fwd ? [p.from, p.to] : [p.to, p.from];
        removeFromCell(p.id, from);
        const card = cards.get(p.id);
        Object.assign(card, { column: to.column, row: to.row ?? null });
        cellOf(card.column, card.row).splice(clampIndex(to.index, cellOf(card.column, card.row).length), 0, p.id);
        break;
      }
      case 'card-rebind': {
        const [from, to] = fwd ? [String(p.from), String(p.to)] : [String(p.to), String(p.from)];
        const card = cards.get(from), cell = card && cellOf(card.column, card.row);
        if (!card) break;
        cards.delete(from); card.id = to; cards.set(to, card);
        const i = cell.indexOf(from);
        if (i >= 0) cell[i] = to;
        break;
      }
      case 'column-add':
      case 'row-add': {
        const list = op.op === 'column-add' ? columns : rows;
        const rec = clone(op.op === 'column-add' ? p.column : p.row);
        if (fwd) list.splice(clampIndex(p.index, list.length), 0, rec);
        else list.splice(list.findIndex((r) => r.id === rec.id), 1);
        break;
      }
      case 'column-remove':
      case 'row-remove': {
        const isCol = op.op === 'column-remove';
        const list = isCol ? columns : rows;
        const rec = clone(isCol ? p.column : p.row);
        if (fwd) {
          for (const c of p.cells) { c.ids.forEach((id) => cards.delete(id)); cells.delete(cellKey(c.column, c.row)); }
          list.splice(list.findIndex((r) => r.id === rec.id), 1);
        } else {
          list.splice(clampIndex(p.index, list.length), 0, rec);
          for (const c of p.cells) c.cards.forEach((card, i) => insertRecord(card, { column: c.column, row: c.row, index: i }));
        }
        break;
      }
      case 'column-update':
      case 'row-update': {
        const list = op.op === 'column-update' ? columns : rows;
        ops.applyPatch(list.find((r) => r.id === p.id), fwd ? p.patch : p.prev);
        break;
      }
      case 'column-move':
      case 'row-move': {
        const list = op.op === 'column-move' ? columns : rows;
        const [from, to] = fwd ? [p.from, p.to] : [p.to, p.from];
        const i = list.findIndex((r) => r.id === p.id);
        const [rec] = list.splice(i, 1);
        list.splice(clampIndex(to, list.length), 0, rec);
        break;
      }
      default: throw new Error('sisflow board: unknown op ' + op.op);
    }
    emit(op, dir);
    return true;
  }

  function validate(doc) {
    const seen = new Set();
    for (const rec of [...doc.columns, ...doc.rows, ...doc.cards]) {
      if (seen.has(rec.id)) throw new Error('sisflow board: duplicate id ' + rec.id);
      seen.add(rec.id);
    }
    for (const card of doc.cards) {
      if (!doc.columns.some((c) => c.id === card.column)) throw new Error('sisflow board: unknown column ' + card.column);
      if (card.row != null && !doc.rows.some((r) => r.id === card.row)) throw new Error('sisflow board: unknown row ' + card.row);
    }
  }
  const store = {
    /** @param {BoardDocument} [doc] */ load(doc = {}) {
      const next = normalizeBoard(doc);
      validate(next);
      history.runInFlight(() => { cards.clear(); cells.clear(); columns = clone(next.columns); rows = clone(next.rows);
        for (const card of next.cards) insertRecord(card, { column: card.column, row: card.row, index: cellOf(card.column, card.row).length });
        history.clear(); rev += 1; emitter.emit('change', { reason: 'load' }); emitter.emit('board:change', { op: snapshot({ op: 'board-load', payload: { reason: 'load' } }), dir: 'do', revision: rev });
      });
    },
    ingest(doc = {}, at = {}) {
      const firstColumn = columns[0]?.id ?? null;
      const next = createIdGenerator('c', idSet());
      history.runInFlight(() => {
        for (const raw of doc.cards || []) {
          const card = normalizeCard({ ...raw, column: at.column ?? raw.column, row: at.row ?? raw.row }, { firstColumn, nextId: next });
          if (!knownCell(card.column, card.row)) throw new Error('sisflow board: unknown cell');
          ops.assertFreeId(state(), card.id);
          applyOp(ops.cardAdd(card, { column: card.column, row: card.row, index: at.index ?? cellOf(card.column, card.row).length }), 'do');
        }
      });
    },
    addCard(card, at = {}) {
      const firstColumn = columns[0]?.id ?? null;
      const rec = normalizeCard({ ...card, column: at.column ?? card.column, row: at.row ?? card.row }, { firstColumn, nextId: createIdGenerator('c', idSet()) });
      if (!knownCell(rec.column, rec.row)) throw new Error('sisflow board: unknown cell');
      ops.assertFreeId(state(), rec.id);
      return submit(ops.cardAdd(rec, { column: rec.column, row: rec.row, index: at.index ?? cellOf(rec.column, rec.row).length })) ? rec.id : false;
    },
    updateCard: (id, patch) => submit(ops.cardUpdate(state(), id, patch)),
    removeCard: (id) => submit(ops.cardRemove(state(), id)),
    moveCard(id, to) {
      if (!knownCell(to.column, to.row ?? null)) throw new Error('sisflow board: unknown cell');
      return submit(ops.cardMove(state(), id, to));
    },
    addColumn(col, index = columns.length) {
      const rec = normalizeColumn(col, createIdGenerator('col', idSet()));
      ops.assertFreeId(state(), rec.id); return submit(ops.columnAdd(rec, clampIndex(index, columns.length)));
    },
    updateColumn: (id, patch) => submit(ops.columnUpdate(state(), id, patch)),
    removeColumn: (id) => submit(ops.columnRemove(state(), id)),
    moveColumn: (id, index) => submit(ops.columnMove(state(), id, index)),
    addRow(row, index = rows.length) {
      const rec = normalizeRow(row, createIdGenerator('r', idSet()));
      ops.assertFreeId(state(), rec.id); return submit(ops.rowAdd(rec, clampIndex(index, rows.length)));
    },
    updateRow: (id, patch) => submit(ops.rowUpdate(state(), id, patch)),
    removeRow: (id) => submit(ops.rowRemove(state(), id)),
    moveRow: (id, index) => submit(ops.rowMove(state(), id, index)),
    card: (id) => cards.get(String(id)) || null,
    column: (id) => columns.find((c) => c.id === String(id)) || null,
    row: (id) => rows.find((r) => r.id === String(id)) || null,
    cell: (column, row = null) => cellOf(column, row),
    columns: () => columns,
    rows: () => rows,
    cardCount: () => cards.size,
    revision: () => rev,
    serialize() {
      const out = { cards: [], columns: clone(columns), rows: clone(rows) };
      const laneIds = [null, ...rows.map((r) => r.id)];
      for (const col of columns) for (const row of laneIds) for (const id of cellOf(col.id, row)) out.cards.push(clone(cards.get(id)));
      return out;
    },
    applyOp,
  };
  return store;
}
