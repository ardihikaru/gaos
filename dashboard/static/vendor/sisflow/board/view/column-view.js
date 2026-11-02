import { cellKey } from '../core/store.js';
import { h, icon } from './dom.js';
import { renderCard } from './card-view.js';
import { createVirtualCell, shouldVirtualize } from './virtual-list.js';

function badge(column) {
  const text = column.cardLimit == null ? String(column.count) : column.count + '/' + column.cardLimit;
  return h('span', { class: ['sfb-count', column.over && 'is-over'] }, text);
}

function header(column, state, actions, i18n) {
  const collapsed = column.collapsed || state.collapsedColumns.has(column.id);
  const menuLabel = i18n.t('column.menu') + ': ' + (column.label || column.id);
  const toggle = h('button', {
    class: 'sfb-icon-button',
    type: 'button',
    title: collapsed ? i18n.t('column.expand') : i18n.t('column.collapse'),
    onClick: () => actions.toggleColumn(column.id),
  }, icon(collapsed ? 'chevron-right' : 'chevron-down'));
  if (collapsed) return h('header', { class: 'sfb-column-header' }, toggle);
  return h('header', { class: 'sfb-column-header' }, [
    toggle,
    h('div', { class: 'sfb-column-title' }, column.label || column.id),
    badge(column),
    h('button', {
      class: 'sfb-icon-button',
      type: 'button',
      title: menuLabel,
      aria: { label: menuLabel, haspopup: 'menu' },
      dataset: { menuTrigger: 'column' },
    }, icon('dots')),
  ]);
}

function addAndEdit(deps, column, laneId) {
  const id = deps.exec?.('addCard', { patch: {}, at: { column: column.id, row: laneId } });
  if (id != null) deps.exec?.('editCard', { id });
}

export function renderColumn(column, lane, projection, deps, state, actions) {
  const collapsed = column.collapsed || state.collapsedColumns.has(column.id);
  const laneId = lane?.id ?? null;
  const ids = projection.cells.get(cellKey(column.id, laneId)) || [];
  if (collapsed) {
    return h('section', { class: ['sfb-column', 'is-collapsed'], dataset: { columnId: column.id } }, [
      header(column, state, actions, deps.i18n),
      h('div', { class: 'sfb-collapsed-label' }, column.label || column.id),
    ]);
  }
  const cell = shouldVirtualize(ids, deps.virtual)
    ? createVirtualCell(column.id, laneId, ids, deps)
    : h('div', { class: 'sfb-cell', dataset: { columnId: column.id, laneId: laneId ?? '' } },
      ids.map((id) => renderCard(deps.store.card(id), deps)));
  return h('section', { class: 'sfb-column', dataset: { columnId: column.id }, role: 'list' }, [
    header(column, state, actions, deps.i18n),
    cell,
    column.addCard ? h('footer', { class: 'sfb-column-footer' }, h('button', {
      class: 'sfb-add-card',
      type: 'button',
      onClick: () => addAndEdit(deps, column, laneId),
    }, [icon('plus'), deps.i18n.t('column.addCard')])) : null,
  ]);
}
