import { h, icon } from './dom.js';

const vals = (v) => Array.isArray(v) ? v : [];
const optId = (o) => String(o && typeof o === 'object' ? o.id : o);
const optLabel = (o, i18n) => {
  const raw = o && typeof o === 'object' ? (o.label ?? o.id) : o;
  return typeof raw === 'string' && raw.includes('.') ? i18n.t(raw) : String(raw ?? '');
};

export function editorFields(shape = {}) {
  const out = [{ kind: 'text', key: 'label', label: 'editor.title', required: true }];
  if (shape.description) out.push({ kind: 'textarea', key: 'description', label: 'editor.description' });
  if (shape.priority) out.push({ kind: 'select', key: 'priority', label: 'editor.priority', options: shape.priority.options, clearable: true });
  if (shape.progress) out.push({ kind: 'slider', key: 'progress', label: 'editor.progress', min: 0, max: 1, step: 0.1 });
  if (shape.deadline) out.push({ kind: 'date', key: 'deadline', label: 'editor.deadline', clearable: true });
  if (shape.tags?.options?.length) out.push({ kind: 'multiselect', key: 'tags', label: 'editor.tags', options: shape.tags.options });
  if (shape.users?.options?.length) out.push({ kind: 'multiselect', key: 'users', label: 'editor.users', options: shape.users.options });
  if (shape.cover) out.push({ kind: 'text', key: 'cover', label: 'editor.cover' });
  return out;
}

function labeled(field, control, i18n) {
  return h('label', { class: 'sfb-editor-field' }, [
    h('span', {}, i18n.t(field.label)),
    control,
  ]);
}

function textLike(field, value, onChange, i18n, tag) {
  const input = h(tag, {
    type: tag === 'input' ? 'text' : null,
    required: field.required,
    value: value ?? '',
    onInput: (ev) => {
      if (tag === 'textarea') ev.currentTarget.style.height = ev.currentTarget.scrollHeight + 'px';
      onChange(ev.currentTarget.value);
    },
  });
  if (tag === 'textarea') queueMicrotask(() => { input.style.height = input.scrollHeight + 'px'; });
  return labeled(field, input, i18n);
}

function selectField(field, value, onChange, i18n) {
  const select = h('select', { onChange: (ev) => onChange(ev.currentTarget.value || null) }, [
    field.clearable ? h('option', { value: '' }, i18n.t('editor.clear')) : null,
    (field.options || []).map((opt) => h('option', { value: optId(opt), selected: String(value ?? '') === optId(opt) }, optLabel(opt, i18n))),
  ]);
  return labeled(field, select, i18n);
}

function sliderField(field, value, onChange, i18n) {
  const pct = (v) => Math.round(Number(v || 0) * 100) + '%';
  const out = h('output', {}, pct(value));
  const input = h('input', {
    type: 'range',
    min: field.min,
    max: field.max,
    step: field.step,
    value: value ?? 0,
    onInput: (ev) => {
      const next = Number(ev.currentTarget.value);
      out.textContent = pct(next);
      onChange(next);
    },
  });
  return labeled(field, h('div', { class: 'sfb-editor-slider' }, [input, out]), i18n);
}

function dateField(field, value, onChange, i18n) {
  return labeled(field, h('input', {
    type: 'date',
    value: value ? String(value).slice(0, 10) : '',
    onInput: (ev) => onChange(ev.currentTarget.value || null),
  }), i18n);
}

function multiselectField(field, value, onChange, i18n) {
  let selected = vals(value).map(String);
  const box = h('div', { class: 'sfb-multiselect' });
  const emit = (next) => { selected = next; onChange(next); render(''); };
  const toggle = (id) => emit(selected.includes(id) ? selected.filter((v) => v !== id) : [...selected, id]);
  function render(filter) {
    const q = filter.toLowerCase();
    const matches = (field.options || []).filter((o) => optLabel(o, i18n).toLowerCase().includes(q));
    box.replaceChildren(
      h('div', { class: 'sfb-ms-chips' }, selected.map((id) => h('button', {
        type: 'button',
        class: 'sfb-ms-chip',
        onClick: () => toggle(id),
      }, [id, icon('close')]))),
      h('input', { type: 'search', placeholder: i18n.t('editor.filter'), value: filter, onInput: (ev) => render(ev.currentTarget.value) }),
      h('div', { class: 'sfb-ms-menu', role: 'listbox' }, matches.length ? matches.map((opt) => {
        const id = optId(opt);
        return h('button', { type: 'button', class: selected.includes(id) && 'is-selected', onClick: () => toggle(id) }, optLabel(opt, i18n));
      }) : h('span', { class: 'sfb-ms-empty' }, i18n.t('editor.empty'))),
    );
  }
  render('');
  return labeled(field, box, i18n);
}

export function renderField(field, value, onChange, i18n) {
  if (field.kind === 'textarea') return textLike(field, value, onChange, i18n, 'textarea');
  if (field.kind === 'select') return selectField(field, value, onChange, i18n);
  if (field.kind === 'slider') return sliderField(field, value, onChange, i18n);
  if (field.kind === 'date') return dateField(field, value, onChange, i18n);
  if (field.kind === 'multiselect') return multiselectField(field, value, onChange, i18n);
  return textLike(field, value, onChange, i18n, 'input');
}
