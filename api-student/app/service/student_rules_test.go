package service

import (
	"testing"

	"api-student/app/model"
	"api-student/helper"
)

func TestValidateCreate(t *testing.T) {
	// Menguji input yang salah semua — kini lewat tag deklaratif.
	req := model.CreateStudentRequest{Name: "", NIM: 0, Grade: -10}
	errs := helper.ValidateStruct(req)

	if len(errs) != 3 {
		t.Errorf("harap 3 error, dapat %d: %v", len(errs), errs)
	}
}

func TestValidateReplace(t *testing.T) {
	// Menguji input yang valid.
	req := model.ReplaceStudentRequest{Name: "Sari", NIM: 220001, Grade: 95.5, IsActive: true}
	if errs := helper.ValidateStruct(req); len(errs) != 0 {
		t.Errorf("tidak seharusnya ada error untuk data valid, dapat: %v", errs)
	}
}

func TestApplyPatch(t *testing.T) {
	initial := model.Student{ID: 1, NIM: 220001, Name: "Sari", Grade: 90, IsActive: true}

	newGrade := 95.0

	// ApplyPatch kini hanya menggabungkan, tanpa validasi.
	result := ApplyPatch(initial, model.PatchStudentRequest{Grade: &newGrade})
	if result.Grade != 95.0 {
		t.Errorf("grade seharusnya berubah menjadi 95.0, dapat %f", result.Grade)
	}
	if result.Name != "Sari" {
		t.Error("field name yang tidak dikirim seharusnya tidak berubah")
	}
}

func TestPatchValidation(t *testing.T) {
	// {"name":""} DITOLAK — omitnil tidak melewatkan non-nil.
	empty := ""
	if errs := helper.ValidateStruct(model.PatchStudentRequest{Name: &empty}); errs["name"] == "" {
		t.Error(`{"name":""} seharusnya ditolak`)
	}

	// Body tanpa name sama sekali DITERIMA (nil dilewati omitnil).
	grade := 90.0
	if errs := helper.ValidateStruct(model.PatchStudentRequest{Grade: &grade}); len(errs) != 0 {
		t.Errorf("patch tanpa name seharusnya lolos, dapat: %v", errs)
	}

	// Nama berangka DITOLAK oleh custom validation nodigit.
	digit := "Budi123"
	if errs := helper.ValidateStruct(model.PatchStudentRequest{Name: &digit}); errs["name"] == "" {
		t.Error(`nama "Budi123" seharusnya ditolak nodigit`)
	}
}

func TestCanAccessStudent(t *testing.T) {
	perms := helper.NewPermissionSet(map[string][]string{
		"admin": {"student:read:any", "student:update:any"},
		"user":  {},
	})

	owner := model.AuthUser{UserID: 7, Username: "sari", Role: "user"}
	stranger := model.AuthUser{UserID: 9, Username: "budi", Role: "user"}
	admin := model.AuthUser{UserID: 1, Username: "root", Role: "admin"}

	if !CanAccessStudent(owner, 7, perms, "student:read:any") {
		t.Error("pemilik seharusnya boleh mengakses datanya sendiri")
	}
	if CanAccessStudent(stranger, 7, perms, "student:read:any") {
		t.Error("user lain tanpa permission seharusnya ditolak")
	}
	if !CanAccessStudent(admin, 7, perms, "student:read:any") {
		t.Error("admin dengan permission :any seharusnya boleh")
	}
	if CanAccessStudent(stranger, 7, nil, "student:read:any") {
		t.Error("permission nil seharusnya fail closed")
	}
}
