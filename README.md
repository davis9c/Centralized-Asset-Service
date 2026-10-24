# Centralized Asset Service (CAS)

MVP API CAS dalam Go, dikemas sebagai Docker Compose dengan PostgreSQL dan volume konten privat. Implementasi ini mengikuti dokumen `plan/` dan sengaja belum menerapkan deduplikasi.

## Menjalankan dengan Docker

1. Salin `.env.example` menjadi `.env` dan ganti seluruh nilai contoh dengan secret acak.
2. Jalankan `docker compose up --build -d`.
3. Periksa `GET http://localhost:8080/healthz`.

Tidak ada proses build atau server yang dijalankan selama penulisan ini. Jangan gunakan kredensial contoh di lingkungan nyata.

## Autentikasi

- API aplikasi menggunakan `X-Application-Key`. Kunci disimpan sebagai SHA-256, bukan plaintext.
- `CAS_BOOTSTRAP_APP_ID` dan `CAS_BOOTSTRAP_APP_KEY` membuat satu aplikasi pertama hanya jika ID belum terdaftar. Kunci minimal 32 karakter; perubahan env tidak merotasi kunci yang sudah ada.
- Halaman admin menerima username/password UserGate dan mengirimkannya ke endpoint login melalui server CAS. API key UserGate hanya dikirim dari server ke server. UserGate access token dipakai oleh UI selama tab aktif; password tidak disimpan.
- Pada login sukses pertama, CAS mencatat user tersebut sebagai `SUPER_ADMIN` lokal jika belum ada user CAS. Sesudah setup awal, user baru dikelola dan ditambahkan di UserGate. Saat user UserGate yang belum dikenal mencoba login ke CAS, CAS mencatatnya secara lokal dengan role kosong/nonaktif; `SUPER_ADMIN` kemudian menetapkan role. Role UserGate tidak menggantikan role lokal CAS.
- API key UserGate tidak boleh dikirim ke browser.

Halaman login terpisah tersedia di `GET /login/`. Akses `/admin/` tanpa sesi akan dialihkan ke halaman login; setelah login berhasil, browser kembali ke dashboard. Sesi disimpan pada `sessionStorage` tab browser dan dihapus saat logout. `SUPER_ADMIN` dapat mengelola user lokal dan aplikasi; `ADMIN` dapat mengelola aplikasi; `AUDITOR` hanya dapat melihat aplikasi, aset, serta audit. User lokal dengan role kosong tetap tersimpan tetapi nonaktif dan ditolak saat login dengan pesan untuk menghubungi administrator. `SUPER_ADMIN` mengaktifkan user dengan menetapkan salah satu role. User ID harus diambil dari UserGate. Dashboard menyediakan unduhan CAS Tester dan **Panduan Integrasi (.md)** yang berisi kontrak API dan instruksi untuk AI coding agent; panduan dapat diunggah bersama repository aplikasi target agar agent mengimplementasikan integrasi. CORS untuk file lokal dibatasi pada origin `null` (`CAS_TESTER_ORIGIN=null`).

## Endpoint MVP

Semua endpoint API di bawah memakai JSON response dengan `status`, `message`, dan/atau `data`.

| Metode | Endpoint | Fungsi |
|---|---|---|
| POST | `/api/v1/admin/login` | Login username/password melalui UserGate; login CAS pertama mendapat peran `SUPER_ADMIN` |
| GET | `/api/v1/admin/local-users` | Daftar user yang dikenali CAS; user baru tersinkron otomatis saat login UserGate pertama kali (khusus `SUPER_ADMIN`) |
| PATCH | `/api/v1/admin/local-users/{user_id}` | Ubah role user lokal (khusus `SUPER_ADMIN`); selalu harus tersisa satu `SUPER_ADMIN` |
| GET | `/api/v1/applications/me` | Memvalidasi application key dan menampilkan identitas aplikasi |
| POST | `/api/v1/assets` | Upload multipart (`file`, opsional `metadata` berupa objek JSON) |
| GET | `/api/v1/assets` | Daftar aset milik aplikasi; mendukung `limit`, `offset`, `q`, dan `status` |
| GET | `/api/v1/assets/{id}` | Metadata aset, berdasarkan hak `metadata.read` |
| GET/HEAD | `/api/v1/assets/{id}/content` | Download/stream dengan dukungan HTTP Range |
| DELETE | `/api/v1/assets/{id}` | Meminta penghapusan; tidak menghapus konten fisik |
| POST | `/api/v1/assets/{id}/cancel-deletion` | Membatalkan sebelum purge dimulai |
| GET/POST/DELETE | `/api/v1/assets/{id}/references` | Membaca, menambahkan, atau menonaktifkan referensi |
| GET/POST/DELETE | `/api/v1/assets/{id}/grants` | Membaca, memberi atau mencabut izin |
| GET/POST | `/api/v1/admin/applications` | Daftar dan registrasi aplikasi |
| PATCH | `/api/v1/admin/applications/{id}` | Status `ACTIVE`, `DISABLED`, atau `RETIRED` |
| GET/POST | `/api/v1/admin/applications/{id}/keys` | Daftar dan buat API key dengan label, lingkungan, izin, dan masa aktif |
| DELETE | `/api/v1/admin/applications/{id}/keys/{key_id}` | Cabut satu API key tanpa mencabut key lain |
| PATCH | `/api/v1/admin/applications/{id}/keys/{key_id}` | Ubah permission, batas usage, atau tambah masa aktif key |
| GET | `/api/v1/admin/assets` | Explorer metadata terbatas (maksimum 200) |
| GET | `/api/v1/admin/audit` | Audit terbaru (maksimum 200) |

