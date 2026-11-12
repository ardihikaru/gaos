Repository ini adalah platform yang menjalankan agen, dan selama ini ia menjalankannya tanpa pernah bertanya apakah rencananya bagus. Setiap task keluar dari GAOS sebagai sebuah brief — catatan produk, kriteria penerimaan, aturan yang berlaku — dan kembali sebagai blok handoff. Di antara kedua titik itu tidak pernah ada momen ketika seseorang yang tidak berkepentingan dengan percakapan itu membaca rancangan dan berkata, "seharusnya begini cara membangunnya".

Itu bukan keluhan terhadap agen-agennya. Itu keluhan terhadap bentuk loop-nya. GAOS punya Blueprint, rubrik readiness, egress pass, dan kontrak handoff — dan keempatnya bekerja pada pekerjaan yang **sudah ada**. Rubrik readiness menilai catatan produk. Egress pass menyaring paket. Kontrak handoff menilai balasan. Tidak pernah ada yang menilai sebuah *desain*.

Jadi saya melakukan apa yang contoh implementasinya minta. Saya membawa masuk Elephant-Goldfish Model milik Google, menulis dokumen desain untuk perubahan yang memperkenalkan metodenya, lalu meminta lima peninjau baru membaca dokumen itu saja dan menyerangnya.

Hasilnya tidak nyaman, dan tidak nyaman di tempat yang tidak saya perkirakan.

## Metodenya, dalam satu paragraf

Elephant-Goldfish Model itu bukan model dan bukan agen. Itu Markdown. Agen yang memegang konteks proyek menyusun spesifikasi; peninjau terpisah tanpa ingatan percakapan itu hanya menerima spesifikasi dan tidak yang lain, lalu berusaha meruntuhkannya. Konteks yang menyusun rancangan adalah konteks yang tidak bisa melihat titik buta rancangan itu.

Peninjau yang menemukan paling banyak di workspace saya sendiri bukan yang paling pintar. Yang menolak berhenti menanyakan apakah perubahannya lebih besar daripada cacatnya.

## Apa yang ditemukan para peninjau di workspace yang saya kira sudah saya kuasai

Saya masuk dengan ekspektasi akan menemukan satu atau dua hal. Saya menemukan sepuluh di workspace, dan lalu para peninjau membalik audit yang saya tulis tentang mereka dan menemukan empat lagi — di audit itu sendiri.

Temuan pertama membuat saya menulis ulang dokumen. Daftar perubahan saya punya tabel ringkasan dan judul per bagian, dan keduanya tidak saling cocok. Tabelnya menghitung file; judulnya menghitung entri bernomor. Sebuah direktori dihitung sebagai file di tabel, sementara paragraf di bawahnya bilang direktori tidak dihitung. Sebuah file yang saya masukkan ke "Modified" tidak ada. Dokumen itu menulis, dengan tebal, bahwa angkanya diturunkan dan bukan diklaim — dan angkanya sendiri adalah klaim. Tiga peninjau independen menemukan ini, masing-masing dari arah berbeda, dan itu sinyal terkuat yang bisa dihasilkan sebuah review.

Temuan kedua: bukti utama saya memakai dua dasar pengukuran sekaligus. Saya menulis bahwa workspace agen-nya "164 KB", lalu tiga baris kemudian menulis bahwa satu file "27.509 byte". Angka 164 KB itu hitungan blok dari `du`, yang membulatkan tiap file ke atas ke 4 KB; angka jujurnya 95.762 byte. Saya juga menulis bahwa sebuah skill vendored "memuat 20 sebutan teknologi keluarga React" — dan tidak satu pun kosakata kata itu menghasilkan sekaligus angka 20 dan angka 2 yang saya bandingkan. Kesimpulannya bertahan; angkanya tidak. Klaim yang tidak bisa diturunkan ulang bukan bukti, dan review itu menyatakannya dengan cara yang tidak bisa saya bantah.

