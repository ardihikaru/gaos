/* GAOS landing page — tema, reveal, dan putaran Board. Satu-satunya skrip
 * di halaman ini.
 *
 * DIMUAT SECARA BLOCKING DI <head>, bukan dengan defer, dan bukan sebagai
 * skrip inline. Blocking karena data-theme harus sudah tertulis sebelum
 * paint pertama, kalau tidak halaman berkedip terang dulu di mode gelap.
 * Eksternal karena index.html mengunci CSP ke 'self': skrip inline butuh
 * 'unsafe-inline' atau hash yang basi setiap kali file disunting.
 *
 * Klasik, bukan modul, dan tanpa 'use strict'. */

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

  // Diikat di sini, bukan lewat onclick=, supaya CSP-nya tetap
  // `script-src 'self'` tanpa 'unsafe-inline'.
  var btn = document.querySelector('.theme');
  if (btn) btn.addEventListener('click', gaosToggleTheme);

  // Indeks jeda: urutan .rv di antara saudara-saudaranya, 50ms per langkah
  // (--i di 01-tokens.css). Dibatasi 5, supaya kelompok sebesar apa pun
  // selesai muncul dalam 300ms + 250ms. Lewat CSSOM, bukan atribut style,
  // jadi CSP `style-src 'self'` tidak tersentuh.
  var targets = document.querySelectorAll('.rv');
  targets.forEach(function (el) {
    var i = 0, s = el.previousElementSibling;
    while (s) { if (s.classList.contains('rv')) i++; s = s.previousElementSibling }
    if (i) el.style.setProperty('--i', Math.min(i, 5));
  });

  var board = document.querySelector('.board');

  if (!('IntersectionObserver' in window)) {
    // Tanpa observer semuanya langsung terlihat, dan Board tetap diam.
    // Konten tidak boleh tergantung pada animasi yang gagal jalan.
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

  // Putaran Board hanya berjalan selagi Board terlihat. Di luar layar
  // kelasnya dilepas dan animasinya dijeda (animation-play-state).
  if (board) {
    new IntersectionObserver(function (entries) {
      entries.forEach(function (en) { board.classList.toggle('is-live', en.isIntersecting) });
    }, { threshold: .3 }).observe(board);
  }
});
