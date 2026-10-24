# Centralized Asset Service (CAS)

## Dokumen Spesifikasi Fungsional untuk Codex

**Status:** Rancangan fungsional --- siap dijadikan dasar analisis dan
perencanaan implementasi\
**Versi dokumen:** 1.0\
**Tanggal:** 24 Oktober 2026\
**Instruksi utama:** Dokumen ini menjelaskan kebutuhan dan perilaku
sistem. Jangan langsung menganggap pilihan teknologi, skema database,
framework, atau detail implementasi sudah ditetapkan.

------------------------------------------------------------------------

## 1. Instruksi untuk Codex

Gunakan dokumen ini sebagai sumber kebutuhan awal untuk proyek
Centralized Asset Service (CAS).

### Aturan kerja

1.  **Jangan langsung menulis atau mengubah kode.** Mulailah dengan
    memeriksa repositori, memahami struktur proyek dan teknologi yang
    sudah digunakan, lalu laporkan temuan dan rencana implementasi.
2.  Cocokkan rancangan dengan kondisi repositori. Jangan mengganti
    framework, database, atau infrastruktur yang sudah ada tanpa alasan
    dan persetujuan.
3.  Identifikasi kebutuhan yang belum diputuskan dan ajukan pertanyaan
    yang paling penting sebelum mengambil keputusan arsitektur yang
    berdampak besar.
4.  Pisahkan kebutuhan fungsional yang telah disepakati dari rekomendasi
    yang masih perlu persetujuan.
5.  Setelah rencana disetujui, kerjakan secara bertahap, dengan
    pengujian dan dokumentasi untuk setiap tahap.
6.  Jangan menambahkan fitur atau asumsi keamanan yang bertentangan
    dengan dokumen ini.
7.  Jangan menyimpan rahasia, token, kredensial, atau isi file ke log
    aplikasi.
8.  Gunakan nama, status, dan konsep dalam dokumen ini sebagai istilah
    domain awal. Usulkan perubahan istilah bila repositori memiliki
    konvensi yang lebih tepat.
9.  Jangan menganggap dokumen ini sebagai mandat untuk membangun seluruh
    fitur sekaligus. Usulkan tahapan dan dependensi terlebih dahulu.
10. Jika ada konflik antara kondisi repositori, persyaratan keamanan,
    dan dokumen ini, jelaskan konfliknya sebelum meneruskan.

### Tahap pertama yang diminta

Sebelum implementasi, berikan: - Ringkasan arsitektur dan struktur
repositori yang ditemukan. - Teknologi dan konvensi yang sudah
digunakan. - Pemetaan fitur CAS terhadap komponen yang sudah ada. -
Risiko keamanan dan pertanyaan yang belum terjawab. - Rencana
implementasi bertahap beserta pengujian. - Daftar keputusan yang
membutuhkan persetujuan manusia.

------------------------------------------------------------------------

## 2. Tujuan dan ruang lingkup

CAS adalah layanan penyimpanan aset/file terpusat yang digunakan oleh
banyak aplikasi yang dikembangkan secara terpisah. Aplikasi
mengintegrasikan CAS melalui API sehingga file tidak perlu disimpan dan
dikelola secara terpisah di setiap aplikasi.

Jenis aset yang dituju terutama file berukuran kecil hingga menengah,
termasuk: - Dokumen. - Gambar. - Video pendek. - Audio. - Jenis file
lain yang diizinkan oleh kebijakan CAS.

CAS juga menyediakan antarmuka web admin untuk pengelolaan operasional,
metadata, aplikasi terdaftar, akses, audit, dan pemantauan.

### Sasaran utama

-   Penyimpanan terpusat dan akses file melalui API.
-   Kepemilikan aset yang jelas.
-   Akses lintas aplikasi yang eksplisit dan dapat dicabut.
-   Metadata standar dan metadata khusus aplikasi.
-   Download, preview, dan streaming.
-   Penghapusan yang aman dan mempertimbangkan dependensi.
-   Audit dan pengelolaan aplikasi.
-   Efisiensi penyimpanan; deduplikasi merupakan tujuan desain, tetapi
    aturan dan implementasinya **belum ditetapkan**.

### Di luar keputusan saat ini

