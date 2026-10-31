# Memasang storage node CAS di server lain

Panduan ini memasang satu storage node HTTPS di host Linux terpisah, lalu mendaftarkannya ke CAS. Ulangi langkah node untuk setiap server baru. Deployment memakai Docker image CAS yang sudah dibangun dari versi/release yang sama dengan server CAS.

## 1. Siapkan host dan jaringan

- Siapkan host Linux dengan Docker Engine, DNS yang dapat diakses server CAS, dan disk/volume khusus untuk objek CAS.
- Tentukan node ID stabil, URL HTTPS, dan failure domain. Contoh: `storage-jkt-a`, `https://storage-a.internal:8081`, `rack-a`. Gunakan nilai failure domain yang berbeda untuk lokasi fisik yang benar-benar terpisah.
- Pada firewall host, izinkan TCP port `8081` hanya dari alamat server CAS. Jangan expose port ini ke internet atau browser klien.
- Sinkronkan waktu host melalui NTP agar pemeriksaan sertifikat dan log dapat dipercaya.

## 2. Siapkan sertifikat dan token node

Gunakan sertifikat server yang ditandatangani CA internal. SAN sertifikat harus cocok dengan nama host di URL CAS, misalnya `storage-a.internal`. Salin sertifikat node, private key, dan CA certificate ke host node; batasi permission private key dan jangan masukkan private key ke image/repository.

Buat token service acak dan unik per node menggunakan secret manager. Buat file environment yang hanya dapat dibaca administrator, misalnya `/etc/cas/storage-node.env`:

```dotenv
CAS_ROLE=storage-node
CAS_ADDR=:8081
CAS_STORAGE_DIR=/data
CAS_NODE_SERVICE_TOKEN=<token-acak-dari-secret-manager>
CAS_NODE_TLS_CERT=/certs/node.crt
CAS_NODE_TLS_KEY=/certs/node.key
CAS_NODE_MAX_OBJECT_BYTES=10737418240
```

Simpan file dengan permission `0600`. Jangan menulis token ke command line, URL, browser, atau log.

## 3. Jalankan storage node

Pasang image CAS yang sama versinya dengan CAS server. Jika image belum tersedia dari registry internal, build image dari checkout release yang disetujui lalu pindahkan melalui registry/artifact store tepercaya.

Pada mesin build yang memiliki checkout release:

```sh
docker build -t <registry-internal>/cas:<release> .
docker push <registry-internal>/cas:<release>
```

Di host storage node, tarik tag yang sama sebelum menjalankan container:

```sh
docker pull <registry-internal>/cas:<release>
```

Buat volume data dan jalankan container. Perintah berikut menggunakan named volume Docker agar permission direktori mengikuti image:

```sh
docker volume create cas-storage-a-data
docker run -d \
  --name cas-storage-a \
  --restart unless-stopped \
  -p 8081:8081 \
  --mount source=cas-storage-a-data,target=/data \
  --mount type=bind,source=/etc/cas/certs,target=/certs,readonly \
  --env-file /etc/cas/storage-node.env \
  <registry-internal>/cas:<release>
```

Periksa container dan pastikan log tidak mencetak secret:

```sh
docker ps --filter name=cas-storage-a
docker logs --tail 50 cas-storage-a
```

Storage node melayani HTTPS di port `8081`. Endpoint health dan capacity memakai bearer token yang sama; request tanpa token seharusnya ditolak.

## 4. Daftarkan node di CAS

Tambahkan CA certificate ke path yang hanya dapat dibaca container CAS, misalnya mount read-only `/certs/ca.crt`. Tambahkan node ke konfigurasi rahasia CAS `CAS_STORAGE_NODES`:

```json
[
  {
    "id": "storage-jkt-a",
    "url": "https://storage-a.internal:8081",
    "failure_domain": "rack-a",
    "token": "<ambil-dari-secret-manager>",
    "ca_file": "/certs/ca.crt"
  }
]
```

Untuk beberapa node, isi satu objek per node dan gunakan failure domain sesuai topologi nyata. Hindari menyimpan token dalam file `.env` yang masuk source control; gunakan secret manager atau mekanisme secrets deployment. Semua instance CAS yang melayani upload harus menerima konfigurasi node yang sama.

CAS mendaftarkan node saat startup, memeriksa heartbeat/capacity setiap 20 detik, dan menolak sertifikat yang tidak dipercaya atau hostname yang tidak cocok. Setelah konfigurasi diperbarui, restart/redeploy CAS. Jangan mengaktifkan `replica_target` lebih tinggi dari jumlah failure domain yang tersedia.

## 5. Verifikasi koneksi dan kapasitas

Gunakan curl config file lokal berpermission `0600` agar bearer token tidak muncul di riwayat shell. Contoh `/etc/cas/node-check.curl`:

```text
header = "Authorization: Bearer <token-node>"
cacert = "/etc/cas/certs/ca.crt"
silent
show-error
```

Lalu periksa dari server CAS atau host yang diizinkan firewall:

```sh
curl --config /etc/cas/node-check.curl https://storage-a.internal:8081/internal/v1/health
curl --config /etc/cas/node-check.curl https://storage-a.internal:8081/internal/v1/capacity
```

Di CAS, verifikasi `GET /api/v1/admin/storage/nodes` menunjukkan node `ACTIVE`, heartbeat terbaru, dan kapasitas. Setelah upload dengan target replika lebih dari satu, periksa `GET /api/v1/admin/storage/jobs` dan metadata aset hingga `storage_status` menjadi `AVAILABLE`.

## 6. Maintenance dan penggantian host

Sebelum maintenance, set node ke `DRAINING` melalui API admin CAS. Tunggu sampai `ready_replicas`, `drain_jobs_pending`, dan `drain_jobs_failed` semuanya nol. Worker membuat salinan pengganti, memverifikasi checksum/ukuran dan failure domain, kemudian menghapus file lama dan menonaktifkan metadata replika sumber. Jangan mematikan host saat ada replika siap atau job drain tertunda/gagal.

Sebelum melepas disk, backup harus tersedia dan kebijakan retensi/secure erase organisasi harus dipenuhi. Untuk mengembalikan node ke placement, pastikan volume dan sertifikat siap lalu set status `ACTIVE`. Lihat [runbook operasi storage](storage-operations.md) untuk monitoring, rotasi sertifikat/token, backup, dan latihan restore.
