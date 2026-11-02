import en from './en.js';
import id from './id.js';

export const BUILTIN = { en, id };

const DAY = 86_400_000;
const localDate = (iso) => {
  if (iso instanceof Date) return iso;
  const [y, m, d] = String(iso || '').slice(0, 10).split('-').map(Number);
  return y && m && d ? new Date(y, m - 1, d) : new Date(iso);
};
const startOf = (date) => new Date(date.getFullYear(), date.getMonth(), date.getDate());

export function createI18n({ locale = 'en', catalogs = {}, now = () => new Date() } = {}) {
  let current = locale;
  const cache = new Map();
  const catalogFor = (set, code) => set?.[code] || null;
  const chain = () => [catalogFor(catalogs, current), catalogFor(BUILTIN, current), BUILTIN.en];
  const formatter = (ctor, opts = {}) => {
    const key = ctor + ':' + current + ':' + JSON.stringify(opts);
    if (!cache.has(key)) cache.set(key, new Intl[ctor](current, opts));
    return cache.get(key);
  };
  return {
    t(key, params = null) {
      let text = null;
      for (const cat of chain()) if (cat && key in cat) { text = cat[key]; break; }
      if (text == null) return key;
      return String(text).replace(/\{(\w+)\}/g, (_, k) => params && k in params ? params[k] : `{${k}}`);
    },
    date(iso) {
      return formatter('DateTimeFormat', { dateStyle: 'medium' }).format(localDate(iso));
    },
    number(n) {
      return formatter('NumberFormat').format(n);
    },
    relativeDays(iso) {
      const days = Math.round((startOf(localDate(iso)) - startOf(now())) / DAY);
      return formatter('RelativeTimeFormat', { numeric: 'auto' }).format(days, 'day');
    },
    setLocale(code) {
      current = code;
      cache.clear();
    },
    locale() {
      return current;
    },
    _cacheSize() {
      return cache.size;
    },
  };
}
