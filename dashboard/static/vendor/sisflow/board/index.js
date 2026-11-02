import { createEmitter } from '../core/events.js';
import { createHistory } from '../core/history.js';
import { createBoardStore } from './core/store.js';
import { normalizeCardShape } from './core/shapes.js';
import { createBoardCommands } from './commands.js';
import { buildProjection, createQueryState } from './query/projection.js';
import { applyTheme, bindAutoTheme, exportColors, resolveBoardTheme } from './theme/tokens.js';
import { mountBoardView } from './view/board-view.js';
import { createBoardMenus } from './view/menu.js';
import { createI18n } from './i18n/i18n.js';
import { rewriteRefs } from './provider/rest.js';
import { buildExportLayout } from './export/layout.js';
import { renderPng } from './export/png.js';
import { renderPdf, widthOf } from './export/pdf.js';
import { renderXlsx } from './export/xlsx.js';

const VERSION = '0.3.0';
const unavailable = (phase) => () => { throw new Error('sisflow: not available until phase ' + phase); };
const blob = (data, type) => new Blob([data instanceof Uint8Array ? data : Uint8Array.from(data, (c) => c.charCodeAt(0))], { type });

/** @typedef {import('../../types/board.d.ts').BoardOptions} BoardOptions */
/** @typedef {import('../../types/board.d.ts').Board} Board */

function docFrom(opts) {
  if (opts.cards || opts.columns || opts.rows) return { cards: opts.cards || [], columns: opts.columns || [], rows: opts.rows || [] };
  return opts.load || {};
}

