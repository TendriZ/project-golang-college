package service

import (
	"testing"
	"api-student/app/model"
)

func TestValidateCreate(t *testing.T) {
	// Menguji input yang salah semua
	req := model.CreateStudentRequest{Name: "", NIM: 0, Grade: -10}
	errs := ValidateCreate(req)
	
	if len(errs) != 3 {
		t.Errorf("harap 3 error, dapat %d", len(errs))
	}
}

func TestValidateReplace(t *testing.T) {
	// Menguji input yang valid
	req := model.ReplaceStudentRequest{Name: "Sari", NIM: 220001, Grade: 95.5, IsActive: true}
	errs := ValidateReplace(req)
	
	if len(errs) != 0 {
		t.Errorf("tidak seharusnya ada error untuk data valid, dapat: %v", errs)
	}
}

func TestApplyPatch(t *testing.T) {
	initial := model.Student{ID: 1, NIM: 220001, Name: "Sari", Grade: 90, IsActive: true}
	
	newName := ""
	newGrade := 95.0
	
	// Test error: nama dikosongkan pada PATCH
	_, errs := ApplyPatch(initial, model.PatchStudentRequest{Name: &newName})
	if len(errs) == 0 {
		t.Error("seharusnya ada error saat nama dikosongkan")
	}
	
	// Test sukses: update sebagian (grade saja)
	result, errs2 := ApplyPatch(initial, model.PatchStudentRequest{Grade: &newGrade})
	if len(errs2) != 0 {
		t.Errorf("tidak seharusnya ada error, dapat: %v", errs2)
	}
	if result.Grade != 95.0 {
		t.Errorf("grade seharusnya berubah menjadi 95.0, dapat %f", result.Grade)
	}
	if result.Name != "Sari" {
		t.Error("field name yang tidak dikirim seharusnya tidak berubah")
	}
}