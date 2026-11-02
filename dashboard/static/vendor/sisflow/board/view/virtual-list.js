import { cellKey } from '../core/store.js';
import { renderCard } from './card-view.js';
import { h } from './dom.js';
import { buildHeights, loadMoreState, patchMeasurements, renderTop, visibleRange } from './virtual-math.js';

const defaults = { threshold: 30, force: false, itemEstimate: 96, overscan: 4, loadThresholdPx: 600 };
const laneOf = (lane) => lane ?? null;
const keyOf = (column, lane) => cellKey(column, laneOf(lane));
const raf = (fn) => typeof requestAnimationFrame === 'function' ? requestAnimationFrame(fn) : setTimeout(fn, 0);

export { lowerBound, upperBound, visibleRange, buildHeights, patchMeasurements, renderTop, loadMoreState } from './virtual-math.js';

export function virtualOptions(opts = {}) {
  return { ...defaults, ...(opts || {}) };
}

export function shouldVirtualize(ids, opts = {}) {
  const cfg = virtualOptions(opts);
  return !!cfg.force || ids.length > cfg.threshold;
}

function stateFor(cache, column, lane, ids, cfg) {
  const key = keyOf(column, lane);
  let state = cache.get(key);
  if (!state) state = { heights: new Map(), loading: false, exhausted: false, scrollTop: 0 };
  state.key = key;
  state.column = column;
  state.row = laneOf(lane);
  if (state.loading && ids.length !== state.requestedOffset) state.loading = false;
  state.model = buildHeights(ids, state.heights, cfg.itemEstimate);
  cache.set(key, state);
  return state;
}

function maybeLoad(el, state, cfg, deps) {
  const model = activeModel(state, cfg);
  const next = loadMoreState({
    scrollTop: el.scrollTop,
    viewH: el.clientHeight,
    totalH: model.P[model.n],
    thresholdPx: cfg.loadThresholdPx,
    loading: state.loading,
    exhausted: state.exhausted,
  });
  if (!next.shouldLoad) return;
  state.loading = true;
  state.requestedOffset = model.n;
  deps.emitter?.emit('board:load-more', { column: state.column, row: state.row, offset: model.n });
}

function patchFromNodes(el, state, cfg, deps) {
  const model = activeModel(state, cfg);
  const measures = [...el.querySelectorAll('.sfb-card')].map((node) => ({
    index: Number(node.dataset.virtualIndex),
    height: node.getBoundingClientRect?.().height || node.offsetHeight || cfg.itemEstimate,
  }));
  const patched = patchMeasurements(model, measures, el.scrollTop);
  for (let i = 0; i < model.n; i += 1) if (model.known[i]) state.heights.set(model.ids[i], model.h[i]);
  if (patched.changed) {
    el.scrollTop = patched.scrollTop;
    deps.emitter?.emit('board:virtual-measure', { column: state.column, row: state.row });
    renderVirtualCell(el, state, deps, cfg);
  }
}

function observe(el, state, cfg, deps) {
  if (typeof ResizeObserver === 'undefined') {
    raf(() => patchFromNodes(el, state, cfg, deps));
    return;
  }
  const ro = new ResizeObserver(() => {
    if (state.measureRaf) return;
    state.measureRaf = raf(() => {
      state.measureRaf = 0;
      patchFromNodes(el, state, cfg, deps);
    });
  });
  el.querySelectorAll('.sfb-card').forEach((node) => ro.observe(node));
  state.ro?.disconnect();
  state.ro = ro;
}

function placeholderMeta(state) {
  const p = state.placeholder;
  return p && p.column === state.column && (p.row ?? null) === state.row ? p : null;
}

function activeModel(state, cfg) {
  if (!state.dragId) return state.model;
  return buildHeights(state.model.ids.filter((id) => id !== state.dragId), state.heights, cfg.itemEstimate);
}

export function renderVirtualCell(el, state, deps, cfg) {
  const model = activeModel(state, cfg);
  const ph = placeholderMeta(state);
  const range = visibleRange(model.P, model.n, el.scrollTop, el.clientHeight || 1, cfg.overscan);
  const frag = document.createDocumentFragment();
  const totalH = model.P[model.n] + (ph ? ph.height : 0);
  const inner = h('div', { class: 'sfb-virtual-inner', style: { height: totalH + 'px' } });
  const addPlaceholder = (slot) => inner.append(h('div', {
    class: 'sfb-placeholder',
    style: { position: 'absolute', top: renderTop(model.P, slot, ph.slot, ph.height) + 'px', height: ph.height + 'px', left: '0', right: '0' },
  }));
  if (ph && ph.slot <= range.first) addPlaceholder(ph.slot);
  for (let i = range.first; i <= range.last; i += 1) {
    if (ph && ph.slot === i) addPlaceholder(i);
    const id = model.ids[i];
    const card = renderCard(deps.store.card(id), deps);
    card.dataset.virtualIndex = String(i);
    card.style.position = 'absolute';
    card.style.left = '0';
    card.style.right = '0';
    card.style.top = renderTop(model.P, i, ph?.slot ?? -1, ph?.height ?? 0) + 'px';
    inner.append(card);
  }
  if (ph && ph.slot > range.last) addPlaceholder(ph.slot);
  frag.append(inner);
  el.replaceChildren(frag);
  el.dataset.virtual = 'true';
  el._sfbVirtual = { state, P: model.P, n: model.n, setPlaceholder: (p) => {
    state.placeholder = p;
    renderVirtualCell(el, state, deps, cfg);
  }, setDragId: (id) => {
    state.dragId = id;
    renderVirtualCell(el, state, deps, cfg);
  } };
  observe(el, state, cfg, deps);
  maybeLoad(el, state, cfg, deps);
}

export function createVirtualCell(column, lane, ids, deps) {
  const cfg = virtualOptions(deps.virtual);
  const state = stateFor(deps.virtualState, column, lane, ids, cfg);
  const el = h('div', {
    class: ['sfb-cell', 'sfb-virtual-cell'],
    dataset: { columnId: column, laneId: lane ?? '', virtual: 'true' },
    onScroll: (ev) => {
      state.scrollTop = ev.currentTarget.scrollTop;
      renderVirtualCell(ev.currentTarget, state, deps, cfg);
    },
  });
  raf(() => {
    el.scrollTop = state.scrollTop;
    renderVirtualCell(el, state, deps, cfg);
  });
  return el;
}
