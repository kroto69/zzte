package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"olt-monitor/internal/cache"
	"olt-monitor/internal/domain"
	"olt-monitor/internal/snmp"
)

// ONUService handles ONU-related operations
type ONUService struct {
	manager *OLTManager
	cache   *cache.RedisCache
}

// NewONUService creates a new ONU service
func NewONUService(manager *OLTManager, cache *cache.RedisCache) *ONUService {
	return &ONUService{
		manager: manager,
		cache:   cache,
	}
}

// GetONUList retrieves list of ONUs on a specific PON port
// Returns minimal data (ID, Name, Status) for fast response
// Use GetONUDetail for complete ONU information
func (s *ONUService) GetONUList(ctx context.Context, oltID string, board, pon int, forceFresh bool) ([]domain.ONU, error) {
	// Check cache first (unless forced)
	if s.cache != nil && !forceFresh {
		cacheData, err := s.cache.GetONUList(ctx, oltID, board, pon)
		if err == nil {
			var onus []domain.ONU
			if json.Unmarshal(cacheData, &onus) == nil {
				log.Debug().Str("oltId", oltID).Int("board", board).Int("pon", pon).Msg("ONU list from cache")
				return onus, nil
			}
		} else if err != redis.Nil {
			log.Warn().Err(err).Msg("Cache read error")
		}
	}

	// Get SNMP client
	client, err := s.manager.GetClient(oltID)
	if err != nil {
		return nil, err
	}

	release, err := s.manager.AcquireSNMPLock(ctx, oltID)
	if err != nil {
		return nil, err
	}
	defer release()

	start := time.Now()

	// Calculate ifIndex
	ifIndex := snmp.CalculateIfIndex(1, board, pon)
	log.Debug().
		Str("oltId", oltID).
		Int("board", board).
		Int("pon", pon).
		Int("ifIndex", ifIndex).
		Msg("Fetching ONU list (Minimal)")

	// Map to hold ONUs
	onuMap := make(map[int]*domain.ONU)

	// Helper to get or create ONU
	getONU := func(id int) *domain.ONU {
		if _, exists := onuMap[id]; !exists {
			onuMap[id] = &domain.ONU{
				OltID: oltID,
				Board: board,
				Pon:   pon,
				OnuID: id,
			}
		}
		return onuMap[id]
	}

	// 1. Walk Status (discovery + status)
	statusOID := snmp.BuildWalkOID(snmp.OIDONUStatus, ifIndex)
	log.Debug().Str("oid", statusOID).Msg("Walking ONU status")

	statusResults, err := client.Walk(statusOID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to walk ONU status")
		return nil, fmt.Errorf("SNMP walk failed: %w", err)
	}

	for _, pdu := range statusResults {
		if id, err := extractONUID(pdu.Name, statusOID); err == nil {
			onu := getONU(id)
			onu.Status = snmp.ParseONUStatus(&pdu)
			onu.StatusCode = snmp.ParseStatusToInt(&pdu)
		}
	}

	if len(onuMap) == 0 {
		log.Debug().Msg("No ONUs found")
		return []domain.ONU{}, nil
	}

	log.Debug().Int("count", len(onuMap)).Msg("ONUs discovered")

	// NOTE: Offline reason is intentionally not walked for list to keep it fast.

	// 2. Walk Names
	nameOID := snmp.BuildWalkOID(snmp.OIDONUName, ifIndex)
	if results, err := client.Walk(nameOID); err == nil {
		for _, pdu := range results {
			if id, err := extractONUID(pdu.Name, nameOID); err == nil {
				getONU(id).Name = strings.TrimSpace(snmp.PduToString(&pdu))
			}
		}
	}

	// 3. Walk Serial Numbers
	snOID := snmp.BuildWalkOID(snmp.OIDONUSerialNumber, ifIndex)
	if results, err := client.Walk(snOID); err == nil {
		for _, pdu := range results {
			if id, err := extractONUID(pdu.Name, snOID); err == nil {
				getONU(id).SerialNumber = snmp.ParseSerialNumber(&pdu)
			}
		}
	}

	// 4. Walk RX Power
	adapter, _ := s.manager.GetAdapter(oltID)
	rxOID := snmp.BuildWalkOID(snmp.OIDONURXPower, ifIndex)
	if results, err := client.Walk(rxOID); err == nil {
		for _, pdu := range results {
			if id, err := extractONUID(pdu.Name, rxOID); err == nil {
				raw := snmp.GetPDUInt(&pdu)
				if adapter != nil {
					getONU(id).RXPower = adapter.ConvertPower(raw)
				} else {
					getONU(id).RXPower = float64(raw-10000) / 100.0
				}
			}
		}
	}

	// Convert map to sorted slice
	onus := make([]domain.ONU, 0, len(onuMap))
	for _, onu := range onuMap {
		onus = append(onus, *onu)
	}

	// Sort by ONU ID
	sort.Slice(onus, func(i, j int) bool {
		return onus[i].OnuID < onus[j].OnuID
	})

	// Cache the result (short TTL since minimal data)
	if s.cache != nil && len(onus) > 0 {
		if data, err := json.Marshal(onus); err == nil {
			ttl := adaptiveTTL(cache.TTLONUList, time.Since(start))
			if err := s.cache.SetONUListWithTTL(ctx, oltID, board, pon, data, ttl); err != nil {
				log.Warn().Err(err).Msg("Failed to cache ONU list")
			}
		}
	}

	log.Debug().Int("count", len(onus)).Msg("ONU list fetch completed")
	return onus, nil
}

