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

// ToSigned16 reinterprets a raw SNMP value as a signed 16-bit integer.
// The OLT reports optical power in two's complement: a real reading of
// -30.456 dBm arrives as the unsigned value 65308 (=-228). Treating it as
// unsigned produced the absurd +100.616 dBm seen before this fix.
func ToSigned16(raw int) int {
	if raw > 32767 {
		return raw - 65536
	}
	return raw
}

// SanitizePower returns 0.0 ("no reading") when a converted dBm value is
// physically impossible for GPON optics (e.g. a dead ONU returning a
// sentinel-like raw that converts outside the window).
// Real GPON readings live roughly between -40 dBm and +5 dBm; the
// [-50, +10] window is deliberately generous so no real reading is dropped.
func SanitizePower(dbm float64) float64 {
	if dbm < -50 || dbm > 10 {
		return 0.0
	}
	return dbm
}

// ConvertPower converts raw SNMP power value to dBm.
// Formula: (signed16(raw) - 15000) / 500 — the same formula as the
// V1/V2 adapters. Proven against NetNumen on a live OLT: raw 65308
// (=-228) converts to -30.456 dBm, matching NetNumen's ~-30 dBm.
// (The earlier (raw-10000)/100 formula was wrong: it gives -102.28 dBm
// for the same raw value.)
// Sentinel raw values 0 and 65535 mean "no reading" and are checked
// BEFORE the signed reinterpretation: 65535 as signed would be -1,
// which would falsely convert to -30.002 dBm.
func ConvertPower(raw int) float64 {
	if raw == 65535 || raw == 0 {
		return 0.0
	}
	return SanitizePower(float64(ToSigned16(raw)-15000) / 500.0)
}
