package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"olt-monitor/internal/snmp"
)

// OLTSystemInfo contains system-level metrics for an OLT
type OLTSystemInfo struct {
	OltID       string `json:"oltId"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	SysDescr    string `json:"sysDescr"`
	SysName     string `json:"sysName"`
	Uptime      string `json:"uptime"`
	UptimeTicks int64  `json:"uptimeTicks"`
	CPUUsage    int    `json:"cpuUsage"`
	MemoryUsage int    `json:"memoryUsage"`
	IsOnline    bool   `json:"isOnline"`
}

// SystemService handles OLT system-level operations
type SystemService struct {
	oltManager *OLTManager
}

// NewSystemService creates a new system service
func NewSystemService(manager *OLTManager) *SystemService {
	return &SystemService{
		oltManager: manager,
	}
}

// OIDs for System Info
const (
	OIDSysDescr    = ".1.3.6.1.2.1.1.1.0"
	OIDSysName     = ".1.3.6.1.2.1.1.5.0"
	OIDSysUptime   = ".1.3.6.1.2.1.1.3.0"
	OIDCPUUsage    = ".1.3.6.1.4.1.3902.1015.2.1.1.3.1.9.1.1.1"
	OIDMemoryUsage = ".1.3.6.1.4.1.3902.1015.2.1.1.3.1.11.1.1.1"
)

// GetSystemInfo retrieves system information for a specific OLT
func (s *SystemService) GetSystemInfo(ctx context.Context, oltID string) (*OLTSystemInfo, error) {
	olt, err := s.oltManager.GetOLT(oltID)
	if err != nil {
		return nil, fmt.Errorf("OLT not found: %s", oltID)
	}

	client, err := s.oltManager.GetClient(oltID)
	if err != nil {
		log.Warn().Err(err).Str("oltId", oltID).Msg("Failed to get SNMP client")
		// Return offline status
		return &OLTSystemInfo{
			OltID:    oltID,
			Name:     olt.Name,
			Host:     olt.SNMP.Host,
			IsOnline: false,
		}, nil
	}

	release, err := s.oltManager.AcquireSNMPLock(ctx, oltID)
	if err != nil {
		return nil, err
	}
	defer release()

	info := &OLTSystemInfo{
		OltID:    oltID,
		Name:     olt.Name,
		Host:     olt.SNMP.Host,
		IsOnline: true,
	}

	// Get all system OIDs
	oids := []string{OIDSysDescr, OIDSysName, OIDSysUptime, OIDCPUUsage, OIDMemoryUsage}
	result, err := client.GetMultiple(oids)
	if err != nil {
		log.Warn().Err(err).Str("oltId", oltID).Msg("Failed to get system info")
		info.IsOnline = false
		return info, nil
	}

	// Parse results
	for _, pdu := range result {
		switch {
		case strings.HasSuffix(pdu.Name, ".1.3.6.1.2.1.1.1.0"):
			info.SysDescr = snmp.PduToString(&pdu)
		case strings.HasSuffix(pdu.Name, ".1.3.6.1.2.1.1.5.0"):
			info.SysName = snmp.PduToString(&pdu)
		case strings.HasSuffix(pdu.Name, ".1.3.6.1.2.1.1.3.0"):
			info.UptimeTicks = snmp.GetPDUInt64(&pdu)
			info.Uptime = ParseUptime(info.UptimeTicks)
		case strings.HasSuffix(pdu.Name, ".1.3.6.1.4.1.3902.1015.2.1.1.3.1.9.1.1.1"):
			info.CPUUsage = int(snmp.GetPDUInt64(&pdu))
		case strings.HasSuffix(pdu.Name, ".1.3.6.1.4.1.3902.1015.2.1.1.3.1.11.1.1.1"):
			info.MemoryUsage = int(snmp.GetPDUInt64(&pdu))
		}
	}

	return info, nil
}

// GetAllSystemInfo retrieves system info for all registered OLTs
func (s *SystemService) GetAllSystemInfo(ctx context.Context) ([]*OLTSystemInfo, error) {
	olts := s.oltManager.ListOLTs()
	result := make([]*OLTSystemInfo, 0, len(olts))

	for _, olt := range olts {
		info, _ := s.GetSystemInfo(ctx, olt.ID)
		if info != nil {
			result = append(result, info)
		}
	}

	return result, nil
}

// ParseUptime converts timeticks (1/100th second) to human readable string
func ParseUptime(ticks int64) string {
	seconds := ticks / 100
	minutes := seconds / 60
	hours := minutes / 60
	days := hours / 24

	hours = hours % 24
	minutes = minutes % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
