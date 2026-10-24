# API Reference — Lengkap

> **Cara Pakai Dokumen Ini:**
> Upload file ini ke AI lalu minta:
> "Jelaskan endpoint [nama endpoint] pada UserGate" atau
> "Buatkan request ke endpoint [nama endpoint] dengan parameter ini: ..."
> AI akan memberikan contoh request/response yang akurat.

---

## Base URL

```text
https://usergate.sandalgurun.web.id/api/v1
```

Jika environment masih menggunakan HTTP, ubah skema menjadi `http://`.

---

## Header Wajib

### Untuk Semua Endpoint

```http
X-API-Key: <api_key>
Content-Type: application/json
Accept: application/json
```

### Untuk Endpoint yang Butuh Login

Tambahkan:

```http
Authorization: Bearer <access_token>
```

---

## Format Response

### Sukses

```json
{
  "status": true,
  "message": "Success",
  "data": {}
}
```

### Error

```json
{
  "status": false,
  "message": "Error message"
}
```

### Error dengan Detail Validasi

```json
{
  "status": false,
  "message": "Validation failed.",
  "errors": {
    "email": "The email field must contain a valid email address."
  }
}
```

---

## 1. Login

Membuat access token dan refresh token.

```http
POST /api/v1/auth/login
```

**Headers:**

```http
X-API-Key: <api_key>
Content-Type: application/json
Accept: application/json
```

**Request Body:**

| Field | Tipe | Wajib | Keterangan |
|-------|------|-------|------------|
| `username` | string | Ya | Username user |
| `password` | string | Ya | Password user |

**Request Example:**

```json
{
  "username": "admin",
  "password": "password-user"
}
```

**Response `200`:**

```json
{
  "status": true,
  "message": "Authenticated successfully.",
  "data": {
    "access_token": "eyJ...",
    "token_type": "Bearer",
    "expires_in": 900,
    "refresh_token": "rt_...",
    "refresh_expires_in": 2592000,
    "user": {
      "id": "uuid-user",
      "username": "admin",
      "email": "admin@example.com",
      "full_name": "Administrator",
      "status": "ACTIVE",
      "roles": ["SUPER_ADMIN"],
      "is_super_admin": true
    }
  }
}
```

**Catatan:**

- `access_token` berlaku 900 detik (15 menit).
- `refresh_token` berlaku 30 hari.
- Login dibatasi rate limit per IP.
- Username/password salah menghasilkan response yang sama (mencegah kebocoran info).

**Error Responses:**

| Status | Message | Arti |
|--------|---------|------|
| `401` | `Invalid credentials.` | Kredensial salah, user tidak aktif, atau tidak ditemukan |
| `401` | `API Key is required.` | Header API key tidak dikirim |
| `401` | `Invalid or inactive API Key.` | API key salah atau tidak aktif |
| `429` | — | Terlalu banyak percobaan login |

---

## 2. Refresh Token

Mengganti refresh token dengan pasangan token baru.

```http
POST /api/v1/auth/refresh
```

**Headers:**

```http
X-API-Key: <api_key>
Content-Type: application/json
Accept: application/json
```

**Request Body:**

| Field | Tipe | Wajib | Keterangan |
|-------|------|-------|------------|
| `refresh_token` | string | Ya | Refresh token aktif |

**Request Example:**

```json
{
  "refresh_token": "rt_..."
}
```

**Response `200`:**

```json
{
  "status": true,
  "message": "Token refreshed successfully.",
  "data": {
    "access_token": "eyJ...",
    "token_type": "Bearer",
    "expires_in": 900,
    "refresh_token": "rt_new...",
    "refresh_expires_in": 2592000,
    "user": {
      "id": "uuid-user",
      "username": "admin",
      "email": "admin@example.com",
      "full_name": "Administrator",
      "status": "ACTIVE",
      "roles": ["SUPER_ADMIN"],
      "is_super_admin": true
    }
  }
}
```

**Catatan:**

- Refresh token **one-time use** — dirotasi setiap kali digunakan.
- Refresh token harus menggunakan API key yang sama saat penerbitan.

**Error Responses:**

| Status | Message |
|--------|---------|
| `401` | `Invalid or expired refresh token.` |
| `401` | `API Key is required.` |
| `401` | `Invalid or inactive API Key.` |

