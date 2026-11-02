import { clone } from './core/shapes.js';
import { castBack, laneKeyOf } from './query/group.js';

const asIndex = (v, fallback) => Number.isFinite(Number(v)) ? Math.trunc(Number(v)) : fallback;
const optionShape = (shape, field) => shape?.[field] || null;
const atOf = (p) => p.at || { column: p.column, row: p.row, index: p.index };
const patchWithGroup = (patch, at, query, shape) => {
  const field = query.getGroupBy?.();
  if (!field || at.row == null) return patch;
  return { ...patch, [field]: castBack(at.row, optionShape(shape, field)) };
};

function gated(readOnly, fn) {
  return (payload) => (readOnly() ? false : fn(payload || {}));
}

export function createBoardCommands({ store, history, query, emitter, readOnly = false, cardShape = {} }) {
  const isReadOnly = typeof readOnly === 'function' ? readOnly : () => !!readOnly;
  const commands = {};
  const batch = (name, fn) => history.batch(name, fn);
  const add = (name, fn, mutating = true) => {
    if (commands[name]) throw new Error('sisflow board: command exists ' + name);
    commands[name] = mutating ? gated(isReadOnly, fn) : fn;
  };

  add('addCard', (payload) => batch('add-card', () => {
    const at = atOf(payload);
    const group = query.getGroupBy?.();
    const realAt = group ? { ...at, row: null } : at;
    return store.addCard(patchWithGroup(payload.patch || {}, at, query, cardShape), realAt);
  }));
  add('updateCard', ({ id, patch = {} }) => batch('edit-card', () => store.updateCard(id, patch)));
  add('deleteCard', ({ id }) => batch('delete-card', () => store.removeCard(id)));
  add('moveCard', ({ id, to }) => batch('move-card', () => {
    const card = store.card(id);
    const field = query.getGroupBy?.();
    if (!field) return store.moveCard(id, to);
    if (laneKeyOf(card, field) !== (to.row ?? null)) {
      store.updateCard(id, { [field]: castBack(to.row ?? '', optionShape(cardShape, field)) });
    }
    return store.moveCard(id, { column: to.column, row: card.row, index: to.index });
  }));
  add('duplicateCard', ({ id }) => batch('duplicate-card', () => {
    const card = store.card(id);
    const ids = store.cell(card.column, card.row);
    const copy = clone(card);
    delete copy.id;
    return store.addCard(copy, { column: card.column, row: card.row, index: ids.indexOf(card.id) + 1 });
  }));
  add('addColumn', ({ col = {}, index }) => batch('add-column', () => store.addColumn(col, index)));
  add('updateColumn', ({ id, patch = {} }) => batch('edit-column', () => store.updateColumn(id, patch)));
  add('renameColumn', ({ id, label }) => batch('rename-column', () => store.updateColumn(id, { label })));
  add('deleteColumn', ({ id }) => batch('delete-column', () => store.removeColumn(id)));
  add('moveColumn', ({ id, index }) => batch('move-column', () => store.moveColumn(id, index)));
  add('setWipLimit', ({ id, cardLimit }) => batch('set-wip-limit', () => store.updateColumn(id, { cardLimit })));
  add('addRow', ({ row = {}, index }) => batch('add-row', () => store.addRow(row, index)));
  add('updateRow', ({ id, patch = {} }) => batch('edit-row', () => store.updateRow(id, patch)));
  add('renameRow', ({ id, label }) => batch('rename-row', () => store.updateRow(id, { label })));
  add('deleteRow', ({ id }) => batch('delete-row', () => store.removeRow(id)));
  add('moveRow', ({ id, index }) => batch('move-row', () => store.moveRow(id, index)));
  add('editCard', ({ id }) => emitter.emit('board:editor-open', { id }), false);
  add('undo', () => history.undo(), false);
  add('redo', () => history.redo(), false);
  add('exportPng', () => { throw new Error('sisflow: not available until phase 8'); }, false);
  add('exportPdf', () => { throw new Error('sisflow: not available until phase 8'); }, false);
  add('exportXlsx', () => { throw new Error('sisflow: not available until phase 8'); }, false);

  commands.register = (name, fn) => {
    if (commands[name]) throw new Error('sisflow board: command exists ' + name);
    commands[name] = gated(isReadOnly, fn);
    return () => { delete commands[name]; };
  };
  commands.exec = (name, payload = {}) => {
    if (!commands[name]) throw new Error('sisflow board: unknown command ' + name);
    return commands[name](payload);
  };
  return commands;
}
