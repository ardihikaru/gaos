import { applyAutoScroll } from './autoscroll.js';
import { cellDropIndex, hitCell, sameSlot, unchangedDrop } from './drop-math.js';
import { createDragVisual, placePlaceholder } from './ghost.js';

const THRESHOLD = 4;
const laneOf = (el) => el?.dataset.laneId === '' ? null : el?.dataset.laneId ?? null;
const formish = (el) => el.closest('button,a,input,textarea,select,[contenteditable="true"]');

function prefixFor(cellEl) {
  if (cellEl._sfbVirtual) return { P: cellEl._sfbVirtual.P, n: cellEl._sfbVirtual.n };
  const cards = [...cellEl.querySelectorAll('.sfb-card:not(.sfb-drag-source)')];
  const P = [0];
  for (const card of cards) P.push(P[P.length - 1] + card.getBoundingClientRect().height);
  return { P, n: cards.length };
}

function buildCache(root) {
  const cells = [...root.querySelectorAll('.sfb-cell')].map((el) => {
    const p = prefixFor(el);
    return {
      el,
      column: el.dataset.columnId,
      row: laneOf(el),
      rect: el.getBoundingClientRect(),
      scrollTop: el.scrollTop,
      ...p,
    };
  });
  const columns = [...root.querySelectorAll('.sfb-column')].map((el) => ({
    id: el.dataset.columnId,
    rect: el.getBoundingClientRect(),
    collapsed: el.classList.contains('is-collapsed'),
    n: 0,
  }));
  return { cells, columns, boardRect: root.querySelector('.sfb-body')?.getBoundingClientRect() || root.getBoundingClientRect() };
}

function sourceOf(cardEl) {
  const cell = cardEl.closest('.sfb-cell');
  if (cell._sfbVirtual) {
    const index = Number(cardEl.dataset.virtualIndex);
    return { id: cardEl.dataset.cardId, column: cell.dataset.columnId, row: laneOf(cell), index, liftedIndex: index };
  }
  const cards = [...cell.querySelectorAll('.sfb-card')];
  return {
    id: cardEl.dataset.cardId,
    column: cell.dataset.columnId,
    row: laneOf(cell),
    index: cards.indexOf(cardEl),
    liftedIndex: cards.indexOf(cardEl),
  };
}

export function attachCardDrag(root, { exec, readOnly, announcer, i18n, emitter }) {
  let session = null;
  const end = (commit) => {
    if (!session) return;
    const s = session;
    session = null;
    root.classList.remove('sfb-dragging');
    s.visual?.destroy();
    s.sourceCell?._sfbVirtual?.setDragId(null);
    s.virtualCell?._sfbVirtual?.setPlaceholder(null);
    if (commit && s.target && !unchangedDrop(s.source, s.target)) exec('moveCard', { id: s.source.id, to: s.target });
    document.removeEventListener('pointermove', onMove);
    document.removeEventListener('pointerup', onUp);
    document.removeEventListener('pointercancel', onCancel);
    document.removeEventListener('keydown', onKey);
  };
  const updateTarget = (ev) => {
    let cell = hitCell(session.cache, ev.clientX, ev.clientY) || session.lastCell;
    if (!cell) return;
    session.lastCell = cell;
    const index = cell.append ? cell.n : cellDropIndex(cell, ev.clientY);
    const target = { column: cell.column, row: cell.row ?? null, index };
    if (!sameSlot(session.target, target)) {
      session.target = target;
      if (session.virtualCell && session.virtualCell !== cell.el) session.virtualCell._sfbVirtual?.setPlaceholder(null);
      if (cell.el?._sfbVirtual) {
        session.virtualCell = cell.el;
        cell.el._sfbVirtual.setPlaceholder({
          column: target.column,
          row: target.row,
          slot: index,
          height: session.visual.rect.height,
        });
      } else {
        session.virtualCell = null;
        placePlaceholder(cell.el, session.visual.placeholder, index);
      }
      if (announcer) announcer.textContent = i18n?.t
        ? i18n.t('dnd.position', { column: target.column, index: index + 1 })
        : `Moved to ${target.column}, position ${index + 1}`;
    }
  };
  const tick = () => {
    session.raf = 0;
    const ev = session.event;
    session.visual.move(ev.clientX, ev.clientY);
    updateTarget(ev);
    const cell = session.lastCell;
    const body = root.querySelector('.sfb-body');
    const scrolled = applyAutoScroll({ boardEl: body, cellEl: cell?.el, boardRect: session.cache.boardRect, cellRect: cell?.rect, px: ev.clientX, py: ev.clientY });
    if (scrolled.vx || scrolled.vy) session.cache = buildCache(root);
  };
  const startDrag = (ev) => {
    session.visual = createDragVisual(session.cardEl, { x: ev.clientX, y: ev.clientY, grabDX: session.grabDX, grabDY: session.grabDY });
    session.sourceCell = session.cardEl.closest('.sfb-cell');
    session.sourceCell?._sfbVirtual?.setDragId(session.source.id);
    session.cache = buildCache(root);
    root.classList.add('sfb-dragging');
    updateTarget(ev);
  };
  function onMove(ev) {
    if (!session) return;
    session.event = ev;
    const dx = ev.clientX - session.startX, dy = ev.clientY - session.startY;
    if (!session.visual && Math.hypot(dx, dy) >= THRESHOLD) startDrag(ev);
    if (session.visual && !session.raf) session.raf = requestAnimationFrame(tick);
  }
  function onUp() { end(!!session?.visual); }
  function onCancel() { end(false); }
  function onKey(ev) { if (ev.key === 'Escape') end(false); }
  function onDown(ev) {
    const cardEl = ev.target.closest('.sfb-card');
    if (!cardEl || formish(ev.target) || readOnly()) return;
    const rect = cardEl.getBoundingClientRect();
    session = { cardEl, source: sourceOf(cardEl), startX: ev.clientX, startY: ev.clientY, grabDX: ev.clientX - rect.left, grabDY: ev.clientY - rect.top };
    cardEl.setPointerCapture?.(ev.pointerId);
    document.addEventListener('pointermove', onMove);
    document.addEventListener('pointerup', onUp, { once: true });
    document.addEventListener('pointercancel', onCancel, { once: true });
    document.addEventListener('keydown', onKey);
  }
  root.addEventListener('pointerdown', onDown);
  const offMeasure = emitter?.on?.('board:virtual-measure', () => { if (session?.visual) session.cache = buildCache(root); });
  return { cancel: () => end(false), destroy: () => { end(false); offMeasure?.(); root.removeEventListener('pointerdown', onDown); } };
}
