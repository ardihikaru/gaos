const contains = (r, x, y) => r && x >= r.left && x < r.right && y >= r.top && y < r.bottom;
const rowAt = (cache, py) => {
  let best = null, dist = Infinity;
  for (const c of cache.cells || []) {
    const d = py < c.rect.top ? c.rect.top - py : py >= c.rect.bottom ? py - c.rect.bottom : 0;
    if (d < dist) {
      dist = d;
      best = c.row ?? null;
    }
  }
  return best;
};

export function dropIndex(P, n, y) {
  let lo = 0, hi = Math.max(0, Math.min(n, P.length - 1));
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    const m = P[mid] + (P[mid + 1] - P[mid]) / 2;
    if (m <= y) lo = mid + 1;
    else hi = mid;
  }
  return lo;
}

export function hitCell(cache, px, py) {
  for (const c of cache.cells || []) if (contains(c.rect, px, py)) return c;
  const col = (cache.columns || []).find((c) => c.collapsed && contains(c.rect, px, py));
  return col ? { column: col.id, row: rowAt(cache, py), index: col.n ?? 0, n: col.n ?? 0, append: true } : null;
}

export function cellDropIndex(cell, py) {
  if (cell.append) return cell.n ?? 0;
  return dropIndex(cell.P || [0], cell.n || 0, py - cell.rect.top + (cell.scrollTop || 0));
}

export function sameSlot(a, b) {
  return a && b && String(a.column) === String(b.column) && (a.row ?? null) === (b.row ?? null) && a.index === b.index;
}

export function unchangedDrop(source, target) {
  return sameSlot({
    column: source.column,
    row: source.row ?? null,
    index: source.liftedIndex ?? source.index,
  }, target);
}
