import { h, icon } from './dom.js';

const formish = (el) => /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName) || el.isContentEditable;
const mut = (board, name, payload) => board.exec(name, payload);

function defaults(ctx) {
  const { board, kind } = ctx;
  const t = board.t;
  if (kind === 'card') return [
    { label: t('menu.edit'), icon: 'edit', group: 'primary', readOnly: true, run: () => mut(board, 'editCard', { id: ctx.id }) },
    { label: t('menu.duplicate'), icon: 'copy', group: 'primary', run: () => mut(board, 'duplicateCard', { id: ctx.id }) },
    { label: t('menu.moveTop'), icon: 'arrowup', group: 'move', when: () => ctx.index > 0, run: () => mut(board, 'moveCard', { id: ctx.id, to: { column: ctx.card.column, row: ctx.card.row, index: 0 } }) },
    { label: t('menu.moveBottom'), icon: 'arrowdown', group: 'move', when: () => ctx.count > 1, run: () => mut(board, 'moveCard', { id: ctx.id, to: { column: ctx.card.column, row: ctx.card.row, index: ctx.count - 1 } }) },
    ...board._store.columns().filter((c) => c.id !== ctx.card.column).map((c) => ({
      label: t('menu.moveToColumn', { column: c.label || c.id }),
      icon: 'columns',
      group: 'move',
      run: () => mut(board, 'moveCard', { id: ctx.id, to: { column: c.id, row: ctx.card.row, index: board._store.cell(c.id, ctx.card.row).length } }),
    })),
    { label: t('menu.delete'), icon: 'trash', group: 'danger', danger: true, run: () => mut(board, 'deleteCard', { id: ctx.id }) },
  ];
  if (kind === 'column') return [
    { label: t('menu.addCard'), icon: 'plus', group: 'primary', when: () => ctx.column.addCard, run: () => {
      const id = mut(board, 'addCard', { patch: {}, at: { column: ctx.id, row: null } });
      if (id != null) mut(board, 'editCard', { id });
    } },
    { label: ctx.column.collapsed ? t('menu.expand') : t('menu.collapse'), icon: ctx.column.collapsed ? 'chevron-right' : 'chevron-down', group: 'primary', run: () => ctx.actions.toggleColumn(ctx.id) },
    { label: t('menu.moveLeft'), icon: 'arrowleft', group: 'move', when: () => ctx.index > 0, run: () => mut(board, 'moveColumn', { id: ctx.id, index: ctx.index - 1 }) },
    { label: t('menu.moveRight'), icon: 'arrowright', group: 'move', when: () => ctx.index < board._store.columns().length - 1, run: () => mut(board, 'moveColumn', { id: ctx.id, index: ctx.index + 1 }) },
    { label: t('menu.deleteColumn'), icon: 'trash', group: 'danger', danger: true, run: () => mut(board, 'deleteColumn', { id: ctx.id }) },
  ];
  if (kind === 'lane') return [
    { label: t('menu.collapse'), icon: 'chevron-down', group: 'primary', run: () => ctx.actions.toggleLane(ctx.id) },
    { label: t('menu.deleteLane'), icon: 'trash', group: 'danger', danger: true, run: () => mut(board, 'deleteRow', { id: ctx.id }) },
  ];
  return [{ label: t('menu.addColumn'), icon: 'plus', group: 'primary', run: () => mut(board, 'addColumn', { col: { label: t('column.new') } }) }];
}

function contextFrom(root, board, actions, ev) {
  const cardEl = ev.target.closest('.sfb-card');
  if (cardEl) {
    const card = board.getCard(cardEl.dataset.cardId);
    const ids = board._store.cell(card.column, card.row);
    return { kind: 'card', id: card.id, card, index: ids.indexOf(card.id), count: ids.length, board };
  }
  const colEl = ev.target.closest('.sfb-column');
  if (colEl) {
    const id = colEl.dataset.columnId;
    const cols = board._store.columns();
    return { kind: 'column', id, column: board._store.column(id), index: cols.findIndex((c) => c.id === id), board, actions };
  }
  const laneEl = ev.target.closest('.sfb-lane-header');
  if (laneEl) {
    const id = laneEl.closest('.sfb-lane')?.dataset.laneId;
    return { kind: 'lane', id, lane: board._store.row(id), board, actions };
  }
  return root.contains(ev.target) ? { kind: 'background', board } : null;
}

