/* GAOS landing page — the two languages.
 *
 * English is the text in index.html. This file holds the Indonesian string
 * for every key the page marks with data-i18n="key" or
 * data-i18n-attr="attribute:key", decides which language a visitor gets, and
 * swaps the text.
 *
 * LOADED BLOCKING IN <head>, after theme.js. The language is decided before
 * the body is parsed, so an Indonesian visitor never sees the English text
 * flash first.
 *
 * WHICH LANGUAGE. First match wins:
 *   1. ?lang=en or ?lang=id in the URL (and it is remembered);
 *   2. the choice saved by the switch in the header;
 *   3. the device's time zone is one of Indonesia's → Indonesian;
 *   4. English.
 * Nothing here touches the network — the page's CSP says connect-src 'none',
 * and the time zone is the only location signal a browser gives without a
 * call. A wrong guess costs the visitor one click, and step 2 remembers it.
 *
 * THE DICTIONARY IS ONE KEY PER LINE, and that is load-bearing:
 * deploy/check-stacks.sh reads the keys with grep and fails when the page and
 * this file disagree. Keep the  'key': 'text',  shape.
 *
 * Classic script, not a module, and no 'use strict'. */

var GAOS_LANG_KEY = 'gaos.landing.lang';
var GAOS_ID_ZONES = ['Asia/Jakarta', 'Asia/Pontianak', 'Asia/Makassar', 'Asia/Jayapura', 'Asia/Ujung_Pandang'];

