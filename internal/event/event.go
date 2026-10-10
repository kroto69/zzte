// Package event: jalur event-driven untuk status ONU.
//
// Prinsip #1: JANGAN BEBANI OLT. OLT hanya "didorong" mengirim syslog saat ada
// kejadian (up/down). Tidak ada polling status di jalur ini — OLT tidak pernah
// ditanya, hanya didengarkan. Beban CPU/memory OLT praktis nol.
//
// Prinsip #2: multi-OLT sejak awal. Setiap event membawa OLTID yang di-resolve
// dari IP pengirim (lihat mapper.go).
package event

import "time"

// Kind jenis event.
type Kind string

const (
	KindUp   Kind = "up"
	KindDown Kind = "down"
)

// Cause penyebab down (diklasifikasikan dari alarm ZTE).
const (
	CausePower   = "power"   // dying-gasp: adaptor dicabut / listrik padam
	CauseLOS     = "los"     // sinyal optik hilang: fiber / konektor
	CauseMass    = "mass"    // bagian dari mass-down satu PON port
	CauseUnknown = "unknown"
)

// Event satu kejadian ONU.
type Event struct {
	OLTID      string    `json:"oltId"`
	Board      int       `json:"board"`
	Pon        int       `json:"pon"`
	OnuID      int       `json:"onuId"`
	Kind       Kind      `json:"kind"`
	Cause      string    `json:"cause"`
	Count      int       `json:"count,omitempty"` // untuk cause=mass: jumlah ONU
	Raw        string    `json:"raw,omitempty"`   // baris syslog asli (dipotong)
	SourceIP   string    `json:"sourceIp,omitempty"`
	ReceivedAt time.Time `json:"receivedAt"`
}

// Key untuk dedup: ONU + jenis event.
func (e Event) dedupKey() string {
	return e.OLTID + "/" + itoa(e.Board) + "/" + itoa(e.Pon) + ":" + itoa(e.OnuID) + ":" + string(e.Kind)
}

// PonKey untuk agregasi mass-down.
func (e Event) ponKey() string {
	return e.OLTID + "/" + itoa(e.Board) + "/" + itoa(e.Pon)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