/** @param {HTMLElement} el @param {BoardOptions} [opts] @returns {Board} */
export function createBoard(el, opts = {}) {
  if (!el) throw new Error('sisflow board: container required');
  const emitter = createEmitter();
  const history = createHistory({ emitter, depth: opts.historyDepth ?? 200, readOnly: !!opts.readOnly });
  const store = createBoardStore({ emitter, history });
  const query = createQueryState({ emitter });
  const cardShape = normalizeCardShape(opts.cardShape);
  const i18n = createI18n({ locale: opts.locale || 'en', catalogs: opts.catalogs || {} });
  let commands = null, providerHandle = null;
  let themeHandle = null, view = null;
  let currentTheme = opts.theme || 'sismaya-light';
  const projection = () => buildProjection(store, query, { cardShape });
  const colors = (name) => exportColors(resolveBoardTheme(name || (currentTheme === 'auto' ? themeHandle?.theme : currentTheme)));
  const layoutFor = (theme, measure) => buildExportLayout(projection(), { tokens: colors(theme), measure });
  const canvasMeasure = () => {
    const c = document.createElement('canvas').getContext('2d');
    return (text, size, bold) => {
      c.font = `${bold ? '600 ' : ''}${size}px Inter, system-ui, sans-serif`;
      return c.measureText(text).width;
    };
  };
  history.setApplier(store.applyOp);
  store.load(docFrom(opts));

  const board = {
    version: VERSION,
    readOnly: !!opts.readOnly,
    container: el,
    getCard: (id) => store.card(id),
    addCard: (patch, at) => board.exec('addCard', { patch, at }),
    updateCard: (id, patch) => board.exec('updateCard', { id, patch }),
    deleteCard: (id) => board.exec('deleteCard', { id }),
    moveCard: (id, to) => board.exec('moveCard', { id, to }),
    addColumn: (col, index) => board.exec('addColumn', { col, index }),
    updateColumn: (id, patch) => board.exec('updateColumn', { id, patch }),
    deleteColumn: (id) => board.exec('deleteColumn', { id }),
    moveColumn: (id, index) => board.exec('moveColumn', { id, index }),
    addRow: (row, index) => board.exec('addRow', { row, index }),
    updateRow: (id, patch) => board.exec('updateRow', { id, patch }),
    deleteRow: (id) => board.exec('deleteRow', { id }),
    moveRow: (id, index) => board.exec('moveRow', { id, index }),
    serialize: () => store.serialize(),
    setFilter: (spec) => query.setFilter(spec),
    setSort: (keys) => query.setSort(keys),
    setGroupBy: (field) => query.setGroupBy(field),
    getQuery: () => ({ filter: query.getFilter(), sort: query.getSort(), groupBy: query.getGroupBy() }),
    undo: () => history.undo(),
    redo: () => history.redo(),
    canUndo: () => history.canUndo(),
    canRedo: () => history.canRedo(),
    isDirty: () => history.isDirty(),
    markClean: () => history.markClean(),
    exec(name, payload = {}) {
      return commands.exec(name, payload);
    },
    registerCommand(name, fn) {
      return commands.register(name, fn);
    },
    menus: createBoardMenus(),
    export: {
      async toPNG(exportOpts = {}) {
        return renderPng(layoutFor(exportOpts.theme, canvasMeasure()), exportOpts);
      },
      async toPDF(exportOpts = {}) {
        return blob(renderPdf(layoutFor(exportOpts.theme, widthOf), { warn: console.warn, ...exportOpts }), 'application/pdf');
      },
      async toXLSX(exportOpts = {}) {
        return blob(renderXlsx({ projection: projection(), store, i18n, cardShape, opts: exportOpts }), 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet');
      },
    },
    setTheme(name) {
      themeHandle?.disconnect();
      themeHandle = null;
      currentTheme = name;
      return applyTheme(view.root, name);
    },
    setLocale(code) {
      i18n.setLocale(code);
      view?.render();
      emitter.emit('board:locale', { locale: code });
    },
    t: i18n.t,
    on: (event, fn) => emitter.on(event, fn),
    off: (event, fn) => emitter.off(event, fn),
    destroy() {
      providerHandle?.flush();
      providerHandle?.destroy();
      themeHandle?.disconnect();
      view?.destroy();
      emitter.dispose();
    },
    _store: store,
    _query: query,
  };

  commands = createBoardCommands({ store, history, query, emitter, readOnly: () => board.readOnly, cardShape });

  view = mountBoardView(el, {
    store, query, history, emitter, board, cardShape, i18n,
    cardTemplate: opts.cardTemplate,
    virtual: opts.virtual,
    exec: board.exec.bind(board),
    theme: () => currentTheme,
  });
  themeHandle = opts.theme === 'auto' || opts.theme == null ? bindAutoTheme(view.root) : null;
  if (!themeHandle) applyTheme(view.root, currentTheme);
  if (opts.provider) providerHandle = attachProvider({ provider: opts.provider, store, history, emitter });
  return board;
}

export function attachProvider({ provider, store, history, emitter, doc = globalThis.document }) {
  const off = [];
  const flush = (keepalive = false) => provider.flush?.({ keepalive });
  const rollback = (item) => {
    const ops = item.ops || [];
    history.runInFlight(() => {
      if (item.dir === 'undo') for (const op of ops) store.applyOp(op, 'do');
      else for (let i = ops.length - 1; i >= 0; i -= 1) store.applyOp(ops[i], 'undo');
    });
    if (!item.dir || item.dir === 'do') history.dropEntry?.(item.entry);
  };
  const rebind = ({ from, to }) => {
    history.runInFlight(() => store.applyOp({ op: 'card-rebind', payload: { from, to } }, 'do'));
    history.rewriteEntries?.((entry) => rewriteRefs(entry, from, to));
    provider.rebind?.(from, to);
    emitter.emit('board:card-rebind', { from, to });
  };

  off.push(history.on('change', ({ entry, reason }) => {
    if (!entry || reason === 'clear' || reason === 'drop') return;
    provider.push({ entry, ops: entry.ops, dir: reason === 'undo' ? 'undo' : 'do' });
  }));
  off.push(emitter.on('board:load-more', async (req) => {
    try {
      const page = await provider.loadPage(req);
      store.ingest({ cards: page.cards || [] }, req);
      emitter.emit('board:page-loaded', { ...req, exhausted: !!page.exhausted });
    } catch (err) {
      emitter.emit('board:provider-error', { error: err, status: err.status ?? 0 });
    }
  }));
  off.push(provider.onError?.((err, item) => {
    rollback(item);
    emitter.emit('board:provider-error', { error: err, ops: item.ops, status: err.status ?? 0 });
  }));
  off.push(provider.onRebind?.(rebind));
  if (doc?.addEventListener) {
    const onHide = () => { if (doc.visibilityState === 'hidden') flush(true); };
    doc.addEventListener('visibilitychange', onHide);
    off.push(() => doc.removeEventListener('visibilitychange', onHide));
  }
  provider.load?.()
    .then((data) => { if (data?.columns || data?.rows || data?.cards) store.load(data); })
    .catch((err) => emitter.emit('board:provider-error', { error: err, status: err.status ?? 0 }));
  return { flush, destroy: () => off.splice(0).forEach((fn) => fn?.()) };
}

export { createRestProvider } from './provider/rest.js';
export default { createBoard };
