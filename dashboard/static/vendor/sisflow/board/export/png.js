function roundRect(ctx, x, y, w, h, r) {
  if (ctx.roundRect) { ctx.roundRect(x, y, w, h, r); return; }
  const rr = Math.min(r, w / 2, h / 2);
  ctx.moveTo(x + rr, y); ctx.arcTo(x + w, y, x + w, y + h, rr);
  ctx.arcTo(x + w, y + h, x, y + h, rr); ctx.arcTo(x, y + h, x, y, rr);
  ctx.arcTo(x, y, x + w, y, rr); ctx.closePath();
}

function drawRect(ctx, it) {
  ctx.beginPath();
  if (it.radius) roundRect(ctx, it.x, it.y, it.w, it.h, it.radius);
  else ctx.rect(it.x, it.y, it.w, it.h);
  if (it.fill) { ctx.fillStyle = it.fill; ctx.fill(); }
  if (it.stroke) { ctx.strokeStyle = it.stroke; ctx.lineWidth = it.lineWidth || 1; ctx.stroke(); }
}

function loadImage(src) {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.onload = () => resolve(img);
    img.onerror = reject;
    img.src = src;
  });
}

async function images(items) {
  const map = new Map();
  const list = items.filter((it) => it.kind === 'image' && it.src);
  const loaded = await Promise.allSettled(list.map((it) => loadImage(it.src)));
  loaded.forEach((res, i) => { if (res.status === 'fulfilled') map.set(list[i].src, res.value); });
  return map;
}

export async function renderPng(layout, { scale = 2 } = {}) {
  if (typeof document === 'undefined') throw new Error('sisflow: PNG export requires a browser canvas');
  const canvas = document.createElement('canvas');
  canvas.width = Math.ceil(layout.width * scale);
  canvas.height = Math.ceil(layout.height * scale);
  const ctx = canvas.getContext('2d');
  const imgs = await images(layout.items);
  ctx.scale(scale, scale);
  for (const it of layout.items) {
    if (it.kind === 'rect') drawRect(ctx, it);
    else if (it.kind === 'text') {
      ctx.fillStyle = it.color; ctx.font = `${it.bold ? '600 ' : ''}${it.size}px Inter, system-ui, sans-serif`;
      ctx.textBaseline = 'top'; ctx.fillText(it.text, it.x, it.y, it.maxW);
    } else if (it.kind === 'badge') {
      drawRect(ctx, { ...it, radius: it.h / 2 });
      ctx.fillStyle = it.color; ctx.font = `600 10px Inter, system-ui, sans-serif`; ctx.textBaseline = 'middle';
      ctx.fillText(it.text, it.x + Math.max(4, (it.w - ctx.measureText(it.text).width) / 2), it.y + it.h / 2);
    } else if (it.kind === 'bar') {
      drawRect(ctx, { ...it, fill: it.track, radius: it.h / 2 });
      drawRect(ctx, { ...it, w: it.w * Math.max(0, Math.min(1, it.value)), fill: it.fill, radius: it.h / 2 });
    } else if (it.kind === 'image') {
      const img = imgs.get(it.src);
      if (img) ctx.drawImage(img, it.x, it.y, it.w, it.h);
      else drawRect(ctx, { ...it, radius: 6 });
    }
  }
  return await new Promise((resolve, reject) => canvas.toBlob((b) => b ? resolve(b) : reject(new Error('sisflow: PNG export failed')), 'image/png'));
}
