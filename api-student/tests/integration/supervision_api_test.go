//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"api-student/app/model"
	"api-student/app/repository"
	"api-student/app/service"
	"api-student/config"
	"api-student/helper"
	"api-student/route"
)

// api adalah aplikasi lengkap yang memakai PostgreSQL sungguhan. Bedanya
// dengan production hanya satu: database-nya db_students_test.
type api struct {
	app *fiber.App
	jwt *helper.JWTManager
}

func newAPI(t *testing.T) api {
	t.Helper()

	raw, err := repository.NewRoleRepository(pool).LoadPermissions(context.Background())
	if err != nil {
		t.Fatalf("memuat permission: %v", err)
	}
	perms := helper.NewPermissionSet(raw)
	jwtManager := helper.NewJWTManager("secret-khusus-test-minimal-32-karakter", "test", time.Minute)

	studentRepository := repository.NewStudentRepository(pool)
	supervisorRepository := repository.NewSupervisorRepository(pool)
	supervisionRepository := repository.NewSupervisionRepository(pool)
	outboxRepository := repository.NewOutboxRepository(pool)
	txManager := repository.NewTxManager(pool)

	app := config.NewApp(slog.New(slog.NewTextHandler(io.Discard, nil)), route.Dependencies{
		Pool:           pool,
		JWT:            jwtManager,
		Permissions:    perms,
		StudentService: service.NewStudentService(studentRepository, perms),
		SupervisionService: service.NewSupervisionService(
			studentRepository, supervisorRepository, supervisionRepository,
			outboxRepository, txManager,
		),
	})
	return api{app: app, jwt: jwtManager}
}

// request mengirim request JSON atas nama user dan mengembalikan status
// code beserta body yang sudah diurai. Fungsi ini tidak memakai *testing.T
// karena juga dipanggil dari goroutine.
func (a api) request(user model.User, method, path, body string) (int, map[string]any, error) {
	token, err := a.jwt.GenerateAccess(user)
	if err != nil {
		return 0, nil, err
	}

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.app.Test(req, -1)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil, nil
	}

	var parsed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, parsed, nil
}

// call adalah versi request yang boleh memanggil t.Fatalf, untuk dipakai
// dari goroutine test utama.
func (a api) call(t *testing.T, user model.User, method, path, body string) (int, map[string]any) {
	t.Helper()

	status, parsed, err := a.request(user, method, path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return status, parsed
}

func admin() model.User {
	return model.User{ID: 1, Username: "admin", Role: "admin"}
}

func (a api) assign(t *testing.T, supervisorID, studentID int) (int, string) {
	t.Helper()

	body := `{"student_id": ` + strconv.Itoa(studentID) + `}`
	status, parsed := a.call(t, admin(), http.MethodPost,
		"/api/v1/supervisors/"+strconv.Itoa(supervisorID)+"/assign", body)
	code, _ := parsed["code"].(string)
	return status, code
}

func countRows(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return n
}

// PATCH /supervisors/:id mengubah kuota DAN mencatat event ke outbox dalam
// satu transaction. Test ini memeriksa dua hal sekaligus: permintaan yang
// berhasil meninggalkan tepat satu event, dan permintaan yang ditolak
// (version sudah usang) tidak meninggalkan event tambahan.
func TestUpdateQuotaEndpoint_WritesOutboxOnlyOnSuccess(t *testing.T) {
	a := newAPI(t)
	supervisor := seedSupervisor(t, 5)

	path := "/api/v1/supervisors/" + strconv.Itoa(supervisor.ID)
	body := `{"quota": 4, "version": 1}`

	status, parsed := a.call(t, admin(), http.MethodPatch, path, body)
	if status != http.StatusOK {
		t.Fatalf("update pertama: dapat %d %v, seharusnya 200", status, parsed)
	}
	data := parsed["data"].(map[string]any)
	if data["quota"] != float64(4) || data["version"] != float64(2) {
		t.Errorf("data = %v, seharusnya quota 4 dan version 2", data)
	}

	events := countRows(t,
		`SELECT COUNT(*) FROM outbox WHERE (payload->>'supervisor_id')::INT = $1 AND event_type = $2`,
		supervisor.ID, model.EventQuotaChanged)
	if events != 1 {
		t.Errorf("event supervisor.quota_changed = %d, seharusnya 1", events)
	}

	// Version 1 sudah usang; request ini harus ditolak dan TIDAK menambah
	// event baru.
	status, parsed = a.call(t, admin(), http.MethodPatch, path, body)
	if status != http.StatusConflict || parsed["code"] != "VERSION_CONFLICT" {
		t.Fatalf("update kedua: dapat %d %v, seharusnya 409 VERSION_CONFLICT", status, parsed["code"])
	}

	events = countRows(t,
		`SELECT COUNT(*) FROM outbox WHERE (payload->>'supervisor_id')::INT = $1 AND event_type = $2`,
		supervisor.ID, model.EventQuotaChanged)
	if events != 1 {
		t.Errorf("event setelah penolakan = %d, seharusnya tetap 1", events)
	}
}

// Sepuluh mahasiswa berebut dua slot pada SATU pembimbing secara serentak.
// UPDATE bersyarat (supervised_count < quota) harus menjamin hanya dua yang
// berhasil; sisanya mendapat 409 SUPERVISOR_FULL, bukan 500 dari constraint.
func TestAssignEndpoint_ConcurrentQuotaLimit(t *testing.T) {
	a := newAPI(t)
	supervisor := seedSupervisor(t, 2)

	const attempts = 10
	students := make([]model.Student, attempts)
	for i := range students {
		owner := seedUser(t)
		students[i] = seedStudent(t, owner.ID)
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		byCode  = map[string]int{}
		byState = map[int]int{}
		start   = make(chan struct{})
	)

	for _, student := range students {
		wg.Add(1)
		go func(studentID int) {
			defer wg.Done()
			<-start // tunggu aba-aba agar semua request berangkat bersamaan

			body := `{"student_id": ` + strconv.Itoa(studentID) + `}`
			status, parsed, err := a.request(admin(), http.MethodPost,
				"/api/v1/supervisors/"+strconv.Itoa(supervisor.ID)+"/assign", body)
			if err != nil {
				t.Errorf("request assign: %v", err)
				return
			}
			code, _ := parsed["code"].(string)

			mu.Lock()
			byState[status]++
			byCode[code]++
			mu.Unlock()
		}(student.ID)
	}
	close(start) // aba-aba: semua goroutine dilepas pada saat yang sama
	wg.Wait()

	if byState[http.StatusCreated] != 2 {
		t.Errorf("berhasil = %d, seharusnya 2 (status: %v, code: %v)",
			byState[http.StatusCreated], byState, byCode)
	}
	if byState[http.StatusConflict] != attempts-2 {
		t.Errorf("ditolak 409 = %d, seharusnya %d (status: %v)",
			byState[http.StatusConflict], attempts-2, byState)
	}
	if byCode["SUPERVISOR_FULL"] != attempts-2 {
		t.Errorf("SUPERVISOR_FULL = %d, seharusnya %d (code: %v)",
			byCode["SUPERVISOR_FULL"], attempts-2, byCode)
	}

	count := countRows(t, `SELECT supervised_count FROM supervisors WHERE id = $1`, supervisor.ID)
	if count != 2 {
		t.Errorf("supervised_count = %d, seharusnya 2", count)
	}
	assigned := countRows(t, `SELECT COUNT(*) FROM supervisions WHERE supervisor_id = $1`, supervisor.ID)
	if assigned != 2 {
		t.Errorf("baris supervisions = %d, seharusnya 2", assigned)
	}
}
