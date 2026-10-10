package event

import (
	"regexp"
	"strings"
)

// Pola syslog ZTE. Format teks bisa beda antar firmware — pola yang tidak
// dikenal dikembalikan sebagai ok=false agar dicatat untuk kalibrasi,
// bukan di-drop diam-diam.
var (
	// gpon-onu_1/2/3:4, gpon_onu 1/2/3:4, onu 1/2/3:4 ...
	onuRe = regexp.MustCompile(`(?i)gpon[-_ ]?onu[_ ]?(\d+)/(\d+)/(\d+)[:_/](\d+)`)

	causeRes = []struct {
		cause string
		re    *regexp.Regexp
	}{
		{CausePower, regexp.MustCompile(`(?i)dying[-_ ]?gasp`)},
		{CauseLOS, regexp.MustCompile(`(?i)\blos\b|link[-_ ]?lost|\blofi\b|\bsdi\b`)},
	}

	downRes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)deregister`),
		regexp.MustCompile(`(?i)\boff ?line\b`),
		regexp.MustCompile(`(?i)\bdown\b`),
	}
	upRes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bon ?line\b`),
		regexp.MustCompile(`(?i)register.*success|authorized.*success`),
		regexp.MustCompile(`(?i)los recovery|ranging.*success`),
		regexp.MustCompile(`(?i)phase.*working`),
	}

	// Header syslog "<132>Oct  9 14:00:00 hostname ..." — buang agar regex fokus ke isi.
	headerRe = regexp.MustCompile(`^<\d+>\S*\s*`)
)

// Parsed hasil parse satu baris syslog.
type Parsed struct {
	Board, Pon, OnuID int
	Kind              Kind
	Cause             string
}

// ParseLine parse satu baris syslog ZTE. ok=false bila tidak dikenali.
func ParseLine(line string) (p Parsed, ok bool) {
	line = headerRe.ReplaceAllString(line, "")
	line = strings.TrimSpace(line)
	if line == "" {
		return p, false
	}
	m := onuRe.FindStringSubmatch(line)
	if m == nil {
		return p, false
	}
	p.Board = atoi(m[2])
	p.Pon = atoi(m[3])
	p.OnuID = atoi(m[4])
	// m[1] = shelf, jarang dipakai di ZTE C320 (selalu 1)

	p.Cause = CauseUnknown
	for _, cr := range causeRes {
		if cr.re.MatchString(line) {
			p.Cause = cr.cause
			break
		}
	}
	for _, re := range upRes {
		if re.MatchString(line) {
			p.Kind = KindUp
			p.Cause = ""
			return p, true
		}
	}
	for _, re := range downRes {
		if re.MatchString(line) {
			p.Kind = KindDown
			return p, true
		}
	}
	// Alarm dying-gasp/LOS secara definisi = down, walau tanpa kata "down".
	if p.Cause == CausePower || p.Cause == CauseLOS {
		p.Kind = KindDown
		return p, true
	}
	return p, false
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
