//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"latihan-fiber/app/model"
)

// call mengirim request JSON atas nama student dan mengembalikan status
// code beserta field "code" pada response.
func (a api) call(t *testing.T, student model.User, method, path, body string) (int, string) {
	t.Helper()

	token, err := a.jwt.GenerateAccess(student)
	if err != nil {
		t.Fatalf("membuat token: %v", err)
	}

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	var parsed struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&parsed)
	return resp.StatusCode, parsed.Code
}

// D.2 butir 1: kelas tujuan sudah penuh.
//
// Yang diperiksa bukan hanya status 409, tetapi juga bahwa transaksi
// benar-benar utuh: mahasiswa tetap di kelas asal, hitungan kedua kelas
// tidak berubah, dan tidak ada event enrollment.swapped di outbox.
func TestSwapEndpoint_TargetFullKeepsSourceEnrollment(t *testing.T) {
	a := newAPI(t)
	student := seedStudent(t)
	other := seedStudent(t)
	from := seedCourse(t, 3, 40)
	target := seedCourse(t, 3, 1) // hanya satu kursi

	if status, code := a.enroll(t, student, from.ID); status != http.StatusCreated {
		t.Fatalf("daftar kelas asal: dapat %d %s, seharusnya 201", status, code)
	}
	if status, code := a.enroll(t, other, target.ID); status != http.StatusCreated {
		t.Fatalf("isi kelas tujuan: dapat %d %s, seharusnya 201", status, code)
	}

	body := `{"from_course_id": ` + strconv.Itoa(from.ID) + `, "to_course_id": ` + strconv.Itoa(target.ID) + `}`
	status, code := a.call(t, student, http.MethodPost, "/api/v1/enrollments/swap", body)
	if status != http.StatusConflict || code != "COURSE_FULL" {
		t.Fatalf("swap: dapat %d %s, seharusnya 409 COURSE_FULL", status, code)
	}

	stillEnrolled := countRows(t,
		`SELECT COUNT(*) FROM enrollments WHERE course_id = $1 AND user_id = $2`,
		from.ID, student.ID)
	if stillEnrolled != 1 {
		t.Errorf("mahasiswa kehilangan kelas asal: baris = %d, seharusnya 1", stillEnrolled)
	}
	moved := countRows(t,
		`SELECT COUNT(*) FROM enrollments WHERE course_id = $1 AND user_id = $2`,
		target.ID, student.ID)
	if moved != 0 {
		t.Errorf("mahasiswa tercatat di kelas tujuan: baris = %d, seharusnya 0", moved)
	}

	fromCount := countRows(t, `SELECT enrolled_count FROM courses WHERE id = $1`, from.ID)
	if fromCount != 1 {
		t.Errorf("enrolled_count kelas asal = %d, seharusnya 1", fromCount)
	}
	targetCount := countRows(t, `SELECT enrolled_count FROM courses WHERE id = $1`, target.ID)
	if targetCount != 1 {
		t.Errorf("enrolled_count kelas tujuan = %d, seharusnya 1", targetCount)
	}

	swapped := countRows(t,
		`SELECT COUNT(*) FROM outbox WHERE (payload->>'user_id')::INT = $1 AND event_type = $2`,
		student.ID, model.EventEnrollmentSwapped)
	if swapped != 0 {
		t.Errorf("event enrollment.swapped = %d, seharusnya 0 karena swap gagal", swapped)
	}
}

// D.2 butir 2: berhasil keluar dari kelas.
func TestDropEndpoint_RemovesEnrollmentAndWritesOutbox(t *testing.T) {
	a := newAPI(t)
	student := seedStudent(t)
	course := seedCourse(t, 3, 40)

	if status, code := a.enroll(t, student, course.ID); status != http.StatusCreated {
		t.Fatalf("daftar kelas: dapat %d %s, seharusnya 201", status, code)
	}

	status, code := a.call(t, student, http.MethodDelete,
		"/api/v1/courses/"+strconv.Itoa(course.ID)+"/enroll", "")
	if status != http.StatusNoContent {
		t.Fatalf("drop: dapat %d %s, seharusnya 204", status, code)
	}

	enrolled := countRows(t,
		`SELECT COUNT(*) FROM enrollments WHERE course_id = $1 AND user_id = $2`,
		course.ID, student.ID)
	if enrolled != 0 {
		t.Errorf("baris enrollment = %d, seharusnya 0", enrolled)
	}

	count := countRows(t, `SELECT enrolled_count FROM courses WHERE id = $1`, course.ID)
	if count != 0 {
		t.Errorf("enrolled_count = %d, seharusnya 0", count)
	}

	dropped := countRows(t,
		`SELECT COUNT(*) FROM outbox WHERE (payload->>'user_id')::INT = $1 AND event_type = $2`,
		student.ID, model.EventEnrollmentDropped)
	if dropped != 1 {
		t.Errorf("event enrollment.dropped = %d, seharusnya 1", dropped)
	}
}
