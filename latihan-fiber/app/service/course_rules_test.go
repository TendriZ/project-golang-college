package service

import "testing"

func TestWithinCreditLimit(t *testing.T) {
	cases := []struct {
		taken  int
		adding int
		want   bool
	}{
		{0, 3, true},
		{21, 3, true},
		{22, 3, false},
		{24, 0, true},
		{24, 3, false},
	}

	for _, tc := range cases {
		if got := WithinCreditLimit(tc.taken, tc.adding); got != tc.want {
			t.Errorf(
				"taken=%d adding=%d: harap %v, dapat %v",
				tc.taken,
				tc.adding,
				tc.want,
				got,
			)
		}
	}
}

func TestCreditsAfterSwap(t *testing.T) {
	cases := []struct {
		taken   int
		leaving int
		joining int
		want    int
	}{
		{21, 3, 3, 21},
		{20, 3, 4, 21},
		{24, 3, 3, 24},
		{12, 2, 5, 15},
	}

	for _, tc := range cases {
		if got := CreditsAfterSwap(tc.taken, tc.leaving, tc.joining); got != tc.want {
			t.Errorf(
				"taken=%d leaving=%d joining=%d: harap %d, dapat %d",
				tc.taken,
				tc.leaving,
				tc.joining,
				tc.want,
				got,
			)
		}
	}
}
