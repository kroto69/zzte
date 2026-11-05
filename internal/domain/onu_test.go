package domain

import "testing"

// Same conversion as the adapters: signed 16-bit raw, formula
// (signed16(raw) - 15000) / 500, sanitized to [-50, +10] dBm.
// Proven against NetNumen: raw 65308 (=-228) is a real -30.456 dBm reading.
func TestConvertPower(t *testing.T) {
	cases := []struct {
		name string
		raw  int
		want float64
	}{
		{"normal RX", 5750, -18.5},
		{"normal TX", 17500, 5.0},
		{"zero dBm", 15000, 0.0},
		{"negative reading (NetNumen -30)", 65308, -30.456},
		{"negative reading, signed input", -228, -30.456},
		{"sentinel 65535", 65535, 0.0},
		{"sentinel 0", 0, 0.0},
		{"upper boundary valid", 20000, 10.0},
		{"just above upper boundary", 20001, 0.0},
		{"lower boundary valid", -10000, -50.0},
		{"just below lower boundary", -10001, 0.0},
		{"out-of-range garbage", 40000, 0.0},
	}
	for _, tc := range cases {
		if got := ConvertPower(tc.raw); got != tc.want {
			t.Errorf("%s: ConvertPower(%d) = %v, want %v", tc.name, tc.raw, got, tc.want)
		}
	}
}
