package snmp

import "testing"

// Regression test: a dead/LOS ONU makes the OLT return near-sentinel raw
// values (e.g. 65308). Before the fix, (65308-15000)/500 = +100.616 dBm was
// returned as a real reading; it must be treated as "no reading" (0.0).
func TestConvertPower(t *testing.T) {
	adapters := map[string]FirmwareAdapter{
		"v1": &V1Adapter{},
		"v2": &V2Adapter{},
	}
	cases := []struct {
		name string
		raw  int
		want float64
	}{
		{"normal RX", 5750, -18.5},
		{"normal TX", 17500, 5.0},
		{"zero dBm", 15000, 0.0},
		{"near-sentinel garbage (reported bug)", 65308, 0.0},
		{"sentinel 65535", 65535, 0.0},
		{"sentinel 0", 0, 0.0},
		{"upper boundary valid", 20000, 10.0},
		{"just above upper boundary", 20001, 0.0},
		{"lower boundary valid", -10000, -50.0},
		{"just below lower boundary", -10001, 0.0},
	}
	for _, tc := range cases {
		for ver, a := range adapters {
			if got := a.ConvertPower(tc.raw); got != tc.want {
				t.Errorf("%s %s: ConvertPower(%d) = %v, want %v", ver, tc.name, tc.raw, got, tc.want)
			}
		}
	}
}
