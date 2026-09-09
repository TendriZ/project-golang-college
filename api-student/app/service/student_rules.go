package service

import (
	"strings"
	"api-student/app/model"
)

// ValidateCreate memeriksa isian POST
func ValidateCreate(req model.CreateStudentRequest) map[string]string {
	errs := map[string]string{}
	if strings.TrimSpace(req.Name) == "" {
		errs["name"] = "nama wajib diisi"
	}
	if req.NIM <= 0 {
		errs["nim"] = "nim wajib diisi dan berupa angka positif"
	}
	if req.Grade < 0 || req.Grade > 100 {
		errs["grade"] = "grade harus di antara 0 dan 100"
	}
	return errs
}

// ValidateReplace memeriksa isian PUT
// Seluruh field wajib ada karena PUT mengganti isi secara keseluruhan
func ValidateReplace(req model.ReplaceStudentRequest) map[string]string {
	errs := map[string]string{}
	if strings.TrimSpace(req.Name) == "" {
		errs["name"] = "nama wajib diisi pada PUT"
	}
	if req.NIM <= 0 {
		errs["nim"] = "nim wajib diisi pada PUT"
	}
	if req.Grade < 0 || req.Grade > 100 {
		errs["grade"] = "grade harus di antara 0 dan 100"
	}
	return errs
}

// ApplyPatch menyalin field yang dikirim ke data yang sudah ada
// Field yang bernilai nil dibiarkan apa adanya
func ApplyPatch(current model.Student, req model.PatchStudentRequest) (model.Student, map[string]string) {
	errs := map[string]string{}
	
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			errs["name"] = "tidak boleh kosong"
		} else {
			current.Name = *req.Name
		}
	}
	if req.NIM != nil {
		if *req.NIM <= 0 {
			errs["nim"] = "harus berupa angka positif"
		} else {
			current.NIM = *req.NIM
		}
	}
	if req.Grade != nil {
		if *req.Grade < 0 || *req.Grade > 100 {
			errs["grade"] = "harus di antara 0 dan 100"
		} else {
			current.Grade = *req.Grade
		}
	}
	if req.IsActive != nil {
		current.IsActive = *req.IsActive
	}
	
	return current, errs
}