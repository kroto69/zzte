package domain

import "time"

// ONU represents an Optical Network Unit connected to an OLT (full detail)
type ONU struct {
	OltID         string    `json:"oltId"`
	Board         int       `json:"board"`
	Pon           int       `json:"pon"`
	OnuID         int       `json:"onuId"`
	Name          string    `json:"name"`
	SerialNumber  string    `json:"serialNumber"`
	Type          string    `json:"type"`
	Status        string    `json:"status"`
	StatusCode    int       `json:"statusCode"`
	RXPower       float64   `json:"rxPower"`
	TXPower       float64   `json:"txPower"`
	DistanceM     int       `json:"distanceM,omitempty"`
	DistanceKm    float64   `json:"distanceKm,omitempty"`
	LastOnline    time.Time `json:"lastOnline"`
	LastOffline   time.Time `json:"lastOffline"`
	OfflineReason string    `json:"offlineReason"`
	OfflineCode   int       `json:"offlineCode"`
	WanIp         string    `json:"wanIp"`
}

// ONUListItem is the lightweight response for list endpoints (only populated fields)
type ONUListItem struct {
	OltID        string  `json:"oltId"`
	Board        int     `json:"board"`
	Pon          int     `json:"pon"`
	OnuID        int     `json:"onuId"`
	Name         string  `json:"name"`
	SerialNumber string  `json:"serialNumber"`
	Status       string  `json:"status"`
	StatusCode   int     `json:"statusCode"`
	RXPower      float64 `json:"rxPower"`
}

// ONUNameEntry caches static ONU identity (Name + SerialNumber) with long TTL
type ONUNameEntry struct {
	OnuID        int    `json:"onuId"`
	Name         string `json:"name"`
	SerialNumber string `json:"serialNumber"`
}

// ToListItem converts a full ONU to a lightweight list item
func (o *ONU) ToListItem() ONUListItem {
	return ONUListItem{
		OltID:        o.OltID,
		Board:        o.Board,
		Pon:          o.Pon,
		OnuID:        o.OnuID,
		Name:         o.Name,
		SerialNumber: o.SerialNumber,
		Status:       o.Status,
		StatusCode:   o.StatusCode,
		RXPower:      o.RXPower,
	}
}

// ONUStatus constants
const (
	ONUStatusOffline         = 1
	ONUStatusRanging         = 2
	ONUStatusOnline          = 3
	ONUStatusLOS             = 4
	ONUStatusDyingGasp       = 5
	ONUStatusPowerOff        = 6
	ONUStatusUnauthorized    = 7
	ONUStatusAutoConfig      = 8
	ONUStatusFirmwareUpgrade = 9
)

// ONUOfflineReason constants
const (
	OfflineReasonNormal         = 0
	OfflineReasonLOS            = 2
	OfflineReasonDyingGasp      = 6
	OfflineReasonManualShutdown = 7
)

// StatusCodeToString converts ONU status code to human-readable string
func StatusCodeToString(code int) string {
	switch code {
	case ONUStatusOffline:
		return "Offline"
	case ONUStatusRanging:
		return "Ranging"
	case ONUStatusOnline:
		return "Online"
	case ONUStatusLOS:
		return "LOS"
	case ONUStatusDyingGasp:
		return "DyingGasp"
	case ONUStatusPowerOff:
		return "PowerOff"
	case ONUStatusUnauthorized:
		return "Unauthorized"
	case ONUStatusAutoConfig:
		return "AutoConfig"
	case ONUStatusFirmwareUpgrade:
		return "FirmwareUpgrade"
	default:
		return "Unknown"
	}
}

// OfflineCodeToString converts offline reason code to human-readable string
func OfflineCodeToString(code int) string {
	switch code {
	case OfflineReasonNormal:
		return "Normal"
	case OfflineReasonLOS:
		return "LOS"
	case OfflineReasonDyingGasp:
		return "DyingGasp"
	case OfflineReasonManualShutdown:
		return "ManualShutdown"
	default:
		return "Unknown"
	}
}

// ConvertPower converts raw SNMP power value to dBm
// Formula: (raw - 10000) / 100
func ConvertPower(raw int) float64 {
	return float64(raw-10000) / 100.0
}