Belum diputuskan: - Bahasa pemrograman, framework, database, object
storage, dan infrastruktur. - Skema tabel atau model penyimpanan fisik
final. - Batas ukuran file dan daftar MIME type yang diizinkan. - Format
API dan versi endpoint final. - Kebijakan deduplikasi. - Detail
autentikasi teknis, selain prinsip autentikasi aplikasi dan otorisasi
per aset. - Target ketersediaan, RPO/RTO, dan kebijakan backup. -
Transformasi gambar, thumbnail, transcoding, dan versioning file. -
Kebijakan final pembatalan permintaan penghapusan yang diajukan
administrator.

------------------------------------------------------------------------

## 3. Konsep domain

### 3.1 Application

Aplikasi yang terdaftar dan dapat memanggil CAS. Setiap aplikasi
memiliki identitas stabil yang tidak bergantung pada nama tampilannya.

Aplikasi pemanggil harus terautentikasi. Mengetahui `application_id`
saja tidak cukup untuk membuktikan identitas.

Status aplikasi yang diusulkan: - `ACTIVE`: aplikasi dapat mengakses CAS
sesuai izin. - `DISABLED`: aplikasi dinonaktifkan; aksesnya diblokir. -
`RETIRED`: aplikasi dihentikan dari operasi normal, tetapi identitas
historisnya dipertahankan.

Status dan nama di atas merupakan rancangan domain awal yang harus
dicocokkan dengan arsitektur repositori.

### 3.2 Asset

Identitas logis suatu aset, dengan metadata, status, satu pemilik utama,
serta riwayat siklus hidup.

Setiap aset memiliki tepat satu pemilik utama. Pemilik awal ditetapkan
oleh CAS sebagai aplikasi terautentikasi yang berhasil mengunggah aset.

Identitas aset harus stabil dan tidak bergantung pada nama file, URL
akses, atau lokasi file fisik.

### 3.3 Stored Content

Konten file yang disimpan secara fisik. Konten fisik harus dibedakan
dari identitas logis aset.

Pemisahan ini menjaga kompatibilitas dengan tujuan deduplikasi pada masa
mendatang. Jangan menganggap kepemilikan aset sama dengan kepemilikan
konten fisik. Aturan deduplikasi ditunda dan tidak boleh diasumsikan
sudah disepakati.

### 3.4 Asset Reference

Relasi yang menyatakan bahwa aplikasi atau entitas bisnis menggunakan
atau merujuk suatu aset.

Referensi berbeda dari izin: - Izin menjawab apakah aplikasi boleh
melakukan suatu tindakan. - Referensi menjawab apakah aplikasi masih
memiliki ketergantungan penggunaan pada aset.

Aset dapat memiliki izin tanpa referensi aktif. Referensi aktif juga
tidak dengan sendirinya memberikan hak akses.

### 3.5 Access Grant

Catatan pemberian izin kepada aplikasi penerima untuk melakukan tindakan
tertentu pada aset tertentu.

Informasi yang disarankan: - Aset yang dituju. - Aplikasi penerima. -
Tindakan yang diizinkan. - Pemberi izin dan waktu pemberian. - Status
izin. - Waktu kedaluwarsa, jika digunakan. - Riwayat pencabutan.

Aplikasi penerima tidak boleh meneruskan izin secara otomatis.
Re-sharing memerlukan kebijakan eksplisit yang belum disetujui.

### 3.6 Audit Event

Catatan tindakan penting, termasuk upload, perubahan metadata, akses,
pemberian/pencabutan izin, penghapusan, pembatalan, perubahan status
aplikasi, dan tindakan administrator.

Audit dapat mencakup waktu, pelaku, aplikasi pemanggil, tindakan, asset
ID, hasil, correlation/request ID, dan alasan bila relevan. Jangan
merekam token, secret, atau isi file.

------------------------------------------------------------------------

## 4. Keputusan rancangan yang telah disepakati

### D-01 --- CAS adalah penyimpanan terpusat

Banyak aplikasi menggunakan CAS melalui API. CAS menjadi tempat terpusat
untuk metadata, kebijakan akses, dan pengelolaan konten file.

### D-02 --- Aplikasi pengunggah adalah pemilik utama

Aplikasi terautentikasi yang melakukan upload menjadi pemilik utama
aset. Satu aset memiliki tepat satu pemilik utama.

Pemilik, pengunggah, pemakai aset, pemberi izin, dan administrator
merupakan peran konseptual berbeda, meskipun beberapa dapat merujuk pada
entitas yang sama dalam suatu operasi.

### D-03 --- Akses lintas aplikasi membutuhkan izin

