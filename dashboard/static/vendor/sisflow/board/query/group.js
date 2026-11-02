const optionList = (options) => {
  if (Array.isArray(options)) return options;
  if (Array.isArray(options?.options)) return options.options;
  return [];
};

const optionId = (opt) => (opt && typeof opt === 'object' ? opt.id : opt);
const optionLabel = (opt) => {
  if (opt && typeof opt === 'object' && opt.label != null) return String(opt.label);
  const id = optionId(opt);
  return id == null ? '' : String(id);
};

export function laneKeyOf(card, field) {
  const value = card?.[field];
  if (value == null) return null;
  if (Array.isArray(value)) {
    if (value.length === 0 || value[0] == null || value[0] === '') return null;
    return String(value[0]);
  }
  return value === '' ? null : String(value);
}

export function deriveLanes(field, cards, options = null) {
  const lanes = [], seen = new Set();
  for (const opt of optionList(options)) {
    const id = optionId(opt);
    if (id == null || seen.has(String(id))) continue;
    lanes.push({ id: String(id), label: optionLabel(opt), derived: true });
    seen.add(String(id));
  }
  for (const card of cards) {
    const id = laneKeyOf(card, field);
    if (id !== null && !seen.has(id)) {
      lanes.push({ id, label: id, derived: true });
      seen.add(id);
    }
  }
  lanes.push({ id: '', label: '(none)', derived: true });
  return lanes;
}

export function castBack(laneId, options = null) {
  if (laneId === '') return null;
  for (const opt of optionList(options)) {
    const id = optionId(opt);
    if (String(id) === String(laneId)) return id;
  }
  return laneId;
}