---

## 3. Current User

Mengambil informasi user dari access token aktif.

```http
GET /api/v1/auth/me
```

**Headers:**

```http
X-API-Key: <api_key>
Authorization: Bearer <access_token>
Accept: application/json
```

**Request Body:** Tidak ada.

**Response `200`:**

```json
{
  "status": true,
  "message": "Current user retrieved successfully.",
  "data": {
    "id": "uuid-user",
    "username": "admin",
    "email": "admin@example.com",
    "full_name": "Administrator",
    "status": "ACTIVE",
    "roles": ["SUPER_ADMIN"],
    "is_super_admin": true
  }
}
```

**Catatan:**

- Password dan password hash **tidak pernah** dikembalikan.

**Error Responses:**

| Status | Message |
|--------|---------|
| `401` | `Invalid or expired access token.` |

---

## 4. Logout

Mencabut access token aktif beserta pasangan tokennya.

```http
POST /api/v1/auth/logout
```

**Headers:**

```http
X-API-Key: <api_key>
Authorization: Bearer <access_token>
```

**Request Body:** Tidak ada.

**Response `200`:**

```json
{
  "status": true,
  "message": "Logged out successfully.",
  "data": null
}
```

**Catatan:**

- Token yang sudah dicabut tidak dapat digunakan kembali.

---

## 5. Daftar User

Mengambil daftar user dengan pagination dan pencarian.

```http
GET /api/v1/users
```

**Headers:**

```http
X-API-Key: <api_key>
Authorization: Bearer <access_token>
Accept: application/json
```

**Query Parameters:**

| Parameter | Tipe | Default | Keterangan |
|-----------|------|---------|------------|
| `page` | integer | `1` | Nomor halaman |
| `per_page` | integer | `20` | Jumlah data per halaman (maksimum `100`) |
| `search` | string | kosong | Mencari berdasarkan username, email, atau nama |

**Response `200`:**

```json
{
  "status": true,
  "message": "Users retrieved successfully.",
  "data": [
    {
      "id": "uuid-user",
      "username": "budi",
      "email": "budi@example.com",
      "full_name": "Budi Santoso",
      "status": "ACTIVE",
      "created_at": "2026-09-05 10:00:00",
      "updated_at": "2026-09-05 10:00:00"
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 20,
    "total": 1,
    "total_pages": 1
  }
}
```

**Error Responses:**

| Status | Message |
|--------|---------|
| `401` | `Invalid or expired access token.` |
| `403` | `API Key does not have user.read permission.` |

---

## 6. Detail User

Mengambil data satu user berdasarkan ID.

```http
GET /api/v1/users/{id}
```

**Headers:**

```http
X-API-Key: <api_key>
Authorization: Bearer <access_token>
Accept: application/json
```

**Path Parameters:**

| Parameter | Tipe | Keterangan |
|-----------|------|------------|
| `id` | string (UUID) | ID user |

**Response `200`:**

```json
{
  "status": true,
  "message": "User retrieved successfully.",
  "data": {
    "id": "uuid-user",
    "username": "budi",
    "email": "budi@example.com",
    "full_name": "Budi Santoso",
    "status": "ACTIVE",
    "created_at": "2026-09-05 10:00:00",
    "updated_at": "2026-09-05 10:00:00"
  }
}
```

**Error Responses:**

| Status | Message |
|--------|---------|
| `401` | `Invalid or expired access token.` |
| `403` | `API Key does not have user.read permission.` |
| `404` | `User not found.` |

---

## 7. Membuat User

Membuat user baru. Password disimpan sebagai hash.

```http
POST /api/v1/users
```

**Headers:**

```http
X-API-Key: <api_key>
Authorization: Bearer <access_token>
Content-Type: application/json
```

**Request Body:**

| Field | Tipe | Wajib | Validasi |
|-------|------|-------|----------|
| `username` | string | Ya | 3-100 karakter, alfanumerik saja |
| `email` | string | Ya | Harus valid |
| `full_name` | string | Ya | 3-150 karakter |
| `password` | string | Ya | Minimal 8 karakter |

**Request Example:**

```json
{
  "username": "budi",
  "email": "budi@example.com",
  "full_name": "Budi Santoso",
  "password": "password-minimal-8"
}
```