Pemilik atau pihak yang secara eksplisit berwenang dapat memberikan izin
sesuai kebijakan CAS. Aplikasi penerima hanya memperoleh hak yang
diberikan; tidak otomatis memperoleh hak pengelolaan pemilik atau hak
untuk membagikan ulang aset.

### D-04 --- Akses bersifat application-first dan user-aware bila diperlukan

CAS harus mengenali aplikasi pemanggil. Konteks pengguna dapat digunakan
ketika operasi membutuhkan identitas pengguna, atribusi, atau kebijakan
tingkat pengguna. Identitas pengguna yang hanya dikirim sebagai teks
bebas tidak boleh dipercaya tanpa validasi.

### D-05 --- Akses konten

Fitur inti yang diinginkan: - Upload melalui API. - Download. - Preview
untuk tipe file yang didukung. - Streaming media.

Akses privat harus memeriksa autentikasi dan otorisasi. Mengetahui asset
ID saja tidak memberikan akses.

### D-06 --- Metadata

CAS menyimpan metadata inti dan mendukung metadata khusus aplikasi.
Metadata bisnis tidak boleh dapat mengubah pemilik atau melewati kontrol
keamanan.

Metadata inti awal yang disarankan: - `asset_id` -
`owner_application_id` - `original_filename` - `mime_type` -
`size_bytes` - `checksum` - `created_at` - `created_by_application_id` -
`asset_status` - metadata khusus aplikasi

Nama atribut dan skema final belum ditetapkan.

### D-07 --- Web admin

Web admin digunakan untuk: - Dashboard operasional. - Penelusuran dan
pemeriksaan aset. - Pengelolaan aplikasi terdaftar. - Pengelolaan izin
dan referensi sesuai kewenangan. - Audit. - Pemantauan storage, error,
dan proses penghapusan.

### D-08 --- Penghapusan tertunda

Pemilik boleh meminta penghapusan, tetapi penghapusan fisik ditunda
sampai: - Tidak ada referensi aktif. - Kebijakan retensi terpenuhi. -
Tidak ada penahanan atau larangan lain yang berlaku. - Dependensi konten
fisik tidak menghalangi pembersihan. - Semua syarat lain yang ditetapkan
kebijakan CAS terpenuhi.

Permintaan penghapusan bukan perintah untuk langsung menghapus file
fisik.

### D-09 --- Akses selama penghapusan tertunda

Aplikasi yang sudah memiliki referensi aktif tetap dapat mengakses aset
sesuai izin yang telah berlaku, selama izin dan kebijakan keamanan masih
mengizinkan.

Saat aset memasuki penghapusan tertunda: - Referensi baru diblokir. -
Pemberian izin baru diblokir. - Perluasan izin yang ada diblokir. -
Pencabutan izin tetap diperbolehkan. - Penghapusan/nonaktifkan referensi
tetap diperbolehkan. - Akses lama tetap diperiksa terhadap status
aplikasi, status aset, izin, dan status referensi.

### D-10 --- Pembatalan penghapusan

Pemilik dapat membatalkan permintaan penghapusan selama pembersihan
fisik belum dimulai dan tidak ada kebijakan yang melarang pembatalan.

-   Pembatalan dan dimulainya pembersihan fisik harus tersinkronisasi
    sehingga tidak dapat sama-sama menang.
-   Setelah pembersihan fisik dimulai, pembatalan biasa ditolak.
    Pemulihan, jika tersedia, merupakan proses terpisah.
-   Pembatalan yang berhasil mengembalikan aset ke status aktif bila
    kebijakan mengizinkan.
-   Izin yang telah dicabut tidak dipulihkan otomatis.
-   Referensi lama yang masih valid tetap tercatat.
-   Riwayat penghapusan dan pembatalan dipertahankan.

### D-11 --- Aplikasi pemilik dinonaktifkan atau dihentikan

Penonaktifan atau penghentian aplikasi pemilik tidak menghapus aset atau
mengubah kepemilikan.

-   Akses aplikasi pemilik diblokir selama dinonaktifkan/dihentikan.
-   Akses aplikasi lain tetap mengikuti izin, status referensi, status
    aset, dan kebijakan.
-   Identitas historis aplikasi dipertahankan untuk kepemilikan dan
    audit.
-   Penonaktifan aplikasi tidak otomatis mengajukan penghapusan semua
    aset.

### D-12 --- Kewenangan administrator atas aset aplikasi nonaktif