function titleFor(ctx) {
  if (ctx.kind === 'card') return ctx.card.label || ctx.id;
  if (ctx.kind === 'column') return ctx.column.label || ctx.id;
  if (ctx.kind === 'lane') return ctx.lane?.label || ctx.id;
  return ctx.board.t('board.label');
}

function grouped(items) {
  const order = ['primary', 'move', 'custom', 'danger'];
  const map = new Map(order.map((key) => [key, []]));
  for (const item of items) map.get(item.group || 'custom')?.push(item);
  return order.map((key) => map.get(key)).filter((group) => group.length);
}

function menuButton(it, ctx, close) {
  return h('button', {
    class: ['sfb-menu-item', it.danger && 'is-danger'],
    type: 'button',
    role: 'menuitem',
    onClick: () => { it.run(ctx); close(); },
  }, [it.icon ? icon(it.icon) : h('span', { class: 'sfb-menu-spacer' }), h('span', { class: 'sfb-menu-label' }, it.label)]);
}

function renderMenu(root, ctx, x, y, close) {
  const tables = ctx.board.menus?._tables || {};
  const items = defaults(ctx).concat(tables[ctx.kind] || []).filter((it) => !it.when || it.when(ctx));
  if (ctx.board.readOnly) items.splice(0, items.length, ...items.filter((it) => it.readOnly));
  const menu = h('div', { class: 'sfb-menu', role: 'menu', tabindex: '-1' }, [
    h('div', { class: 'sfb-menu-head' }, [
      h('span', { class: 'sfb-menu-kicker' }, ctx.kind),
      h('strong', {}, titleFor(ctx)),
    ]),
    grouped(items).map((group, index) => h('div', { class: 'sfb-menu-group', role: 'none' }, [
      index ? h('div', { class: 'sfb-menu-separator', role: 'separator' }) : null,
      group.map((it) => menuButton(it, ctx, close)),
    ])),
  ]);
  root.append(menu);
  const r = menu.getBoundingClientRect();
  menu.style.left = Math.max(4, Math.min(x, innerWidth - r.width - 4)) + 'px';
  menu.style.top = Math.max(4, Math.min(y, innerHeight - r.height - 4)) + 'px';
  menu.querySelector('button')?.focus();
  return menu;
}

export function attachBoardMenu(root, { board, actions }) {
  let menu = null;
  const close = () => { menu?.remove(); menu = null; };
  const show = (ev, x, y) => {
    if (formish(ev.target)) return;
    const ctx = contextFrom(root, board, actions, ev);
    if (!ctx) return;
    ev.preventDefault();
    ev.stopPropagation();
    close();
    menu = renderMenu(root, ctx, x, y, close);
  };
  const open = (ev) => show(ev, ev.clientX, ev.clientY);
  const click = (ev) => {
    const btn = ev.target.closest('button[data-menu-trigger]');
    if (!btn || !root.contains(btn)) return;
    const r = btn.getBoundingClientRect();
    show(ev, r.left, r.bottom + 4);
  };
  const key = (ev) => {
    if (!menu) return;
    const items = [...menu.querySelectorAll('button')], i = items.indexOf(document.activeElement);
    if (ev.key === 'Escape') { ev.preventDefault(); close(); }
    else if (ev.key === 'ArrowDown') { ev.preventDefault(); items[(i + 1) % items.length]?.focus(); }
    else if (ev.key === 'ArrowUp') { ev.preventDefault(); items[(i - 1 + items.length) % items.length]?.focus(); }
    else if (ev.key === 'Home') { ev.preventDefault(); items[0]?.focus(); }
    else if (ev.key === 'End') { ev.preventDefault(); items[items.length - 1]?.focus(); }
  };
  root.addEventListener('contextmenu', open);
  root.addEventListener('click', click);
  document.addEventListener('click', close);
  document.addEventListener('keydown', key);
  return { close, destroy: () => { close(); root.removeEventListener('contextmenu', open); root.removeEventListener('click', click); document.removeEventListener('click', close); document.removeEventListener('keydown', key); } };
}

export function createBoardMenus() {
  const tables = { card: [], column: [], lane: [], background: [] };
  return {
    _tables: tables,
    card: { add: (item) => tables.card.push(item) },
    column: { add: (item) => tables.column.push(item) },
    lane: { add: (item) => tables.lane.push(item) },
    background: { add: (item) => tables.background.push(item) },
  };
}
