package service

import (
	"testing"

	"api-student/app/model"
	"api-student/helper"
)

func TestValidateRegister(t *testing.T) {
	req := model.RegisterRequest{Username: "ab", Email: "bukan-email", Password: "123"}
	errs := helper.ValidateStruct(req)

	if _, ok := errs["username"]; !ok {
		t.Error("username terlalu pendek seharusnya menghasilkan error")
	}
	if _, ok := errs["email"]; !ok {
		t.Error("email tidak valid seharusnya menghasilkan error")
	}
	if _, ok := errs["password"]; !ok {
		t.Error("password lemah seharusnya menghasilkan error")
	}

	valid := model.RegisterRequest{Username: "sari", Email: "sari@unair.ac.id", Password: "rahasia123"}
	if errs := helper.ValidateStruct(valid); len(errs) != 0 {
		t.Errorf("registrasi valid seharusnya lolos, dapat: %v", errs)
	}
}

func TestPasswordStrength(t *testing.T) {
	cases := []struct {
		name     string
		password string
		wantErr  string // "" berarti harus lolos
	}{
		{"terlalu pendek", "abc", "minimal 8 karakter"},
		{"tanpa angka", "passwordsaja", "harus memuat huruf dan angka"},
		{"tanpa huruf", "12345678", "harus memuat huruf dan angka"},
		{"password umum", "password123", "password terlalu umum"},
		{"valid", "rahasia123", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := model.RegisterRequest{Username: "sari", Email: "sari@unair.ac.id", Password: tc.password}
			errs := helper.ValidateStruct(req)

			if tc.wantErr == "" && len(errs) != 0 {
				t.Errorf("%q: seharusnya lolos, tapi ditolak: %v", tc.password, errs)
			}
			if tc.wantErr != "" && errs["password"] != tc.wantErr {
				t.Errorf("%q: harap pesan %q, dapat %q", tc.password, tc.wantErr, errs["password"])
			}
		})
	}
}

func TestUsernameTag(t *testing.T) {
	valid := []string{"raka", "raka_123", "raka.razzani"}
	invalid := []string{"raka razzani", "raka!", "raka@kampus"}

	for _, u := range valid {
		req := model.RegisterRequest{Username: u, Email: "a@unair.ac.id", Password: "rahasia123"}
		if errs := helper.ValidateStruct(req); errs["username"] != "" {
			t.Errorf("%q seharusnya valid, dapat: %v", u, errs)
		}
	}
	for _, u := range invalid {
		req := model.RegisterRequest{Username: u, Email: "a@unair.ac.id", Password: "rahasia123"}
		if errs := helper.ValidateStruct(req); errs["username"] == "" {
			t.Errorf("%q seharusnya tidak valid", u)
		}
	}
}
