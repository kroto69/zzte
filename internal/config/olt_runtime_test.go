package config

import "testing"

func TestResolvedOLTConfigsPreferEnvironmentSecrets(t *testing.T) {
	t.Setenv("OLT_BARU_SNMP_COMMUNITY", "private-community")
	t.Setenv("OLT_BARU_TELNET_USER", "olt-user")
	t.Setenv("OLT_BARU_TELNET_PASSWORD", "olt-pass")

	olt := &OLTConfig{
		Host:      "10.0.0.1",
		Port:      161,
		Community: "public",
		Timeout:   5,
		Retries:   2,
		Telnet: TelnetConfig{
			User:     "legacy-user",
			Password: "legacy-pass",
			Port:     23,
		},
	}

	snmp := olt.ResolvedSNMPConfig("baru")
	if snmp.Community != "private-community" {
		t.Fatalf("expected env SNMP community, got %q", snmp.Community)
	}

	telnet := olt.ResolvedTelnetConfig("baru")
	if telnet.User != "olt-user" || telnet.Password != "olt-pass" {
		t.Fatalf("expected env telnet credentials, got %+v", telnet)
	}

	instance := olt.ToOLTInstance("baru")
	if instance.SNMP.Community != "private-community" {
		t.Fatalf("expected ToOLTInstance SNMP env override, got %q", instance.SNMP.Community)
	}
	if instance.Telnet.User != "olt-user" || instance.Config.Telnet.Password != "olt-pass" {
		t.Fatalf("expected ToOLTInstance telnet env override, got instance=%+v", instance.Telnet)
	}
}

func TestNormalizeOLTEnvSegment(t *testing.T) {
	testCases := map[string]string{
		"olt_1":     "OLT_1",
		"baru baru": "BARU_BARU",
		"-":         "DEFAULT",
	}

	for input, expected := range testCases {
		if actual := normalizeOLTEnvSegment(input); actual != expected {
			t.Fatalf("normalizeOLTEnvSegment(%q) = %q, want %q", input, actual, expected)
		}
	}
}