Administrator yang berwenang dapat: - Meninjau aset. - Meninjau metadata
dan riwayat sesuai cakupan. - Memperbaiki metadata operasional yang
diizinkan. - Mengajukan penghapusan sesuai kebijakan.

Administrator tidak otomatis menjadi pemilik. Kewenangan tersebut tidak
otomatis memberi hak untuk: - Mengubah pemilik. - Memberikan izin kepada
aplikasi lain. - Mengunduh atau melihat isi file. - Melewati retensi,
referensi aktif, atau alur penghapusan.

Perubahan metadata administratif dan pengajuan penghapusan harus
diaudit.

### D-13 --- Deduplikasi

Efisiensi melalui deduplikasi adalah tujuan desain, tetapi aturan
deduplikasi sengaja ditunda. Jangan mengimplementasikan aturan
deduplikasi atau membuat asumsi terkait berbagi blob fisik sebelum
keputusan khusus dibuat.

------------------------------------------------------------------------

## 5. Spesifikasi fitur

### F-01 --- Upload API

CAS menerima file dari aplikasi terautentikasi.

Kebutuhan: - Validasi identitas dan status aplikasi. - Validasi ukuran
dan tipe file sesuai kebijakan yang kelak ditetapkan. - Validasi
metadata. - Penetapan pemilik dari identitas aplikasi terautentikasi,
bukan dari klaim pemilik bebas dalam request. - Pembuatan asset ID
stabil. - Respons berisi identitas aset dan status operasi. - Error yang
konsisten dan dapat ditindaklanjuti. - Pertimbangkan idempotency agar
retry tidak menghasilkan operasi tak sengaja yang berulang. - Tentukan
kebijakan pemindaian malware/quarantine sebelum produksi.

Belum ditetapkan: ukuran maksimum, upload banyak file, resumable upload,
dan status pemrosesan.

### F-02 --- Download, preview, dan streaming

-   Semua permintaan konten privat melewati otorisasi.
-   Preview tidak boleh melewati aturan akses konten.
-   Streaming harus mendukung mekanisme yang sesuai untuk media;
    dukungan HTTP Range perlu dipertimbangkan.
-   Jika URL sementara digunakan, tentukan masa berlaku, cakupan, cache,
    dan pencabutannya.
-   Jangan menganggap URL sementara otomatis tidak dapat digunakan
    setelah aplikasi dinonaktifkan; perilakunya harus dirancang
    eksplisit.

### F-03 --- Metadata

-   Simpan metadata inti CAS.
-   Dukung metadata khusus aplikasi.
-   Pisahkan metadata operasional, metadata bisnis, dan atribut
    keamanan/identitas.
-   Validasi tipe dan batas metadata.
-   Tentukan apakah metadata khusus menggunakan JSON bebas, skema per
    aplikasi, atau pendekatan hibrida.
-   Metadata khusus tidak boleh mengubah kepemilikan atau izin.

### F-04 --- Web admin

Area awal: - Dashboard penggunaan storage, aktivitas, error, dan
penggunaan per aplikasi. - Asset Explorer untuk mencari dan memfilter
aset. - Detail aset: metadata, pemilik, status, referensi, izin, dan
riwayat yang dapat dilihat oleh peran terkait. - Application Management:
registrasi, status, kredensial, dan izin aplikasi. - Audit Explorer. -
Monitoring proses penghapusan dan kegagalan.

Akses terhadap isi file harus memiliki izin terpisah dari akses
metadata.

### F-05 --- Sharing lintas aplikasi

-   Pemberian izin dilakukan oleh pemilik atau pihak yang secara
    eksplisit diberi kewenangan.
-   Izin terkait aset, aplikasi penerima, tindakan, dan status.
-   Pencabutan izin tersedia.
-   Re-sharing tidak diizinkan secara default.
-   Pemberian izin baru diblokir selama penghapusan tertunda.
-   Pencabutan izin tetap diperbolehkan selama penghapusan tertunda.
-   CAS tidak dapat menjamin penghapusan salinan file yang sudah diunduh
    ke luar CAS.

### F-06 --- Storage management

-   Pantau pemakaian storage total dan per aplikasi.
-   Sediakan informasi pertumbuhan, kegagalan, dan kapasitas.
-   Dukung kebijakan retensi dan pembersihan konten yang tidak lagi
    dibutuhkan.
-   Deduplikasi tetap ditunda sampai ada keputusan tersendiri.

