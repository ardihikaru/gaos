import { createZip, dosStamp } from './zip.js';

const enc = new TextEncoder();
const clean = (s) => String(s ?? '').replace(/[\x00-\x08\x0B\x0C\x0E-\x1F]/g, '');
const esc = (s) => clean(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
const attr = (s) => esc(s).replace(/"/g, '&quot;');

export function colLetter(n) {
  let s = '';
  while (n > 0) {
    n -= 1;
    s = String.fromCharCode(65 + (n % 26)) + s;
    n = (n / 26) | 0;
  }
  return s;
}

function optionLabel(shape, field, value) {
  const opt = shape?.[field]?.options?.find((o) => String(o.id) === String(value));
  return opt?.label ?? (value ?? '');
}

function rowsFromProjection(projection) {
  const rows = [];
  for (const lane of projection.lanes || [null]) for (const col of projection.columns || []) {
    const ids = projection.cells.get(String(col.id) + '\u0000' + (lane?.id ?? '')) || [];
    for (const id of ids) rows.push({ card: projection.cards.get(id), col, lane });
  }
  return rows;
}

function rowsFromStore(store) {
  const doc = store.serialize();
  const cols = new Map(doc.columns.map((c) => [c.id, c]));
  const lanes = new Map(doc.rows.map((r) => [r.id, r]));
  return doc.cards.map((card) => ({ card, col: cols.get(card.column), lane: card.row == null ? null : lanes.get(card.row) }));
}

function cell(r, c, value, type = 's') {
  const ref = colLetter(c) + r;
  return type === 'n' ? `<c r="${ref}"><v>${Number(value) || 0}</v></c>`
    : `<c r="${ref}" t="inlineStr"><is><t>${esc(value)}</t></is></c>`;
}

function sheetXml(rows, t, cardShape) {
  const headers = ['column', 'lane', 'title', 'description', 'priority', 'progress', 'deadline', 'tags', 'users']
    .map((k) => t('export.' + k));
  let xml = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>';
  xml += `<row r="1">${headers.map((h, i) => cell(1, i + 1, h)).join('')}</row>`;
  rows.forEach(({ card, col, lane }, i) => {
    const r = i + 2;
    const vals = [
      col?.label || '', lane?.label || '', card?.label || '', card?.description || '',
      optionLabel(cardShape, 'priority', card?.priority), card?.progress ?? 0,
      card?.deadline || '', (card?.tags || []).join(', '), (card?.users || []).map((u) => optionLabel(cardShape, 'users', u)).join(', '),
    ];
    xml += `<row r="${r}">${vals.map((v, ci) => cell(r, ci + 1, v, ci === 5 ? 'n' : 's')).join('')}</row>`;
  });
  return xml + '</sheetData></worksheet>';
}

export function renderXlsx({ projection, store, i18n, cardShape = {}, opts = {} }) {
  const t = i18n?.t || ((k) => k);
  const rows = opts.all && store ? rowsFromStore(store) : rowsFromProjection(projection);
  const parts = [
    ['[Content_Types].xml', '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>'],
    ['_rels/.rels', '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>'],
    ['xl/workbook.xml', `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="${attr(t('export.sheetName'))}" sheetId="1" r:id="rId1"/></sheets></workbook>`],
    ['xl/_rels/workbook.xml.rels', '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>'],
    ['xl/worksheets/sheet1.xml', sheetXml(rows, t, cardShape)],
  ];
  const zip = createZip(opts.stamp || dosStamp(new Date(2026, 0, 1)));
  for (const [name, xml] of parts) zip.add(name, enc.encode(xml));
  return zip.end();
}
