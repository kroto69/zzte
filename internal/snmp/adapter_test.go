package snmp

import "testing"

// The OLT reports optical power as signed 16-bit two's complement, but the
// SNMP value arrives unsigned: a real reading of -30.456 dBm comes back as
// raw 65308 (=-228). Treating it as unsigned produced +100.616 dBm.
// Proven against NetNumen on a live OLT (~-30 dBm for the same ONU).
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
		{"negative reading (NetNumen -30)", 65308, -30.456},
		{"negative reading, signed input", -228, -30.456},
		{"negative reading 2", 65280, -30.512},
		{"sentinel 65535", 65535, 0.0},
		{"sentinel 0", 0, 0.0},
		{"upper boundary valid", 20000, 10.0},
		{"just above upper boundary", 20001, 0.0},
		{"lower boundary valid", -10000, -50.0},
		{"just below lower boundary", -10001, 0.0},
		{"out-of-range garbage", 40000, 0.0},
	}
	for _, tc := range cases {
		for ver, a := range adapters {
			if got := a.ConvertPower(tc.raw); got != tc.want {
				t.Errorf("%s %s: ConvertPower(%d) = %v, want %v", ver, tc.name, tc.raw, got, tc.want)
			}
		}
	}
}
