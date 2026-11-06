package service

import (
	"testing"
)

func fptr(v float64) *float64 { return &v }

func assertPower(t *testing.T, name string, got, want *float64) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s: got %v, want nil", name, *got)
		}
		return
	}
	if got == nil {
		t.Errorf("%s: got nil, want %v", name, *want)
		return
	}
	if *got != *want {
		t.Errorf("%s: got %v, want %v", name, *got, *want)
	}
}

func TestParsePowerAttenuation_LiveOutput(t *testing.T) {
	// Output asli dari OLT juragan (ONU board 2 pon 1 id 57).
	raw := `           OLT                  ONU              Attenuation
--------------------------------------------------------------------------
 up      Rx :-28.358(dbm)      Tx:2.468(dbm)        30.826(dB)     
 
 down    Tx :6.033(dbm)        Rx:-23.468(dbm)      29.501(dB)   `

	att := ParsePowerAttenuation(raw)

	assertPower(t, "OltRx (up Rx)", att.OltRx, fptr(-28.358))
	assertPower(t, "OnuTx (laser)", att.OnuTx, fptr(2.468))
	assertPower(t, "AttUp", att.AttUp, fptr(30.826))
	assertPower(t, "OltTx (down Tx)", att.OltTx, fptr(6.033))
	assertPower(t, "OnuRx (down Rx)", att.OnuRx, fptr(-23.468))
	assertPower(t, "AttDown", att.AttDown, fptr(29.501))
}

func TestParsePowerAttenuation_NoSignal(t *testing.T) {
	// ONU tanpa sinyal: up Rx / down Rx "no signal" / "N/A",
	// tapi down Tx OLT tetap terbaca.
	raw := `           OLT                  ONU              Attenuation
--------------------------------------------------------------------------
 up      Rx :no signal Tx:N/A N/A
 down    Tx :6.892(dbm) Rx:N/A N/A`

	att := ParsePowerAttenuation(raw)

	assertPower(t, "OltRx", att.OltRx, nil)
	assertPower(t, "OnuTx", att.OnuTx, nil)
	assertPower(t, "AttUp", att.AttUp, nil)
	assertPower(t, "OltTx", att.OltTx, fptr(6.892))
	assertPower(t, "OnuRx", att.OnuRx, nil)
	assertPower(t, "AttDown", att.AttDown, nil)
}

func TestParsePowerAttenuation_Garbage(t *testing.T) {
	for _, raw := range []string{"", "hello\nworld", " up\n down\n"} {
		att := ParsePowerAttenuation(raw)
		assertPower(t, "OltRx", att.OltRx, nil)
		assertPower(t, "OnuTx", att.OnuTx, nil)
		assertPower(t, "OltTx", att.OltTx, nil)
		assertPower(t, "OnuRx", att.OnuRx, nil)
		assertPower(t, "AttUp", att.AttUp, nil)
		assertPower(t, "AttDown", att.AttDown, nil)
	}
}
