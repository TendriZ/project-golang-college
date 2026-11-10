package service

import (
	"reflect"
	"testing"
)

func TestNormalizeTags(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "huruf kecil, trim, dan duplikat dibuang",
			in:   []string{"Tugas-Banyak", " dosen-jelas ", "tugas-banyak"},
			want: []string{"tugas-banyak", "dosen-jelas"},
		},
		{
			name: "string kosong dibuang",
			in:   []string{"", "  ", "Praktikal"},
			want: []string{"praktikal"},
		},
		{
			name: "urutan pertama dipertahankan",
			in:   []string{"seru", "data", "Seru"},
			want: []string{"seru", "data"},
		},
		{
			name: "slice kosong menghasilkan slice kosong bukan nil",
			in:   nil,
			want: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeTags(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("NormalizeTags(%v) = %#v, harap %#v", tc.in, got, tc.want)
			}
		})
	}
}
