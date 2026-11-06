package snmp

import "testing"

// Regression test: the TX power column is .14, not .9. Column .9 returns
// noSuchInstance for every ONU, which made TX Power show 0 dBm for all ONUs.
// Column .14 is live-verified on C300/C320 V2.1.0
// (megadata-dev/go-snmp-olt-zte-c320): same table, same instance encoding,
// e.g. tx_power 2.534 dBm. RX stays on column .10 (proven against NetNumen).
func TestPowerOIDs(t *testing.T) {
	if OIDONUTXPower != OIDBase+".3.50.12.1.1.14" {
		t.Errorf("OIDONUTXPower = %s, want %s", OIDONUTXPower, OIDBase+".3.50.12.1.1.14")
	}
	if OIDONURXPower != OIDBase+".3.50.12.1.1.10" {
		t.Errorf("OIDONURXPower = %s, want %s", OIDONURXPower, OIDBase+".3.50.12.1.1.10")
	}
	if OIDONUTXPower == OIDONURXPower {
		t.Errorf("TX and RX power OIDs must differ, both are %s", OIDONUTXPower)
	}
}
