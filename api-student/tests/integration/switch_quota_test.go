//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"api-student/app/model"
)

// D.2 butir 1: pembimbing tujuan sudah penuh.
//
// Yang diperiksa bukan hanya status 409, tetapi juga bahwa transaksi
// benar-benar utuh: mahasiswa tetap di pembimbing asal, hitungan kedua
// pembimbing tidak berubah, dan tidak ada event kuota di outbox.
func TestSwitchEndpoint_TargetFullKeepsSourceSupervisor(t *testing.T) {
	a := newAPI(t)
	owner := seedUser(t)
	otherOwner := seedUser(t)
	student := seedStudent(t, owner.ID)
	other := seedStudent(t, otherOwner.ID)
	source := seedSupervisor(t, 5)
	target := seedSupervisor(t, 1) // hanya satu slot

	if status, code := a.assign(t, source.ID, student.ID); status != http.StatusCreated {
		t.Fatalf("tetapkan pembimbing asal: dapat %d %s, seharusnya 201", status, code)
	}
	if status, code := a.assign(t, target.ID, other.ID); status != http.StatusCreated {
		t.Fatalf("isi pembimbing tujuan: dapat %d %s, seharusnya 201", status, code)
	}

	body := `{"student_id": ` + strconv.Itoa(student.ID) +
		`, "supervisor_id": ` + strconv.Itoa(target.ID) + `}`
	status, parsed := a.call(t, admin(), http.MethodPost, "/api/v1/supervisions/switch", body)
	if status != http.StatusConflict || parsed["code"] != "SUPERVISOR_FULL" {
		t.Fatalf("switch: dapat %d %v, seharusnya 409 SUPERVISOR_FULL", status, parsed["code"])
	}

	var currentSupervisor int
	if err := pool.QueryRow(context.Background(),
		`SELECT supervisor_id FROM supervisions WHERE student_id = $1`, student.ID).
		Scan(&currentSupervisor); err != nil {
		t.Fatalf("membaca penetapan mahasiswa: %v", err)
	}
	if currentSupervisor != source.ID {
		t.Errorf("mahasiswa berpindah ke pembimbing %d, seharusnya tetap %d",
			currentSupervisor, source.ID)
	}

	sourceCount := countRows(t, `SELECT supervised_count FROM supervisors WHERE id = $1`, source.ID)
	if sourceCount != 1 {
		t.Errorf("supervised_count pembimbing asal = %d, seharusnya 1", sourceCount)
	}
	targetCount := countRows(t, `SELECT supervised_count FROM supervisors WHERE id = $1`, target.ID)
	if targetCount != 1 {
		t.Errorf("supervised_count pembimbing tujuan = %d, seharusnya 1", targetCount)
	}

	events := countRows(t,
		`SELECT COUNT(*) FROM outbox WHERE event_type = $1
		   AND (payload->>'supervisor_id')::INT IN ($2, $3)`,
		model.EventQuotaChanged, source.ID, target.ID)
	if events != 0 {
		t.Errorf("event kuota = %d, seharusnya 0 karena switch gagal", events)
	}
}

// D.2 butir 2: perubahan kuota yang berhasil.
//
// Selain status dan nilai kolom, test ini membaca isi event di outbox.
// Event adalah kontrak dengan worker: bila payload menyebut kuota yang
// salah, email yang terkirim juga salah walaupun kolom database benar.
func TestUpdateQuotaEndpoint_WritesCorrectEventPayload(t *testing.T) {
	a := newAPI(t)
	supervisor := seedSupervisor(t, 5)

	body := `{"quota": 2, "version": 1}`
	status, parsed := a.call(t, admin(), http.MethodPatch,
		"/api/v1/supervisors/"+strconv.Itoa(supervisor.ID), body)
	if status != http.StatusOK {
		t.Fatalf("update kuota: dapat %d %v, seharusnya 200", status, parsed)
	}

	var quota, version int
	if err := pool.QueryRow(context.Background(),
		`SELECT quota, version FROM supervisors WHERE id = $1`, supervisor.ID).
		Scan(&quota, &version); err != nil {
		t.Fatalf("membaca pembimbing: %v", err)
	}
	if quota != 2 {
		t.Errorf("quota = %d, seharusnya 2", quota)
	}
	if version != 2 {
		t.Errorf("version = %d, seharusnya 2 setelah satu perubahan", version)
	}

	var raw []byte
	err := pool.QueryRow(context.Background(),
		`SELECT payload FROM outbox
		 WHERE event_type = $1 AND (payload->>'supervisor_id')::INT = $2
		 ORDER BY id DESC LIMIT 1`,
		model.EventQuotaChanged, supervisor.ID).Scan(&raw)
	if err != nil {
		t.Fatalf("event outbox tidak ditemukan: %v", err)
	}

	var event model.QuotaChangedEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatalf("payload event bukan JSON yang sah: %v", err)
	}
	if event.SupervisorID != supervisor.ID {
		t.Errorf("supervisor_id = %d, seharusnya %d", event.SupervisorID, supervisor.ID)
	}
	if event.SupervisorName != supervisor.Name {
		t.Errorf("supervisor_name = %q, seharusnya %q", event.SupervisorName, supervisor.Name)
	}
	if event.Quota != 2 {
		t.Errorf("quota pada event = %d, seharusnya 2 (nilai BARU)", event.Quota)
	}
}
