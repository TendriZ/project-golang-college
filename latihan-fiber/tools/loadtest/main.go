// Alat uji beban untuk Modul 9.
//
// Jalankan dari folder latihan-fiber:
//
//	go run ./tools/loadtest -mode crowd
//	go run ./tools/loadtest -mode credits
//	go run ./tools/loadtest -mode swap
//
// Server harus sedang berjalan di terminal lain. Jalankan reset.sql
// sebelum setiap uji beban agar keadaan database kembali bersih.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"latihan-fiber/app/model"
	"latihan-fiber/app/service"
	"latihan-fiber/config"
	"latihan-fiber/database"
	"latihan-fiber/helper"
)

const serviceMaxCredits = service.MaxCreditsPerSemester

type testUser struct {
	ID       int
	Username string
	Role     string
	Token    string
}

type result struct {
	status int
	code   string
	err    error
}

func (r result) label() string {
	if r.status == 0 {
		return "ERROR"
	}
	if r.status < 400 {
		return fmt.Sprintf("%d OK", r.status)
	}
	if r.code != "" {
		return fmt.Sprintf("%d %s", r.status, r.code)
	}
	return fmt.Sprintf("%d %s", r.status, http.StatusText(r.status))
}

// fire menjalankan n pekerjaan secara serentak dan menghitung hasilnya.
//
// Setiap goroutine menunggu di depan "gerbang" start yang sama. Gerbang
// baru dibuka setelah SELURUH goroutine siap, sehingga request benar-benar
// berangkat hampir bersamaan — bukan bergiliran sesuai urutan dibuatnya.
func fire(n int, job func(i int) result) map[string]int {
	var wg sync.WaitGroup
	start := make(chan struct{})

	results := make(chan result, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results <- job(i)
		}(i)
	}

	close(start) // buka gerbang
	wg.Wait()    // tunggu SEMUA goroutine selesai
	close(results)

	tally := map[string]int{}
	for r := range results {
		tally[r.label()]++
	}
	return tally
}

func doJSON(client *http.Client, method, url, token string, body any) result {
	var reader io.Reader = http.NoBody

	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return result{err: err}
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return result{err: err}
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return result{err: err}
	}
	defer resp.Body.Close()

	var payload struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload)

	return result{status: resp.StatusCode, code: payload.Code}
}

func loadUsers(
	ctx context.Context,
	pool *pgxpool.Pool,
	jwtManager *helper.JWTManager,
	limit int,
) ([]testUser, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, username, role
         FROM users
         WHERE username LIKE 'mhs%'
         ORDER BY username
         LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("mengambil akun uji: %w", err)
	}
	defer rows.Close()

	users := []testUser{}
	for rows.Next() {
		var u testUser
		if err := rows.Scan(&u.ID, &u.Username, &u.Role); err != nil {
			return nil, fmt.Errorf("membaca akun uji: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("membaca hasil akun uji: %w", err)
	}

	if len(users) < limit {
		return nil, fmt.Errorf(
			"akun uji kurang: butuh %d, tersedia %d (jalankan migration 006)",
			limit, len(users),
		)
	}

	for i := range users {
		token, err := jwtManager.GenerateAccess(model.User{
			ID:       users[i].ID,
			Username: users[i].Username,
			Role:     users[i].Role,
		})
		if err != nil {
			return nil, fmt.Errorf("membuat token %s: %w", users[i].Username, err)
		}
		users[i].Token = token
	}

	return users, nil
}

func courseByCode(ctx context.Context, pool *pgxpool.Pool, code string) (int, error) {
	var id int
	err := pool.QueryRow(ctx,
		`SELECT id FROM courses WHERE code = $1`, code).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("mencari kelas %s: %w", code, err)
	}
	return id, nil
}

func creditCourses(ctx context.Context, pool *pgxpool.Pool) ([]int, error) {
	rows, err := pool.Query(ctx,
		`SELECT id FROM courses
         WHERE code <> 'IF399' AND credits = 3
         ORDER BY code
         LIMIT 12`)
	if err != nil {
		return nil, fmt.Errorf("mengambil kelas untuk mode credits: %w", err)
	}
	defer rows.Close()

	courses := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("membaca kelas: %w", err)
		}
		courses = append(courses, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("membaca hasil kelas: %w", err)
	}
	if len(courses) < 12 {
		return nil, fmt.Errorf("butuh 12 kelas 3 SKS, hanya tersedia %d", len(courses))
	}
	return courses, nil
}