func isOnlineLikeStatus(code int) bool {
	switch code {
	case domain.ONUStatusOnline,
		domain.ONUStatusRanging,
		domain.ONUStatusAutoConfig,
		domain.ONUStatusFirmwareUpgrade:
		return true
	default:
		return false
	}
}

func adaptiveTTL(base time.Duration, duration time.Duration) time.Duration {
	if duration <= 0 {
		return base
	}

	ttl := base + (duration * 2)
	if ttl < base {
		ttl = base
	}

	max := base * 5
	if ttl > max {
		ttl = max
	}

	return ttl
}

// extractONUID extracts ONU ID from walked OID
func extractONUID(fullOID, baseOID string) (int, error) {
	suffix := strings.TrimPrefix(fullOID, baseOID+".")
	if suffix == fullOID {
		return 0, fmt.Errorf("OID doesn't match base: %s", fullOID)
	}
	parts := strings.Split(suffix, ".")
	if len(parts) < 1 || parts[0] == "" {
		return 0, fmt.Errorf("invalid OID format: %s", fullOID)
	}
	return strconv.Atoi(parts[0])
}

// GetONUDetail retrieves complete info for a single ONU
func (s *ONUService) GetONUDetail(ctx context.Context, oltID string, board, pon, onuID int, forceFresh bool) (*domain.ONU, error) {
	// Check cache first (unless forced)
	if s.cache != nil && !forceFresh {
		cacheData, err := s.cache.GetONUDetail(ctx, oltID, board, pon, onuID)
		if err == nil {
			var onu domain.ONU
			if json.Unmarshal(cacheData, &onu) == nil {
				return &onu, nil
			}
		} else if err != redis.Nil {
			log.Warn().Err(err).Msg("Cache read error")
		}
	}

	client, err := s.manager.GetClient(oltID)
	if err != nil {
		return nil, err
	}

	release, err := s.manager.AcquireSNMPLock(ctx, oltID)
	if err != nil {
		return nil, err
	}
	defer release()

	start := time.Now()

	adapter, _ := s.manager.GetAdapter(oltID)
	ifIndex := snmp.CalculateIfIndex(1, board, pon)

	onu := &domain.ONU{
		OltID: oltID,
		Board: board,
		Pon:   pon,
		OnuID: onuID,
	}

	getString := func(oid string) string {
		if str, err := client.GetString(oid); err == nil {
			return strings.TrimSpace(str)
		}
		return ""
	}

	getPDU := func(oid string) *gosnmp.SnmpPDU {
		if pdu, err := client.Get(oid); err == nil {
			return pdu
		}
		return nil
	}

	onu.Name = getString(snmp.BuildONUOID(snmp.OIDONUName, ifIndex, onuID, ""))

	if pdu := getPDU(snmp.BuildONUOID(snmp.OIDONUSerialNumber, ifIndex, onuID, "")); pdu != nil {
		onu.SerialNumber = snmp.ParseSerialNumber(pdu)
	}

	onu.Type = getString(snmp.BuildONUOID(snmp.OIDONUType, ifIndex, onuID, ""))

	if pdu := getPDU(snmp.BuildONUOID(snmp.OIDONUStatus, ifIndex, onuID, "")); pdu != nil {
		onu.Status = snmp.ParseONUStatus(pdu)
		onu.StatusCode = snmp.ParseStatusToInt(pdu)
	}

	if pdu := getPDU(snmp.BuildONUOID(snmp.OIDONURXPower, ifIndex, onuID, ".1")); pdu != nil {
		raw := snmp.GetPDUInt(pdu)
		if adapter != nil {
			onu.RXPower = adapter.ConvertPower(raw)
		} else {
			onu.RXPower = float64(raw-10000) / 100.0
		}
	}

	if pdu := getPDU(snmp.BuildONUOID(snmp.OIDONUTXPower, ifIndex, onuID, ".1")); pdu != nil {
		raw := snmp.GetPDUInt(pdu)
		if adapter != nil {
			onu.TXPower = adapter.ConvertPower(raw)
		} else {
			onu.TXPower = float64(raw-10000) / 100.0
		}
	}

	// Distance intentionally skipped to reduce SNMP load in detail response

	onu.LastOnline = snmp.ParseTime(getString(snmp.BuildONUOID(snmp.OIDONULastOnline, ifIndex, onuID, "")))
	onu.LastOffline = snmp.ParseTime(getString(snmp.BuildONUOID(snmp.OIDONULastOffline, ifIndex, onuID, "")))

	if pdu := getPDU(snmp.BuildONUOID(snmp.OIDONUOfflineReason, ifIndex, onuID, "")); pdu != nil {
		onu.OfflineReason = snmp.ParseOfflineReason(pdu)
		onu.OfflineCode = snmp.ParseOfflineReasonToInt(pdu)
	}

	onu.WanIp = getString(snmp.BuildONUOID(snmp.OIDONUWANIp, ifIndex, onuID, ""))

	if s.cache != nil {
		if data, err := json.Marshal(onu); err == nil {
			ttl := adaptiveTTL(cache.TTLONUDetail, time.Since(start))
			s.cache.SetONUDetailWithTTL(ctx, oltID, board, pon, onuID, data, ttl)
		}
	}

	return onu, nil
}

