import { SISMAYA_LIGHT, SISMAYA_DARK } from '../../theme/themes.js';

const THEMES = {
  'sismaya-light': SISMAYA_LIGHT,
  'sismaya-dark': SISMAYA_DARK,
};

export function boardVars(tokens = SISMAYA_LIGHT) {
  return {
    '--sfb-bg': tokens.canvasBg,
    '--sfb-surface': tokens.node.default.fill,
    '--sfb-menu-surface': tokens === SISMAYA_DARK ? '#1C252E' : '#FFFFFF',
    '--sfb-menu-hover': tokens === SISMAYA_DARK ? '#26323D' : '#F4F6F8',
    '--sfb-menu-danger-hover': tokens === SISMAYA_DARK ? '#3A2728' : '#FFF1ED',
    '--sfb-border': tokens.node.default.stroke,
    '--sfb-text': tokens.node.default.text,
    '--sfb-text-muted': tokens.node.muted.text,
    '--sfb-column-bg': tokens.node.muted.fill,
    '--sfb-accent': tokens.selection.halo,
    '--sfb-info': tokens.node.start.stroke,
    '--sfb-success': tokens.node.success.stroke,
    '--sfb-warning': tokens.node.warning.stroke,
    '--sfb-danger': tokens.node.error.stroke,
    '--sfb-priority-1': tokens.node.success.stroke,
    '--sfb-priority-2': tokens.node.warning.stroke,
    '--sfb-priority-3': tokens.node.error.stroke,
    '--sfb-tag-bg': tokens.node.start.fill,
    '--sfb-shadow': tokens === SISMAYA_DARK ? '0 1px 2px rgba(0,0,0,.4)' : '0 1px 2px rgba(28,37,46,.08)',
    '--sfb-z-menu': '40',
    '--sfb-z-editor': '30',
    '--sfb-z-ghost': '50',
  };
}

export function resolveBoardTheme(name = 'sismaya-light') {
  return THEMES[name] ? name : 'sismaya-light';
}

export function exportColors(name = 'sismaya-light') {
  return boardVars(THEMES[resolveBoardTheme(name)]);
}

export function applyTheme(el, name = 'sismaya-light') {
  const resolved = resolveBoardTheme(name);
  for (const [key, value] of Object.entries(exportColors(resolved))) el.style.setProperty(key, value);
  el.dataset.sfbTheme = resolved;
  return resolved;
}

export function bindAutoTheme(el, apply = applyTheme) {
  if (typeof document === 'undefined' || typeof MutationObserver === 'undefined') {
    apply(el, 'sismaya-light');
    return { theme: 'sismaya-light', disconnect() {} };
  }
  const current = () => document.documentElement.getAttribute('data-theme') === 'dark'
    ? 'sismaya-dark' : 'sismaya-light';
  let theme = apply(el, current());
  const observer = new MutationObserver(() => { theme = apply(el, current()); });
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
  return { get theme() { return theme; }, disconnect: () => observer.disconnect() };
}
