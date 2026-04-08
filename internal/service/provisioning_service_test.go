package service

import (
	"regexp"
	"strings"
	"testing"

	"olt-monitor/internal/domain"
)

func TestParseUnconfiguredOutputSupportsTableAndKeyValueFormats(t *testing.T) {
	t.Parallel()

	raw := `Random banner
gpon-onu_1/2/3:4         ZTEG12345678        unknown
gpon-onu_1/5/6:7         SN:ZTEG87654321     pending`

	onus, err := parseUnconfiguredOutput(raw)
	if err != nil {
		t.Fatalf("parseUnconfiguredOutput() error = %v", err)
	}

	if len(onus) != 2 {
		t.Fatalf("expected 2 parsed ONUs, got %d", len(onus))
	}

	if onus[0].Board != 2 || onus[0].PON != 3 || onus[0].ONUID != 4 || onus[0].SN != "ZTEG12345678" {
		t.Fatalf("unexpected first ONU: %+v", onus[0])
	}

	if onus[1].Board != 5 || onus[1].PON != 6 || onus[1].ONUID != 7 || onus[1].SN != "ZTEG87654321" {
		t.Fatalf("unexpected second ONU: %+v", onus[1])
	}
	if onus[1].Type != "ZTE" || onus[1].State != "Unconfigured" {
		t.Fatalf("unexpected ONU metadata: %+v", onus[1])
	}
}

func TestParseUnconfiguredLineRejectsInvalidRows(t *testing.T) {
	t.Parallel()

	reTable := regexp.MustCompile(`gpon-onu_\d+/(\d+)/(\d+):(\d+)\s+([A-Za-z0-9]+)`)
	reKeyValue := regexp.MustCompile(`gpon-onu_\d+/(\d+)/(\d+):(\d+)\s+.*?SN:([A-Za-z0-9]+)`)

	if _, ok := parseUnconfiguredLine("not-an-onu-row", reTable, reKeyValue); ok {
		t.Fatal("expected non-ONU row to be ignored")
	}

	if _, ok := parseUnconfiguredLine("gpon-onu_1/x/3:4 SN:BADROW", reTable, reKeyValue); ok {
		t.Fatal("expected malformed ONU row to be rejected")
	}
}

func TestApplyVLANProfilesUsesMappedAndFallbackProfiles(t *testing.T) {
	olt := &domain.OLTInstance{
		Config: domain.OLTConfig{
			VlanProfiles: map[string]string{
				"202": "P202-CUSTOM",
			},
		},
	}
	req := domain.ProvisioningRequest{
		VlanInternet: 202,
		VlanHotspot:  303,
	}

	applyVLANProfiles(olt, &req)

	if req.VlanProfileInternet != "P202-CUSTOM" {
		t.Fatalf("expected mapped VLAN profile, got %q", req.VlanProfileInternet)
	}
	if req.VlanProfileHotspot != "P303" {
		t.Fatalf("expected fallback VLAN profile, got %q", req.VlanProfileHotspot)
	}
}

func TestGenerateProvisioningCommandsRendersTemplateData(t *testing.T) {
	req := domain.ProvisioningRequest{
		TemplateID:          "zte_v2",
		Board:               1,
		PON:                 2,
		ONUID:               3,
		SN:                  "SN12345",
		Name:                "Customer A",
		Bandwidth:           "50M",
		VlanInternet:        202,
		VlanHotspot:         303,
		VlanProfileInternet: "P202",
		VlanProfileHotspot:  "P303",
		PPPoEUser:           "alice",
		PPPoEPass:           "secret",
	}

	commands, err := generateProvisioningCommands(req)
	if err != nil {
		t.Fatalf("generateProvisioningCommands() error = %v", err)
	}

	joined := "\n" + strings.Join(commands, "\n") + "\n"
	assertContains := func(substr string) {
		if !strings.Contains(joined, substr) {
			t.Fatalf("expected rendered commands to contain %q, got:\n%s", substr, joined)
		}
	}

	assertContains("interface gpon-olt_1/1/2")
	assertContains("onu 3 type ZTE-F609 sn SN12345")
	assertContains("wan-ip 1 mode pppoe username alice password secret vlan-profile P202 host 1")
	assertContains("service HOTSPOT gemport 2 vlan 303")
}
