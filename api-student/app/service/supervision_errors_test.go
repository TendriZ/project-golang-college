package service

// White-box test: file ini memakai package service, bukan service_test,
// karena translateSupervisionError sengaja tidak di-export. Fungsi kecil
// inilah yang memutuskan status code setiap kegagalan repository.

import (
	"errors"
	"fmt"
	"testing"

	"api-student/app/repository"
	"api-student/helper"
)

func TestTranslateSupervisionError(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{repository.ErrNotFound, 404, helper.CodeNotFound},
		{repository.ErrSupervisorFull, 409, "SUPERVISOR_FULL"},
		{repository.ErrAlreadySupervised, 409, "ALREADY_SUPERVISED"},
		{repository.ErrNotSupervised, 409, "NOT_SUPERVISED"},
		{repository.ErrSameSupervisor, 409, "SAME_SUPERVISOR"},
		{repository.ErrVersionConflict, 409, "VERSION_CONFLICT"},
		{repository.ErrQuotaBelowSupervised, 409, "QUOTA_BELOW_SUPERVISED"},
		{fmt.Errorf("lapisan luar: %w", repository.ErrSupervisorFull), 409, "SUPERVISOR_FULL"},
		{errors.New("koneksi PostgreSQL putus"), 500, helper.CodeInternal},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			var appErr *helper.AppError
			if !errors.As(translateSupervisionError(tt.err), &appErr) {
				t.Fatalf("hasil bukan *helper.AppError")
			}
			if appErr.Status != tt.wantStatus || appErr.Code != tt.wantCode {
				t.Errorf("dapat %d %s, seharusnya %d %s",
					appErr.Status, appErr.Code, tt.wantStatus, tt.wantCode)
			}
		})
	}
}