Temuan ketiga tentang para peninjaunya sendiri. Probe isolasi yang saya rancang menanam penanda di file memori always-on proyek, lalu menyatakan penanda itu tidak muncul di laporan mana pun. Di bawah Claude Code — satu-satunya harness yang jadi sasaran desain — probe itu hampa secara konstruksi. Para peninjau membawa `omitClaudeMd: true`, jadi penandanya tinggal di file yang tidak pernah mereka muat; pemeriksaannya pasti hijau baik mekanismenya berfungsi maupun kuncinya diam-diam diabaikan. Yang lebih buruk, ia memeriksa satu-satunya file yang dijamin dikecualikan dan tidak satu pun file yang tidak. Probe yang tidak bisa gagal bukan probe. Sekarang ia punya run kendali: di situ eksklusinya sengaja dimatikan dan penanda **wajib** muncul.

Yang keempat milik saya. Rancangan itu bilang lapisan always-on akan menyusut karena aturannya pindah ke direktori rules berscope. Kalau begitu, tidak akan menyusut. Dokumentasi Claude Code sendiri menyatakan file rule tanpa frontmatter `paths` "dimuat saat start dengan prioritas yang sama dengan `.claude/CLAUDE.md`". Saya cuma memindahkan byte antara dua bagian dari lapisan always-on yang sama dan menyebutnya pengurangan. Filenya sekarang 50 baris, bukan 166, dan klaimnya jadi benar, bukan sekadar keinginan, karena aturannya membawa scope.

## Bloat-nya lebih kecil dari yang saya kira, dan lebih buruk dari yang saya kira

Workspace-nya 95.762 byte, yang memang tidak banyak. Tapi empat dari temuan itu sama sekali bukan soal ukuran.

Satu skill adalah yatim. Tidak ada — tidak ada agen, aturan, dokumen, atau skrip — yang menyebut namanya. Itu bukan sekadar tak terpakai: ia menggambarkan jalur deployment produksi yang ditolak oleh agen yang bertanggung jawab atas deployment produksi. Ia memberi tahu operator bahwa prod dibangun dari apa pun yang sedang di-check-out, sementara skripnya menolak deploy prod tanpa tag atau override eksplisit. Perintah usang di sebuah menu bukan masalah kosmetik ketika orang yang mengikutinya diberi tahu sesuatu yang salah tentang dari mana kodenya berasal.

Skill kedua adalah 27,5 KB panduan React, Sonner, dan Tailwind di repository yang file agennya sendiri berbunyi "plain JS only … no framework, no bundler, no npm". Dua skill yang tidak bergantung pada stack tetap. Keputusan penghapusan tidak sama sekali bergantung pada hitungan framework — ia bergantung pada stack — dan review yang memaksa saya mengatakannya dengan terang-terangan, bukan bersandar pada angka yang tidak bisa direproduksi.

Lalu validator menemukan sesuatu yang tidak disebut reviewer mana pun, dan bagian itulah yang selalu saya ingat kembali. Aturan proyek saya masih menunjuk empat file yang sudah dihapus bersama lapisan eksekusi command-line lama di v0.29.0: `deploy/kill-mid-task.sh`, `lease_test.go`, `reconcile_test.go`, dan `smoke_test.go`. Nama-nama itu berada di file yang dimuat setiap sesi. Aturan yang menunjuk file yang tak pernah dipindahkan adalah aturan yang tak pernah diikuti siapa pun.

## Gerbang yang harus memperoleh kepercayaannya

Saya menulis validator lebih dulu dan menjalankannya terhadap pohon pra-perubahan, di worktree bersih, di mana ia melaporkan **delapan belas kegagalan**: skill yatim, skill yang stack-nya tidak cocok, empat file terhapus yang masih disebut, dan setiap angka yang basi. Lalu saya menerapkan sapuannya dan ia melaporkan tidak ada.

Urutan itu penting. Validator yang tak pernah gagal bukan validator — itu skrip yang mencetak hijau. Angka yang ditemukannya adalah angka yang akan lolos diam-diam, dan lebih besar daripada yang akan saya temukan dengan membaca.

