package snmp

import (
	"olt-monitor/internal/domain"

	"github.com/rs/zerolog/log"
)

// FirmwareAdapter interface for firmware-specific SNMP operations
type FirmwareAdapter interface {
	GetVersion() string
	GetFullVersion() string
	ConvertPower(raw int) float64
}

// BaseAdapter contains common adapter fields
type BaseAdapter struct {
	version     string
	fullVersion string
}

// V1Adapter handles V1 firmware
type V1Adapter struct {
	BaseAdapter
}

// V2Adapter handles V2 firmware
type V2Adapter struct {
	BaseAdapter
}

// GetVersion returns normalized version (v1 or v2)
func (a *BaseAdapter) GetVersion() string {
	return a.version
}

// GetFullVersion returns full version string
func (a *BaseAdapter) GetFullVersion() string {
	return a.fullVersion
}

// ConvertPower converts raw power value to dBm
// Universal formula for V1 and V2: (signed16(raw) - 15000) / 500
func (a *V1Adapter) ConvertPower(raw int) float64 {
	// Special values (checked BEFORE the signed reinterpretation:
	// 65535 as signed would be -1, falsely converting to -30.002 dBm)
	if raw == 65535 || raw == 0 {
		return 0.0
	}
	// Universal: (signed16(raw) - 15000) / 500.
	// The OLT sends negative readings as unsigned two's complement
	// (e.g. -30.456 dBm arrives as 65308); see domain.ToSigned16.
	return domain.SanitizePower(float64(domain.ToSigned16(raw)-15000) / 500.0)
}

// ConvertPower converts raw power value to dBm
// Universal formula for V1 and V2: (signed16(raw) - 15000) / 500
func (a *V2Adapter) ConvertPower(raw int) float64 {
	// Special values (checked BEFORE the signed reinterpretation:
	// 65535 as signed would be -1, falsely converting to -30.002 dBm)
	if raw == 65535 || raw == 0 {
		return 0.0
	}
	// Universal: (signed16(raw) - 15000) / 500.
	// The OLT sends negative readings as unsigned two's complement
	// (e.g. -30.456 dBm arrives as 65308); see domain.ToSigned16.
	return domain.SanitizePower(float64(domain.ToSigned16(raw)-15000) / 500.0)
}

// DetectFirmware auto-detects firmware version from OLT
func DetectFirmware(client *Client) (FirmwareAdapter, error) {
	// Get firmware version from OLT
	versionStr, err := client.GetString(OIDFirmwareVersion)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get firmware version, defaulting to V1")
		return &V1Adapter{
			BaseAdapter: BaseAdapter{
				version:     "v1",
				fullVersion: "Unknown",
			},
		}, nil
	}

	// Parse version using new parser
	version := ParseFirmwareVersion(versionStr)
	log.Info().Str("fullVersion", versionStr).Str("detected", version).Msg("Detected firmware version")

	if version == "v2" {
		return &V2Adapter{
			BaseAdapter: BaseAdapter{
				version:     "v2",
				fullVersion: versionStr,
			},
		}, nil
	}

	return &V1Adapter{
		BaseAdapter: BaseAdapter{
			version:     "v1",
			fullVersion: versionStr,
		},
	}, nil
}

// NewAdapterFromVersion creates adapter from known version
func NewAdapterFromVersion(version, fullVersion string) FirmwareAdapter {
	if version == "v2" {
		return &V2Adapter{
			BaseAdapter: BaseAdapter{
				version:     "v2",
				fullVersion: fullVersion,
			},
		}
	}
	return &V1Adapter{
		BaseAdapter: BaseAdapter{
			version:     "v1",
			fullVersion: fullVersion,
		},
	}
}
