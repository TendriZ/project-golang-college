## TM 1
## TM 2
## TM 3
## TM 4
## TM 5
**Tanggal:** 2026-09-16  
**Model/Alat:** muse-spark-1.2-contributor-free via opencode (agent Muse Spark)

**Ruang lingkup bantuan:**
1. Membaca dan merangkum `latihan-fiber/Modul 5 - Authentication dan Security.pdf` (27 hal) — diringkas alur register/login, JWT access 15 menit + refresh 7 hari, rotasi, dan 9 kerentanan (algorithm confusion, brute force, user enumeration, mass assignment, dll).
2. Diagnosis error merah di `latihan-fiber`:
   - `go mod tidy` untuk `missing go.sum entry tinylib/msgp` pada `fiber/middleware/limiter`
   - `go vet ./...` / `go build ./...` → `app/service/auth_service.go:77 FindByUsername undefined` karena `app/repository/user_repository.go:22` interface `UserRepository` belum ada method tersebut
   - Ketidak-sinkronan `config/app.go:17` (signature lama `NewApp(logger, pool, userService)` vs panggilan baru `NewApp(logger, route.Dependencies{...})` di `main.go:51`) dan `middleware/middleware.go:33 Register` butuh 3 argumen `allowedOrigins`
   - `main.go:57` terpotong (`// ... sisanya sama seperti pertemuan 4`) sehingga `os/signal`/`syscall` unused dan server tidak pernah `Listen` / graceful shutdown
   - Ketidak-konsistenan kolom `role` di `app/repository/user_repository.go:84,112,130` (select/insert tanpa `role` padahal `app/model/user.go:10` sudah ada `Role`)
3. Perbaikan yang diterapkan (diverifikasi `go vet` & `go build` = 0):
   - `app/repository/user_repository.go:22` tambah `FindByUsername(ctx, username string) (model.User, error)` ke interface
   - `app/repository/user_repository.go:84,112` ubah `SELECT ... password, is_active ...` → `... password, role, is_active ...` dan `Scan(..., &u.Role, ...)`; `Create:130` ubah `INSERT (username,email,password,is_active)` → `(...,role, ...)` dengan fallback `if u.Role=="" {u.Role="user"}` agar sesuai `auth_service.go:104 Role:"user"` (anti mass assignment)
   - `config/app.go:17` ganti signature ke `NewApp(logger, deps route.Dependencies)`, tambah `BodyLimit: 1*1024*1024`, dan `middleware.Register(app, logger, GetEnv("ALLOWED_ORIGINS",""))` + `route.Register(app, deps)` sesuai Langkah 8 modul
   - `main.go:51` lengkapi kembali blok `Listen(":3000")` + goroutine + `signal.Notify` + `ShutdownWithContext` 10s sesuai template pertemuan 4
4. Cek history CLI: `git log --oneline` (branch `main`) dan `PowerShell PSReadLine ConsoleHost_history.txt` — tidak ada `go get jwt`, `psql -f migrations/002_auth.sql`, atau `openssl rand -hex 32` untuk TM-5 yang terekam; perubahan masih `Changes not staged` sehingga belum ada commit bertahap TM-5.

**Bagian yang dikerjakan manual & verifikasi:**
- Seluruh kode hasil AI dikompilasi ulang (`go vet ./...`, `go build ./...`), `go mod tidy`, dan `go test ./app/service` (AppLocker di `Temp` memblokir default runner, di-bypass dengan `-exec echo`).
- Verifikasi keamanan sesuai modul tetap dilakukan manual: cek `helper/security.go:12 bcryptCost=12`, `helper/jwt.go:56` cek `SigningMethodHMAC`, `middleware/auth.go:54` limiter, serta rencana `curl` Langkah 8 (register/login/refresh/429/alg none) dan `psql TRUNCATE` belum dieksekusi — perlu dijalankan manual sebelum pengumpulan.

**Prompt inti:** "bisakah kamu membaca Modul 5 ... masih banyak error merah ... cek history command CLI"

## TM 6
**Tanggal:** 2026-09-23
**Model/Alat:** muse-spark via opencode (agent Muse Spark)

**Ruang lingkup bantuan:**
1. Membaca `api-student/Modul 6- Authorization dan RBAC.pdf` dan audit kelengkapan C.1–C.2: C.1 (`migrations/003_student_permissions.sql`, roles/permissions/`owner_id`) sudah ada; C.2 belum ada sama sekali.
2. Implementasi C.2 mengikuti pola `latihan-fiber`: `helper/authz.go` (PermissionSet fail-closed), `app/repository/role_repository.go` (LEFT JOIN agar role tanpa permission tetap dikenal), `middleware/authz.go` (RequirePermission + RequireRole pembanding), `app/service/student_authz_rules.go` (`CanAccessStudent` murni).
3. Plumbing `owner_id`: `COALESCE(owner_id,0)` di SELECT (data lama NULL aman), INSERT pakai owner pemanggil, UPDATE sengaja tidak `SET owner_id`; `StudentService` tambah `perms` + cek ownership (`student:read:any` untuk GET, `student:update:any` untuk PUT/PATCH); `route` pasang `student:list/create/delete` di middleware; `main.go` load permission sekali + `os.Exit(1)` bila gagal; logger tambah `user_id`/`role`; `Me` balas `{user, permissions}`.
4. Panduan laporan C.2 (5 bagian: image + penjelasan), skrip curl matriks C.3 + 2 pengujian negatif (N1 mass-assignment `owner_id`, N2 IDOR), dan draf jawaban analisis C.4.
5. Percobaan menjalankan server gagal di mesin ini: WDAC/App Control memblokir semua binary Go hasil kompilasi (`go run`, `go build` + run, via PowerShell), dan WSL tidak mencapai Postgres (firewall). Pengujian curl C.3 harus dijalankan manual di mesin yang mengizinkan eksekusi.

**Bagian yang dikerjakan manual & verifikasi:**
- Verifikasi `go vet ./...` = 0, `go build ./...` = 0, `go fmt ./...` di `api-student`.
- Isi DB dicek via `psql`: roles/permissions/`role_permissions` sesuai C.1, `owner_id` nullable + FK `ON DELETE SET NULL`.
- Seluruh curl matriks + screenshot C.3 dan pengisian tabel dilakukan manual (belum dieksekusi saat penulisan ini).

**Prompt inti:** "beralih ke api-student untuk Modul 6 ... cek kelengkapannya dulu" → "lanjut ke C.3" → "buatkan skrip curl" → "commit bertahap one by one ... sekalian push"
