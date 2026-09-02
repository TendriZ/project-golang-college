# API Students - Tugas Mandiri Modul 3

Proyek ini adalah implementasi RESTful API untuk manajemen data Mahasiswa (Students) menggunakan arsitektur Repository Pattern, Fiber v2, dan PostgreSQL. Proyek ini merupakan penyelesaian Praktikum Pemrograman Backend Lanjut.

## 1. Persyaratan Environment Variable

Sebelum menjalankan aplikasi, Anda harus membuat sebuah file `.env` di **root** direktori proyek. Anda dapat menggunakan file `.env.example` sebagai acuan formatnya.

Berikut adalah daftar variabel yang wajib diisi:

| Variabel | Keterangan | Contoh Nilai Bawaan |
|---|---|---|
| `APP_PORT` | Port tempat server berjalan | `3000` |
| `DB_HOST` | Alamat host PostgreSQL | `localhost` |
| `DB_PORT` | Port PostgreSQL | `5432` |
| `DB_USER` | Username PostgreSQL Anda | `postgres` |
| `DB_PASSWORD` | Password PostgreSQL Anda | **(isi dengan password milik Anda)** |
| `DB_NAME` | Nama database yang digunakan | `db_students` |
| `DB_SSLMODE` | Mode SSL (wajib `disable` di lokal) | `disable` |
| `DB_MAX_CONNS` | Batas maksimal connection pool | `10` |

> **Catatan:** Jangan pernah melakukan **commit** file `.env` ke Git. Jika ada variabel baru, tambahkan format kosongnya ke `.env.example`.

## 2. Cara Menyiapkan Basis Data dari Nol (Setup)

Aplikasi ini tidak menggunakan fitur auto-migrasi. Agar aplikasi dapat berjalan dengan normal, siapkan basis data di mesin lokal Anda dengan mengikuti langkah-langkah berikut.

### 1. Buat database

Jalankan perintah berikut pada terminal:

```bash
psql -U postgres -c "CREATE DATABASE db_students;"
```

Apabila berhasil, PostgreSQL akan menampilkan:

```text
CREATE DATABASE
```

### 2. Jalankan migration

Setelah database berhasil dibuat, jalankan file migration untuk membentuk tabel dan indeks.

```bash
psql -U postgres -d db_students -f migrations/001_create_students.sql
```

Apabila berhasil, PostgreSQL akan menampilkan keluaran seperti berikut:

```text
CREATE TABLE
CREATE INDEX
CREATE INDEX
```

### 3. Verifikasi struktur tabel (Opsional)

Untuk memastikan tabel berhasil dibuat, jalankan:

```bash
psql -U postgres -d db_students -c "\d students"
```

Apabila berhasil, akan ditampilkan struktur tabel beserta indeks yang telah dibuat.

### 4. Jalankan aplikasi

Setelah database selesai disiapkan, jalankan aplikasi dengan perintah:

```bash
go run .
```

## 3. Skema Tabel (students)

Aplikasi ini menggunakan satu tabel utama bernama `students`. Berikut adalah rincian skema dan batasan (constraint) yang diterapkan:

| Nama Kolom | Tipe Data | Batasan / Constraint | Keterangan |
|---|---|---|---|
| id | SERIAL | PRIMARY KEY | Dibuat otomatis (auto-increment) |
| nim | INT | NOT NULL, UNIQUE | Di-set unique agar tidak ada data ganda |
| name | VARCHAR(100) | NOT NULL | Nama lengkap mahasiswa |
| grade | NUMERIC(5,2) | NOT NULL | Nilai (contoh: 95.50) |
| is_active | BOOLEAN | NOT NULL, DEFAULT TRUE | Status keaktifan mahasiswa |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Waktu data masuk ke sistem |

Indeks Tambahan untuk Performa:

- UNIQUE INDEX pada kolom `nim` (`students_nim_key`) untuk mencegah race condition dan memastikan integritas data.
- INDEX pada `LOWER(name)` (`students_name_lower_idx`) untuk mempercepat pencarian data secara case-insensitive tanpa harus melakukan pemindaian seluruh tabel (sequential scan).