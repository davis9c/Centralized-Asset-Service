# Panduan Integrasi Centralized Asset Service (CAS)

> Dokumen ini ditujukan untuk developer dan AI coding agent. Unggah file Markdown ini bersama konteks/repository aplikasi yang akan diintegrasikan, lalu minta agent mengikuti kontrak API di bawah dan menerapkan integrasi langsung ke pola arsitektur project tersebut.

## Instruksi untuk AI coding agent

Anda bertugas mengintegrasikan aplikasi ini dengan CAS. Sebelum mengubah kode:

1. Periksa framework, pola konfigurasi, autentikasi, penyimpanan file, UI, dan standar error handling yang sudah dipakai project.
2. Implementasikan client/service CAS di sisi server. Jangan menaruh `CAS_APPLICATION_KEY` di source code, bundle JavaScript publik, HTML, log, atau respons yang dapat dibaca browser.
3. Baca base URL dan application key dari environment/secret manager. Gunakan HTTPS untuk deployment.
4. Buat fitur yang relevan dengan kebutuhan aplikasi: upload, menyimpan `asset_id` sebagai referensi, mengambil metadata, dan mengunduh/menampilkan konten yang diizinkan.
5. Pertahankan nama file, metadata bisnis yang diperlukan, dan `asset_id` CAS pada model aplikasi. Jangan menganggap metadata CAS sebagai pengganti relasi bisnis aplikasi.
6. Tangani timeout, network failure, status HTTP, dan respons JSON sesuai panduan ini. Jangan mengulang operasi yang dapat mengubah data secara membabi buta.
7. Ikuti konvensi project untuk konfigurasi, validasi, UI, dan dokumentasi. Tambahkan contoh environment tanpa secret sungguhan.
8. Sebelum selesai, laporkan file yang diubah, alur integrasi, konfigurasi yang perlu diset, dan langkah verifikasi yang sesuai dengan kemampuan environment. Jangan menjalankan Docker jika environment developer tidak memilikinya.

Jika kebutuhan produk atau stack tidak jelas dari repository, tanyakan hal yang benar-benar menghalangi implementasi. Selain itu, buat keputusan implementasi yang konsisten dengan project.

## Konfigurasi

Konfigurasikan nilai berikut di server aplikasi:

```dotenv
CAS_BASE_URL=https://cas.example.com
CAS_APPLICATION_KEY=<application-key-dari-admin-CAS>
```

- `CAS_BASE_URL` adalah origin CAS, tanpa path API. Contoh: `https://cas.example.com`.
- `CAS_APPLICATION_KEY` adalah key aplikasi yang dibuat dari Dashboard CAS. Simpan sebagai secret server-side.
- Setiap request aplikasi terautentikasi mengirim header `X-Application-Key`.
- Jangan menggunakan application key untuk login admin. Login admin menggunakan UserGate dan merupakan alur terpisah.
- Gunakan koneksi HTTPS di luar development lokal.
- Batas upload default CAS adalah 50 MiB dan dapat diubah melalui `CAS_MAX_UPLOAD_BYTES` di server CAS. Batas reverse proxy dan server aplikasi pemanggil juga harus mengizinkan ukuran yang diperlukan.

## Autentikasi dan format umum

Header untuk endpoint aplikasi:

```http
X-Application-Key: <CAS_APPLICATION_KEY>
Accept: application/json
```

Tambahkan `Content-Type: application/json` untuk request JSON. Untuk upload multipart, biarkan HTTP client menetapkan `Content-Type` beserta boundary secara otomatis.

Respons JSON umumnya memakai bentuk berikut:

```json
{
  "status": true,
  "data": {}
}
```

Error memakai HTTP status yang sesuai dan bentuk seperti:

```json
{
  "status": false,
  "message": "unauthorized"
}
```

CAS mengirim `X-Request-ID` pada response untuk membantu diagnosis. Catat request ID bersama log error aplikasi, tetapi jangan catat application key, isi file, atau kredensial.

## Endpoint aplikasi

Semua endpoint di bagian ini menggunakan application key.

