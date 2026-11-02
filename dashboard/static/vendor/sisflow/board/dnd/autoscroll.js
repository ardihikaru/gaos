export const EDGE = 48;
export const VMAX = 24;

export function edgeVelocity(d, sign, edge = EDGE, vmax = VMAX) {
  if (d >= edge) return 0;
  const t = (edge - Math.max(d, 0)) / edge;
  return sign * Math.ceil(vmax * t * t);
}

export function scrollVelocity(rect, px, py, edge = EDGE, vmax = VMAX) {
  return {
    vx: edgeVelocity(px - rect.left, -1, edge, vmax) + edgeVelocity(rect.right - px, 1, edge, vmax),
    vy: edgeVelocity(py - rect.top, -1, edge, vmax) + edgeVelocity(rect.bottom - py, 1, edge, vmax),
  };
}

export function applyAutoScroll({ boardEl, cellEl, boardRect, cellRect, px, py }) {
  const b = scrollVelocity(boardRect, px, py);
  const c = cellRect ? scrollVelocity(cellRect, px, py) : { vy: 0 };
  if (boardEl && b.vx) boardEl.scrollLeft += b.vx;
  if (cellEl && c.vy) cellEl.scrollTop += c.vy;
  return { vx: b.vx, vy: c.vy };
}
