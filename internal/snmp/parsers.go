package snmp

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// --- Firmware Parser ---

// ParseFirmwareVersion parses firmware version string
// Returns "v1", "v2", or "unknown"
func ParseFirmwareVersion(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "V1.") {
		return "v1"
	} else if strings.HasPrefix(value, "V2.") {
		return "v2"
	}
	return "unknown"
}

// --- ONU Name Parser ---

// ParseONUName cleans up ONU name
func ParseONUName(value string) string {
	return strings.TrimSpace(value)
}

// --- Serial Number Parser ---

// ParseSerialNumber parses hybrid ASCII/Hex serial numbers
// Rule: 8 bytes total. First 4 bytes = Vendor Code (ASCII), Last 4 bytes = Device ID (Hex)
func ParseSerialNumber(pdu *gosnmp.SnmpPDU) string {
	bytes := getPDUBytes(pdu)
	if len(bytes) != 8 {
		// Fallback for non-standard length
		return strings.ToUpper(hex.EncodeToString(bytes))
	}

	vendorCode := string(bytes[0:4])
	deviceID := strings.ToUpper(hex.EncodeToString(bytes[4:8]))

	return vendorCode + deviceID
}

// --- Power Parser ---

// ParsePower converts raw power value to dBm
// Formula: (raw - 10000) / 100
// Returns 0.0 if raw is 65535 (invalid/no signal)
func ParsePower(pdu *gosnmp.SnmpPDU) float64 {
	raw := GetPDUInt(pdu)
	if raw == 65535 {
		return 0.0
	}
	return float64(raw-10000) / 100.0
}

// --- Distance Parser ---

// ParseDistance converts meter value to km
func ParseDistance(pdu *gosnmp.SnmpPDU) float64 {
	meters := GetPDUInt(pdu)
	return float64(meters) / 1000.0
}

// --- Status Parsers ---

// ParseONUStatus parses status enumeration
func ParseONUStatus(pdu *gosnmp.SnmpPDU) string {
	code := GetPDUInt(pdu)
	switch code {
	case 1:
		return "Offline"
	case 2:
		return "Ranging"
	case 3:
		return "Online"
	case 4:
		return "LOS"
	case 5:
		return "DyingGasp"
	case 6:
		return "PowerOff"
	case 7:
		return "Unauthorized"
	case 8:
		return "AutoConfig"
	case 9:
		return "FirmwareUpgrade"
	default:
		return fmt.Sprintf("Unknown(%d)", code)
	}
}

// ParseStatusToInt returns raw status code
func ParseStatusToInt(pdu *gosnmp.SnmpPDU) int {
	return GetPDUInt(pdu)
}

// ParseOfflineReason parses offline reason enumeration
func ParseOfflineReason(pdu *gosnmp.SnmpPDU) string {
	code := GetPDUInt(pdu)
	switch code {
	case 0:
		return "Normal"
	case 2:
		return "LOS"
	case 6:
		return "DyingGasp"
	case 7:
		return "ManualShutdown"
	default:
		return fmt.Sprintf("Unknown(%d)", code)
	}
}

// ParseOfflineReasonToInt returns raw offline reason code
func ParseOfflineReasonToInt(pdu *gosnmp.SnmpPDU) int {
	return GetPDUInt(pdu)
}

// --- Time Parser ---

// ParseTime parses OLT timestamp format
// Format: "YYYY-MM-DD HH:MM:SS"
func ParseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "0000-00-00") {
		return time.Time{}
	}

	layout := "2006-01-02 15:04:05"
	t, err := time.Parse(layout, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

// --- Helpers ---

// getPDUBytes extracts bytes from PDU
func getPDUBytes(pdu *gosnmp.SnmpPDU) []byte {
	if pdu == nil || pdu.Value == nil {
		return []byte{}
	}
	switch pdu.Type {
	case gosnmp.OctetString:
		return pdu.Value.([]byte)
	default:
		return []byte{}
	}
}

// GetPDUInt extracts int value from PDU
func GetPDUInt(pdu *gosnmp.SnmpPDU) int {
	if pdu == nil || pdu.Value == nil {
		return 0
	}
	return pduToInt(pdu)
}

// GetPDUInt64 extracts int64 value from PDU (for large values like uptime)
func GetPDUInt64(pdu *gosnmp.SnmpPDU) int64 {
	if pdu == nil || pdu.Value == nil {
		return 0
	}
	switch v := pdu.Value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	default:
		return int64(pduToInt(pdu))
	}
}