Ia juga menemukan dua hal tentang dirinya sendiri. Versi pertama menandai berkas sumbernya sendiri, karena skrip itu memuat awalan penanda yang sedang dicari. Dan ia menandai laporan review saya sendiri, karena laporan itu mengutip kosakata detektornya — kata "password" — sebagai prosa. Keduanya false positive atas teks saya sendiri, dan keduanya adalah hal yang harus benar oleh validator sebelum siapa pun mempercayainya: pemeriksaan yang menangis wolves atas dokumentasi penulisnya sendiri akan diabaikan.

## Tiga masalah yang tidak pernah dimiliki situs web

Di sinilah GAOS berhenti jadi situs web dan jadi platform, dan di sinilah gerbang desain ini menunjukkan nilainya.

**Blueprint dimuat di setiap turn.** GAOS membuat satu workspace per produk — sebuah `CLAUDE.md` dan sekumpulan berkas aturan — dan harness agen memuatnya ke konteks pada setiap turn, termasuk turn pertama dari sesi yang benar-benar baru. Jadi membuka sesi baru tidak sama dengan membuka sesi bersih. Peninjau tiba membawa pernyataan masalah produknya, 50 aturan domainnya, dan seluruh riwayat keputusannya, mau dia memintanya atau tidak.

Fakta tunggal itu merusak mekanisme yang jadi sandaran metode ini. Di sebuah situs web, "konteks baru" berarti konteks baru. Di sini artinya "berkas standing yang sama lagi".

**Retry melanjutkan sesinya dengan sengaja.** Decision log GAOS sendiri menyatakan retry melanjutkan sesi "supaya modelnya melihat balasan gagalnya sendiri dan review yang menolaknya". Goldfish justru butuh kebalikannya: tanpa balasan gagal, tanpa review sebelumnya, tanpa jejak asumsi yang memulai ini. kebijakan sesi platform dan kebutuhan isolasi metode ini menunjuk arah yang berlawanan, dan aturan platform menang kecuali ada yang ditulis untuk mengalahkannya.

**Satu turn hidup per workspace.** Aturan yang mencegah dua agen mengedit berkas yang sama juga berarti satu turn review dan satu turn implementasi tidak bisa tumpang tindih di dalam satu produk. Gerbangnya serial secara konstruksi, dan tiap review menempati satu-satunya slot produk itu.

Saya bisa saja membuat desain yang menutupi ketiga hal itu. Sebaliknya, separuh platform ditulis dan **dibangun**: dua keputusan, lima aturan bisnis, satu milestone dengan lima task, dan tujuh forward specification. Mekanisme yang membuat peninjau platform menjadi zero-context sudah diputuskan — task pertama dari lima itu membangunnya, bukan memutuskan — dan empat lainnya menunggu task itu.

Mekanisme itu bagian yang jujur, dan hasilnya tidak seperti yang saya perkirakan. Saya selama ini menganggap berkas standing itu adalah kebocorannya. ternyata bukan: mereka adalah rubriknya. Aturan domain dirender ke berkas aturan di dalam workspace, jadi peninjau yang tidak bisa melihatnya tidak bisa menilai sebuah desain terhadap aturan itu, dan workspace yang memegang rubrik tapi tidak Blueprint produk adalah, atas definisi, workspace yang sudah menyimpang. Memangkas berkas standing tidak pernah jadi salah satu opsinya. Itu pertanyaan yang salah, dan membaca kode yang merakit packet itulah yang menunjukkan itu pada saya.

## Yang tidak dilakukan metode ini

Metode ini tidak menemukan fakta. Setiap peninjau terikat pada dokumen yang diberikan, jadi klaim salah tentang repository akan selamat dari setiap peninjau dan mendarat di laporan seolah-olah benar. Dua klaim di draf pertama saya sendiri salah, dan saya memeriksanya dengan tangan. Konteks baru memberi Anda kemandirian dari asumsi penulis, bukan akses pada kebenaran.

