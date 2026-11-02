export function lowerBound(P, n, v) {
  let lo = 0, hi = Math.max(0, n) + 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (P[mid] < v) lo = mid + 1;
    else hi = mid;
  }
  return Math.min(lo, n);
}

export function upperBound(P, n, v) {
  let lo = 0, hi = Math.max(0, n) + 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (P[mid] <= v) lo = mid + 1;
    else hi = mid;
  }
  return Math.min(lo, n);
}

export function visibleRange(P, n, scrollTop, viewH, overscan = 4) {
  if (n <= 0) return { first: 0, last: -1, topPad: 0, bottomPad: 0 };
  const top = Math.max(0, Math.min(Number(scrollTop) || 0, P[n]));
  const bottom = Math.max(0, Math.min(top + Math.max(0, Number(viewH) || 0), P[n]));
  const firstVisible = Math.max(0, Math.min(n - 1, upperBound(P, n, top) - 1));
  const lastVisible = Math.max(0, Math.min(n - 1, lowerBound(P, n, bottom) - 1));
  const first = Math.max(0, firstVisible - Math.max(0, overscan | 0));
  const last = Math.min(n - 1, lastVisible + Math.max(0, overscan | 0));
  return { first, last, topPad: P[first], bottomPad: P[n] - P[last + 1] };
}

export function buildHeights(ids, previousHeights = new Map(), estimate = 96) {
  const h = new Float64Array(ids.length);
  const P = new Float64Array(ids.length + 1);
  const known = new Uint8Array(ids.length);
  ids.forEach((id, i) => {
    const prev = previousHeights instanceof Map ? previousHeights.get(id) : previousHeights?.[id];
    h[i] = Math.max(1, Number(prev) || estimate);
    known[i] = prev == null ? 0 : 1;
    P[i + 1] = P[i] + h[i];
  });
  return { ids: ids.slice(), n: ids.length, h, P, known };
}

export function patchMeasurements(state, measurements, scrollTop = 0) {
  const byIndex = new Map();
  for (const m of measurements || []) {
    const index = Array.isArray(m) ? m[0] : m.index;
    const height = Array.isArray(m) ? m[1] : m.height;
    if (index >= 0 && index < state.n && Number(height) > 0) byIndex.set(index, Number(height));
  }
  let anchorDelta = 0, runningDelta = 0, changed = false;
  const items = [...byIndex].sort((a, b) => a[0] - b[0]);
  for (const [index, height] of items) {
    const delta = height - state.h[index];
    if (Math.abs(delta) < 0.5) continue;
    const top = state.P[index] + runningDelta;
    state.h[index] = height;
    state.known[index] = 1;
    if (top + height <= scrollTop) anchorDelta += delta;
    runningDelta += delta;
    changed = true;
  }
  if (!changed) return { changed: false, scrollTop, delta: 0 };
  state.P[0] = 0;
  for (let i = 0; i < state.n; i += 1) state.P[i + 1] = state.P[i] + state.h[i];
  return { changed: true, scrollTop: scrollTop + anchorDelta, delta: anchorDelta };
}

export function renderTop(P, index, placeholderSlot = -1, placeholderH = 0) {
  return P[index] + (index >= placeholderSlot && placeholderSlot >= 0 ? placeholderH : 0);
}

export function loadMoreState({ scrollTop = 0, viewH = 0, totalH = 0, thresholdPx = 600, loading = false, exhausted = false } = {}) {
  const shouldLoad = !loading && !exhausted && scrollTop + viewH >= totalH - thresholdPx;
  return { shouldLoad, loading: loading || shouldLoad, exhausted };
}
