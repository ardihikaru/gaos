export function createDragVisual(cardEl, { x, y, grabDX, grabDY }) {
  const rect = cardEl.getBoundingClientRect();
  const layer = document.createElement('div');
  layer.className = 'sfb-ghost-layer';
  const ghost = cardEl.cloneNode(true);
  ghost.classList.add('sfb-ghost');
  ghost.style.width = rect.width + 'px';
  layer.append(ghost);
  document.body.append(layer);
  cardEl.classList.add('sfb-drag-source');
  moveGhost(ghost, x, y, grabDX, grabDY);
  return {
    rect,
    ghost,
    placeholder: createPlaceholder(rect.height),
    move: (px, py) => moveGhost(ghost, px, py, grabDX, grabDY),
    destroy() {
      cardEl.classList.remove('sfb-drag-source');
      layer.remove();
      this.placeholder.remove();
    },
  };
}

export function createPlaceholder(height) {
  const el = document.createElement('div');
  el.className = 'sfb-placeholder';
  el.style.height = Math.max(24, height) + 'px';
  return el;
}

export function moveGhost(ghost, px, py, grabDX, grabDY) {
  ghost.style.transform = `translate(${Math.round(px - grabDX)}px, ${Math.round(py - grabDY)}px)`;
}

export function placePlaceholder(cellEl, placeholder, index) {
  if (!cellEl) return;
  const cards = [...cellEl.querySelectorAll('.sfb-card:not(.sfb-drag-source)')];
  const before = cards[Math.max(0, Math.min(index, cards.length))] || null;
  if (placeholder.parentNode !== cellEl || placeholder.nextSibling !== before) {
    cellEl.insertBefore(placeholder, before);
  }
}
