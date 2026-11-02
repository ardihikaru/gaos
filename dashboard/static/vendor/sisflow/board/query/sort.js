export function normalizeSort(keys = null) {
  if (!Array.isArray(keys) || keys.length === 0) return [];
  return keys.filter((k) => k && k.by != null).map((k) => ({
    by: String(k.by),
    dir: k.dir === -1 || k.dir === 'desc' ? -1 : 1,
  }));
}

function compareValue(a, b) {
  if (typeof a === 'string' && typeof b === 'string') return a.localeCompare(b);
  if (a < b) return -1;
  if (a > b) return 1;
  return 0;
}

export function makeComparator(keys = null) {
  const normalized = normalizeSort(keys);
  return (a, b) => {
    for (const { by, dir } of normalized) {
      const x = a[by], y = b[by];
      if (x == null && y == null) continue;
      if (x == null) return 1;
      if (y == null) return -1;
      const c = compareValue(x, y);
      if (c) return dir * c;
    }
    return (a._ix ?? 0) - (b._ix ?? 0);
  };
}
