package service

import (
	"slices"
	"testing"
)

func TestNormalizeTags(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "huruf besar dan spasi di tepi", in: []string{" Go ", "DOCKER"}, want: []string{"go", "docker"}},
		{name: "duplikat setelah normalisasi", in: []string{"React", "react", " REACT "}, want: []string{"react"}},
		{name: "string kosong dibuang", in: []string{"", "   ", "postgres"}, want: []string{"postgres"}},
		{name: "urutan kemunculan pertama dipertahankan", in: []string{"b", "a", "B"}, want: []string{"b", "a"}},
		{name: "input kosong", in: []string{}, want: []string{}},
		{name: "input nil", in: nil, want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeTags(tt.in)
			if !slices.Equal(got, tt.want) {
				t.Errorf("NormalizeTags(%q) = %q, seharusnya %q", tt.in, got, tt.want)
			}
			// Hasil tidak boleh nil: response JSON harus berisi [] bukan null.
			if got == nil {
				t.Errorf("NormalizeTags(%q) mengembalikan nil", tt.in)
			}
		})
	}
}
