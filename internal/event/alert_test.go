package event

import (
	"testing"
	"time"
)

func downEv(olt string, board, pon, onu int, cause string) Event {
	return Event{
		OLTID:      olt,
		Board:      board,
		Pon:        pon,
		OnuID:      onu,
		Kind:       KindDown,
		Cause:      cause,
		ReceivedAt: time.Now(),
	}
}

func testEngine() (*Engine, *[]Alert) {
	var got []Alert
	e := NewEngine(AlertConfig{
		Cooldown:      60 * time.Minute,
		AggWindow:     90 * time.Second,
		MassThreshold: 3,
	}, nil)
	e.onEmit = func(a Alert) { got = append(got, a) }
	return e, &got
}

// Tanpa verifier: perilaku lama — semua event jadi alert.
func TestFlushKeyTanpaVerifier(t *testing.T) {
	e, got := testEngine()
	e.add(downEv("olt1", 1, 1, 5, CauseLOS))
	e.add(downEv("olt1", 1, 1, 6, CausePower))
	e.flushKey("olt1/1/1")
	if len(*got) != 2 {
		t.Fatalf("mau 2 alert, dapat %d", len(*got))
	}
}

// Verifier bilang onu 5 sudah online -> alert-nya dibuang, onu 6 tetap.
func TestReverifyBuangYangSudahOnline(t *testing.T) {
	e, got := testEngine()
	e.SetVerifier(func(ev Event) bool {
		return ev.OnuID != 5 // 5 sudah kembali online
	})
	e.add(downEv("olt1", 1, 1, 5, CauseLOS))
	e.add(downEv("olt1", 1, 1, 6, CauseLOS))
	e.flushKey("olt1/1/1")
	if len(*got) != 1 {
		t.Fatalf("mau 1 alert, dapat %d", len(*got))
	}
	if (*got)[0].OnuID != 6 {
		t.Fatalf("mau alert untuk onu 6, dapat onu %d", (*got)[0].OnuID)
	}
}

// Semua sudah online -> tidak ada alert sama sekali.
func TestReverifySemuaOnlineTidakAdaAlert(t *testing.T) {
	e, got := testEngine()
	e.SetVerifier(func(ev Event) bool { return false })
	e.add(downEv("olt1", 1, 1, 5, CauseLOS))
	e.add(downEv("olt1", 1, 1, 6, CauseLOS))
	e.flushKey("olt1/1/1")
	if len(*got) != 0 {
		t.Fatalf("mau 0 alert, dapat %d", len(*got))
	}
}

// Mass-down: sebagian kembali online, sisa di bawah threshold ->
// jadi alert per-ONU untuk yang masih down (bukan mass-down).
func TestReverifyMassDownTurunJadiSingles(t *testing.T) {
	e, got := testEngine() // threshold 3
	e.SetVerifier(func(ev Event) bool {
		return ev.OnuID <= 2 // 3 dan 4 sudah online
	})
	for i := 1; i <= 4; i++ {
		e.add(downEv("olt1", 1, 1, i, CauseLOS))
	}
	e.flushKey("olt1/1/1")
	if len(*got) != 2 {
		t.Fatalf("mau 2 alert single, dapat %d", len(*got))
	}
	for _, a := range *got {
		if a.Kind != "down" {
			t.Fatalf("mau kind=down, dapat %q", a.Kind)
		}
	}
}

// Mass-down asli: semua masih down -> satu alert mass-down.
// (early flush saat threshold tercapai; tidak perlu flushKey manual)
func TestReverifyMassDownAsli(t *testing.T) {
	e, got := testEngine() // threshold 3
	e.SetVerifier(func(ev Event) bool { return true })
	for i := 1; i <= 3; i++ {
		e.add(downEv("olt1", 1, 1, i, CauseLOS))
	}
	if len(*got) != 1 {
		t.Fatalf("mau 1 alert, dapat %d", len(*got))
	}
	if (*got)[0].Kind != "mass-down" || (*got)[0].Count != 3 {
		t.Fatalf("mau mass-down count=3, dapat %+v", (*got)[0])
	}
}

// SetVerifier(nil) menonaktifkan verifikasi -> perilaku lama.
func TestReverifyNilNonaktif(t *testing.T) {
	e, got := testEngine()
	e.SetVerifier(func(ev Event) bool { return false })
	e.SetVerifier(nil)
	e.add(downEv("olt1", 1, 1, 5, CauseLOS))
	e.flushKey("olt1/1/1")
	if len(*got) != 1 {
		t.Fatalf("mau 1 alert, dapat %d", len(*got))
	}
}