| Method | Path | Tujuan |
|---|---|---|
| GET | `/healthz` | Memeriksa CAS dapat dijangkau; tidak memvalidasi application key |
| GET | `/api/v1/applications/me` | Memvalidasi key dan memperoleh identitas aplikasi |
| POST | `/api/v1/assets` | Upload file dan metadata |
| GET | `/api/v1/assets?limit=25&offset=0&q=&status=` | Daftar aset milik aplikasi ini |
| GET | `/api/v1/assets/{asset_id}` | Membaca metadata aset yang diizinkan |
| GET / HEAD | `/api/v1/assets/{asset_id}/content` | Mengunduh/stream konten; mendukung HTTP Range |
| DELETE | `/api/v1/assets/{asset_id}` | Mengajukan penghapusan logis oleh pemilik |
| POST | `/api/v1/assets/{asset_id}/cancel-deletion` | Membatalkan penghapusan yang masih dapat dibatalkan oleh pemilik |
| GET / POST / DELETE | `/api/v1/assets/{asset_id}/references` | Membaca, membuat, atau menonaktifkan referensi |
| GET / POST / DELETE | `/api/v1/assets/{asset_id}/grants` | Membaca, memberi, atau mencabut grant oleh pemilik |

### Memvalidasi koneksi

```bash
curl --fail-with-body "$CAS_BASE_URL/api/v1/applications/me" \
  -H "X-Application-Key: $CAS_APPLICATION_KEY" \
  -H "Accept: application/json"
```

Respons `data` memuat `application_id`, `name`, dan `status`. Status aplikasi harus `ACTIVE`.

### Upload file

Kirim `multipart/form-data` dengan field `file` wajib dan `metadata` opsional. `metadata` harus berupa objek JSON, maksimum 64 KiB. File harus berukuran lebih dari nol dan tidak melebihi batas konfigurasi CAS.

```bash
curl --fail-with-body "$CAS_BASE_URL/api/v1/assets" \
  -H "X-Application-Key: $CAS_APPLICATION_KEY" \
  -F "file=@./invoice.pdf" \
  -F 'metadata={"source":"billing","record_id":"INV-123"}'
```

Contoh respons `201 Created`:

```json
{
  "status": true,
  "message": "asset created",
  "data": {
    "asset_id": "<uuid>",
    "owner_application_id": "<application-id>",
    "original_filename": "invoice.pdf",
    "mime_type": "application/pdf",
    "size_bytes": 12345,
    "checksum": "<sha256-hex>",
    "asset_status": "ACTIVE"
  }
}
```

Simpan `data.asset_id` di record bisnis aplikasi setelah upload berhasil. Jika penyimpanan record dan request CAS tidak berada pada transaksi yang sama, rancang penanganan kegagalan parsial/idempotensi di aplikasi pemanggil. CAS saat ini tidak menyediakan idempotency key.

### Daftar dan cari aset

```bash
curl --fail-with-body --get "$CAS_BASE_URL/api/v1/assets" \
  -H "X-Application-Key: $CAS_APPLICATION_KEY" \
  --data-urlencode "limit=25" \
  --data-urlencode "offset=0" \
  --data-urlencode "q=invoice" \
  --data-urlencode "status=ACTIVE"
```

Parameter opsional: `limit` 1–100 (default 25), `offset` 0–100000 (default 0), `q` pencarian nama file atau ID aset (maksimum 200 karakter), dan `status`. Respons memiliki `data` berupa array aset dan `pagination` berisi `total`, `limit`, serta `offset`. Endpoint hanya mengembalikan aset yang dimiliki application key tersebut.

### Membaca metadata dan mengunduh konten

```bash
curl --fail-with-body "$CAS_BASE_URL/api/v1/assets/<asset-id>" \
  -H "X-Application-Key: $CAS_APPLICATION_KEY"

curl --fail-with-body "$CAS_BASE_URL/api/v1/assets/<asset-id>/content" \
  -H "X-Application-Key: $CAS_APPLICATION_KEY" \
  -o invoice.pdf
```

Metadata mencakup `asset_id`, `owner_application_id`, `original_filename`, `mime_type`, `size_bytes`, `checksum`, `asset_status`, dan `metadata`. Endpoint konten mengembalikan byte file, bukan JSON. CAS mengirim `ETag` berbasis checksum dan `Content-Disposition: attachment`. Request Range didukung oleh server HTTP. Untuk preview inline gunakan `?inline=true`; inline hanya diaktifkan untuk gambar, audio, video, dan PDF.

Jika aplikasi lain mengakses aset, akses metadata/konten membutuhkan grant aktif yang sesuai. Saat penghapusan aset menunggu, aplikasi penerima juga harus memiliki reference aktif.

### Referensi dan grant

Reference menghubungkan aset dengan entitas di aplikasi pemanggil. Buat reference hanya setelah mendapatkan `asset_id`:

```http
POST /api/v1/assets/{asset_id}/references
X-Application-Key: <key>
Content-Type: application/json

{"entity_id":"INV-123"}
```

Pemilik dapat membaca dan menonaktifkan reference milik aplikasinya. Penonaktifan memakai `DELETE /api/v1/assets/{asset_id}/references?reference_id=<id>`. Aplikasi penerima memerlukan grant `reference.read` untuk membaca references; membuat reference atas aset milik aplikasi lain membutuhkan grant `reference.create`.