### F-07 --- File security

-   Validasi file dan metadata.
-   Pertimbangkan quarantine dan malware scanning.
-   Jangan mempercayai nama file atau MIME type dari klien tanpa
    validasi.
-   Lindungi dari akses file tanpa izin.
-   Jangan mengekspos path fisik storage sebagai identitas aset publik.

### F-08 --- Asset lifecycle dan deletion

-   Permintaan penghapusan harus diautentikasi dan diotorisasi.
-   Penghapusan ditunda sampai semua referensi nonaktif dan kebijakan
    terpenuhi.
-   Catat pemohon terpisah dari pemilik.
-   Sediakan pembatalan sebelum pembersihan fisik dimulai jika kebijakan
    mengizinkan.
-   Sediakan penanganan kegagalan dan percobaan ulang yang aman.
-   Pertahankan audit sesuai retensi.
-   Pisahkan penghapusan logis aset dari pembersihan konten fisik.

### F-09 --- Audit trail

Catat operasi penting: - Upload dan hasilnya. - Perubahan metadata. -
Akses konten sesuai kebijakan audit. - Pemberian dan pencabutan izin. -
Perubahan referensi. - Permintaan, pembatalan, dan hasil penghapusan. -
Perubahan status aplikasi. - Tindakan administrator.

Jangan mencatat token, password, secret, atau konten file. Batasi akses
terhadap audit.

### F-10 --- Resilience dan recovery

Rancang backup, pemulihan, pemeriksaan integritas, dan pemulihan
kegagalan setelah kebutuhan ketersediaan, RPO, dan RTO disepakati.
Jangan mengklaim backup atau recovery tersedia sebelum benar-benar
diimplementasikan dan diuji.

### F-11 --- API integration

-   API konsisten dan terdokumentasi.
-   Versi API dan format error ditetapkan sebelum kontrak publik
    dibekukan.
-   Autentikasi aplikasi dan otorisasi aset wajib.
-   Pertimbangkan idempotency untuk operasi yang berisiko diulang.
-   Dokumentasikan limit, timeout, dan perilaku retry.

### F-12 --- Quotas dan monitoring

-   Statistik penggunaan per aplikasi.
-   Batas penggunaan jika kebijakan sudah ditetapkan.
-   Alert untuk pertumbuhan, error, dan kegagalan pembersihan.
-   Metrik proses penghapusan tertunda dan usia antrean.

### F-13 --- Ownership management

-   Satu pemilik utama per aset.
-   Pemilik awal adalah aplikasi pengunggah yang terautentikasi.
-   Nama aplikasi dapat berubah tanpa mengubah identitas pemilik.
-   Penonaktifan tidak mengubah kepemilikan.
-   Perubahan kepemilikan tidak boleh dilakukan melalui edit metadata
    biasa.
-   Prosedur transfer kepemilikan belum ditetapkan.

### F-14 --- Delegated managers

Model delegasi dapat mendukung pengelolaan metadata atau akses dalam
cakupan terbatas. Hak delegasi, transfer kepemilikan, dan penghapusan
perlu dipisahkan. Detail final perlu disepakati sebelum implementasi.

### F-15 --- Asset references

-   Membuat dan menonaktifkan referensi.
-   Menampilkan referensi aktif.
-   Membedakan referensi dari izin.
-   Menolak referensi baru selama penghapusan tertunda.
-   Menyediakan informasi dependensi untuk pemohon yang berwenang.
-   Jangan menghapus referensi otomatis hanya karena lama tidak
    digunakan.

### F-16 --- Access grants

-   Catat target, tindakan, pemberi, status, dan waktu.
-   Periksa autentikasi dan otorisasi pada setiap akses.
-   Tidak ada pewarisan atau re-sharing otomatis.
-   Pemberian izin baru dan perluasan izin diblokir selama penghapusan
    tertunda.
-   Pencabutan izin tetap diizinkan.

### F-17 --- Asset Dependency Map

Tampilkan aplikasi yang memiliki referensi aktif atau izin terhadap
aset. Bedakan keduanya dengan jelas. Informasi dibatasi sesuai
kewenangan.

### F-18 --- Application Lifecycle Management

Kelola status aplikasi, penonaktifan, penghentian, kredensial, dan
aksesnya tanpa menghapus aset secara otomatis.

### F-19 --- Asset Lifecycle Audit

