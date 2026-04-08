package snmp

// OID constants for ZTE C320 OLT
const (
	// Firmware detection OID
	OIDFirmwareVersion = ".1.3.6.1.4.1.3902.1015.2.1.2.2.1.4.1.1.1"

	// Base OID for ONU data (3902.1012)
	OIDBase = ".1.3.6.1.4.1.3902.1012"

	// ONU Name: .1.3.6.1.4.1.3902.1012.3.28.1.1.2.{ifIndex}.{onuId}
	OIDONUName = OIDBase + ".3.28.1.1.2"

	// ONU Serial Number: .1.3.6.1.4.1.3902.1012.3.28.1.1.5.{ifIndex}.{onuId}
	OIDONUSerialNumber = OIDBase + ".3.28.1.1.5"

	// ONU Type: .1.3.6.1.4.1.3902.1012.3.50.11.2.1.17.{ifIndex}.{onuId}
	OIDONUType = OIDBase + ".3.50.11.2.1.17"

	// ONU RX Power: .1.3.6.1.4.1.3902.1012.3.50.12.1.1.10.{ifIndex}.{onuId}.1
	OIDONURXPower = OIDBase + ".3.50.12.1.1.10"

	// ONU TX Power: .1.3.6.1.4.1.3902.1012.3.50.12.1.1.9.{ifIndex}.{onuId}.1
	OIDONUTXPower = OIDBase + ".3.50.12.1.1.9"

	// ONU Distance: .1.3.6.1.4.1.3902.1012.3.11.4.1.2.{ifIndex}.{onuId} (meters)
	OIDONUDistance = OIDBase + ".3.11.4.1.2"

	// ONU Status: .1.3.6.1.4.1.3902.1012.3.28.2.1.4.{ifIndex}.{onuId}
	// Values: 1=Offline, 2=Ranging, 3=Online, 4=LOS, 5=DyingGasp, 6=PowerOff, 7=Unauthorized, 8=AutoConfig, 9=FirmwareUpgrade
	OIDONUStatus = OIDBase + ".3.28.2.1.4"

	// ONU Last Online: .1.3.6.1.4.1.3902.1012.3.28.2.1.5.{ifIndex}.{onuId}
	OIDONULastOnline = OIDBase + ".3.28.2.1.5"

	// ONU Last Offline: .1.3.6.1.4.1.3902.1012.3.28.2.1.6.{ifIndex}.{onuId}
	OIDONULastOffline = OIDBase + ".3.28.2.1.6"

	// ONU Offline Reason: .1.3.6.1.4.1.3902.1012.3.28.2.1.3.{ifIndex}.{onuId}
	// Values: 0=Normal, 2=LOS, 6=DyingGasp, 7=ManualShutdown
	OIDONUOfflineReason = OIDBase + ".3.28.2.1.3"

	// ONU WAN IP: .1.3.6.1.4.1.3902.1012.3.50.16.1.1.10.{ifIndex}.{onuId}
	OIDONUWANIp = OIDBase + ".3.50.16.1.1.10"

	// PON Description: .1.3.6.1.4.1.3902.1012.3.13.1.1.1.{ifIndex}
	OIDPONDescription = OIDBase + ".3.13.1.1.1"
)

// BuildONUOID builds a full OID for a specific ONU attribute
// baseOID: the base OID for the attribute
// ifIndex: calculated interface index
// onuID: the ONU ID
// suffix: optional suffix (e.g., ".1" for power readings)
func BuildONUOID(baseOID string, ifIndex int, onuID int, suffix string) string {
	oid := baseOID + "." + itoa(ifIndex) + "." + itoa(onuID)
	if suffix != "" {
		oid += suffix
	}
	return oid
}

// BuildWalkOID builds OID for SNMP walk on a specific PON port
func BuildWalkOID(baseOID string, ifIndex int) string {
	return baseOID + "." + itoa(ifIndex)
}

// Simple int to string conversion without importing strconv
func itoa(i int) string {
	if i == 0 {
		return "0"
	}

	var result []byte
	negative := i < 0
	if negative {
		i = -i
	}

	for i > 0 {
		result = append([]byte{byte('0' + i%10)}, result...)
		i /= 10
	}

	if negative {
		result = append([]byte{'-'}, result...)
	}

	return string(result)
}
