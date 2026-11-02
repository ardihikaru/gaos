import { cellKey } from '../core/store.js';

export const METRICS = {
  COL_W: 280, COL_GAP: 16, CARD_PAD: 12, CARD_GAP: 10, LINE_H: 18,
  FONT: 13, FONT_SM: 11, HEADER_H: 40, LANE_HEADER_H: 32, RADIUS: 8,
  COVER_H: 120, BAR_H: 6, AVATAR: 24, MARGIN: 24,
};

const color = (tokens, key) => tokens[key];
const clamp01 = (n) => Math.max(0, Math.min(1, Number(n) || 0));
const textOf = (v) => v == null ? '' : String(v);
const has = (v) => v !== undefined && v !== null && v !== '';

function ellipsize(text, maxW, size, measure) {
  let s = text;
  while (s.length && measure(s + '...', size) > maxW) s = s.slice(0, -1);
  return s ? s + '...' : '...';
}

export function wrapText(text, maxW, size, measure, maxLines = Infinity) {
  const words = textOf(text).split(/\s+/).filter(Boolean);
  const lines = [];
  let line = '', consumed = 0;
  for (const word of words) {
    const probe = line ? line + ' ' + word : word;
    if (measure(probe, size) <= maxW || !line) {
      line = probe;
      consumed += 1;
    } else {
      lines.push(line);
      line = word;
      consumed += 1;
      if (lines.length === maxLines - 1) break;
    }
  }
  if (line && lines.length < maxLines) lines.push(line);
  if (lines.length === maxLines && consumed < words.length) {
    lines[maxLines - 1] = ellipsize(lines[maxLines - 1], maxW, size, measure);
  }
  return lines;
}

function cardHeight(card, m, labelLines, descLines) {
  let h = m.CARD_PAD * 2 + labelLines.length * m.LINE_H + 8 + m.FONT_SM + 6 + m.BAR_H;
  if (card.cover) h += m.COVER_H + 8;
  if (descLines.length) h += descLines.length * m.LINE_H + 4;
  return h;
}

function addCard(items, card, x, y, w, tokens, measure, m) {
  const inner = w - m.CARD_PAD * 2;
  const label = wrapText(card.label, inner, m.FONT, measure, 2);
  const desc = wrapText(card.description, inner, m.FONT_SM, measure, 2);
  const h = cardHeight(card, m, label, desc);
  items.push({ kind: 'rect', x, y, w, h, fill: color(tokens, '--sfb-surface'), stroke: color(tokens, '--sfb-border'), lineWidth: 1, radius: m.RADIUS });
  let cy = y + m.CARD_PAD;
  if (card.cover) {
    items.push({ kind: 'image', x: x + m.CARD_PAD, y: cy, w: inner, h: m.COVER_H, src: card.cover, fill: color(tokens, '--sfb-column-bg') });
    cy += m.COVER_H + 8;
  }
  for (const line of label) {
    items.push({ kind: 'text', x: x + m.CARD_PAD, y: cy, size: m.FONT, text: line, color: color(tokens, '--sfb-text'), bold: true, maxW: inner });
    cy += m.LINE_H;
  }
  if (desc.length) cy += 4;
  for (const line of desc) {
    items.push({ kind: 'text', x: x + m.CARD_PAD, y: cy, size: m.FONT_SM, text: line, color: color(tokens, '--sfb-text-muted'), maxW: inner });
    cy += m.LINE_H;
  }
  cy += 4;
  const badgeText = has(card.priority) ? 'P' + card.priority : (card.tags?.[0] ? String(card.tags[0]) : 'meta');
  items.push({ kind: 'badge', x: x + m.CARD_PAD, y: cy, w: Math.min(84, Math.max(36, measure(badgeText, m.FONT_SM) + 16)), h: 20, text: badgeText, fill: color(tokens, '--sfb-tag-bg'), color: color(tokens, '--sfb-text') });
  const meta = [card.comments && card.comments + 'c', card.attachments && card.attachments + 'a', card.tasks && card.tasks + 't'].filter(Boolean).join(' ');
  if (meta) items.push({ kind: 'text', x: x + w - m.CARD_PAD - measure(meta, m.FONT_SM), y: cy + 3, size: m.FONT_SM, text: meta, color: color(tokens, '--sfb-text-muted'), maxW: inner });
  cy += 26;
  items.push({ kind: 'bar', x: x + m.CARD_PAD, y: cy, w: inner, h: m.BAR_H, value: clamp01(card.progress), track: color(tokens, '--sfb-column-bg'), fill: color(tokens, '--sfb-accent') });
  return h;
}

export function buildExportLayout(projection, { tokens, measure, metrics = METRICS } = {}) {
  const m = metrics, items = [];
  const cols = projection.columns || [], lanes = projection.lanes || [null], cards = projection.cards || new Map();
  const width = m.MARGIN * 2 + cols.length * m.COL_W + Math.max(0, cols.length - 1) * m.COL_GAP;
  let y = m.MARGIN;
  items.push({ kind: 'rect', x: 0, y: 0, w: width, h: 1, fill: color(tokens, '--sfb-bg') });
  for (const lane of lanes) {
    const laneY = y;
    items.push({ kind: 'text', x: m.MARGIN, y, size: m.FONT, text: lane?.label ?? '', color: color(tokens, '--sfb-text'), bold: true, maxW: width - m.MARGIN * 2 });
    y += m.LANE_HEADER_H;
    let maxColH = 0;
    cols.forEach((col, ci) => {
      const x = m.MARGIN + ci * (m.COL_W + m.COL_GAP);
      let cy = y + m.HEADER_H;
      items.push({ kind: 'rect', x, y, w: m.COL_W, h: m.HEADER_H, fill: color(tokens, '--sfb-column-bg'), stroke: color(tokens, '--sfb-border'), radius: m.RADIUS });
      items.push({ kind: 'text', x: x + m.CARD_PAD, y: y + 12, size: m.FONT, text: col.label, color: color(tokens, '--sfb-text'), bold: true, maxW: m.COL_W - m.CARD_PAD * 2 });
      const ids = projection.cells?.get(cellKey(col.id, lane?.id ?? null)) || [];
      for (const id of ids) {
        const card = cards.get(id);
        if (!card) continue;
        const h = addCard(items, card, x, cy, m.COL_W, tokens, measure, m);
        cy += h + m.CARD_GAP;
      }
      maxColH = Math.max(maxColH, cy - y);
    });
    y += maxColH + m.CARD_PAD;
    items.push({ kind: 'rect', x: 0, y: laneY, w: width, h: y - laneY, fill: undefined });
  }
  const height = y + m.MARGIN;
  items[0].h = height;
  return { width, height, items };
}
