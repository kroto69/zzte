package domain

// UnconfiguredONU represents an ONU that has been detected but not yet configured
type UnconfiguredONU struct {
	Board int    `json:"board"`
	PON   int    `json:"pon"`
	ONUID int    `json:"onuId"`
	SN    string `json:"sn"`
	Type  string `json:"type"` // e.g., ZTE, HUAWEI
	State string `json:"state"`
}

// ProvisioningRequest contains the data needed to provision an ONU
type ProvisioningRequest struct {
	OLTID      string `json:"oltId"`
	Board      int    `json:"board"`
	PON        int    `json:"pon"`
	ONUID      int    `json:"onuId"`
	SN         string `json:"sn"`
	TemplateID string `json:"templateId"` // e.g., "zte_v2", "huawei_v1"

	// User Inputs
	Name         string `json:"name"`
	Bandwidth    string `json:"bandwidth"` // 50M, 100M, 200M
	VlanInternet int    `json:"vlanInternet"`
	VlanHotspot  int    `json:"vlanHotspot"`
	VlanProfileInternet string `json:"vlanProfileInternet,omitempty"`
	VlanProfileHotspot  string `json:"vlanProfileHotspot,omitempty"`

	// Optional (ZTE Only)
	PPPoEUser string `json:"pppoeUser,omitempty"`
	PPPoEPass string `json:"pppoePass,omitempty"`
}

// ProvisioningLog represents a single step execution log
type ProvisioningLog struct {
	Step    string `json:"step"`
	Command string `json:"command"`
	Status  string `json:"status"` // "success", "failed", "info"
	Message string `json:"message"`
	Time    string `json:"time"`
}

// ProvisioningResponse returns the result of the provisioning process
type ProvisioningResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Logs    []ProvisioningLog `json:"logs"`
}
