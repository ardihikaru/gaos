import { h, icon } from './dom.js';

const has = (v) => Array.isArray(v) ? v.length > 0 : v != null && v !== '';
const initials = (v) => String(v).trim().split(/\s+/).map((p) => p[0]).join('').slice(0, 2).toUpperCase() || '?';
const dayMs = 86_400_000;
const localDate = (iso) => {
  const [y, m, d] = String(iso).slice(0, 10).split('-').map(Number);
  return y && m && d ? new Date(y, m - 1, d) : new Date(iso);
};

function cover(card) {
  if (!card.cover) return null;
  return h('img', {
    class: 'sfb-card-cover',
    src: card.cover,
    alt: '',
    loading: 'lazy',
    onError: (ev) => ev.currentTarget.remove(),
  });
}

function tags(card) {
  if (!has(card.tags)) return null;
  return h('div', { class: 'sfb-tags' }, card.tags.map((tag) => h('span', { class: 'sfb-tag' }, tag)));
}

function progress(card, i18n) {
  if (card.progress == null) return null;
  const pct = Math.round(card.progress * 100);
  return h('div', { class: 'sfb-progress', aria: { label: i18n.t('card.progress', { value: pct }) } }, [
    h('span', { style: { width: pct + '%' } }),
  ]);
}

function deadline(card, i18n) {
  if (!card.deadline) return null;
  const today = new Date();
  const due = localDate(card.deadline);
  const days = Math.round((due - new Date(today.getFullYear(), today.getMonth(), today.getDate())) / dayMs);
  const text = Math.abs(days) <= 7 ? i18n.relativeDays(card.deadline) : i18n.date(card.deadline);
  return h('span', { class: ['sfb-chip', days < 0 && 'is-danger'] }, [icon('calendar'), text]);
}

function users(card, i18n) {
  if (!has(card.users)) return null;
  const shown = card.users.slice(0, 3);
  return h('div', { class: 'sfb-avatars', aria: { label: i18n.t('card.users') } }, [
    shown.map((user) => h('span', { class: 'sfb-avatar', title: String(user) }, initials(user))),
    card.users.length > shown.length ? h('span', { class: 'sfb-avatar more' }, i18n.t('card.moreUsers', { count: card.users.length - shown.length })) : null,
  ]);
}

function counters(card, i18n) {
  const items = [
    ['attachments', card.attachments],
    ['comments', card.comments],
    ['tasks', card.tasks],
  ].filter(([, value]) => value > 0);
  if (items.length === 0) return null;
  return h('div', { class: 'sfb-counters' }, items.map(([name, value]) =>
    h('span', { class: 'sfb-counter' }, i18n.t('card.counter.' + name, { count: i18n.number(value) }))));
}

export function defaultCardFace(card, deps = {}) {
  const i18n = deps.i18n || { t: (k, p) => p?.value ?? k, date: (v) => v, number: (v) => v, relativeDays: (v) => v };
  return [
    cover(card),
    h('div', { class: 'sfb-card-main' }, [
      h('div', { class: 'sfb-priority-strip' }),
      h('div', { class: 'sfb-card-content' }, [
        h('div', { class: 'sfb-card-title' }, card.label || ''),
        card.description ? h('p', { class: 'sfb-card-description' }, card.description) : null,
        tags(card),
        progress(card, i18n),
        h('div', { class: 'sfb-card-meta' }, [deadline(card, i18n), users(card, i18n)]),
        counters(card, i18n),
      ]),
    ]),
  ];
}

export function renderCard(card, deps = {}) {
  const pri = card.priority == null ? 'none' : String(card.priority);
  const t = deps.i18n?.t || ((key) => key);
  const menuLabel = t('menu.edit') + ': ' + (card.label || card.id);
  const shell = h('article', {
    class: ['sfb-card', card.css],
    tabindex: '0',
    role: 'listitem',
    dataset: { cardId: card.id, priority: pri },
    onClick: () => deps.emitter?.emit('card-click', { id: card.id, card }),
  });
  const ctx = {
    t: deps.i18n?.t || ((key) => key),
    locale: deps.i18n?.locale?.() || 'en',
    theme: deps.theme?.(),
    exec: deps.exec,
    defaultRender: (next = card) => h('div', { class: 'sfb-card-default' }, defaultCardFace(next, deps)),
  };
  const custom = deps.cardTemplate?.(card, ctx);
  const content = custom || defaultCardFace(card, deps);
  shell.append(
    h('button', {
      class: ['sfb-icon-button', 'sfb-card-menu-trigger'],
      type: 'button',
      title: menuLabel,
      aria: { label: menuLabel, haspopup: 'menu' },
      dataset: { menuTrigger: 'card' },
    }, icon('dots')),
    ...(Array.isArray(content) ? content : [content]).filter((child) => child != null && child !== false),
  );
  return shell;
}