Metode ini tidak menggantikan pengujian, dan tidak menggantikan manusia. Itu sepasang mata kedua tanpa ingatan — persis apa yang mata pertama tidak punya.

Ini berisik. Penulis proyek hulu memperkirakan sekitar tiga puluh persen dari saran Goldfish layak diperlakukan, dan putaran saya sendiri datang lebih tinggi dari itu. Dari lima puluh delapan temuan mentah di empat lens, empat puluh dua bertahan setelah deduplikasi, dan empat puluh satu dari itu diterima — satu dipertentangkan, dengan alasan, dan sisanya diterapkan. Rasio itu bukan sifat dari metodenya. Itu sifat dari apa yang sedang saya tinjau, yang punya cacat nyata di dalamnya. Yang kembali sebagai bisingan adalah kata-kata, sebuah anchor basi, dan seorang peninjau yang memperhatikan bahwa ringkasan saya sendiri salah.

Dan ini mahal. Lima sesi Opus paralel per desain, dan di repository ini tak ada yang membatasi pengeluaran itu — budget guard yang mengatur turn milik platform sendiri belum dibangun. Itu angka yang saya pilih untuk dipublikasikan, bukan disembunyikan, sebab gerbang yang biayanya tidak pernah diukur adalah gerbang yang tak ada yang bisa memutuskan tentangnya.

## Seperti apa repository ini sekarang

Lapisan always-on-nya seperempat ukuran sebelumnya, dan memuat empat hal: aturan bahwa sebuah test belum selesai sampai register mengatakannya, tiga kosakata tertutup, pemicu gerbang desain, dan ke mana membaca sisanya. Mekanikanya pindah ke berkas rule yang hanya dimuat saat kamu menyentuh sebuah test atau otomasi. Rubrik untuk review adalah aturan repository itu sendiri ditambah register-registernya, jadi seorang peninjau menilai desain terhadap aturan yang sama dengan yang harus dipenuhi implementer — termasuk aturan bahwa katalog event tertutup dan bahwa back office saat ini tidak mengautentikasi siapa pun.

Yang terakhir layak satu baris sendiri. Tindakan privileged satu-satunya dalam gerbang platform adalah membersihkan satu Blocker yang terbuka, dan itu melepas implementasi. Permukaan tempat itu terjadi adalah back office yang seluruh autentikasinya adalah sebuah cookie yang bisa didapat siapa pun dengan mengetik sesuatu yang mengandung `@`. Aturan yang berbunyi "hanya operator yang boleh membersihkannya" tidak dapat diimplementasikan sampai itu berubah, jadi aturan itu sekarang mengatakannya, dan milestone-nya membawa itu sebagai prasyarat yang memblokir, bukan sebagai asumsi.

Lima lens, dan pemisahan yang paling layak biayanya bukan jumlahnya tapi isolasi. Tiap lens adalah sesi zero-context yang terpisah, jadi satu peninjau yang menjalankan lima bagian akan menjadi satu sesi dan asumsi satu penulis yang diterapkan lima kali; dan sebuah temuan ditriage terhadap lens yang menghasilkannya, jadi lens yang tidak pernah ditanya tidak bisa menghasilkan satu.

## Pembaruan: pertanyaan platformnya sudah terjawab

Catatan ini dikirim dengan satu hal yang sengaja dibiarkan terbuka. Separuh platform dari gerbang ini — review *di dalam* GAOS, bukan yang saya jalankan di repository ini — membawa sebuah pertanyaan: bagaimana peninjau yang dijalankan platform menjadi benar-benar zero-context, dan apa yang boleh *berisi* berkas standing yang harus ada di dalamnya?

Sekarang sudah terjawab, dan jawabannya hampir kebalikan dari arah yang saya tuju.