Catat perjalanan aset, perubahan status, dependensi, permintaan
penghapusan, pembatalan, dan hasil pembersihan.

### F-20 --- Deletion Processor

Memproses aset yang memenuhi syarat, menangani kegagalan, dan
menjalankan ulang pekerjaan secara aman. Proses pembersihan harus
terlindung dari perlombaan dengan pembatalan.

### F-21 --- Retention Policy

Cegah penghapusan selama masa retensi atau penahanan yang berlaku.
Detail retensi harus dapat dikonfigurasi setelah kebijakannya
disepakati.

### F-22 --- Deletion Monitoring

Tampilkan antrean penghapusan, usia permintaan, penyebab tertahan,
kegagalan, dan hasil pemrosesan.

### F-23 --- Pending-deletion access policy

Selama penghapusan tertunda: - Akses aplikasi yang sudah memiliki
referensi aktif tetap dapat berjalan sesuai izin sebelumnya. - Referensi
baru dan izin baru ditolak. - Perluasan izin ditolak. - Pencabutan izin
dan penonaktifan referensi tetap diizinkan. - Pemeriksaan otorisasi
tetap berlaku pada setiap permintaan.

### F-24 --- Dependency resolution

Sediakan informasi tentang jumlah referensi aktif, aplikasi yang terkait
sesuai kewenangan, alasan penghapusan tertahan, dan tindakan yang
diperlukan. Ketidakaktifan berdasarkan waktu bukan bukti yang cukup
untuk menghapus referensi.

### F-25 --- Deletion cancellation

-   Pemilik dapat membatalkan sebelum pembersihan fisik dimulai jika
    kebijakan mengizinkan.
-   Pembatalan dan awal pembersihan harus tersinkronisasi.
-   Pembatalan biasa ditolak setelah pembersihan fisik dimulai.
-   Jika pembatalan berhasil, aset kembali aktif hanya jika kebijakan
    mengizinkan.
-   Izin yang sudah dicabut tidak dipulihkan otomatis.
-   Audit pembatalan wajib.

### F-26 --- Application lifecycle

Penonaktifan/penghentian aplikasi memblokir akses aplikasi tersebut,
tetapi mempertahankan aset, kepemilikan, dan audit. Aplikasi lain tetap
harus melewati pemeriksaan izin dan status referensi.

### F-27 --- Assets owned by inactive applications

Sediakan pencarian, peninjauan, pemeriksaan dependensi, pengelolaan
retensi, dan tindakan administratif yang diizinkan untuk aset aplikasi
nonaktif.

### F-28 --- Administrative asset management

Administrator yang berwenang dapat meninjau aset, melihat metadata/audit
sesuai cakupan, memperbaiki metadata operasional, dan mengajukan
penghapusan. Kewenangan tidak otomatis mencakup pengubahan pemilik,
pemberian izin, atau akses isi file.

### F-29 --- Metadata governance

Pisahkan metadata inti, metadata operasional, dan metadata bisnis
aplikasi. Batasi perubahan menurut peran, validasi nilai, dan catat
perubahan yang relevan.

### F-30 --- Administrative deletion request

Permintaan penghapusan oleh administrator menggunakan alur penghapusan
yang sama dengan pemilik. Catat pemohon dan pemilik secara terpisah.
Administrator tidak dapat melewati referensi aktif, retensi, atau proses
pembatalan.

------------------------------------------------------------------------

## 6. Status dan transisi aset

Status yang diusulkan:

-   `ACTIVE`
-   `DELETE_PENDING`
-   `DELETE_BLOCKED`
-   `PURGE_PENDING`
-   `PURGING`
-   `DELETED`
-   `PURGE_FAILED`

Makna: - `ACTIVE`: aset tersedia sesuai izin. - `DELETE_PENDING`:
penghapusan diminta dan sedang dievaluasi. - `DELETE_BLOCKED`:
penghapusan tertahan oleh dependensi atau kebijakan. - `PURGE_PENDING`:
semua syarat penghapusan terpenuhi dan pembersihan menunggu eksekusi. -
`PURGING`: pembersihan fisik telah dimulai. - `DELETED`: penghapusan
selesai sesuai definisi kebijakan CAS. - `PURGE_FAILED`: pembersihan
gagal dan membutuhkan penanganan.

Status aset terpisah dari status permintaan penghapusan, status
referensi, status izin, dan status aplikasi. Model final dapat
menggunakan entitas/status terpisah selama perilakunya tetap memenuhi
kebutuhan ini.

