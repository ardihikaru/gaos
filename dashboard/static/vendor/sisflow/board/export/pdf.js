import { createPdfWriter } from './pdf-writer.js';
import { widthOf } from './pdf-metrics.js';

const A4 = { w: 842, h: 595 };
const pdfColor = (hex) => {
  const s = String(hex || '000000').replace('#', '');
  const n = parseInt(s.length === 3 ? s.replace(/(.)/g, '$1$1') : s.slice(0, 6), 16);
  return [((n >> 16) & 255) / 255, ((n >> 8) & 255) / 255, (n & 255) / 255].map((v) => v.toFixed(3)).join(' ');
};
const esc = (s) => [...String(s || '')].map((ch) => {
  const c = ch.codePointAt(0);
  return ch === '(' || ch === ')' || ch === '\\' ? '\\' + ch : c > 255 ? '?' : ch;
}).join('');
const num = (n) => Number(n.toFixed(3));
const yRect = (H, it) => num(H - it.y - it.h);
const yText = (H, it) => num(H - it.y - it.size * 0.8);

function pathRound(x, y, w, h, r) {
  const k = 0.5523, c = r * k, x2 = x + w, y2 = y + h;
  return `${num(x + r)} ${num(y)} m ${num(x2 - r)} ${num(y)} l ${num(x2 - r + c)} ${num(y)} ${num(x2)} ${num(y + r - c)} ${num(x2)} ${num(y + r)} c ${num(x2)} ${num(y2 - r)} l ${num(x2)} ${num(y2 - r + c)} ${num(x2 - r + c)} ${num(y2)} ${num(x2 - r)} ${num(y2)} c ${num(x + r)} ${num(y2)} l ${num(x + r - c)} ${num(y2)} ${num(x)} ${num(y2 - r + c)} ${num(x)} ${num(y2 - r)} c ${num(x)} ${num(y + r)} l ${num(x)} ${num(y + r - c)} ${num(x + r - c)} ${num(y)} ${num(x + r)} ${num(y)} c h`;
}

function fillRect(it, H, fill) {
  const p = `${pdfColor(fill)} rg `;
  return it.radius ? p + pathRound(it.x, yRect(H, it), it.w, it.h, it.radius) + ' f\n'
    : `${p}${num(it.x)} ${yRect(H, it)} ${num(it.w)} ${num(it.h)} re f\n`;
}

function strokeRect(it, H) {
  if (!it.stroke) return '';
  const path = it.radius ? pathRound(it.x, yRect(H, it), it.w, it.h, it.radius)
    : `${num(it.x)} ${yRect(H, it)} ${num(it.w)} ${num(it.h)} re`;
  return `${pdfColor(it.stroke)} RG ${num(it.lineWidth || 1)} w ${path} S\n`;
}

function text(it, H, x = it.x, y = yText(H, it), alignW = 0) {
  const dx = alignW ? Math.max(0, (alignW - widthOf(it.text, it.size)) / 2) : 0;
  return `${pdfColor(it.color)} rg BT /${it.bold ? 'F2' : 'F1'} ${num(it.size)} Tf 1 0 0 1 ${num(x + dx)} ${num(y)} Tm (${esc(it.text)}) Tj ET\n`;
}

function opsFor(layout, H) {
  let out = 'q\n';
  for (const it of layout.items) {
    if (it.kind === 'rect') {
      if (it.fill) out += fillRect(it, H, it.fill);
      out += strokeRect(it, H);
    } else if (it.kind === 'text') out += text(it, H);
    else if (it.kind === 'bar') {
      out += fillRect({ ...it, fill: it.track, radius: it.h / 2 }, H, it.track);
      out += fillRect({ ...it, w: it.w * Math.max(0, Math.min(1, it.value)), fill: it.fill, radius: it.h / 2 }, H, it.fill);
    } else if (it.kind === 'badge') {
      out += fillRect({ ...it, radius: it.h / 2 }, H, it.fill);
      out += text({ ...it, size: 10 }, H, it.x, H - it.y - it.h / 2 - 3, it.w);
    } else if (it.kind === 'image') {
      out += fillRect({ ...it, radius: 6 }, H, it.fill);
    }
  }
  return out + 'Q\n';
}

export function renderPdf(layout, opts = {}) {
  const scale = Math.min(1, A4.w / layout.width, A4.h / layout.height);
  if (scale < (opts.scaleFloor ?? 0.4)) opts.warn?.(`sisflow: PDF scale ${scale.toFixed(2)} is below recommended floor`);
  const pw = num(layout.width * scale), ph = num(layout.height * scale);
  const ops = `q ${num(scale)} 0 0 ${num(scale)} 0 0 cm\n${opsFor(layout, layout.height)}Q\n`;
  const w = createPdfWriter();
  w.obj('<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>');
  w.obj('<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>');
  const content = w.stream('', ops);
  const page = w.obj(`<< /Type /Page /Parent 5 0 R /MediaBox [0 0 ${pw} ${ph}] /Resources << /Font << /F1 1 0 R /F2 2 0 R >> >> /Contents ${content} 0 R >>`);
  const pages = w.obj(`<< /Type /Pages /Kids [${page} 0 R] /Count 1 >>`);
  const catalog = w.obj(`<< /Type /Catalog /Pages ${pages} 0 R >>`);
  return w.end(catalog);
}

export { widthOf };
