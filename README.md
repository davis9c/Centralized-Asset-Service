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
- Pada login sukses pertama, CAS mencatat user tersebut sebagai `SUPER_ADMIN` lokal jika belum ada admin CAS. UserGate `SUPER_ADMIN` yang sudah ada juga tetap diterima.
- API key UserGate tidak boleh dikirim ke browser.

Dashboard manajemen aplikasi tersedia di `GET /admin/`. Dashboard dapat melihat aplikasi, mendaftarkan aplikasi baru, dan mengubah statusnya. Application key baru hanya tampil satu kali setelah dibuat.

## Endpoint MVP

Semua endpoint API di bawah memakai JSON response dengan `status`, `message`, dan/atau `data`.

| Metode | Endpoint | Fungsi |
|---|---|---|
| POST | `/api/v1/admin/login` | Login username/password melalui UserGate; login CAS pertama mendapat peran `SUPER_ADMIN` |
| POST | `/api/v1/assets` | Upload multipart (`file`, opsional `metadata` berupa objek JSON) |
| GET | `/api/v1/assets/{id}` | Metadata aset, berdasarkan hak `metadata.read` |
| GET/HEAD | `/api/v1/assets/{id}/content` | Download/stream dengan dukungan HTTP Range |
| DELETE | `/api/v1/assets/{id}` | Meminta penghapusan; tidak menghapus konten fisik |
| POST | `/api/v1/assets/{id}/cancel-deletion` | Membatalkan sebelum purge dimulai |
| GET/POST/DELETE | `/api/v1/assets/{id}/references` | Membaca, menambahkan, atau menonaktifkan referensi |
| GET/POST/DELETE | `/api/v1/assets/{id}/grants` | Membaca, memberi atau mencabut izin |
| GET/POST | `/api/v1/admin/applications` | Daftar dan registrasi aplikasi; key hanya dikembalikan saat dibuat |
| PATCH | `/api/v1/admin/applications/{id}` | Status `ACTIVE`, `DISABLED`, atau `RETIRED` |
| GET | `/api/v1/admin/assets` | Explorer metadata terbatas (maksimum 200) |
| GET | `/api/v1/admin/audit` | Audit terbaru (maksimum 200) |

Untuk mencabut grant: `DELETE /api/v1/assets/{id}/grants?grant_id=...`. Untuk menonaktifkan reference milik aplikasi pemanggil: `DELETE /api/v1/assets/{id}/references?reference_id=...`.

Actions grant yang tersedia: `metadata.read`, `content.read`, `reference.read`, `reference.create`. Pemilik mendapat akses ke asetnya. Aplikasi penerima memerlukan grant aktif; saat aset menunggu penghapusan, akses penerima hanya berjalan jika ia juga memiliki reference aktif. Tambahkan `?inline=true` pada endpoint content untuk preview gambar, audio, video, dan PDF yang telah diotorisasi.

## Batas dan keputusan implementasi awal

- Upload maksimal default 50 MiB, dapat diubah lewat `CAS_MAX_UPLOAD_BYTES`.
- Metadata khusus berupa objek JSON maksimal 64 KiB.
- File disimpan pada volume lokal dan MIME dideteksi dari konten; nama file tidak menjadi storage path.
- Belum ada malware scanner/quarantine, lifecycle worker/purge, retensi/legal hold, transfer kepemilikan, UI admin, rate limit, quota, backup/recovery, serta kebijakan audit retensi. Karena purge worker belum tersedia, permintaan penghapusan hanya mengubah status dan file tetap disimpan.
- Tidak ada deduplikasi. Database dan storage perlu backup bersama dengan prosedur konsistensi yang dirancang sebelum produksi.
- UserGate API Reference tidak mendefinisikan endpoint administrasi aplikasi CAS; registrasi dan status aplikasi karena itu merupakan endpoint internal CAS yang baru.

## Keamanan operasional

Jangan expose PostgreSQL ke internet. Tempatkan CAS di belakang TLS reverse proxy, batasi akses jaringan, atur backup volume dan database, lalu rotasi secret melalui prosedur terencana. CAS tidak menulis token, isi file, maupun application key ke log.
