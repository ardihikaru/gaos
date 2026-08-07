/* Landing page — tema dan reveal. Satu-satunya skrip di halaman ini.
 *
 * DIMUAT SECARA BLOCKING DI <head>, bukan dengan defer, dan bukan sebagai
 * skrip inline. Blocking karena data-theme harus sudah tertulis sebelum
 * paint pertama, kalau tidak halaman berkedip putih dulu di mode gelap.
 * Eksternal karena index.html mengunci CSP ke 'self': skrip inline butuh
 * 'unsafe-inline' atau hash yang basi setiap kali file disunting.
 *
 * Klasik, bukan modul, dan tanpa 'use strict' — sama seperti app-pwa/js. */

var GAOS_THEME_KEY = 'gaos.landing.theme';

(function () {
  var root = document.documentElement;
  root.classList.add('js');            // membuka gaya reveal di 01-tokens.css
  var saved = null;
  try { saved = localStorage.getItem(GAOS_THEME_KEY) } catch (e) { }
  if (saved === 'dark' || saved === 'light') root.dataset.theme = saved;
  // Tanpa pilihan tersimpan data-theme sengaja dibiarkan kosong, supaya
  // media query prefers-color-scheme yang menentukan — termasuk kalau
  // preferensi sistem berubah selagi halaman terbuka.
})();

function gaosToggleTheme() {
  var root = document.documentElement;
  var dark = root.dataset.theme
    ? root.dataset.theme === 'dark'
    : matchMedia('(prefers-color-scheme: dark)').matches;
  var next = dark ? 'light' : 'dark';
  root.dataset.theme = next;
  try { localStorage.setItem(GAOS_THEME_KEY, next) } catch (e) { }
  var btn = document.querySelector('.theme');
  if (btn) btn.setAttribute('aria-label', next === 'dark' ? 'Ganti ke mode terang' : 'Ganti ke mode gelap');
}

document.addEventListener('DOMContentLoaded', function () {
  var y = document.getElementById('tahun');
  if (y) y.textContent = new Date().getFullYear();

  // Diikat di sini, BUKAN lewat onclick= seperti di app-pwa. Halaman ini
  // hanya punya satu kontrol, jadi CSP-nya bisa tetap `script-src 'self'`
  // tanpa 'unsafe-inline' — dan satu handler tidak sepadan dengan itu.
  var btn = document.querySelector('.theme');
  if (btn) btn.addEventListener('click', gaosToggleTheme);

  var targets = document.querySelectorAll('.rv');
  if (!('IntersectionObserver' in window)) {
    // Tanpa observer semuanya langsung terlihat. Konten tidak boleh
    // tergantung pada animasi yang gagal jalan.
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
});