var GAOS_ID = {
  'meta.title': 'GAOS — manajemen produk dan tugas untuk agen koding AI',
  'meta.desc': 'GAOS adalah sistem open source yang dijalankan di server sendiri. GAOS menyimpan kebutuhan, tugas, dan kebijakan produk dalam graph, lalu mengirim setiap tugas ke agen koding AI giliran demi giliran. Berlisensi Apache-2.0.',
  'a11y.skip': 'Langsung ke konten',
  'a11y.brand': 'GAOS, kembali ke awal halaman',

  'nav.label': 'Utama',
  'nav.features': 'Fitur',
  'nav.how': 'Cara kerja',
  'nav.quick': 'Mulai cepat',
  'nav.docs': 'Dokumentasi',
  'nav.github': 'GAOS di GitHub',
  'nav.lang': 'Bahasa',
  'nav.theme': 'Ganti tema terang atau gelap',
  'nav.open': 'Buka Mission Control',

  'hero.title': 'Manajemen produk dan tugas untuk agen koding AI',
  'hero.sub': 'GAOS menyimpan kebutuhan, user story, keputusan, dan kebijakan setiap produk dalam graph. Pekerjaan direncanakan sebagai tugas di papan kanban, lalu setiap tugas dikirim ke agen giliran demi giliran, di ruang kerja yang sudah memuat aturan produk tersebut. State disimpan di graph dan stream event yang bersifat append-only, sehingga orchestrator yang berhenti di tengah tugas dapat membangun ulang keadaannya dari keduanya.',
  'hero.start': 'Mulai',
  'hero.docs': 'Baca dokumentasi',

  'board.aria': 'Papan produk contoh, Laundry Membership, di Mission Control. Kolom Backlog, Ready, Working, Needs you, Review, dan Done, dalam dua lajur: story US-02, Harga per wilayah, dan lajur Plan untuk tugas di luar story. Tugas T-214 sedang dikerjakan dengan Blueprint v7 lalu berpindah ke Review. Tugas T-215 menunggu jawaban operator.',
  'board.lane': 'US-02 · Harga per wilayah',
  'board.t219': 'Laporan selisih harga per cabang',
  'board.t219m': 'menunggu T-214',
  'board.t216': 'Validasi tarif minimum di API',
  'board.t216m': 'siap dikirim',
  'board.t214': 'Migrasi harga ke layanan pricing',
  'board.run': 'A-3 · berjalan',
  'board.rev': 'A-3 · selesai, v7',
  'board.t215': 'Pembulatan harga: ke atas atau terdekat?',
  'board.t211': 'Skema tabel tarif wilayah',
  'board.t220': 'Uji beban endpoint harga',
  'board.t220m': 'belum siap',
  'board.t218': 'Siapkan data contoh cabang',
  'board.t218m': 'A-1 · berjalan',
  'board.t212': 'Dokumentasi API tarif',
  'board.t209': 'Kerangka layanan pricing',

  'proof.label': 'Angka proyek',
  'proof.types': 'tipe dalam skema graph',
  'proof.events': 'tipe event, dalam katalog tertutup',
  'proof.deps': 'dependensi Go pihak ketiga; kedua modul tidak memiliki go.sum',
  'proof.port': 'port yang dibuka oleh stack lokal',

  'feat.title': 'Yang sudah bisa dilakukan GAOS saat ini',
  'feat.lead': 'Fitur di bawah ini sudah diimplementasikan dan diuji. Pekerjaan yang direncanakan tetapi belum dibangun dicantumkan terpisah.',
  'feat.more': 'Selengkapnya',
  'feat.board.h': 'Papan kanban per produk',
  'feat.board.p': 'Setiap produk memiliki satu papan dengan sembilan kolom: Backlog, Ready, Working, Needs you, Review, dan Done, ditambah Blocked, Failed, dan Canceled. Kolom dihitung dari status tugas di graph; papan tidak menyimpan state sendiri. Memindahkan kartu hanyalah permintaan yang dapat ditolak oleh state machine tugas.',
  'feat.bp.h': 'Blueprint berversi',
  'feat.bp.p': 'Kebutuhan produk, kebijakan teknis dan koding, serta repositori acuan diterbitkan sebagai satu unit berversi dan disinkronkan ke ruang kerja produk sebagai aturan bagi agen. Pengiriman tugas ditolak jika isi ruang kerja tidak lagi sama dengan versi yang diterbitkan.',
  'feat.ready.h': 'Penilaian kesiapan',
  'feat.ready.p': 'Setiap catatan produk dinilai dengan skala 100 berdasarkan aturan yang dapat diperiksa mesin. Di bawah 45, planner menolak menyusun rencana dan mengembalikan daftar hal yang masih kurang.',
  'feat.gate.h': 'Gerbang desain',
  'feat.gate.p': 'Setiap story yang direncanakan diawali dengan tugas desain. Peninjau yang tidak memiliki akses ke konteks penulis membaca desain itu, dan tugas implementasi baru dimulai setelah desain lolos tinjauan. Desain yang tidak lolos dikembalikan ke penulisnya dan ditinjau ulang.',
  'feat.inbox.h': 'Kotak masuk operator',
  'feat.inbox.p': 'Ketika agen atau klien eksternal mengajukan pertanyaan, tugas menunggu di kolom Needs you sampai ada orang yang menjawab. Asal setiap jawaban ikut dicatat.',
  'feat.time.h': 'Linimasa dan pemutaran ulang',
  'feat.time.p': 'Perubahan dicatat sebagai event di stream yang bersifat append-only, dan linimasa memutar ulang riwayat produk dari stream itu. Jika orchestrator berhenti di tengah percobaan, keadaannya dibangun kembali dari graph.',
  'feat.api.h': 'API mesin dan sinkronisasi QA',
  'feat.api.p': 'Alat QA dan klien lain terhubung melalui REST API dengan bearer token. GAOS menyimpan katalog pengujian per produk, menyinkronkannya dengan alat QA, dan memanggil webhook saat operator menjawab pertanyaan klien. Tampilan Integrations menunjukkan status terkini setiap koneksi dan tidak pernah menampilkan secret.',

  'unbuilt.h': 'Belum dibangun',
  'unbuilt.lead': 'Tiga bagian ini sudah dispesifikasikan di dokumentasi, tetapi belum diimplementasikan.',
  'unbuilt.tag': 'Direncanakan',
  'unbuilt.score.h': 'Mesin penilaian hasil',
  'unbuilt.score.p': 'Menggabungkan lima dimensi berbobot dari hasil kerja agen menjadi satu skor, dengan batas pengaman yang menghentikan pekerjaan berapa pun totalnya.',
  'unbuilt.retry.h': 'Pengaman percobaan ulang',
  'unbuilt.retry.p': 'Berhenti dan meminta keputusan ketika percobaan gagal dengan cara yang sama dua kali.',
  'unbuilt.budget.h': 'Pengaman anggaran',
  'unbuilt.budget.p': 'Menolak pengiriman tugas setelah produk mencapai batas biayanya. Percobaan yang sedang berjalan tidak dihentikan.',

  'bp.title': 'Blueprint ditulis ke ruang kerja, dan setiap percobaan mencatat versinya',
  'bp.one': '<b>Satu unit berversi.</b> Kebutuhan bisnis, kebijakan teknis dan koding, serta repositori yang polanya diikuti agen.',
  'bp.two': '<b>Dimuat di setiap giliran.</b> Blueprint ditulis sebagai CLAUDE.md dan .claude/rules, sehingga agen memuatnya di setiap giliran, termasuk giliran yang dilanjutkan.',
  'bp.three': '<b>Versi yang sudah terbit tidak berubah.</b> Pengiriman ditolak sampai ruang kerja memuat versi yang terbit, dan setiap percobaan mencatat versi yang digunakannya.',
  'bp.aria': 'Tab Blueprint di Mission Control. Versi 7 sudah terbit dan disinkronkan ke ruang kerja; versi 6 dan 5 berada di bawahnya. Isinya CLAUDE.md dan delapan berkas aturan: kontrak GAOS, BRD, story, keputusan, kebijakan teknis, kebijakan koding, kebijakan keamanan, dan sumber pengetahuan. Percobaan A-3 untuk tugas T-214 terikat pada Blueprint v7.',
  'bp.v7': 'terbit · di ruang kerja',
  'bp.pub': 'terbit',
  'bp.draft': 'draf',
  'bp.changed': '2 kebijakan diubah',
  'bp.turn': 'satu giliran melalui ai-manager',

  'how.title': 'Dari catatan produk sampai percobaan yang tercatat, dalam empat langkah',
  'how.lead': 'Setiap langkah menulis ke graph. Label di atas setiap langkah menyebutkan catatan yang ditulisnya.',
  'how.s1.h': 'Deskripsikan produk',
  'how.s1.p': 'Catat masalah, tujuan, cakupan, aturan domain, serta user story beserta kriteria penerimaannya.',
  'how.s2.w': 'skor kesiapan',
  'how.s2.h': 'Lolos gerbang kesiapan',
  'how.s2.p': 'Catatan dinilai dengan skala 100. Planner menerima skor 45 atau lebih.',
  'how.s3.h': 'Rencanakan tugas',
  'how.s3.p': 'Planner mengubah story menjadi tugas. Setiap tugas mencakup satu hal dan memiliki minimal satu kriteria penerimaan; setiap kali ditulis, dependensinya diperiksa agar tidak membentuk siklus.',
  'how.s4.h': 'Jalankan setiap tugas melalui gateway',
  'how.s4.p': 'ai-manager menjalankan satu giliran di ruang kerja produk dengan Blueprint yang sudah terbit. Worker seperti ai-claude-cli mengerjakan tugasnya, dan GAOS mencatat percobaan beserta event-nya.',

  'arch.title': 'Tiga lapisan, masing-masing dengan satu tanggung jawab',
  'arch.gaos.p': 'Menyimpan catatan produk, tugas, dan Blueprint, serta menentukan apa yang dikirim. GAOS sendiri tidak menjalankan tool model.',
  'arch.aim.p': 'Gateway. Menjalankan setiap giliran model di ruang kerja yang disediakan untuk produk tersebut.',
  'arch.worker.h': 'Worker',
  'arch.worker.p': 'Agen seperti ai-claude-cli yang mengerjakan tugas di ruang kerja tersebut.',
  'arch.note1': 'Secara default, giliran model nonaktif. Atur <code>GAOS_MODEL_MODE=aimanager</code> dan arahkan <code>GAOS_AIM_URL</code> ke gateway ai-manager untuk mengaktifkannya.',
  'arch.note2': 'Biaya model dibayar langsung ke penyedia model Anda. GAOS tidak pernah berada di jalur penagihan itu.',

  'qs.title': 'Jalankan secara lokal',
  'qs.lead': 'Stack lokal membutuhkan Docker dan membuka satu port.',
  'qs.term': 'Perintah mulai cepat',
  'qs.copy': 'Salin',
  'qs.copied': 'Tersalin',
  'qs.then': 'Lalu buka <code>http://localhost:8083/</code>:',
  'qs.p1': 'halaman utama',
  'qs.p2': 'Mission Control: produk, Blueprint, papan, dan linimasa',
  'qs.p3': 'dokumentasi',
  'qs.p4': 'pemeriksaan kesehatan, termasuk koneksi ke database',
  'qs.n1': 'Tanpa database, dashboard berjalan dengan penyimpanan di memori. Atur <code>GAOS_STORE=arcadedb</code> untuk menggunakan ArcadeDB.',
  'qs.n2': 'Ubah port dengan <code>GAOS_EDGE_PORT</code>.',
  'qs.n3': 'Daftar lengkap pengaturan tersedia di dokumentasi.',

  'oss.title': 'Open source dan lengkap tanpa komponen tambahan',
  'oss.lic.h': 'Lisensi Apache-2.0',
  'oss.lic.p': 'Anda boleh menggunakan, mengubah, dan mendistribusikannya kembali. Tidak ada kunci lisensi, masa uji coba, atau fitur yang dikunci. Fork perlu memakai nama sendiri; alasannya dijelaskan di berkas NOTICE.',
  'oss.lic.a': 'Baca lisensi',
  'oss.con.h': 'Berkontribusi',
  'oss.con.p': 'Kontribusi memerlukan sign-off DCO (<code>git commit -s</code>); tidak ada CLA. Perubahan seukuran fitur dimulai dari dokumen desain yang dibaca peninjau independen sebelum kode ditulis.',
  'oss.con.a': 'Baca panduan kontribusi',
  'oss.stack.h': 'Dibangun dengan',
  'oss.stack.p': 'Pustaka standar Go dan htmx untuk dashboard, ArcadeDB untuk graph, dan Docker Compose untuk deployment. Modul Go-nya tidak memiliki dependensi pihak ketiga.',
  'oss.stack.a': 'Lihat kode sumber',

  'end.title': 'Mulai dari satu catatan produk',
  'end.p': 'Jalankan stack lokal, deskripsikan satu produk, lalu lihat hasil gerbang kesiapan.',
  'end.start': 'Mulai',
  'end.github': 'Lihat di GitHub',

  'foot.copy': 'Kontributor GAOS. Apache-2.0.',
  'foot.label': 'Kaki halaman',
  'foot.docs': 'Dokumentasi',
  'foot.log': 'Catatan rilis',
  'foot.lic': 'Lisensi',
  'foot.con': 'Berkontribusi',
  'foot.note': 'GAOS masih dikembangkan secara aktif. Catatan rilis memuat apa yang ditambahkan di setiap rilis.'
};

