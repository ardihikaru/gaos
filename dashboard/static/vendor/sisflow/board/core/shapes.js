/* sisflow board - record normalization. Input records are intentionally loose:
 * known fields are defaulted/clamped, unknown fields pass through for hosts. */

export const clone = (v) => (v === undefined ? v : structuredClone(v));

const strId = (v) => (v === undefined || v === null || v === '' ? null : String(v));
const bool = (v, fallback) => (v === undefined ? fallback : !!v);
const count = (v) => (Number.isFinite(Number(v)) ? Math.max(0, Math.trunc(Number(v))) : 0);

export function createIdGenerator(prefix, used = new Set()) {
  let n = 0;
  return () => {
    let id;
    do {
      n += 1;
      id = prefix + n;
    } while (used.has(id));
    used.add(id);
    return id;
  };
}

function normalizeOptions(value) {
  if (value === undefined || value === null) return [];
  if (!Array.isArray(value)) throw new Error('sisflow board: cardShape options must be arrays');
  return value.map((opt) => (opt && typeof opt === 'object' ? clone(opt) : { id: opt, label: String(opt) }));
}

export function normalizeCardShape(cardShape = null) {
  const base = {
    priority: { options: [1, 2, 3] },
    tags: { options: [] },
    users: { options: [] },
  };
  if (cardShape === true || cardShape === null || cardShape === undefined) return base;
  if (typeof cardShape !== 'object') throw new Error('sisflow board: cardShape must be an object');
  const out = { ...clone(cardShape) };
  out.priority = { ...(out.priority || {}), options: normalizeOptions(out.priority?.options ?? base.priority.options) };
  out.tags = { ...(out.tags || {}), options: normalizeOptions(out.tags?.options) };
  out.users = { ...(out.users || {}), options: normalizeOptions(out.users?.options) };
  return out;
}

export function normalizeColumn(input = {}, nextId = createIdGenerator('col')) {
  const id = strId(input.id) || nextId();
  return {
    ...clone(input),
    id,
    label: input.label === undefined ? '' : String(input.label),
    cardLimit: input.cardLimit == null ? null : Math.max(0, Math.trunc(Number(input.cardLimit)) || 0),
    addCard: bool(input.addCard, true),
    collapsed: bool(input.collapsed, false),
    css: input.css ?? null,
  };
}

export function normalizeRow(input = {}, nextId = createIdGenerator('r')) {
  const id = strId(input.id) || nextId();
  return {
    ...clone(input),
    id,
    label: input.label === undefined ? '' : String(input.label),
    collapsed: bool(input.collapsed, false),
  };
}

export function normalizeCard(input = {}, { firstColumn = null, nextId = createIdGenerator('c') } = {}) {
  const progress = input.progress == null ? null : Math.max(0, Math.min(1, Number(input.progress) || 0));
  return {
    ...clone(input),
    id: strId(input.id) || nextId(),
    label: input.label === undefined ? '' : String(input.label),
    description: input.description === undefined ? '' : String(input.description),
    column: strId(input.column) || firstColumn,
    row: input.row == null ? null : strId(input.row),
    priority: input.priority == null ? null : input.priority,
    progress,
    deadline: input.deadline ?? null,
    tags: Array.isArray(input.tags) ? clone(input.tags) : [],
    users: Array.isArray(input.users) ? clone(input.users) : [],
    attachments: count(input.attachments),
    comments: count(input.comments),
    tasks: count(input.tasks),
    cover: input.cover ?? null,
    css: input.css ?? null,
  };
}

export function normalizeBoard(doc = {}) {
  const used = new Set();
  const nextCol = createIdGenerator('col', used);
  const nextRow = createIdGenerator('r', used);
  const nextCard = createIdGenerator('c', used);
  const columns = (doc.columns || []).map((c) => {
    if (c?.id != null) used.add(String(c.id));
    return normalizeColumn(c, nextCol);
  });
  if (columns.length === 0) columns.push(normalizeColumn({ id: 'col1', label: '' }, nextCol));
  const rows = (doc.rows || []).map((r) => {
    if (r?.id != null) used.add(String(r.id));
    return normalizeRow(r, nextRow);
  });
  const firstColumn = columns[0].id;
  const cards = (doc.cards || []).map((c) => {
    if (c?.id != null) used.add(String(c.id));
    return normalizeCard(c, { firstColumn, nextId: nextCard });
  });
  return { ...clone(doc), cards, columns, rows };
}
