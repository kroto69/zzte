package domain

import "testing"

// Same guard as the adapter path: out-of-range converted values are
// "no reading" (0.0), never an impossible dBm figure.
func TestConvertPower(t *testing.T) {
	cases := []struct {
		name string
		raw  int
		want float64
	}{
		{"normal RX", 8150, -18.5},
		{"normal TX", 10500, 5.0},
		{"zero dBm", 10000, 0.0},
		{"near-sentinel garbage", 65308, 0.0},
		{"sentinel 65535", 65535, 0.0},
		{"sentinel 0", 0, 0.0},
		{"upper boundary valid", 11000, 10.0},
		{"just above upper boundary", 11001, 0.0},
		{"lower boundary valid", 5000, -50.0},
		{"just below lower boundary", 4999, 0.0},
	}
	for _, tc := range cases {
		if got := ConvertPower(tc.raw); got != tc.want {
			t.Errorf("%s: ConvertPower(%d) = %v, want %v", tc.name, tc.raw, got, tc.want)
		}
	}
}