var gaosLang = 'en';
var gaosI18nNodes = null;   // [{el, key, attr, en}] — filled once, on DOMContentLoaded

/* Decide the language before the body exists. The whole block is one try:
 * whatever goes wrong, the wait class must not survive it. The listener is
 * registered BEFORE the class is added, so the class always has something
 * that will remove it; 01-tokens.css adds a script-free safety net on top. */
(function () {
  var root = document.documentElement;
  try {
    var lang = null;
    try {
      var q = new URLSearchParams(location.search).get('lang');
      if (q === 'en' || q === 'id') {
        lang = q;
        try { localStorage.setItem(GAOS_LANG_KEY, q) } catch (e) { }
      }
    } catch (e) { }
    if (!lang) {
      try {
        var saved = localStorage.getItem(GAOS_LANG_KEY);
        if (saved === 'en' || saved === 'id') lang = saved;
      } catch (e) { }
    }
    if (!lang) {
      try {
        var tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
        if (GAOS_ID_ZONES.indexOf(tz) !== -1) lang = 'id';
      } catch (e) { }
    }
    gaosLang = lang || 'en';
    document.addEventListener('DOMContentLoaded', gaosI18nInit);
    if (gaosLang === 'id') {
      root.lang = 'id';
      root.classList.add('i18n-wait');
    }
  } catch (e) {
    root.classList.remove('i18n-wait');
  }
})();

