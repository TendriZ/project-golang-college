package service_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"api-student/app/model"
	"api-student/app/service"
	"api-student/config"
	"api-student/helper"
	"api-student/route"
)

// Test di file ini memakai package service_test (black-box): ia hanya dapat
// memakai apa yang di-export oleh package service, persis seperti kode lain
// di luar package. Yang diuji adalah perilaku yang terlihat dari luar —
// status code dan isi response — bukan detail di dalam service.

const (
	ownerID   = 7
	stranger  = 8
	adminID   = 1
	studentID = 1
)

type testEnv struct {
	app *fiber.App
	jwt *helper.JWTManager
}

// newTestEnv menyusun aplikasi lengkap — middleware, route, dan error
// handler yang SAMA dengan production — tetapi repository-nya diganti fake.
// Setiap test memanggil fungsi ini sendiri sehingga mendapat fake yang baru.
func newTestEnv(t *testing.T) testEnv {
	t.Helper()

	students := newFakeStudentRepo()
	students.nextID = studentID
	students.students[studentID] = model.Student{
		ID: studentID, NIM: 220001, Name: "Sari", Grade: 90,
		IsActive: true, OwnerID: ownerID, CreatedAt: time.Now(),
	}

	jwtManager := helper.NewJWTManager("secret-khusus-test-minimal-32-karakter", "test", time.Minute)
	perms := helper.NewPermissionSet(map[string][]string{
		"admin": {
			"student:list", "student:create", "student:delete",
			"student:read:any", "student:update:any",
		},
		"user": {},
	})

	app := config.NewApp(slog.New(slog.NewTextHandler(io.Discard, nil)), route.Dependencies{
		JWT:            jwtManager,
		Permissions:    perms,
		StudentService: service.NewStudentService(students, perms),
	})

	return testEnv{app: app, jwt: jwtManager}
}

// do mengirim request ke aplikasi TANPA membuka port jaringan. app.Test
// menjalankan seluruh rantai middleware dan handler secara langsung.
func (e testEnv) do(t *testing.T, user model.User, method, path, body string) (int, map[string]any) {
	t.Helper()

	token, err := e.jwt.GenerateAccess(user)
	if err != nil {
		t.Fatalf("membuat token: %v", err)
	}

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.app.Test(req, -1) // -1: tanpa batas waktu
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	// 204 tidak membawa body; jangan memaksa decode JSON.
	if resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}

	var parsed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("%s %s: response bukan JSON: %v", method, path, err)
	}
	return resp.StatusCode, parsed
}

func user(id int, role string) model.User {
	return model.User{ID: id, Username: "user" + strconv.Itoa(id), Role: role}
}

func TestListStudents_ReturnsPage(t *testing.T) {
	env := newTestEnv(t)

	status, body := env.do(t, user(adminID, "admin"), http.MethodGet, "/api/v1/students?limit=10", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, seharusnya 200 (body: %v)", status, body)
	}
	if data := body["data"].([]any); len(data) != 1 {
		t.Errorf("jumlah mahasiswa = %d, seharusnya 1", len(data))
	}
	meta := body["meta"].(map[string]any)
	if meta["limit"] != float64(10) {
		t.Errorf("meta.limit = %v, seharusnya 10", meta["limit"])
	}
}

func TestGetStudent_OwnershipAndPermission(t *testing.T) {
	tests := []struct {
		name       string
		user       model.User
		wantStatus int
	}{
		{"pemilik data", user(ownerID, "user"), http.StatusOK},
		{"user lain tanpa permission", user(stranger, "user"), http.StatusForbidden},
		{"admin dengan student:read:any", user(adminID, "admin"), http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			status, body := env.do(t, tt.user, http.MethodGet, "/api/v1/students/1", "")
			if status != tt.wantStatus {
				t.Errorf("status = %d, seharusnya %d (body: %v)", status, tt.wantStatus, body)
			}
		})
	}
}

func TestCreateStudent_OwnerFromToken(t *testing.T) {
	env := newTestEnv(t)

	// owner_id sengaja dikirim: client tidak boleh dapat mengaku sebagai
	// pemilik lain (mass assignment).
	body := `{"nim": 220099, "name": "Budi", "grade": 85, "owner_id": 999}`
	status, parsed := env.do(t, user(adminID, "admin"), http.MethodPost, "/api/v1/students", body)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, seharusnya 201 (body: %v)", status, parsed)
	}
	data := parsed["data"].(map[string]any)
	if data["owner_id"] != float64(adminID) {
		t.Errorf("owner_id = %v, seharusnya %d dari token", data["owner_id"], adminID)
	}
}

func TestPatchStudent_UpdatesOnlySentFields(t *testing.T) {
	env := newTestEnv(t)

	status, body := env.do(t, user(ownerID, "user"), http.MethodPatch, "/api/v1/students/1", `{"grade": 88.5}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, seharusnya 200 (body: %v)", status, body)
	}
	data := body["data"].(map[string]any)
	if data["grade"] != 88.5 {
		t.Errorf("grade = %v, seharusnya 88.5", data["grade"])
	}
	if data["name"] != "Sari" {
		t.Errorf("name = %v, seharusnya tidak berubah", data["name"])
	}

	status, body = env.do(t, user(ownerID, "user"), http.MethodPatch, "/api/v1/students/1", `{}`)
	if status != http.StatusBadRequest || body["code"] != "BAD_REQUEST" {
		t.Errorf("PATCH kosong: dapat %d %v, seharusnya 400 BAD_REQUEST", status, body["code"])
	}
}

func TestDeleteStudent(t *testing.T) {
	env := newTestEnv(t)

	status, body := env.do(t, user(adminID, "admin"), http.MethodDelete, "/api/v1/students/1", "")
	if status != http.StatusNoContent {
		t.Fatalf("status = %d, seharusnya 204 (body: %v)", status, body)
	}

	status, body = env.do(t, user(adminID, "admin"), http.MethodGet, "/api/v1/students/1", "")
	if status != http.StatusNotFound {
		t.Errorf("GET setelah DELETE: dapat %d, seharusnya 404 (body: %v)", status, body)
	}
}
