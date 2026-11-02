const asText = (v) => String(v).toLowerCase();
const hasText = (v, needle) => {
  if (Array.isArray(v)) return v.some((x) => x != null && asText(x).includes(needle));
  return v != null && asText(v).includes(needle);
};

export const OPS = Object.freeze({
  eq: (a, b) => a === b,
  neq: (a, b) => a !== b,
  lt: (a, b) => a != null && a < b,
  lte: (a, b) => a != null && a <= b,
  gt: (a, b) => a != null && a > b,
  gte: (a, b) => a != null && a >= b,
  in: (a, b) => Array.isArray(b) && b.includes(a),
  contains: (a, b) => Array.isArray(a) && a.includes(b),
});

export function normalizeFilter(spec = null) {
  if (!spec) return null;
  const by = Array.isArray(spec.by) && spec.by.length > 0 ? spec.by.map(String) : ['label', 'description', 'tags'];
  const where = Array.isArray(spec.where) ? spec.where
    .filter((c) => c && c.field != null)
    .map((c) => ({ field: String(c.field), op: OPS[c.op] ? c.op : 'eq', value: c.value })) : [];
  const text = spec.text == null ? '' : String(spec.text);
  if (text.trim() === '' && where.length === 0) return null;
  return { text, by, where };
}

export function compileFilter(spec = null) {
  const normalized = normalizeFilter(spec);
  if (!normalized) return () => true;
  const needle = normalized.text.trim().toLowerCase();
  const clauses = normalized.where.map(({ field, op, value }) => {
    const fn = OPS[op] || OPS.eq;
    return (card) => fn(card[field], value);
  });
  if (!needle && clauses.length === 0) return () => true;
  return (card) => {
    if (needle && !normalized.by.some((field) => hasText(card[field], needle))) return false;
    return clauses.every((fn) => fn(card));
  };
}
