export function createPdfWriter() {
  const parts = [];
  const offsets = [];
  let pos = 0;
  const push = (s) => { parts.push(s); pos += s.length; };

  push('%PDF-1.4\n%\xE2\xE3\xCF\xD3\n');

  function obj(body) {
    offsets.push(pos);
    const n = offsets.length;
    push(`${n} 0 obj\n${body}\nendobj\n`);
    return n;
  }

  function stream(dict, content) {
    return obj(`<< ${dict} /Length ${content.length} >>\nstream\n${content}\nendstream`);
  }

  function end(rootNum) {
    const xrefAt = pos;
    push(`xref\n0 ${offsets.length + 1}\n0000000000 65535 f \n`);
    for (const o of offsets) push(`${String(o).padStart(10, '0')} 00000 n \n`);
    push(`trailer\n<< /Size ${offsets.length + 1} /Root ${rootNum} 0 R >>\nstartxref\n${xrefAt}\n%%EOF`);
    return parts.join('');
  }

  return { obj, stream, end };
}