/* Record the English text of every keyed node once. English is never stored
 * twice: it is whatever index.html said when the page loaded. */
function gaosI18nCollect() {
  var nodes = [];
  document.querySelectorAll('[data-i18n]').forEach(function (el) {
    var isTitle = el.tagName === 'TITLE';
    nodes.push({ el: el, key: el.getAttribute('data-i18n'), attr: null, text: isTitle,
      en: isTitle ? el.textContent : el.innerHTML });
  });
  document.querySelectorAll('[data-i18n-attr]').forEach(function (el) {
    var spec = el.getAttribute('data-i18n-attr'), at = spec.indexOf(':');
    if (at < 1) return;
    var attr = spec.slice(0, at);
    nodes.push({ el: el, key: spec.slice(at + 1), attr: attr, en: el.getAttribute(attr) });
  });
  return nodes;
}

/* A key missing from the dictionary keeps its English text: the page never
 * shows a key name or an empty element. */
function gaosI18nApply(lang) {
  if (!gaosI18nNodes) gaosI18nNodes = gaosI18nCollect();
  gaosI18nNodes.forEach(function (n) {
    var s = lang === 'id' && Object.prototype.hasOwnProperty.call(GAOS_ID, n.key) ? GAOS_ID[n.key] : n.en;
    if (n.attr) n.el.setAttribute(n.attr, s);
    else if (n.text) n.el.textContent = s;
    else n.el.innerHTML = s;
  });
  document.documentElement.lang = lang;
  document.querySelectorAll('.lang [data-lang]').forEach(function (b) {
    b.setAttribute('aria-pressed', b.getAttribute('data-lang') === lang ? 'true' : 'false');
  });
  gaosLang = lang;
}