Pemilik memberi grant kepada application ID lain:

```http
POST /api/v1/assets/{asset_id}/grants
X-Application-Key: <owner-key>
Content-Type: application/json

{"application_id":"reporting-service","action":"content.read"}
```

Action yang didukung: `metadata.read`, `content.read`, `reference.read`, `reference.create`. Pemilik aset tidak membutuhkan grant untuk aset sendiri. Daftar grant tersedia melalui `GET` pada path yang sama dan grant dapat dicabut dengan `DELETE /api/v1/assets/{asset_id}/grants?grant_id=<id>`.

### Penghapusan logis

```bash
curl --fail-with-body -X DELETE \
  "$CAS_BASE_URL/api/v1/assets/<asset-id>" \
  -H "X-Application-Key: $CAS_APPLICATION_KEY"
```

Respons `202` memberi `asset_status` berupa `DELETE_PENDING` atau `DELETE_BLOCKED` jika masih ada reference aktif. `POST /api/v1/assets/{asset_id}/cancel-deletion` dapat memulihkan status menjadi `ACTIVE` sebelum purge. **Penghapusan logis tidak menghapus file fisik.** CAS versi ini belum memiliki purge worker; jangan menyampaikan kepada pengguna akhir bahwa file sudah dihapus permanen.

## Status HTTP dan penanganan error

| HTTP | Makna umum | Tindakan |
|---|---|---|
| 400 | Request tidak valid | Periksa format/path yang dikirim |
| 401 | Key tidak ada, salah, atau aplikasi tidak aktif | Periksa secret dan status aplikasi; jangan retry tanpa perubahan |
| 403 | Aplikasi tidak memiliki izin/grant | Periksa kepemilikan, action grant, dan reference |
| 404 | Aset/route tidak ditemukan | Periksa ID dan base URL |
| 405 | Method tidak didukung | Gunakan method yang tercantum di kontrak |
| 409 | State aset berubah/tidak mengizinkan operasi | Muat ulang metadata lalu tentukan aksi berikutnya |
| 413 | Upload melebihi batas | Kurangi ukuran atau koordinasikan perubahan limit server |
| 422 | Validasi parameter, file, atau metadata gagal | Perbaiki request sebelum mencoba kembali |
| 5xx | Kegagalan server/storage/database | Catat `X-Request-ID`; retry hanya untuk operasi yang aman dan dengan backoff |

Jangan mengulang upload secara otomatis setelah timeout karena server mungkin sudah berhasil menyimpan aset walau respons tidak sampai ke aplikasi. Periksa daftar aset atau desain reconciliation di aplikasi sebelum retry.

## Rekomendasi implementasi

- Buat wrapper client CAS terpusat dengan timeout yang wajar, bukan request tersebar di UI.
- Simpan key pada environment server/secret manager. Browser frontend sebaiknya memanggil backend aplikasi sendiri.
- Validasi ukuran file sebelum upload dan tampilkan progress bila stack mendukungnya.
- Simpan `asset_id`, nama file, MIME, ukuran, checksum, dan relasi entitas yang relevan sesuai kebutuhan aplikasi.
- Escape nama file saat ditampilkan dan jangan membangun filesystem path dari `original_filename`.
- Tangani download sebagai stream untuk file besar; jangan selalu memuat seluruh konten ke memori.
- Jangan menganggap `DELETE` sebagai physical purge. Tampilkan status yang sebenarnya.
- CORS CAS default mengizinkan origin `null` untuk file CAS Tester lokal. Integrasi server-to-server tidak membutuhkan CORS; bila browser memanggil CAS secara langsung, origin harus dikonfigurasi secara eksplisit dan key tidak boleh menjadi secret publik.

## Contoh client Go minimal

```go
type CASClient struct {
    BaseURL string
    Key     string
    HTTP    *http.Client
}

func (c *CASClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
    req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
    if err != nil {
        return nil, err
    }
    req.Header.Set("X-Application-Key", c.Key)
    req.Header.Set("Accept", "application/json")
    return req, nil
}
```

Untuk multipart Go, buat `multipart.Writer`, tambahkan part bernama `file` dan field teks `metadata`, lalu set request `Content-Type` dari `writer.FormDataContentType()`. Tetapkan `http.Client.Timeout` atau deadline context. Periksa status HTTP sebelum decode respons; download konten harus disalin/di-stream dari response body.

> Potongan Go di atas hanya kerangka. AI agent harus menyesuaikan implementasi, tipe respons, error handling, serta cara konfigurasi dengan project target.
