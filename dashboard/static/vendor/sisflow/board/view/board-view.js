import { buildProjection } from '../query/projection.js';
import { clear, h, icon } from './dom.js';
import { renderColumn } from './column-view.js';
import { attachCardDrag } from '../dnd/pointer.js';
import { attachBoardMenu } from './menu.js';
import { mountEditor } from './editor.js';

function implicit(lanes) {
  return lanes.length === 1 && lanes[0] === null;
}

function toolbar(deps, projection) {
  const t = deps.i18n.t;
  return h('div', { class: 'sfb-toolbar' }, [
    h('div', { class: 'sfb-search-shell' }, [icon('search'), h('input', {
      class: 'sfb-search-input',
      type: 'search',
      placeholder: t('toolbar.search'),
      value: deps.query.getFilter()?.text || '',
      onInput: (ev) => deps.query.setFilter(ev.currentTarget.value ? { text: ev.currentTarget.value } : null),
    })]),
    projection.hiddenByFilter ? h('span', { class: 'sfb-filter-hint' }, t('toolbar.hidden', { count: deps.i18n.number(projection.hiddenByFilter) })) : null,
    h('button', { class: 'sfb-icon-button', type: 'button', title: t('toolbar.undo'), onClick: () => deps.exec('undo') }, icon('undo')),
    h('button', { class: 'sfb-icon-button', type: 'button', title: t('toolbar.redo'), onClick: () => deps.exec('redo') }, icon('redo')),
    h('button', { class: 'sfb-icon-button', type: 'button', title: t('toolbar.export') }, icon('export')),
  ]);
}

function laneHeader(lane, state, actions, i18n) {
  if (!lane) return h('div', { class: 'sfb-lane-header' }, h('div', { class: 'sfb-lane-title' }, i18n.t('lane.none')));
  const collapsed = state.collapsedLanes.has(lane.id);
  const t = i18n.t;
  return h('div', { class: 'sfb-lane-header' }, [
    h('button', {
      class: 'sfb-icon-button',
      type: 'button',
      title: collapsed ? t('lane.expand') : t('lane.collapse'),
      onClick: () => actions.toggleLane(lane.id),
    }, icon(collapsed ? 'chevron-right' : 'chevron-down')),
    h('div', { class: 'sfb-lane-title' }, lane.label || lane.id || t('lane.none')),
  ]);
}

function renderLane(lane, projection, deps, state, actions, showLaneChrome) {
  const collapsed = lane && state.collapsedLanes.has(lane.id);
  return h('section', { class: ['sfb-lane', collapsed && 'is-collapsed'], dataset: { laneId: lane?.id ?? '' } }, [
    showLaneChrome ? laneHeader(lane, state, actions, deps.i18n) : null,
    collapsed ? null : h('div', { class: 'sfb-column-strip' },
      projection.columns.map((col) => renderColumn(col, lane, projection, deps, state, actions))),
  ]);
}

export function mountBoardView(container, deps) {
  const root = h('div', { class: 'sisflow-board', role: 'region', tabindex: '0', aria: { label: deps.i18n.t('board.label') } });
  const body = h('div', { class: 'sfb-body' });
  const announcer = h('div', { class: 'sfb-sr-only', aria: { live: 'polite' } });
  const state = { collapsedColumns: new Set(), collapsedLanes: new Set() };
  deps.virtualState = deps.virtualState || new Map();
  const actions = {
    toggleColumn(id) {
      if (state.collapsedColumns.has(id)) state.collapsedColumns.delete(id);
      else state.collapsedColumns.add(id);
      render();
    },
    toggleLane(id) {
      if (state.collapsedLanes.has(id)) state.collapsedLanes.delete(id);
      else state.collapsedLanes.add(id);
      render();
    },
  };
  let projection = null;
  function render() {
    const keepSearch = typeof document !== 'undefined' && root.querySelector('.sfb-search-input') === document.activeElement;
    projection = buildProjection(deps.store, deps.query, { cardShape: deps.cardShape });
    root.setAttribute('aria-label', deps.i18n.t('board.label'));
    clear(root);
    root.append(toolbar(deps, projection), body, announcer);
    clear(body);
    const showLaneChrome = !implicit(projection.lanes);
    body.append(...projection.lanes.map((lane) => renderLane(lane, projection, deps, state, actions, showLaneChrome)));
    if (keepSearch) {
      const input = root.querySelector('.sfb-search-input');
      input?.focus();
      input?.setSelectionRange(input.value.length, input.value.length);
    }
  }
  function focusCard(delta) {
    const cards = [...root.querySelectorAll('.sfb-card')];
    const i = cards.indexOf(document.activeElement);
    const next = i < 0 ? 0 : i + delta;
    cards[Math.max(0, Math.min(next, cards.length - 1))]?.focus();
  }
  function onKey(ev) {
    if (ev.isComposing || /^(INPUT|TEXTAREA|SELECT)$/.test(ev.target.tagName)) return;
    const card = ev.target.closest?.('.sfb-card');
    if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 'z') { ev.preventDefault(); deps.exec(ev.shiftKey ? 'redo' : 'undo'); }
    else if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 'y') { ev.preventDefault(); deps.exec('redo'); }
    else if (card && (ev.key === 'Enter' || ev.key === 'F2')) deps.exec('editCard', { id: card.dataset.cardId });
    else if (card && (ev.key === 'Delete' || ev.key === 'Backspace')) deps.exec('deleteCard', { id: card.dataset.cardId });
    else if (['ArrowDown', 'ArrowRight'].includes(ev.key)) { ev.preventDefault(); focusCard(1); }
    else if (['ArrowUp', 'ArrowLeft'].includes(ev.key)) { ev.preventDefault(); focusCard(-1); }
    else if (ev.key === '/') { ev.preventDefault(); root.querySelector('.sfb-search-input')?.focus(); }
  }
  container.replaceChildren(root);
  const offChange = deps.emitter.on('board:change', render);
  const offQuery = deps.emitter.on('board:query', render);
  const drag = attachCardDrag(root, { exec: deps.exec, readOnly: () => deps.board.readOnly, announcer, i18n: deps.i18n, emitter: deps.emitter });
  const menu = attachBoardMenu(root, { board: deps.board, actions });
  const editor = mountEditor(root, deps);
  root.addEventListener('keydown', onKey);
  render();
  return {
    root,
    render,
    projection: () => projection,
    destroy() {
      offChange();
      offQuery();
      drag.destroy();
      menu.destroy();
      editor.destroy();
      root.removeEventListener('keydown', onKey);
      container.replaceChildren();
    },
  };
}