func printTally(tally map[string]int) {
	keys := make([]string, 0, len(tally))
	for key := range tally {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if tally[keys[i]] != tally[keys[j]] {
			return tally[keys[i]] > tally[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys {
		fmt.Printf("  %-32s %d\n", key, tally[key])
	}
}

func printConsistency(
	ctx context.Context,
	pool *pgxpool.Pool,
	codes ...string,
) (bool, error) {
	rows, err := pool.Query(ctx,
		`SELECT c.code, c.quota, c.enrolled_count, COUNT(e.id)
         FROM courses c
         LEFT JOIN enrollments e ON e.course_id = c.id
         WHERE c.code = ANY($1)
         GROUP BY c.id, c.code, c.quota, c.enrolled_count
         ORDER BY c.code`, codes)
	if err != nil {
		return false, fmt.Errorf("memeriksa konsistensi: %w", err)
	}
	defer rows.Close()

	allConsistent := true
	for rows.Next() {
		var code string
		var quota, enrolled, baris int
		if err := rows.Scan(&code, &quota, &enrolled, &baris); err != nil {
			return false, fmt.Errorf("membaca konsistensi: %w", err)
		}

		status := "KONSISTEN"
		if enrolled != baris {
			status = "TIDAK KONSISTEN"
			allConsistent = false
		}

		fmt.Printf(
			"%s  kuota=%d  enrolled_count=%d  baris enrollments=%d  -> %s\n",
			code, quota, enrolled, baris, status,
		)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("membaca hasil konsistensi: %w", err)
	}
	return allConsistent, nil
}

func printStudentCredits(
	ctx context.Context,
	pool *pgxpool.Pool,
	users []testUser,
) (bool, error) {
	ids := make([]int, len(users))
	for i, user := range users {
		ids[i] = user.ID
	}

	rows, err := pool.Query(ctx,
		`SELECT u.username, COALESCE(SUM(c.credits), 0)
         FROM users u
         LEFT JOIN enrollments e ON e.user_id = u.id
         LEFT JOIN courses c ON c.id = e.course_id
         WHERE u.id = ANY($1)
         GROUP BY u.id, u.username
         ORDER BY u.username`, ids)
	if err != nil {
		return false, fmt.Errorf("menghitung SKS mahasiswa: %w", err)
	}
	defer rows.Close()

	allWithinLimit := true
	for rows.Next() {
		var username string
		var total int64
		if err := rows.Scan(&username, &total); err != nil {
			return false, fmt.Errorf("membaca SKS mahasiswa: %w", err)
		}
		if total > int64(serviceMaxCredits) {
			allWithinLimit = false
		}
		fmt.Printf("  %s: %d SKS\n", username, total)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("membaca hasil SKS: %w", err)
	}

	if allWithinLimit {
		fmt.Printf("seluruh %d mahasiswa <= %d SKS -> KONSISTEN\n",
			len(users), serviceMaxCredits)
	} else {
		fmt.Printf("ADA mahasiswa yang melebihi %d SKS -> TIDAK KONSISTEN\n",
			serviceMaxCredits)
	}
	return allWithinLimit, nil
}

func deadlockCount(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx,
		`SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()`).
		Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("membaca statistik deadlock: %w", err)
	}
	return count, nil
}

func runCrowd(
	ctx context.Context,
	pool *pgxpool.Pool,
	client *http.Client,
	base string,
	jwtManager *helper.JWTManager,
) error {
	users, err := loadUsers(ctx, pool, jwtManager, 60)
	if err != nil {
		return err
	}

	courseID, err := courseByCode(ctx, pool, "IF399")
	if err != nil {
		return err
	}

	fmt.Println("60 mahasiswa berebut kelas IF399 secara serentak...")
	start := time.Now()
	tally := fire(len(users), func(i int) result {
		return doJSON(
			client,
			http.MethodPost,
			fmt.Sprintf("%s/api/v1/courses/%d/enroll", base, courseID),
			users[i].Token,
			nil,
		)
	})

	fmt.Println("hasil request:")
	printTally(tally)
	fmt.Printf("waktu total : %s\n", time.Since(start).Round(time.Millisecond))

	_, err = printConsistency(ctx, pool, "IF399")
	return err
}

func runCredits(
	ctx context.Context,
	pool *pgxpool.Pool,
	client *http.Client,
	base string,
	jwtManager *helper.JWTManager,
) error {
	users, err := loadUsers(ctx, pool, jwtManager, 10)
	if err != nil {
		return err
	}

	courses, err := creditCourses(ctx, pool)
	if err != nil {
		return err
	}

	fmt.Printf(
		"10 mahasiswa, masing-masing mendaftar ke %d kelas 3 SKS secara serentak (batas %d SKS)...\n",
		len(courses), serviceMaxCredits,
	)

	start := time.Now()
	tally := fire(len(users)*len(courses), func(i int) result {
		student := users[i/len(courses)]
		courseID := courses[i%len(courses)]
		return doJSON(
			client,
			http.MethodPost,
			fmt.Sprintf("%s/api/v1/courses/%d/enroll", base, courseID),
			student.Token,
			nil,
		)
	})

	fmt.Println("hasil request:")
	printTally(tally)
	fmt.Printf("waktu total : %s\n", time.Since(start).Round(time.Millisecond))

	_, err = printStudentCredits(ctx, pool, users)
	return err
}

func runSwap(
	ctx context.Context,
	pool *pgxpool.Pool,
	client *http.Client,
	base string,
	jwtManager *helper.JWTManager,
) error {
	users, err := loadUsers(ctx, pool, jwtManager, 40)
	if err != nil {
		return err
	}

	if101, err := courseByCode(ctx, pool, "IF101")
	if err != nil {
		return err
	}
	if102, err := courseByCode(ctx, pool, "IF102")
	if err != nil {
		return err
	}

	// Siapkan 20 pasang: yang genap mulai dari IF101, yang ganjil dari IF102.
	fmt.Println("menyiapkan 20 pasang mahasiswa di IF101 dan IF102...")
	setup := fire(len(users), func(i int) result {
		courseID := if101
		if i%2 == 1 {
			courseID = if102
		}
		return doJSON(
			client,
			http.MethodPost,
			fmt.Sprintf("%s/api/v1/courses/%d/enroll", base, courseID),
			users[i].Token,
			nil,
		)
	})

	if setup["201 OK"] != len(users) {
		fmt.Println("hasil persiapan:")
		printTally(setup)
		return fmt.Errorf("persiapan gagal; jalankan reset.sql lalu coba lagi")
	}

	before, err := deadlockCount(ctx, pool)
	if err != nil {
		return err
	}

	fmt.Println("20 pasang mahasiswa pindah kelas berlawanan arah secara serentak...")
	start := time.Now()
	tally := fire(len(users), func(i int) result {
		body := model.SwapRequest{FromCourseID: if101, ToCourseID: if102}
		if i%2 == 1 {
			body = model.SwapRequest{FromCourseID: if102, ToCourseID: if101}
		}
		return doJSON(
			client,
			http.MethodPost,
			base+"/api/v1/enrollments/swap",
			users[i].Token,
			body,
		)
	})

	after, err := deadlockCount(ctx, pool)
	if err != nil {
		return err
	}

	fmt.Println("hasil request:")
	printTally(tally)
	fmt.Printf("waktu total : %s\n", time.Since(start).Round(time.Millisecond))
	fmt.Printf("deadlock terdeteksi PostgreSQL selama uji: %d\n", after-before)

	_, err = printConsistency(ctx, pool, "IF101", "IF102")
	return err
}

// tokenForUser membuat access token untuk satu username TANPA login.
// Dipakai pada uji fungsional C.4: akun mhs001..mhs060 sengaja tidak
// memiliki password bcrypt yang sah, sehingga token dibuat langsung
// dengan JWT_SECRET yang sama seperti server.
func tokenForUser(
	ctx context.Context,
	pool *pgxpool.Pool,
	jwtManager *helper.JWTManager,
	username string,
) (string, error) {
	var u testUser
	err := pool.QueryRow(ctx,
		`SELECT id, username, role FROM users WHERE username = $1`, username).
		Scan(&u.ID, &u.Username, &u.Role)
	if err != nil {
		return "", fmt.Errorf("mencari user %s: %w", username, err)
	}

	token, err := jwtManager.GenerateAccess(model.User{
		ID:       u.ID,
		Username: u.Username,
		Role:     u.Role,
	})
	if err != nil {
		return "", fmt.Errorf("membuat token %s: %w", username, err)
	}
	return token, nil
}

func main() {
	mode := flag.String("mode", "crowd", "mode uji: crowd, credits, swap, token")
	baseURL := flag.String("base", "", "base URL server (default http://localhost:$APP_PORT)")
	username := flag.String("user", "", "username untuk mode token")
	flag.Parse()

	config.LoadEnv()

	ctx := context.Background()
	pool, err := database.NewPool(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gagal terhubung ke database:", err)
		os.Exit(1)
	}
	defer pool.Close()

	jwtSecret := config.GetEnv("JWT_SECRET", "")
	if len(jwtSecret) < 32 {
		fmt.Fprintln(os.Stderr, "JWT_SECRET tidak diisi atau kurang dari 32 karakter")
		os.Exit(1)
	}

	jwtManager := helper.NewJWTManager(
		jwtSecret,
		config.GetEnv("JWT_ISSUER", "praktikum-backend"),
		time.Duration(config.GetEnvInt("JWT_ACCESS_TTL_MINUTES", 15))*time.Minute,
	)

	base := *baseURL
	if base == "" {
		base = "http://localhost:" + config.GetEnv("APP_PORT", "3000")
	}
	base = strings.TrimRight(base, "/")

	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        200,
			MaxIdleConnsPerHost: 200,
			MaxConnsPerHost:     200,
		},
	}

	// Mode token hanya mencetak token ke stdout — tanpa baris lain —
	// supaya dapat ditangkap dengan $(...) pada shell.
	if *mode == "token" {
		if *username == "" {
			fmt.Fprintln(os.Stderr, "mode token membutuhkan -user <username>")
			os.Exit(2)
		}
		token, err := tokenForUser(ctx, pool, jwtManager, *username)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gagal:", err)
			os.Exit(1)
		}
		fmt.Println(token)
		return
	}

	fmt.Println("catatan: jalankan reset.sql sebelum setiap uji beban")

	switch *mode {
	case "crowd":
		err = runCrowd(ctx, pool, client, base, jwtManager)
	case "credits":
		err = runCredits(ctx, pool, client, base, jwtManager)
	case "swap":
		err = runSwap(ctx, pool, client, base, jwtManager)
	default:
		fmt.Fprintf(os.Stderr, "mode tidak dikenal: %s\n", *mode)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "gagal:", err)
		os.Exit(1)
	}
}
