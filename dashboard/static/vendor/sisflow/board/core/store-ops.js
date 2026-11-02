/* sisflow board - plain-data op constructors. These read store state once and
 * carry the snapshots needed by inverse replay. */

import { clone } from './shapes.js';

const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
const has = (map, id) => map.has(String(id));
const indexOf = (items, id) => items.findIndex((r) => r.id === String(id));

export function applyPatch(rec, patch) {
  for (const [k, v] of Object.entries(patch)) {
    if (v === undefined) delete rec[k];
    else rec[k] = clone(v);
  }
}

export function prevOf(rec, patch) {
  const prev = {};
  for (const k of Object.keys(patch)) prev[k] = Object.hasOwn(rec, k) ? clone(rec[k]) : undefined;
  return prev;
}

export function cardAdd(card, at) {
  return { op: 'card-add', payload: { card: clone(card), at: clone(at) } };
}

export function cardRemove({ cards, cellOf }, id) {
  const card = cards.get(String(id));
  if (!card) throw new Error('sisflow board: unknown card ' + id);
  const at = { column: card.column, row: card.row, index: cellOf(card.column, card.row).indexOf(card.id) };
  return { op: 'card-remove', payload: { id: card.id, card: clone(card), at } };
}

export function cardUpdate({ cards }, id, patch) {
  const card = cards.get(String(id));
  if (!card) throw new Error('sisflow board: unknown card ' + id);
  if ('id' in patch) throw new Error('sisflow board: cannot update id');
  const clean = clone(patch);
  if (Object.keys(clean).length === 0) return null;
  if (Object.keys(clean).every((k) => same(card[k], clean[k]))) return null;
  return { op: 'card-update', payload: { id: card.id, patch: clean, prev: prevOf(card, clean) } };
}

export function cardMove({ cards, cellOf }, id, to) {
  const card = cards.get(String(id));
  if (!card) throw new Error('sisflow board: unknown card ' + id);
  const fromCell = cellOf(card.column, card.row);
  const from = { column: card.column, row: card.row, index: fromCell.indexOf(card.id) };
  const sameCell = from.column === String(to.column) && (from.row ?? null) === (to.row ?? null);
  const max = sameCell ? Math.max(0, fromCell.length - 1) : cellOf(to.column, to.row).length;
  const index = Math.max(0, Math.min(Number.isFinite(Number(to.index)) ? Math.trunc(Number(to.index)) : max, max));
  const dest = { column: String(to.column), row: to.row ?? null, index };
  if (sameCell && from.index === dest.index) return null;
  return { op: 'card-move', payload: { id: card.id, from, to: dest } };
}

export function columnAdd(column, index) {
  return { op: 'column-add', payload: { column: clone(column), index } };
}

export function columnRemove({ columns, rows, cards, cellOf }, id) {
  id = String(id);
  const index = indexOf(columns, id);
  if (index < 0) throw new Error('sisflow board: unknown column ' + id);
  const laneIds = [null, ...rows.map((r) => r.id)];
  const cells = laneIds.map((row) => {
    const ids = cellOf(id, row).slice();
    return { column: id, row, ids, cards: ids.map((cid) => clone(cards.get(cid))) };
  }).filter((c) => c.ids.length > 0);
  return { op: 'column-remove', payload: { column: clone(columns[index]), index, cells } };
}

export function columnUpdate({ columns }, id, patch) {
  id = String(id);
  const rec = columns.find((c) => c.id === id);
  if (!rec) throw new Error('sisflow board: unknown column ' + id);
  if ('id' in patch) throw new Error('sisflow board: cannot update id');
  const clean = clone(patch);
  if (Object.keys(clean).length === 0 || Object.keys(clean).every((k) => same(rec[k], clean[k]))) return null;
  return { op: 'column-update', payload: { id, patch: clean, prev: prevOf(rec, clean) } };
}

export function columnMove({ columns }, id, to) {
  id = String(id);
  const from = indexOf(columns, id);
  if (from < 0) throw new Error('sisflow board: unknown column ' + id);
  const index = Math.max(0, Math.min(Math.trunc(Number(to)), columns.length - 1));
  if (from === index) return null;
  return { op: 'column-move', payload: { id, from, to: index } };
}

export function rowAdd(row, index) {
  return { op: 'row-add', payload: { row: clone(row), index } };
}

export function rowRemove({ columns, rows, cards, cellOf }, id) {
  id = String(id);
  const index = indexOf(rows, id);
  if (index < 0) throw new Error('sisflow board: unknown row ' + id);
  const cells = columns.map((col) => {
    const ids = cellOf(col.id, id).slice();
    return { column: col.id, row: id, ids, cards: ids.map((cid) => clone(cards.get(cid))) };
  }).filter((c) => c.ids.length > 0);
  return { op: 'row-remove', payload: { row: clone(rows[index]), index, cells } };
}

export function rowUpdate({ rows }, id, patch) {
  id = String(id);
  const rec = rows.find((r) => r.id === id);
  if (!rec) throw new Error('sisflow board: unknown row ' + id);
  if ('id' in patch) throw new Error('sisflow board: cannot update id');
  const clean = clone(patch);
  if (Object.keys(clean).length === 0 || Object.keys(clean).every((k) => same(rec[k], clean[k]))) return null;
  return { op: 'row-update', payload: { id, patch: clean, prev: prevOf(rec, clean) } };
}

export function rowMove({ rows }, id, to) {
  id = String(id);
  const from = indexOf(rows, id);
  if (from < 0) throw new Error('sisflow board: unknown row ' + id);
  const index = Math.max(0, Math.min(Math.trunc(Number(to)), rows.length - 1));
  if (from === index) return null;
  return { op: 'row-move', payload: { id, from, to: index } };
}

export function assertFreeId({ cards, columns, rows }, id) {
  id = String(id);
  if (has(cards, id) || columns.some((c) => c.id === id) || rows.some((r) => r.id === id)) {
    throw new Error('sisflow board: duplicate id ' + id);
  }
}