**Response `201`:**

```json
{
  "status": true,
  "message": "User created successfully.",
  "data": {
    "id": "uuid-user",
    "username": "budi",
    "email": "budi@example.com",
    "full_name": "Budi Santoso",
    "status": "ACTIVE"
  }
}
```

**Error Responses:**

| Status | Message |
|--------|---------|
| `401` | `Invalid or expired access token.` |
| `403` | `API Key does not have user.create permission.` |
| `409` | `Username already exists.` |
| `409` | `Email already exists.` |
| `422` | `Validation failed.` |

---

## 8. Mengubah User

Mengubah data user yang sudah ada.

```http
PUT /api/v1/users/{id}
```

**Headers:**

```http
X-API-Key: <api_key>
Authorization: Bearer <access_token>
Content-Type: application/json
```

**Path Parameters:**

| Parameter | Tipe | Keterangan |
|-----------|------|------------|
| `id` | string (UUID) | ID user |

**Request Body (semua field opsional):**

| Field | Tipe | Keterangan |
|-------|------|------------|
| `username` | string | Username baru (3-100 karakter, alfanumerik) |
| `email` | string | Email baru (harus valid) |
| `full_name` | string | Nama lengkap baru (3-150 karakter) |
| `status` | string | `ACTIVE` atau `INACTIVE` |

**Request Example:**

```json
{
  "username": "budi-update",
  "email": "budi-update@example.com",
  "full_name": "Budi Santoso Update",
  "status": "ACTIVE"
}
```

**Response `200`:**

```json
{
  "status": true,
  "message": "User updated successfully.",
  "data": {
    "id": "uuid-user",
    "username": "budi-update",
    "email": "budi-update@example.com",
    "full_name": "Budi Santoso Update",
    "status": "ACTIVE",
    "created_at": "2026-09-05 10:00:00",
    "updated_at": "2026-09-19 12:00:00"
  }
}
```

**Error Responses:**

| Status | Message |
|--------|---------|
| `401` | `Invalid or expired access token.` |
| `403` | `API Key does not have user.update permission.` |
| `404` | `User not found.` |
| `409` | `Username already exists.` |
| `409` | `Email already exists.` |
| `422` | `Validation failed.` |

---

## 9. Menghapus User

Menghapus user dan credential terkait. Operasi tidak dapat dibatalkan.

```http
DELETE /api/v1/users/{id}
```

**Headers:**

```http
X-API-Key: <api_key>
Authorization: Bearer <access_token>
```

**Path Parameters:**

| Parameter | Tipe | Keterangan |
|-----------|------|------------|
| `id` | string (UUID) | ID user |

**Response `200`:**

```json
{
  "status": true,
  "message": "User deleted successfully.",
  "data": {
    "id": "uuid-user",
    "username": "budi"
  }
}
```

**Error Responses:**

| Status | Message |
|--------|---------|
| `401` | `Invalid or expired access token.` |
| `403` | `API Key does not have user.delete permission.` |
| `404` | `User not found.` |

---

## Status HTTP Reference

| Status | Arti | Kapan Terjadi |
|--------|------|---------------|
| `200` | Request berhasil | Semua operasi read/update/delete sukses |
| `201` | Resource berhasil dibuat | Create user sukses |
| `401` | Tidak terautentikasi | API key/token tidak valid atau tidak dikirim |
| `403` | Tidak punya izin | API key tidak memiliki permission yang dibutuhkan |
| `404` | Tidak ditemukan | User dengan ID yang diberikan tidak ada |
| `409` | Bentrok | Username atau email sudah digunakan |
| `422` | Validasi gagal | Field tidak sesuai aturan validasi |
| `429` | Rate limit | Terlalu banyak request dalam waktu singkat |
| `500` | Error server | Kesalahan internal — coba lagi atau hubungi admin |

---

## Permission API

| Permission | Endpoint | Operasi |
|-----------|----------|---------|
| `user.read` | `GET /users`, `GET /users/{id}` | Membaca user |
| `user.create` | `POST /users` | Membuat user |
| `user.update` | `PUT /users/{id}` | Mengubah user |
| `user.delete` | `DELETE /users/{id}` | Menghapus user |

API key harus memiliki permission yang sesuai. Jika tidak → response `403`.
