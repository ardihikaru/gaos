const NS = 'http://www.w3.org/2000/svg';
const PATHS = {
  plus: 'M12 5v14M5 12h14',
  edit: 'M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5Z',
  copy: 'M8 8h12v12H8zM4 16H3a1 1 0 0 1-1-1V4a2 2 0 0 1 2-2h11a1 1 0 0 1 1 1v1',
  trash: 'M3 6h18M8 6V4h8v2M6 6l1 15h10l1-15',
  arrowup: 'M12 19V5M5 12l7-7 7 7',
  arrowdown: 'M12 5v14M19 12l-7 7-7-7',
  arrowleft: 'M19 12H5M12 19l-7-7 7-7',
  arrowright: 'M5 12h14M12 5l7 7-7 7',
  columns: 'M4 5h6v14H4zM14 5h6v14h-6z',
  dots: 'M12 8h.01M12 12h.01M12 16h.01',
  'chevron-left': 'm15 18-6-6 6-6',
  'chevron-right': 'm9 18 6-6-6-6',
  'chevron-down': 'm6 9 6 6 6-6',
  close: 'M18 6 6 18M6 6l12 12',
  search: 'M10.5 18a7.5 7.5 0 1 1 5.3-12.8 7.5 7.5 0 0 1-5.3 12.8ZM16 16l4 4',
  undo: 'M9 14 4 9l5-5M4 9h10a6 6 0 0 1 0 12h-1',
  redo: 'm15 14 5-5-5-5M20 9H10a6 6 0 0 0 0 12h1',
  export: 'M12 3v12M7 8l5-5 5 5M5 15v4h14v-4',
  'drag-handle': 'M9 5h.01M15 5h.01M9 12h.01M15 12h.01M9 19h.01M15 19h.01',
  calendar: 'M7 3v4M17 3v4M4 8h16M5 5h14v16H5z',
  tag: 'M4 5v6l8 8 7-7-8-8H4ZM8 8h.01',
  user: 'M20 21a8 8 0 0 0-16 0M12 13a5 5 0 1 0 0-10 5 5 0 0 0 0 10Z',
};

function append(el, child) {
  if (child == null || child === false) return;
  if (Array.isArray(child)) child.forEach((c) => append(el, c));
  else el.append(typeof Node !== 'undefined' && child instanceof Node ? child : document.createTextNode(String(child)));
}

function attr(el, key, value) {
  if (value == null || value === false) return;
  if (key === 'class') el.className = Array.isArray(value) ? value.filter(Boolean).join(' ') : value;
  else if (key === 'dataset') {
    for (const [k, v] of Object.entries(value)) if (v != null) el.dataset[k] = v;
  }
  else if (key === 'aria') {
    for (const [k, v] of Object.entries(value)) if (v != null) el.setAttribute('aria-' + k, v);
  }
  else if (key === 'style' && typeof value === 'object') Object.assign(el.style, value);
  else if (['value', 'checked', 'selected', 'disabled', 'required'].includes(key) && key in el) {
    el[key] = value === true ? true : value;
    if (value === true) el.setAttribute(key, '');
    else if (key !== 'value') el.setAttribute(key, value);
  }
  else if (key === 'type' && 'type' in el) { el.type = value; el.setAttribute(key, value); }
  else if (value === true) el.setAttribute(key, '');
  else el.setAttribute(key, value);
}

export function h(tag, attrs = {}, children = []) {
  const el = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs || {})) {
    if (key.startsWith('on') && typeof value === 'function') el.addEventListener(key.slice(2).toLowerCase(), value);
    else attr(el, key, value);
  }
  append(el, children);
  return el;
}

export function clear(el) {
  el.replaceChildren();
  return el;
}

export function icon(name) {
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('class', 'sfb-icon');
  const path = document.createElementNS(NS, 'path');
  path.setAttribute('d', PATHS[name] || PATHS.dots);
  path.setAttribute('fill', 'none');
  path.setAttribute('stroke', 'currentColor');
  path.setAttribute('stroke-width', '2');
  path.setAttribute('stroke-linecap', 'round');
  path.setAttribute('stroke-linejoin', 'round');
  svg.append(path);
  return svg;
}