### Transisi penting

-   `ACTIVE` → `DELETE_PENDING` saat permintaan yang sah diterima.
-   `DELETE_PENDING` ↔ `DELETE_BLOCKED` sesuai hasil evaluasi
    kebijakan/dependensi.
-   Status tertunda → `PURGE_PENDING` ketika seluruh syarat terpenuhi.
-   `PURGE_PENDING` → `ACTIVE` jika pembatalan disetujui sebelum
    pembersihan dimulai.
-   `PURGE_PENDING` → `PURGING` saat pembersihan fisik benar-benar
    dimulai.
-   `PURGING` → `DELETED` jika berhasil.
-   `PURGING` → `PURGE_FAILED` jika gagal; penanganan ulang harus aman.

Jangan mengizinkan transisi yang mengakibatkan aset dilaporkan aktif
setelah konten fisiknya mulai dihapus tanpa mekanisme recovery yang
terdefinisi.

------------------------------------------------------------------------

## 7. Persyaratan keamanan lintas fitur

1.  **Autentikasi dan otorisasi berbeda.** Aplikasi terautentikasi belum
    tentu boleh mengakses setiap aset.
2.  **Default deny.** Akses ditolak jika identitas, izin, status, atau
    kebijakan tidak dapat diverifikasi.
3.  **Least privilege.** Pisahkan izin membaca metadata, membaca isi,
    mengunduh, streaming, mengelola izin, memperbaiki metadata, dan
    menghapus.
4.  **ID bukan otorisasi.** Asset ID tidak boleh menjadi satu-satunya
    syarat akses.
5.  **Validasi identitas aplikasi.** Jangan mempercayai application ID
    atau user ID bebas dari request.
6.  **Admin terpisah dari pemilik.** Hak administrator harus eksplisit
    dan diaudit.
7.  **Audit aman.** Jangan mencatat rahasia atau isi file.
8.  **URL sementara.** Jika digunakan, masa berlaku, cakupan, cache, dan
    pencabutan harus ditentukan.
9.  **Race conditions.** Upload, perubahan izin, pembuatan referensi,
    pembatalan, dan pembersihan harus konsisten terhadap perubahan
    status.
10. **Penghapusan eksternal.** CAS tidak dapat menjamin penghapusan
    salinan yang telah diunduh di luar layanan.
11. **File security.** Nama file dan MIME type dari klien tidak boleh
    dipercaya tanpa validasi.
12. **Retensi dan legal hold.** Harus dapat menghalangi penghapusan dan
    pembatalan jika kebijakan menyatakan demikian.

------------------------------------------------------------------------

## 8. Persyaratan nonfungsional

Detail target belum ditetapkan, tetapi desain dan rencana implementasi
harus membahas:

-   Keamanan kredensial aplikasi dan rotasinya.
-   Konsistensi otorisasi pada semua jalur akses.
-   Integritas konten dan metadata.
-   Penanganan kegagalan upload dan pembersihan.
-   Idempotency dan retry.
-   Observabilitas, metrik, dan correlation ID.
-   Backup, pemulihan, RPO, dan RTO.
-   Kinerja untuk upload, download, preview, dan streaming.
-   Kuota dan pembatasan penggunaan.
-   Retensi audit dan privasi.
-   Migrasi skema dan kompatibilitas API.
-   Pengujian race condition untuk pembatalan versus pembersihan fisik.

Codex tidak boleh mengarang target numerik untuk availability, latency,
ukuran file, atau recovery tanpa persetujuan.

------------------------------------------------------------------------

## 9. Daftar keputusan terbuka

Hal berikut belum final dan harus ditandai sebelum implementasi yang
bergantung padanya.

### Prioritas tinggi

1.  Teknologi dan arsitektur yang akan digunakan, berdasarkan
    repositori.
2.  Mekanisme autentikasi aplikasi dan rotasi kredensial.
3.  Matriks izin terperinci untuk pemilik, aplikasi penerima, operator,
    dan administrator.
4.  Batas ukuran file dan tipe file yang diperbolehkan.
5.  Kebijakan akses isi file untuk administrator.
6.  Kebijakan retensi dan legal hold.
7.  Pembatalan permintaan penghapusan yang diajukan administrator:
    apakah pemohon sendiri boleh membatalkan, administrator setara boleh
    membatalkan, atau hanya administrator berperan lebih tinggi.