**Batas isolasinya adalah packet, bukan workspace.** Packet milik peninjau membawa desain dan rubrik, dan tidak ada yang lain — tidak ada brief, tidak ada goal, tidak ada handoff, tidak ada riwayat kegagalan, tidak ada jawaban. Workspace-nya adalah workspace milik produk itu sendiri, memuat Blueprint yang dipublikasikan utuh, karena Blueprint itu *adalah* rubriknya dan versi yang dipangkas akan menyimpang.

**Yang sebenarnya harus ditahan adalah state sesi saya sendiri.** Packet dibekukan per desk, dan handoff dari tahap sebelumnya diwarisi tahap berikutnya di desk yang sama — dengan sengaja, supaya tahap-tahap sebuah workflow saling berantai. Review adalah sebuah tahap di desk itu. Tanpa perubahan, peninjau akan mewarisi handoff penulis, pelajaran kegagalan yang turun darinya, dan jawaban operator atas pertanyaan penulis: persis asumsi yang harus dibuang oleh gerbang ini. Aturan produk ikut dibawa; proses penulis tidak.

**Desainnya harus ikut bepergian.** GAOS tidak membaca folder apa pun, jadi desain yang tinggal di repository tidak akan pernah sampai ke peninjau. Desainnya adalah artefak milik GAOS, dibawa di dalam packet dan disaring saat keluar seperti yang lain. Temuan-temuan bepergian balik dengan cara yang sama, sebagai konteks, supaya implementer benar-benar melihat review yang menolaknya.

**Gerbangnya tidak butuh apa pun yang baru.** Itu adalah edge dependensi milik perencana itu sendiri dan status blocked yang sudah ada, jadi tidak ada kosa kata tertutup yang melebar — kosa kata status task dan kind artefak tetap tertutup, masing-masing cuma nambah satu nilai.

Separuh yang masih saya utang ada di catatan di atas: egress pass menyaring apa yang keluar, bukan apa yang kembali. Sebuah balasan disimpan apa adanya, jadi temuan yang mengutip rahasia sudah ada di store append-only pada saat pass keluar menolak pengiriman berikutnya. Backstop bukan obatnya, dan inbound screening adalah pekerjaan terpisah.

**Pembaruan, beberapa jam kemudian:** separuh platform sudah dibangun sekarang. v0.35.0 sudah keluar dengan task review, packet yang membawa desain dan rubrik — tidak ada yang lain — dan gerbang yang membuat sebuah implementasi menunggu review-nya. Dua hal yang tidak akan saya tebak. Gerbang itu perlu *dua* pintu yang ditutup, bukan satu: plan adalah input ke method publik, jadi plan yang sekadar Review-nya dihilangkan harus ditolak; dan rute dispatch milik operator sendiri harus menolak task yang review-nya belum selesai. Dan isolasi seorang peninjau hanya sekuat packet-nya — workspace tempat ia berjalan adalah workspace milik produk itu sendiri, berkas standing dan semuanya, karena berkas-berkas itu adalah rubrik yang harus dinilai seorang peninjau.

## Sumber

- [Elephant-Goldfish Model](https://github.com/google/elephant-goldfish-model) — plugin open-source Google untuk desain dan review berbasis spesifikasi
- [Elephants, goldfish, and the new golden age of software engineering](https://medium.com/@drensin/elephants-goldfish-and-the-new-golden-age-of-software-engineering-c33641a48874) — esai Dave Rensin, sumber angka ~30%
- [The design gate — GAOS](https://gaos.mfardiansyah.id/docs/architecture/design-gate.html) — perubahan ini, dalam dokumentasi platformnya sendiri

## Baca juga

- [Elephant-Goldfish Model: tutorial praktis, diuji pada agen saya sendiri](/id/blog/elephant-goldfish-model-tutorial-id) — putaran pertama, terhadap dokumen desain
- [Saya menjalankan review goldfish pada roster agen saya sendiri, dan ia menemukan 52 error yang tidak pernah ditangkap validator saya](/id/blog/elephant-goldfish-model-on-a-136-agent-roster-id) — metode yang sama, satu tingkat lebih atas, pada roster agen itu sendiri
