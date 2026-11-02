import { clone } from '../core/shapes.js';
import { cellKey } from '../core/store.js';
import { compileFilter, normalizeFilter } from './filter.js';
import { deriveLanes, laneKeyOf } from './group.js';
import { makeComparator, normalizeSort } from './sort.js';

const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
const memo = new WeakMap();

export function createQueryState({ emitter } = {}) {
  let filter = null, predicate = compileFilter(null), sort = [], groupBy = null, rev = 0;
  const emit = () => emitter?.emit('board:query', {
    revision: rev,
    filter: clone(filter),
    sort: clone(sort),
    groupBy,
  });
  const bump = () => { rev += 1; emit(); };
  return {
    setFilter(spec = null) {
      const next = normalizeFilter(spec);
      if (same(next, filter)) return false;
      filter = next;
      predicate = compileFilter(filter);
      bump();
      return true;
    },
    setSort(keys = null) {
      const next = normalizeSort(keys);
      if (same(next, sort)) return false;
      sort = next;
      bump();
      return true;
    },
    setGroupBy(field = null) {
      const next = field == null || field === '' ? null : String(field);
      if (next === groupBy) return false;
      groupBy = next;
      bump();
      return true;
    },
    getFilter: () => clone(filter),
    getSort: () => clone(sort),
    getGroupBy: () => groupBy,
    revision: () => rev,
    _predicate: () => predicate,
  };
}

function cached(store, query) {
  const hit = memo.get(store);
  if (!hit || hit.query !== query) return null;
  if (hit.storeRev === store.revision() && hit.queryRev === query.revision()) return hit.value;
  return null;
}

function remember(store, query, value) {
  memo.set(store, { query, storeRev: store.revision(), queryRev: query.revision(), value });
  return value;
}

function sourceCards(store) {
  const out = [];
  for (const col of store.columns()) {
    for (const row of [null, ...store.rows().map((r) => r.id)]) {
      for (const id of store.cell(col.id, row)) out.push(store.card(id));
    }
  }
  return out;
}

export function buildProjection(store, query, opts = {}) {
  const hit = cached(store, query);
  if (hit) return hit;

  const groupBy = query.getGroupBy();
  const sort = query.getSort();
  const hasSort = sort.length > 0;
  const pred = query._predicate ? query._predicate() : compileFilter(query.getFilter());
  const allCards = sourceCards(store);
  const visible = allCards.filter(pred);
  const lanes = groupBy
    ? deriveLanes(groupBy, visible, opts.cardShape?.[groupBy])
    : [null, ...store.rows().map((r) => clone(r))];
  const laneIds = lanes.map((lane) => lane?.id ?? null);
  const cells = new Map();
  for (const col of store.columns()) for (const lane of laneIds) cells.set(cellKey(col.id, lane), []);

  for (const card of visible) {
    const lane = groupBy ? laneKeyOf(card, groupBy) ?? '' : card.row ?? null;
    const key = cellKey(card.column, lane);
    if (!cells.has(key)) cells.set(key, []);
    cells.get(key).push(card.id);
  }

  if (hasSort) {
    const cmp = makeComparator(sort);
    for (const [key, ids] of cells) {
      cells.set(key, ids.map((id, i) => ({ ...store.card(id), _ix: i }))
        .sort(cmp)
        .map((card) => card.id));
    }
  }

  const columns = store.columns().map((col) => {
    const count = laneIds.reduce((n, lane) => n + (cells.get(cellKey(col.id, lane))?.length ?? 0), 0);
    return { ...clone(col), count, over: col.cardLimit != null && count > col.cardLimit };
  });

  return remember(store, query, {
    lanes,
    columns,
    cells,
    cards: new Map(visible.map((card) => [card.id, clone(card)])),
    hiddenByFilter: allCards.length - visible.length,
    manualOrderDisabled: hasSort,
  });
}