// GetPONList retrieves list of PONs with descriptions for a specific board
func (s *ONUService) GetPONList(ctx context.Context, oltID string, board int) ([]domain.PONInfo, error) {
	client, err := s.manager.GetClient(oltID)
	if err != nil {
		return nil, err
	}

	// Walk PON Descriptions
	// Walk PON Descriptions
	// The OID walk should just be the base OIDPONDescription to discover all.
	// But we only want specific board.

	// Actually, snmpwalk output: .1.3.6.1.4.1.3902.1012.3.13.1.1.1.{ifIndex}
	// We should walk OIDPONDescription and filter by ifIndex decoding.

	descriptions := make([]domain.PONInfo, 0)

	results, err := client.Walk(snmp.OIDPONDescription)
	if err != nil {
		return nil, fmt.Errorf("SNMP walk failed: %w", err)
	}

	for _, pdu := range results {
		// Extract ifIndex
		// OID: ...1012.3.13.1.1.1.{ifIndex} (one level suffix?)
		// Let's check extractONUID logic but this is simpler.

		suffix := strings.TrimPrefix(pdu.Name, snmp.OIDPONDescription+".")
		if suffix == pdu.Name {
			continue
		}

		ifIndex, err := strconv.Atoi(suffix)
		if err != nil {
			continue // invalid format
		}

		_, slot, port := snmp.ParseIfIndex(ifIndex)

		// Filter by board (slot)
		// Assuming shelf is 1, but we can ignore shelf if needed or checking valid range
		if slot == board {
			desc := snmp.PduToString(&pdu)
			// Sometimes desc might be quoted, clean it up?
			// snmp.PduToString usually handles basic string conversion.
			// The user example had STRING: "OLT-1", gosnmp usually returns the string content.

			descriptions = append(descriptions, domain.PONInfo{
				Board:       slot,
				Pon:         port,
				Description: strings.Trim(desc, "\""),
			})
		}
	}

	// Sort by PON ID
	sort.Slice(descriptions, func(i, j int) bool {
		return descriptions[i].Pon < descriptions[j].Pon
	})

	return descriptions, nil
}
