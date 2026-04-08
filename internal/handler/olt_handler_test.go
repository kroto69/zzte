package handler

import (
	"testing"

	"olt-monitor/internal/domain"
)

func TestApplyOLTRequestDefaults(t *testing.T) {
	req := domain.OLTCreateRequest{}

	applyOLTRequestDefaults(&req)

	if req.SNMP.Port != 161 || req.SNMP.Community != "public" || req.SNMP.Timeout != 5 || req.SNMP.Retries != 2 {
		t.Fatalf("unexpected SNMP defaults: %+v", req.SNMP)
	}
	if req.Telnet.Port != 23 {
		t.Fatalf("expected default telnet port 23, got %d", req.Telnet.Port)
	}
}

func TestSanitizePersistedOLTConfigStripsSecrets(t *testing.T) {
	req := domain.OLTCreateRequest{
		ID:   "olt_1",
		Name: "OLT 1",
		SNMP: domain.SNMPConfig{
			Host:      "10.0.0.1",
			Port:      161,
			Community: "private",
			Timeout:   5,
			Retries:   2,
		},
		Telnet: domain.TelnetConfig{
			User:     "admin",
			Password: "secret",
			Port:     2323,
		},
	}
	profiles := map[string]string{"202": "P202"}

	persisted := sanitizePersistedOLTConfig(req, profiles)

	if persisted.Community != "" {
		t.Fatalf("expected sanitized community to be blank, got %q", persisted.Community)
	}
	if persisted.Telnet.User != "" || persisted.Telnet.Password != "" {
		t.Fatalf("expected sanitized telnet credentials to be blank, got %+v", persisted.Telnet)
	}
	if persisted.Telnet.Port != 2323 {
		t.Fatalf("expected telnet port to be preserved, got %d", persisted.Telnet.Port)
	}
	profiles["202"] = "changed"
	if persisted.VlanProfiles["202"] != "P202" {
		t.Fatalf("expected VLAN profiles to be cloned, got %+v", persisted.VlanProfiles)
	}
}

func TestShouldFetchVLANProfilesRequiresCredentials(t *testing.T) {
	if shouldFetchVLANProfiles(domain.TelnetConfig{User: "", Password: "secret"}) {
		t.Fatal("expected missing username to disable VLAN profile fetch")
	}
	if shouldFetchVLANProfiles(domain.TelnetConfig{User: "admin", Password: ""}) {
		t.Fatal("expected missing password to disable VLAN profile fetch")
	}
	if !shouldFetchVLANProfiles(domain.TelnetConfig{User: "admin", Password: "secret"}) {
		t.Fatal("expected credentials to enable VLAN profile fetch")
	}
}

func TestSanitizeOLTInstanceStripsRuntimeSecrets(t *testing.T) {
	olt := domain.OLTInstance{
		ID:     "olt_1",
		Name:   "OLT 1",
		SNMP:   domain.SNMPConfig{Host: "10.0.0.1", Port: 161, Community: "private"},
		Telnet: domain.TelnetConfig{User: "admin", Password: "secret", Port: 23},
		Config: domain.OLTConfig{
			ID:     "olt_1",
			Name:   "OLT 1",
			SNMP:   domain.SNMPConfig{Host: "10.0.0.1", Port: 161, Community: "private"},
			Telnet: domain.TelnetConfig{User: "admin", Password: "secret", Port: 23},
		},
	}

	sanitized := sanitizeOLTInstance(olt)

	if sanitized.SNMP.Community != "" || sanitized.Config.SNMP.Community != "" {
		t.Fatalf("expected SNMP community to be redacted, got %+v %+v", sanitized.SNMP, sanitized.Config.SNMP)
	}
	if sanitized.Telnet.User != "" || sanitized.Telnet.Password != "" {
		t.Fatalf("expected telnet credentials to be redacted, got %+v", sanitized.Telnet)
	}
	if sanitized.Config.Telnet.User != "" || sanitized.Config.Telnet.Password != "" {
		t.Fatalf("expected nested telnet credentials to be redacted, got %+v", sanitized.Config.Telnet)
	}
	if olt.Telnet.Password != "secret" {
		t.Fatal("expected sanitizeOLTInstance not to mutate original input")
	}
}

func TestSanitizeOLTInstancesPreservesSliceLength(t *testing.T) {
	olts := []domain.OLTInstance{
		{ID: "a", SNMP: domain.SNMPConfig{Community: "private"}, Telnet: domain.TelnetConfig{User: "u", Password: "p"}},
		{ID: "b", SNMP: domain.SNMPConfig{Community: "private-2"}, Telnet: domain.TelnetConfig{User: "u2", Password: "p2"}},
	}

	sanitized := sanitizeOLTInstances(olts)

	if len(sanitized) != 2 {
		t.Fatalf("expected 2 OLTs after sanitization, got %d", len(sanitized))
	}
	if sanitized[0].Telnet.Password != "" || sanitized[1].SNMP.Community != "" {
		t.Fatalf("expected sanitized secrets, got %+v", sanitized)
	}
}