var gaosLangPending = null, gaosLangTimer = 0;

function gaosSetLang(lang) {
  if (lang !== 'en' && lang !== 'id') return;
  // Compared with the language on its way in, not the one still on screen:
  // two quick clicks must end on the second, not be dropped as "no change".
  if (lang === (gaosLangPending || gaosLang)) return;
  try { localStorage.setItem(GAOS_LANG_KEY, lang) } catch (e) { }
  // A ?lang= left in the URL would undo this click on the next reload.
  try {
    var u = new URL(location.href);
    if (u.searchParams.has('lang')) {
      u.searchParams.delete('lang');
      history.replaceState(null, '', u.pathname + u.search + u.hash);
    }
  } catch (e) { }
  var root = document.documentElement;
  var still = false;
  try { still = matchMedia('(prefers-reduced-motion: reduce)').matches } catch (e) { }
  if (still) { gaosI18nApply(lang); return }
  gaosLangPending = lang;
  clearTimeout(gaosLangTimer);
  // Out in 90ms, swap, back in 180ms (04-motion.css). The old text leaves
  // faster than the new text arrives, and the two are never both visible.
  root.classList.add('i18n-fade');
  gaosLangTimer = setTimeout(function () {
    gaosLangPending = null;
    try { gaosI18nApply(lang) } finally { root.classList.remove('i18n-fade') }
  }, 90);
}

function gaosI18nInit() {
  var root = document.documentElement;
  try {
    gaosI18nNodes = gaosI18nCollect();
    if (gaosLang === 'id') gaosI18nApply('id');
    else gaosI18nApply('en');
    document.querySelectorAll('.lang [data-lang]').forEach(function (b) {
      b.addEventListener('click', function () { gaosSetLang(b.getAttribute('data-lang')) });
    });
  } finally {
    root.classList.remove('i18n-wait');
  }
}
