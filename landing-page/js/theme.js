/* GAOS landing page — theme, reveal, the board loop and the copy button.
 *
 * LOADED BLOCKING IN <head>, not deferred and not inline. Blocking because
 * data-theme has to be written before first paint, or a dark-mode visitor
 * sees the page flash light. External because index.html locks its CSP to
 * 'self': an inline script needs 'unsafe-inline' or a hash that goes stale
 * every time the file is edited.
 *
 * This is the script that adds the class `js` to <html>, and every start
 * state that hides something (a reveal, the steps line, the terminal lines)
 * is written under html.js. So the script that hides is the script that
 * shows: nothing here depends on motion.js having loaded.
 *
 * Classic script, not a module, and no 'use strict'. */

var GAOS_THEME_KEY = 'gaos.landing.theme';

(function () {
  var root = document.documentElement;
  root.classList.add('js');            // opens the reveal styles in 01-tokens.css
  var saved = null;
  try { saved = localStorage.getItem(GAOS_THEME_KEY) } catch (e) { }
  if (saved === 'dark' || saved === 'light') root.dataset.theme = saved;
  // With no saved choice data-theme is left empty on purpose, so the
  // prefers-color-scheme media query decides — including when the system
  // preference changes while the page is open.
})();

function gaosToggleTheme() {
  var root = document.documentElement;
  var dark = root.dataset.theme
    ? root.dataset.theme === 'dark'
    : matchMedia('(prefers-color-scheme: dark)').matches;
  var next = dark ? 'light' : 'dark';
  root.dataset.theme = next;
  try { localStorage.setItem(GAOS_THEME_KEY, next) } catch (e) { }
  // The button's label names both states, so it does not change here; it is
  // translated like every other string (js/i18n.js).
}

/* The quick-start commands, copied as three lines. The text is already in
 * the page and selectable; this is a convenience. When the clipboard is
 * missing or refuses, the button goes away rather than pretend. */
function gaosBindCopy() {
  var btn = document.querySelector('.copy');
  if (!btn) return;
  if (!navigator.clipboard || !navigator.clipboard.writeText) { btn.hidden = true; return }
  var timer = 0;
  var idle = btn.querySelector('.copy-idle'), done = btn.querySelector('.copy-done');
  var said = document.querySelector('.copy-said');
  // Both labels are in the button and one is transparent — which a screen
  // reader does not know. aria-hidden follows the state, so the button has
  // one name at a time, and the status line announces the copy.
  function show(state) {
    btn.dataset.state = state;
    if (idle) idle.setAttribute('aria-hidden', state === 'done' ? 'true' : 'false');
    if (done) done.setAttribute('aria-hidden', state === 'done' ? 'false' : 'true');
    if (said) said.textContent = state === 'done' && done ? done.textContent : '';
  }
  btn.addEventListener('click', function () {
    var lines = [];
    document.querySelectorAll('.term [data-cmd]').forEach(function (el) { lines.push(el.getAttribute('data-cmd')) });
    navigator.clipboard.writeText(lines.join('\n')).then(function () {
      show('done');
      clearTimeout(timer);
      timer = setTimeout(function () { show('idle') }, 1600);
    }, function () { btn.hidden = true });
  });
}

document.addEventListener('DOMContentLoaded', function () {
  var y = document.getElementById('year');
  if (y) y.textContent = new Date().getFullYear();

  // Bound here, not through onclick=, so the CSP stays `script-src 'self'`
  // with no 'unsafe-inline'.
  var btn = document.querySelector('.theme');
  if (btn) btn.addEventListener('click', gaosToggleTheme);
  gaosBindCopy();

  // Stagger index: an .rv element's position among its .rv siblings, 50ms a
  // step (--i in 01-tokens.css). Capped at 5 so a group of any size finishes
  // within 300ms + 250ms. Through CSSOM, not a style attribute, so the CSP's
  // `style-src 'self'` is untouched.
  var reveals = document.querySelectorAll('.rv');
  reveals.forEach(function (el) {
    var i = 0, s = el.previousElementSibling;
    while (s) { if (s.classList.contains('rv')) i++; s = s.previousElementSibling }
    if (i) el.style.setProperty('--i', Math.min(i, 5));
  });

  // [data-io] elements get the same .in class without the reveal's own fade:
  // the steps line and the terminal lines key their CSS off it.
  var targets = document.querySelectorAll('.rv, [data-io]');
  // .is-live marks a loop as allowed to run: the board's card and the
  // architecture's travelling dots are paused while off-screen.
  var loops = document.querySelectorAll('.board, [data-live]');

  if (!('IntersectionObserver' in window)) {
    // Without an observer everything is simply visible and the loops stay
    // still. Content must not depend on an animation that failed to start.
    targets.forEach(function (el) { el.classList.add('in') });
    return;
  }

  var io = new IntersectionObserver(function (entries) {
    entries.forEach(function (en) {
      if (!en.isIntersecting) return;
      en.target.classList.add('in');
      io.unobserve(en.target);
    });
  }, { rootMargin: '0px 0px -8% 0px', threshold: .08 });
  targets.forEach(function (el) { io.observe(el) });

  var live = new IntersectionObserver(function (entries) {
    entries.forEach(function (en) { en.target.classList.toggle('is-live', en.isIntersecting) });
  }, { threshold: .3 });
  loops.forEach(function (el) { live.observe(el) });
});
