/* sisflow — theme registry and built-in token tables.
 * Tokens are flat JS objects (canvas-painted parts cannot read CSS custom
 * properties); values sourced from the sismaya design system per the
 * theming spec (docs/11 §2). Themes resolve once at sisflow.create time. */

export const SISMAYA_LIGHT = {
  canvasBg: '#F9FAFB',
  gridDot: 'rgba(145,158,171,0.24)',
  node: {
    default: { fill: '#FFFFFF', stroke: '#DFE3E8', text: '#1C252E' },
    start:   { fill: 'rgba(0,184,217,0.16)',  stroke: '#00B8D9' },
    success: { fill: 'rgba(34,197,94,0.16)',  stroke: '#22C55E' },
    error:   { fill: 'rgba(255,86,48,0.16)',  stroke: '#FF5630' },
    warning: { fill: 'rgba(255,171,0,0.16)',  stroke: '#FFAB00' },
    muted:   { fill: '#F4F6F8', stroke: '#919EAB', text: '#637381' },
  },
  edge: {
    stroke: '#919EAB',
    arrow: '#919EAB',
    labelText: '#637381',
    labelBg: 'rgba(249,250,251,0.85)',
    valid: '#22C55E',
    invalid: '#FF5630',
  },
  selection: { halo: '#00A76F', grip: '#00A76F', gripFill: '#FFFFFF' },
  anchor: { dot: '#00B8D9', dotFill: '#F9FAFB' },
  /* level-based accent strip for tree cards (docs/10 parity row 12) */
  levelAccent: ['#00A76F', '#00B8D9', '#FFAB00', '#8E33FF', '#FF5630'],
  /* hover emphasis (docs/08 I-02): edges/labels brighten toward text ink */
  hover: { edge: '#454F5B', labelText: '#1C252E' },
};

export const SISMAYA_DARK = {
  canvasBg: '#141A21',
  gridDot: 'rgba(145,158,171,0.16)',
  node: {
    default: { fill: '#1C252E', stroke: '#454F5B', text: '#FFFFFF' },
    start:   { fill: 'rgba(0,184,217,0.16)',  stroke: '#00B8D9' },
    success: { fill: 'rgba(34,197,94,0.16)',  stroke: '#22C55E' },
    error:   { fill: 'rgba(255,86,48,0.16)',  stroke: '#FF5630' },
    warning: { fill: 'rgba(255,171,0,0.16)',  stroke: '#FFAB00' },
    muted:   { fill: 'rgba(145,158,171,0.12)', stroke: '#637381', text: '#919EAB' },
  },
  edge: {
    stroke: '#637381',
    arrow: '#637381',
    labelText: '#919EAB',
    labelBg: 'rgba(20,26,33,0.85)',
    valid: '#77ED8B',
    invalid: '#FFAC82',
  },
  selection: { halo: '#5BE49B', grip: '#5BE49B', gripFill: '#141A21' },
  anchor: { dot: '#61F3F3', dotFill: '#141A21' },
  /* level-based accent strip for tree cards (docs/10 parity row 12) */
  levelAccent: ['#5BE49B', '#61F3F3', '#FFD666', '#C684FF', '#FFAC82'],
  /* hover emphasis (docs/08 I-02): edges/labels brighten toward text ink */
  hover: { edge: '#C4CDD5', labelText: '#FFFFFF' },
};

const registry = new Map();

/** Theme registration/lookup API exposed as sisflow.themes. */
export const themes = {
  register(name, tokens) {
    registry.set(name, tokens);
  },
  get(name) {
    return registry.get(name) || null;
  },
  names() {
    return [...registry.keys()];
  },
};

/** Resolve a theme by name; unknown names fall back to the light tokens. */
export function resolveTheme(name) {
  return registry.get(name) || SISMAYA_LIGHT;
}
