package event

import "testing"

func TestParseLine(t *testing.T) {
	cases := []struct {
		line      string
		board     int
		pon       int
		onu       int
		kind      Kind
		cause     string
	}{
		{
			`<134>Oct  9 14:00:01 OLT: gpon-onu_1/2/3:4 dying-gasp alarm`,
			2, 3, 4, KindDown, CausePower,
		},
		{
			`gpon-onu_1/5/8:16 LOS alarm raised`,
			5, 8, 16, KindDown, CauseLOS,
		},
		{
			`ONU gpon-onu_1/1/1:1 deregistered`,
			1, 1, 1, KindDown, CauseUnknown,
		},
		{
			`gpon-onu_1/2/3:4 online`,
			2, 3, 4, KindUp, "",
		},
		{
			`gpon-onu_1/2/3:4 LOS recovery`,
			2, 3, 4, KindUp, "",
		},
		{
			`some random log line without onu`,
			0, 0, 0, "", "",
		},
	}
	for i, c := range cases {
		p, ok := ParseLine(c.line)
		if c.kind == "" {
			if ok {
				t.Errorf("case %d: expected not-ok, got %+v", i, p)
			}
			continue
		}
		if !ok {
			t.Errorf("case %d: expected ok, got not-ok for %q", i, c.line)
			continue
		}
		if p.Board != c.board || p.Pon != c.pon || p.OnuID != c.onu ||
			p.Kind != c.kind || p.Cause != c.cause {
			t.Errorf("case %d: got %+v, want board=%d pon=%d onu=%d kind=%s cause=%s",
				i, p, c.board, c.pon, c.onu, c.kind, c.cause)
		}
	}
}
