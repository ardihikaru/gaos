const CRC_TABLE = (() => {
  const t = new Uint32Array(256);
  for (let n = 0; n < 256; n += 1) {
    let c = n;
    for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xEDB88320 ^ (c >>> 1) : c >>> 1;
    t[n] = c;
  }
  return t;
})();

export function crc32(bytes) {
  let c = 0xFFFFFFFF;
  for (let i = 0; i < bytes.length; i += 1) c = CRC_TABLE[(c ^ bytes[i]) & 0xFF] ^ (c >>> 8);
  return (c ^ 0xFFFFFFFF) >>> 0;
}

export function dosStamp(date = new Date()) {
  return {
    dosDate: ((date.getFullYear() - 1980) << 9) | ((date.getMonth() + 1) << 5) | date.getDate(),
    dosTime: (date.getHours() << 11) | (date.getMinutes() << 5) | (date.getSeconds() >> 1),
  };
}

const utf8 = (s) => new TextEncoder().encode(String(s));
const bytesOf = (data) => data instanceof Uint8Array ? data : utf8(data);

export function createZip(stamp = dosStamp(new Date(2026, 0, 1))) {
  const files = [];
  const out = [];
  let pos = 0;
  const put = (...bs) => { out.push(...bs); pos += bs.length; };
  const u16 = (v) => put(v & 255, (v >>> 8) & 255);
  const u32 = (v) => put(v & 255, (v >>> 8) & 255, (v >>> 16) & 255, (v >>> 24) & 255);
  const header = (sig, f, localOffset = 0) => {
    u32(sig);
    if (sig === 0x02014B50) u16(20);
    u16(20); u16(0); u16(0); u16(stamp.dosTime); u16(stamp.dosDate);
    u32(f.crc); u32(f.data.length); u32(f.data.length); u16(f.name.length); u16(0);
    if (sig === 0x02014B50) { u16(0); u16(0); u16(0); u32(0); u32(localOffset); }
    put(...f.name);
  };
  return {
    add(name, data) {
      const rec = { name: utf8(name), data: bytesOf(data) };
      rec.crc = crc32(rec.data);
      files.push(rec);
      return this;
    },
    end() {
      for (const f of files) {
        f.localOffset = pos;
        header(0x04034B50, f);
        put(...f.data);
      }
      const cdOffset = pos;
      for (const f of files) header(0x02014B50, f, f.localOffset);
      const cdSize = pos - cdOffset;
      u32(0x06054B50); u16(0); u16(0); u16(files.length); u16(files.length);
      u32(cdSize); u32(cdOffset); u16(0);
      return new Uint8Array(out);
    },
  };
}
