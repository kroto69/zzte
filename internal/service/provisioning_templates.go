package service

import (
	"fmt"
	"olt-monitor/internal/domain"
	"strconv"
	"strings"
)

type provisioningTemplateData struct {
	Slot                string
	Port                string
	OnuID               string
	SN                  string
	Name                string
	Bandwidth           string
	VLANInternet        string
	VLANHotspot         string
	VLANProfileInternet string
	VLANProfileHotspot  string
	PPPoEUser           string
	PPPoEPass           string
	OnuType             string
}

func applyVLANProfiles(olt *domain.OLTInstance, req *domain.ProvisioningRequest) {
	if req.VlanProfileInternet == "" {
		req.VlanProfileInternet = resolveVLANProfile(olt.Config.VlanProfiles, req.VlanInternet)
	}
	if req.VlanProfileHotspot == "" {
		req.VlanProfileHotspot = resolveVLANProfile(olt.Config.VlanProfiles, req.VlanHotspot)
	}
}

func resolveVLANProfile(profiles map[string]string, vlan int) string {
	if vlan <= 0 {
		return ""
	}

	key := strconv.Itoa(vlan)
	if profiles != nil {
		if name, ok := profiles[key]; ok && name != "" {
			return name
		}
	}

	return fmt.Sprintf("P%d", vlan)
}

func generateProvisioningCommands(req domain.ProvisioningRequest) ([]string, error) {
	rawTemplate, err := provisioningTemplate(req.TemplateID)
	if err != nil {
		return nil, err
	}

	return renderProvisioningTemplate(rawTemplate, newProvisioningTemplateData(req)), nil
}

func newProvisioningTemplateData(req domain.ProvisioningRequest) provisioningTemplateData {
	return provisioningTemplateData{
		Slot:                strconv.Itoa(req.Board),
		Port:                strconv.Itoa(req.PON),
		OnuID:               strconv.Itoa(req.ONUID),
		SN:                  req.SN,
		Name:                req.Name,
		Bandwidth:           req.Bandwidth,
		VLANInternet:        strconv.Itoa(req.VlanInternet),
		VLANHotspot:         strconv.Itoa(req.VlanHotspot),
		VLANProfileInternet: req.VlanProfileInternet,
		VLANProfileHotspot:  req.VlanProfileHotspot,
		PPPoEUser:           req.PPPoEUser,
		PPPoEPass:           req.PPPoEPass,
		OnuType:             defaultONUType(req.TemplateID),
	}
}

func defaultONUType(templateID string) string {
	if strings.HasPrefix(templateID, "huawei_") {
		return "HG8245H"
	}

	return "ZTE-F609"
}