8.  Apakah metadata khusus aplikasi memakai JSON bebas, skema per
    aplikasi, atau pendekatan hibrida.

### Prioritas menengah

9.  Upload multi-file dan resumable upload.
10. Malware scanning dan quarantine.
11. API versioning dan kontrak error.
12. Signed URL versus proxy API untuk download/streaming.
13. Quota, rate limits, dan alert.
14. Backup, recovery, RPO/RTO.
15. Dukungan thumbnail, transformasi, transcoding, dan versi file.
16. Prosedur transfer kepemilikan.
17. Detail deduplikasi---tetap ditunda sampai dibuka kembali secara
    eksplisit.

------------------------------------------------------------------------

## 10. Rencana implementasi yang disarankan

Codex harus memvalidasi urutan ini terhadap repositori dan mengusulkan
revisi bila perlu.

### Tahap 0 --- Discovery dan desain

-   Periksa repositori dan konvensi.
-   Identifikasi arsitektur, auth, storage, database, testing, dan
    deployment yang sudah ada.
-   Petakan keputusan yang sudah final dan masih terbuka.
-   Hasil: laporan discovery, rancangan modul, daftar pertanyaan,
    rencana kerja.
-   Jangan menulis kode sebelum rencana disetujui.

### Tahap 1 --- Fondasi identitas dan aplikasi

-   Registrasi aplikasi.
-   Autentikasi aplikasi.
-   Status aplikasi.
-   Audit dasar.
-   Tes akses untuk aplikasi aktif/nonaktif.

### Tahap 2 --- Aset dan metadata

-   Identitas aset.
-   Kepemilikan aplikasi pengunggah.
-   Metadata inti dan metadata khusus sesuai kebijakan yang disetujui.
-   Upload dan retrieval metadata.
-   Validasi dan tes.

### Tahap 3 --- Penyimpanan dan akses konten

-   Integrasi penyimpanan berdasarkan teknologi yang disetujui.
-   Upload, download, preview, streaming sesuai lingkup.
-   Pemeriksaan otorisasi.
-   Integritas konten, error handling, dan tes.

### Tahap 4 --- Referensi dan izin lintas aplikasi

-   Asset references.
-   Access grants.
-   Pemberian dan pencabutan izin.
-   Penegakan default deny.
-   Tes isolasi antar aplikasi.

### Tahap 5 --- Lifecycle dan penghapusan

-   Status penghapusan.
-   Referensi aktif dan dependency checks.
-   Retensi/hold.
-   Pembatalan.
-   Pembersihan fisik yang aman dan dapat diulang.
-   Tes race condition dan kegagalan.

### Tahap 6 --- Admin dan audit

-   Asset Explorer.
-   Application Management.
-   Metadata governance.
-   Deletion monitoring.
-   Audit Explorer.
-   Kontrol akses administrator.

### Tahap 7 --- Hardening dan operasional

-   Kuota dan monitoring.
-   Backup/recovery sesuai target yang disetujui.
-   Security review.
-   Uji beban dan uji kegagalan.
-   Dokumentasi API, deployment, dan operasi.

------------------------------------------------------------------------

## 11. Definition of Done awal

Sebuah fitur belum dianggap selesai hanya karena endpoint atau antarmuka
tersedia. Fitur dinyatakan selesai setelah:

-   Persyaratan fungsional terpenuhi.
-   Autentikasi dan otorisasi diuji.
-   Kasus negatif dan kegagalan diuji.
-   Audit yang relevan tercatat.
-   Dokumentasi diperbarui.
-   Migrasi dan kompatibilitas ditangani bila diperlukan.
-   Tidak ada rahasia atau isi file dalam log.
-   Perilaku terhadap status aplikasi, referensi, izin, dan penghapusan
    konsisten.
-   Tes otomatis relevan lulus.

------------------------------------------------------------------------

## 12. Tugas pertama Codex

Mulai dengan **discovery repositori saja**, bukan implementasi.

Laporkan: 1. Apa yang sudah ada. 2. Bagaimana struktur dan
arsitekturnya. 3. Bagian mana yang dapat digunakan ulang. 4. Fitur CAS
mana yang belum ada. 5. Keputusan terbuka mana yang menghalangi desain.
6. Tahapan implementasi yang disarankan. 7. Risiko keamanan dan
pengujian yang dibutuhkan.

Setelah laporan itu, tunggu persetujuan sebelum menulis atau mengubah
kode.