Untuk mencabut grant: `DELETE /api/v1/assets/{id}/grants?grant_id=...`. Untuk menonaktifkan reference milik aplikasi pemanggil: `DELETE /api/v1/assets/{id}/references?reference_id=...`.

Actions grant yang tersedia: `metadata.read`, `content.read`, `reference.read`, `reference.create`. Pemilik mendapat akses ke asetnya. Aplikasi penerima memerlukan grant aktif; saat aset menunggu penghapusan, akses penerima hanya berjalan jika ia juga memiliki reference aktif. Tambahkan `?inline=true` pada endpoint content untuk preview gambar, audio, video, dan PDF yang telah diotorisasi.

Setiap aplikasi dapat memiliki banyak API key. Tiap key memiliki label, lingkungan (`DEV`, `TEST`, atau `PRODUCTION`), permission scope, waktu terakhir dipakai, dan masa aktif 7/30/90/180/365 hari atau tanpa kedaluwarsa. Usage dihitung kumulatif untuk setiap request API yang berhasil mengautentikasi key; batas dapat dibiarkan tanpa batas atau ditentukan saat membuat key. Admin dapat mengubah batas menjadi angka baru atau tanpa batas. Request ditolak setelah jumlah usage mencapai batas. Masa aktif key berjangka dapat ditambah 7/30/90/180/365 hari; jika sudah kedaluwarsa, perpanjangan dihitung dari waktu sekarang. Secret dibuat acak kriptografis, hanya hash SHA-256 disimpan, nilai lengkap hanya ditampilkan sekali, dan key dapat dicabut satu per satu. Scope yang tersedia: `application.read`, `assets.read`, `assets.upload`, `assets.delete`, `references.read`, `references.write`, `grants.manage`. Key lama dimigrasikan sebagai key `Legacy` dengan akses penuh agar tetap berfungsi; buat pengganti yang lebih terbatas lalu cabut legacy key melalui manajemen key.

CAS Tester menjadi workspace aset untuk application key yang aktif: menampilkan daftar aset milik aplikasi (pencarian, filter status, dan pagination), upload manual, unduh, lihat metadata, serta ajukan penghapusan logis. Workspace juga menampilkan contoh API cURL/JavaScript. Mode tema mengikuti sistem secara default dan dapat diganti ke terang/gelap. Tester integrasi tetap menjalankan health check dan validasi key, mendukung file pilihan atau file contoh, lalu memeriksa upload, metadata, ukuran, checksum SHA-256, serta download. Laporan JSON tidak menyertakan application key. Penghapusan logis tidak menghapus file fisik karena purge fisik belum tersedia.

## Batas dan keputusan implementasi awal

- Upload maksimal default 50 MiB, dapat diubah lewat `CAS_MAX_UPLOAD_BYTES`.
- Metadata khusus berupa objek JSON maksimal 64 KiB.
- File disimpan pada volume lokal dan MIME dideteksi dari konten; nama file tidak menjadi storage path.
- Belum ada malware scanner/quarantine, lifecycle worker/purge, retensi/legal hold, transfer kepemilikan, UI admin, rate limit, quota, backup/recovery, serta kebijakan audit retensi. Karena purge worker belum tersedia, permintaan penghapusan hanya mengubah status dan file tetap disimpan.
- Tidak ada deduplikasi. Database dan storage perlu backup bersama dengan prosedur konsistensi yang dirancang sebelum produksi.
- UserGate API Reference tidak mendefinisikan endpoint administrasi aplikasi CAS; registrasi dan status aplikasi karena itu merupakan endpoint internal CAS yang baru.

## Keamanan operasional

Jangan expose PostgreSQL ke internet. Tempatkan CAS di belakang TLS reverse proxy, batasi akses jaringan, atur backup volume dan database, lalu rotasi secret melalui prosedur terencana. CAS tidak menulis token, isi file, maupun application key ke log.
