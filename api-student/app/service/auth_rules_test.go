package service

import (
	"testing"

	"api-student/app/model"
)

func TestCheckPasswordStrength(t *testing.T) {
	cases := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"terlalu pendek", "abc123", true},
		{"tanpa angka", "passwordsaja", true},
		{"tanpa huruf", "12345678", true},
		{"password umum", "password123", true},
		{"valid", "rahasia123", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := checkPasswordStrength(tc.password)
			if tc.wantErr && msg == "" {
				t.Errorf("%q: seharusnya ditolak, tapi lolos", tc.password)
			}
			if !tc.wantErr && msg != "" {
				t.Errorf("%q: seharusnya lolos, tapi ditolak: %s", tc.password, msg)
			}
		})
	}
}

func TestIsValidUsername(t *testing.T) {
	valid := []string{"raka", "raka_123", "raka.razzani"}
	invalid := []string{"raka razzani", "raka!", "raka@kampus"}

	for _, u := range valid {
		if !isValidUsername(u) {
			t.Errorf("%q seharusnya valid", u)
		}
	}
	for _, u := range invalid {
		if isValidUsername(u) {
			t.Errorf("%q seharusnya tidak valid", u)
		}
	}
}

func TestValidateRegister(t *testing.T) {
	req := model.RegisterRequest{Username: "ab", Email: "bukan-email", Password: "123"}
	errs := ValidateRegister(req)

	if _, ok := errs["username"]; !ok {
		t.Error("username terlalu pendek seharusnya menghasilkan error")
	}
	if _, ok := errs["email"]; !ok {
		t.Error("email tidak valid seharusnya menghasilkan error")
	}
	if _, ok := errs["password"]; !ok {
		t.Error("password lemah seharusnya menghasilkan error")
	}
}