import { clone } from '../core/shapes.js';
import { h, icon } from './dom.js';
import { editorFields, renderField } from './editor-fields.js';

const same = (a, b) => JSON.stringify(a ?? null) === JSON.stringify(b ?? null);
const esc = (v) => globalThis.CSS?.escape ? CSS.escape(String(v)) : String(v).replace(/"/g, '\\"');

function diff(base, draft, fields) {
  const patch = {};
  for (const f of fields) if (!same(base[f.key], draft[f.key])) patch[f.key] = clone(draft[f.key]);
  return patch;
}

function setEditing(root, id) {
  root.querySelectorAll('.sfb-card.sfb-editing').forEach((el) => el.classList.remove('sfb-editing'));
  if (id != null) root.querySelector(`.sfb-card[data-card-id="${esc(id)}"]`)?.classList.add('sfb-editing');
}

export function mountEditor(root, deps) {
  let id = null, draft = null, panel = null, dirtyPrompt = false;
  let dirtyBanner = null, dirtyState = null, saveButton = null;
  const i18n = () => deps.i18n;
  const fields = () => editorFields(deps.cardShape);
  const card = () => id == null ? null : deps.store.card(id);
  const dirty = () => {
    const c = card();
    return !!c && Object.keys(diff(c, draft || {}, fields())).length > 0;
  };
  const close = () => {
    panel?.remove();
    panel = null;
    dirtyBanner = null;
    dirtyState = null;
    saveButton = null;
    id = null;
    draft = null;
    dirtyPrompt = false;
    setEditing(root, null);
  };
  const save = () => {
    const c = card();
    if (!c || deps.board.readOnly) return false;
    const patch = diff(c, draft, fields());
    if (Object.keys(patch).length) deps.exec('updateCard', { id, patch });
    close();
    return true;
  };
  const remove = () => {
    if (id != null && !deps.board.readOnly) deps.exec('deleteCard', { id });
    close();
  };
  const open = (nextId) => {
    if (id && String(id) !== String(nextId) && dirty()) {
      dirtyPrompt = true;
      render();
      return;
    }
    id = nextId;
    draft = clone(deps.store.card(id));
    dirtyPrompt = false;
    render();
  };
  function updateDirtyState() {
    if (!panel) return;
    const isDirty = dirty();
    panel.classList.toggle('is-dirty', isDirty);
    if (dirtyBanner) {
      dirtyBanner.hidden = !dirtyPrompt;
      dirtyBanner.textContent = dirtyPrompt ? i18n().t('editor.unsaved') : '';
    }
    if (dirtyState) dirtyState.textContent = isDirty ? i18n().t('editor.unsaved') : '';
    if (saveButton) saveButton.disabled = deps.board.readOnly || !isDirty;
  }
  function render() {
    panel?.remove();
    const c = card();
    if (!c) return close();
    const t = i18n().t;
    const onChange = (key, value) => {
      draft = { ...draft, [key]: value };
      dirtyPrompt = false;
      updateDirtyState();
    };
    dirtyBanner = h('div', { class: 'sfb-editor-dirty', hidden: !dirtyPrompt }, dirtyPrompt ? t('editor.unsaved') : '');
    dirtyState = h('span', { class: 'sfb-editor-state' });
    saveButton = h('button', { type: 'button', class: 'sfb-editor-save', disabled: deps.board.readOnly || !dirty(), onClick: save }, t('editor.save'));
    panel = h('aside', { class: ['sfb-editor', dirty() && 'is-dirty'], role: 'dialog', aria: { label: t('editor.heading') } }, [
      h('header', { class: 'sfb-editor-header' }, [
        h('h2', {}, t('editor.heading')),
        h('button', { class: 'sfb-icon-button', type: 'button', title: t('editor.close'), onClick: close }, icon('close')),
      ]),
      dirtyBanner,
      h('div', { class: 'sfb-editor-form' },
        fields().map((f) => renderField(f, draft?.[f.key], (v) => onChange(f.key, v), i18n()))),
      h('footer', { class: 'sfb-editor-actions' }, [
        h('button', { type: 'button', class: 'sfb-editor-delete', disabled: deps.board.readOnly, onClick: remove }, t('editor.delete')),
        dirtyState,
        h('button', { type: 'button', onClick: close }, t('editor.discard')),
        saveButton,
      ]),
    ]);
    if (deps.board.readOnly) panel.querySelectorAll('input, textarea, select').forEach((el) => { el.disabled = true; });
    root.append(panel);
    setEditing(root, id);
    updateDirtyState();
  }
  const dbl = (ev) => {
    const el = ev.target.closest?.('.sfb-card');
    if (el && root.contains(el)) deps.exec('editCard', { id: el.dataset.cardId });
  };
  const key = (ev) => { if (id != null && ev.key === 'Escape') close(); };
  const offOpen = deps.emitter.on('board:editor-open', ({ id: next }) => open(next));
  const offChange = deps.emitter.on('board:change', () => id == null || render());
  const offLocale = deps.emitter.on('board:locale', () => id == null || render());
  root.addEventListener('dblclick', dbl);
  document.addEventListener('keydown', key);
  return { close, render, destroy: () => { offOpen(); offChange(); offLocale(); root.removeEventListener('dblclick', dbl); document.removeEventListener('keydown', key); close(); } };
}