func provisioningTemplate(templateID string) (string, error) {
	switch templateID {
	case "zte_v2":
		return `
conf t
interface gpon-olt_1/{{slot}}/{{port}}
onu {{onu_id}} type {{onu_type}} sn {{sn}}
exit
interface gpon-onu_1/{{slot}}/{{port}}:{{onu_id}}
sn-bind enable sn
name {{name}}
tcont 1 name INET1 profile {{bandwidth}}
tcont 2 name HOTSPOT profile {{bandwidth}}
gemport 1 name INET1 unicast tcont 1 dir both
gemport 1 traffic-limit upstream UP{{bandwidth}} downstream DW{{bandwidth}}
gemport 2 name HOTSPOT unicast tcont 2 dir both
gemport 2 traffic-limit upstream UP{{bandwidth}} downstream DW{{bandwidth}}
service-port 1 vport 1 user-vlan {{vlan_hsi}} vlan {{vlan_hsi}}
service-port 2 vport 2 user-vlan {{vlan_hot}} vlan {{vlan_hot}}
port-identification format DSL-FORUM-PON sport 1
pppoe-intermediate-agent enable sport 1
exit
pon-onu-mng gpon-onu_1/{{slot}}/{{port}}:{{onu_id}}
service INET1 gemport 1 vlan {{vlan_hsi}}
service HOTSPOT gemport 2 vlan {{vlan_hot}}
wan-ip 1 mode pppoe username {{pppoe_user}} password {{pppoe_pass}} vlan-profile {{vlan_profile_hsi}} host 1
wan-ip 1 ping-response enable traceroute-response enable
security-mgmt 1 mode forward state enable ingress-type wan protocol web
end`, nil
	case "zte_v1":
		return `
conf t
interface gpon-olt_1/{{slot}}/{{port}}
onu {{onu_id}} type {{onu_type}} sn {{sn}}
exit
interface gpon-onu_1/{{slot}}/{{port}}:{{onu_id}}
name {{name}}
description name {{name}}
sn-bind enable sn
tcont 1 name INET1 profile {{bandwidth}}
gemport 1 name INET1 unicast tcont 1 dir both
gemport 1 traffic-limit upstream UP{{bandwidth}} downstream DW{{bandwidth}}
switchport mode hybrid vport 1
service-port 1 vport 1 user-vlan {{vlan_hsi}} vlan {{vlan_hsi}}
port-location format dsl-forum sport 1
port-location sub-option remote-id enable sport 1
port-location sub-option remote-id name 20 sport 1
port-location ft-cid {{vlan_hsi}} sport 1
pppoe-plus enable sport 1
pppoe-plus trust true replace sport 1
exit
pon-onu-mng gpon-onu_1/{{slot}}/{{port}}:{{onu_id}}
service INET1 type internet gemport 1 vlan {{vlan_hsi}}
wan-ip 1 mode pppoe username {{pppoe_user}} password {{pppoe_pass}} vlan-profile {{vlan_profile_hsi}} host 1
wan-ip 1 ping-response enable traceroute-response enable
interface wifi wifi_0/1 state unlock
vlan port eth_0/4 mode tag vlan {{vlan_hot}}
vlan port wifi_0/2 mode tag vlan {{vlan_hot}}
security-mng 1 state enable mode permit protocol web
exit
end
write`, nil
	case "huawei_v2":
		return `
conf t
interface gpon-olt_{{slot}}/{{port}}
onu {{onu_id}} type {{onu_type}} sn {{sn}}
exit
interface gpon-onu_{{slot}}/{{port}}:{{onu_id}}
sn-bind enable sn
name {{name}}
tcont 1 name INET1 profile {{bandwidth}}
tcont 2 name HOTSPOT profile {{bandwidth}}
gemport 1 name INET1 tcont 1
gemport 1 traffic-limit upstream UP{{bandwidth}} downstream DW{{bandwidth}}
gemport 2 name HOTSPOT tcont 2
gemport 2 traffic-limit upstream UP{{bandwidth}} downstream DW{{bandwidth}}
service-port 1 vport 1 user-vlan {{vlan_hsi}} vlan {{vlan_hsi}}
service-port 2 vport 2 user-vlan {{vlan_hot}} vlan {{vlan_hot}}
port-identification format DSL-FORUM-PON sport 1
pppoe-intermediate-agent enable sport 1
exit
pon-onu-mng gpon-onu_{{slot}}/{{port}}:{{onu_id}}
service INET1 gemport 1 vlan {{vlan_hsi}}
service HOTSPOT gemport 2 vlan {{vlan_hot}}
end`, nil
	case "huawei_v1":
		return `
conf t
interface gpon-olt_{{slot}}/{{port}}
onu {{onu_id}} type {{onu_type}} sn {{sn}}
exit
conf t
interface gpon-onu_{{slot}}/{{port}}:{{onu_id}}
name {{name}}
description {{name}}
sn-bind enable sn
tcont 1 name INET1 profile {{bandwidth}}
tcont 2 name HOTSPOT profile {{bandwidth}}
gemport 1 name INET1 unicast tcont 1 dir both
gemport 2 name HOTSPOT unicast tcont 2 dir both
gemport 1 traffic-limit upstream UP{{bandwidth}} downstream DW{{bandwidth}}
gemport 2 traffic-limit upstream UP{{bandwidth}} downstream DW{{bandwidth}}
switchport mode hybrid vport 1
switchport mode hybrid vport 2
service-port 1 vport 1 user-vlan {{vlan_hsi}} vlan {{vlan_hsi}}
service-port 2 vport 2 user-vlan {{vlan_hot}} vlan {{vlan_hot}}
exit
pon-onu-mng gpon-onu_{{slot}}/{{port}}:{{onu_id}}
service INET1 gemport 1 vlan {{vlan_hsi}}
service HOTSPOT gemport 2 vlan {{vlan_hot}}
end`, nil
	default:
		return "", fmt.Errorf("Unknown Template ID: %s", templateID)
	}
}

func renderProvisioningTemplate(raw string, data provisioningTemplateData) []string {
	replacer := strings.NewReplacer(
		"{{slot}}", data.Slot,
		"{{port}}", data.Port,
		"{{onu_id}}", data.OnuID,
		"{{sn}}", data.SN,
		"{{name}}", data.Name,
		"{{bandwidth}}", data.Bandwidth,
		"{{vlan_hsi}}", data.VLANInternet,
		"{{vlan_hot}}", data.VLANHotspot,
		"{{vlan_profile_hsi}}", data.VLANProfileInternet,
		"{{vlan_profile_hot}}", data.VLANProfileHotspot,
		"{{pppoe_user}}", data.PPPoEUser,
		"{{pppoe_pass}}", data.PPPoEPass,
		"{{onu_type}}", data.OnuType,
	)

	commands := make([]string, 0)
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "!") {
			continue
		}
		commands = append(commands, replacer.Replace(trimmed))
	}

	return commands
}
