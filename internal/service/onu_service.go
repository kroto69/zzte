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
	"golang.org/x/sync/singleflight"

	"olt-monitor/internal/cache"
	"olt-monitor/internal/domain"
	"olt-monitor/internal/snmp"
)

// ONUService handles ONU-related operations
type ONUService struct {
	manager *OLTManager
	cache   *cache.RedisCache
	sfGroup  singleflight.Group // deduplikasi list per OLT+board+pon
	sfDetail singleflight.Group // PATCH(oltwtch): dedup detail per OLT+board+pon+onu
	sfLive   singleflight.Group // PATCH(oltwtch): dedup live per OLT+board+pon
	hot      *HotTracker        // PATCH(oltwtch-2): tracker port panas (opsional)
}

// NewONUService creates a new ONU service.
// Parameter hotTracker opsional (variadic) agar kode lama tetap kompil.
func NewONUService(manager *OLTManager, cache *cache.RedisCache, hot ...*HotTracker) *ONUService {
	s := &ONUService{
		manager: manager,
		cache:   cache,
	}
	if len(hot) > 0 {
		s.hot = hot[0]
	}
	return s
}

// GetONUList retrieves list of ONUs on a specific PON port
// Returns minimal data (ID, Name, Status) for fast response
// Use GetONUDetail for complete ONU information
func (s *ONUService) GetONUList(ctx context.Context, oltID string, board, pon int, forceFresh bool) ([]domain.ONUListItem, error) {
	s.hot.Touch(oltID, board, pon) // PATCH(oltwtch-2): tandai port panas
	// Check cache first (unless forced)
	if s.cache != nil && !forceFresh {
		cacheData, err := s.cache.GetONUList(ctx, oltID, board, pon)
		if err == nil {
			var items []domain.ONUListItem
			if json.Unmarshal(cacheData, &items) == nil && len(items) > 0 {
				needsNames := false
				for _, item := range items {
					if item.Name == "" {
						needsNames = true
						break
					}
				}
				if !needsNames {
					log.Debug().Str("oltId", oltID).Int("board", board).Int("pon", pon).Msg("ONU list from cache (complete)")
					return items, nil
				}
				// Cache ada tapi tanpa Name/SN — coba merge dari names cache
				namesData, nErr := s.cache.GetONUNames(ctx, oltID, board, pon)
				if nErr == nil {
					var names []domain.ONUNameEntry
					if json.Unmarshal(namesData, &names) == nil {
						namesMap := make(map[int]domain.ONUNameEntry, len(names))
						for _, n := range names {
							namesMap[n.OnuID] = n
						}
						merged := false
						for i := range items {
							if items[i].Name == "" {
								if n, ok := namesMap[items[i].OnuID]; ok {
									items[i].Name = n.Name
									items[i].SerialNumber = n.SerialNumber
									merged = true
								}
							}
						}
						if merged {
							log.Debug().Str("oltId", oltID).Int("board", board).Int("pon", pon).Msg("ONU list from cache + names cache merge")
							return items, nil
						}
					}
				}
				// Names cache kosong/gagal — fallback ke on-demand walk
				log.Debug().Str("oltId", oltID).Int("board", board).Int("pon", pon).Msg("Cache tanpa Name/SN, fallback ke on-demand")
			}
		} else if err != redis.Nil {
			log.Warn().Err(err).Msg("Cache read error")
		}
	}

	// Deduplikasi request SNMP untuk OLT+board+pon yang sama
	sfKey := fmt.Sprintf("onulist:%s:%d:%d", oltID, board, pon)
	v, err, _ := s.sfGroup.Do(sfKey, func() (interface{}, error) {
		// Get SNMP client
		client, err := s.manager.GetClient(oltID)
		if err != nil {
			return nil, err
		}

		// PATCH(oltwtch): lock per-PON, bukan per-OLT
		release, err := s.manager.AcquirePONLock(ctx, oltID, board, pon)
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

		statusResults, err := client.BulkWalk(statusOID)
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
			return []domain.ONUListItem{}, nil
		}

		log.Debug().Int("count", len(onuMap)).Msg("ONUs discovered")

		// NOTE: Offline reason is intentionally not walked for list to keep it fast.

		// 2. Walk RX Power (didahulukan — ini yang dibutuhkan real-time)
		adapter, _ := s.manager.GetAdapter(oltID)
		rxOID := snmp.BuildWalkOID(snmp.OIDONURXPower, ifIndex)
		if results, err := client.BulkWalk(rxOID); err == nil {
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

		// 3. Nama/Serial: coba dari names cache dulu (TTL 10 jam, diisi names
		//    poller). Walk SNMP hanya untuk ONU yang namanya masih kosong.
		//    PATCH(oltwtch-2): menghemat 2 walk di kasus umum.
		needNames := false
		if s.cache != nil {
			if namesData, nErr := s.cache.GetONUNames(ctx, oltID, board, pon); nErr == nil {
				var names []domain.ONUNameEntry
				if json.Unmarshal(namesData, &names) == nil {
					namesMap := make(map[int]domain.ONUNameEntry, len(names))
					for _, n := range names {
						namesMap[n.OnuID] = n
					}
					for id, onu := range onuMap {
						if n, ok := namesMap[id]; ok {
							onu.Name = n.Name
							onu.SerialNumber = n.SerialNumber
						}
					}
				}
			}
		}
		for _, onu := range onuMap {
			if onu.Name == "" {
				needNames = true
				break
			}
		}
		if needNames {
			nameOID := snmp.BuildWalkOID(snmp.OIDONUName, ifIndex)
			if results, err := client.BulkWalk(nameOID); err == nil {
				for _, pdu := range results {
					if id, err := extractONUID(pdu.Name, nameOID); err == nil {
						getONU(id).Name = strings.TrimSpace(snmp.PduToString(&pdu))
					}
				}
			}

			snOID := snmp.BuildWalkOID(snmp.OIDONUSerialNumber, ifIndex)
			if results, err := client.BulkWalk(snOID); err == nil {
				for _, pdu := range results {
					if id, err := extractONUID(pdu.Name, snOID); err == nil {
						getONU(id).SerialNumber = snmp.ParseSerialNumber(&pdu)
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

		// Convert to list items for response
		items := make([]domain.ONUListItem, len(onus))
		for i := range onus {
			items[i] = onus[i].ToListItem()
		}

		// Cache the result (short TTL since minimal data)
		if s.cache != nil && len(items) > 0 {
			if data, err := json.Marshal(items); err == nil {
				ttl := adaptiveTTL(cache.TTLONUList, time.Since(start))
				if err := s.cache.SetONUListWithTTL(ctx, oltID, board, pon, data, ttl); err != nil {
					log.Warn().Err(err).Msg("Failed to cache ONU list")
				}
			}
			// Simpan Name+SN ke names cache terpisah (TTL 6 jam)
			names := make([]domain.ONUNameEntry, 0, len(items))
			for _, item := range items {
				names = append(names, domain.ONUNameEntry{
					OnuID:        item.OnuID,
					Name:         item.Name,
					SerialNumber: item.SerialNumber,
				})
			}
			if namesData, err := json.Marshal(names); err == nil {
				if err := s.cache.SetONUNames(ctx, oltID, board, pon, namesData); err != nil {
					log.Warn().Err(err).Msg("Failed to cache ONU names")
				}
			}
		}

		log.Debug().Int("count", len(items)).Msg("ONU list fetch completed")
		return items, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]domain.ONUListItem), nil
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
	s.hot.Touch(oltID, board, pon) // PATCH(oltwtch-2): tandai port panas
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

	// PATCH(oltwtch): deduplikasi via singleflight — 4 user klik ONU yang sama
	// dalam waktu bersamaan = 1 batch SNMP, hasilnya dibagi ke semua.
	sfKey := fmt.Sprintf("detail:%s:%d:%d:%d", oltID, board, pon, onuID)
	v, err, _ := s.sfDetail.Do(sfKey, func() (interface{}, error) {
		client, err := s.manager.GetClient(oltID)
		if err != nil {
			return nil, err
		}

		// PATCH(oltwtch): lock per-PON, bukan per-OLT
		release, err := s.manager.AcquirePONLock(ctx, oltID, board, pon)
		if err != nil {
			return nil, err
		}
		defer release()

		start := time.Now()

		adapter, _ := s.manager.GetAdapter(oltID)
		ifIndex := snmp.CalculateIfIndex(1, board, pon)

		// Batch GET — 1 SNMP round-trip untuk 10 OID
	oidName := snmp.BuildONUOID(snmp.OIDONUName, ifIndex, onuID, "")
	oidSN := snmp.BuildONUOID(snmp.OIDONUSerialNumber, ifIndex, onuID, "")
	oidType := snmp.BuildONUOID(snmp.OIDONUType, ifIndex, onuID, "")
	oidStatus := snmp.BuildONUOID(snmp.OIDONUStatus, ifIndex, onuID, "")
	oidRXPower := snmp.BuildONUOID(snmp.OIDONURXPower, ifIndex, onuID, ".1")
	oidTXPower := snmp.BuildONUOID(snmp.OIDONUTXPower, ifIndex, onuID, ".1")
	oidLastOnline := snmp.BuildONUOID(snmp.OIDONULastOnline, ifIndex, onuID, "")
	oidLastOffline := snmp.BuildONUOID(snmp.OIDONULastOffline, ifIndex, onuID, "")
	oidOfflineReason := snmp.BuildONUOID(snmp.OIDONUOfflineReason, ifIndex, onuID, "")
	oidWanIp := snmp.BuildONUOID(snmp.OIDONUWANIp, ifIndex, onuID, "")

	results, err := client.GetMultiple([]string{
		oidName, oidSN, oidType, oidStatus,
		oidRXPower, oidTXPower, oidLastOnline, oidLastOffline,
		oidOfflineReason, oidWanIp,
	})
	if err != nil {
		return nil, fmt.Errorf("SNMP GET failed: %w", err)
	}

	pduMap := make(map[string]*gosnmp.SnmpPDU, len(results))
	for i := range results {
		pduMap[results[i].Name] = &results[i]
	}

	getString := func(oid string) string {
		if pdu, ok := pduMap[oid]; ok {
			return strings.TrimSpace(snmp.PduToString(pdu))
		}
		return ""
	}

	onu := &domain.ONU{
		OltID: oltID,
		Board: board,
		Pon:   pon,
		OnuID: onuID,
	}

	onu.Name = getString(oidName)

	if pdu, ok := pduMap[oidSN]; ok {
		onu.SerialNumber = snmp.ParseSerialNumber(pdu)
	}

	onu.Type = getString(oidType)

	if pdu, ok := pduMap[oidStatus]; ok {
		onu.Status = snmp.ParseONUStatus(pdu)
		onu.StatusCode = snmp.ParseStatusToInt(pdu)
	}

	if pdu, ok := pduMap[oidRXPower]; ok {
		raw := snmp.GetPDUInt(pdu)
		if adapter != nil {
			onu.RXPower = adapter.ConvertPower(raw)
		} else {
			onu.RXPower = float64(raw-10000) / 100.0
		}
	}

	if pdu, ok := pduMap[oidTXPower]; ok {
		raw := snmp.GetPDUInt(pdu)
		if adapter != nil {
			onu.TXPower = adapter.ConvertPower(raw)
		} else {
			onu.TXPower = float64(raw-10000) / 100.0
		}
	}

	onu.LastOnline = snmp.ParseTime(getString(oidLastOnline))
	onu.LastOffline = snmp.ParseTime(getString(oidLastOffline))

	if pdu, ok := pduMap[oidOfflineReason]; ok {
		onu.OfflineReason = snmp.ParseOfflineReason(pdu)
		onu.OfflineCode = snmp.ParseOfflineReasonToInt(pdu)
	}

	onu.WanIp = getString(oidWanIp)

		if s.cache != nil {
			if data, err := json.Marshal(onu); err == nil {
				ttl := adaptiveTTL(cache.TTLONUDetail, time.Since(start))
				s.cache.SetONUDetailWithTTL(ctx, oltID, board, pon, onuID, data, ttl)
			}
		}

		return onu, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*domain.ONU), nil
}

// GetONULive — PATCH(oltwtch): jalur cepat khusus troubleshooting ("mode benerin").
// Hanya walk 3 tabel: status + rx + tx. Nama/serial diambil dari cache (tidak berubah
// saat teknisi lagi perbaiki). Dengan MaxRepetitions=50, satu PON port selesai dalam
// ~2-4 detik, bukan belasan detik. Hasilnya juga ditulis ke cache list agar semua
// user langsung melihat data segar.
func (s *ONUService) GetONULive(ctx context.Context, oltID string, board, pon int) ([]domain.ONUListItem, error) {
	s.hot.Touch(oltID, board, pon) // PATCH(oltwtch-2): tandai port panas
	sfKey := fmt.Sprintf("live:%s:%d:%d", oltID, board, pon)
	v, err, _ := s.sfLive.Do(sfKey, func() (interface{}, error) {
		client, err := s.manager.GetClient(oltID)
		if err != nil {
			return nil, err
		}

		release, err := s.manager.AcquirePONLock(ctx, oltID, board, pon)
		if err != nil {
			return nil, err
		}
		defer release()

		start := time.Now()
		adapter, _ := s.manager.GetAdapter(oltID)
		ifIndex := snmp.CalculateIfIndex(1, board, pon)

		onuMap := make(map[int]*domain.ONUListItem)
		getItem := func(id int) *domain.ONUListItem {
			if _, ok := onuMap[id]; !ok {
				onuMap[id] = &domain.ONUListItem{OltID: oltID, Board: board, Pon: pon, OnuID: id}
			}
			return onuMap[id]
		}
		convertPower := func(raw int) float64 {
			if adapter != nil {
				return adapter.ConvertPower(raw)
			}
			return float64(raw-10000) / 100.0
		}

		// 1. status (sekaligus discovery ONU)
		statusOID := snmp.BuildWalkOID(snmp.OIDONUStatus, ifIndex)
		statusResults, err := client.BulkWalk(statusOID)
		if err != nil {
			return nil, fmt.Errorf("walk status gagal: %w", err)
		}
		for _, pdu := range statusResults {
			if id, err := extractONUID(pdu.Name, statusOID); err == nil {
				it := getItem(id)
				it.Status = snmp.ParseONUStatus(&pdu)
				it.StatusCode = snmp.ParseStatusToInt(&pdu)
			}
		}
		if len(onuMap) == 0 {
			return []domain.ONUListItem{}, nil
		}

		// 2. rx power
		rxOID := snmp.BuildWalkOID(snmp.OIDONURXPower, ifIndex)
		if results, err := client.BulkWalk(rxOID); err == nil {
			for _, pdu := range results {
				if id, err := extractONUID(pdu.Name, rxOID); err == nil {
					getItem(id).RXPower = convertPower(snmp.GetPDUInt(&pdu))
				}
			}
		}

		// 3. tx power
		txOID := snmp.BuildWalkOID(snmp.OIDONUTXPower, ifIndex)
		if results, err := client.BulkWalk(txOID); err == nil {
			for _, pdu := range results {
				if id, err := extractONUID(pdu.Name, txOID); err == nil {
					getItem(id).TXPower = convertPower(snmp.GetPDUInt(&pdu))
				}
			}
		}

		// 4. nama/serial dari cache saja — tanpa walk
		if s.cache != nil {
			if namesData, nErr := s.cache.GetONUNames(ctx, oltID, board, pon); nErr == nil {
				var names []domain.ONUNameEntry
				if json.Unmarshal(namesData, &names) == nil {
					namesMap := make(map[int]domain.ONUNameEntry, len(names))
					for _, n := range names {
						namesMap[n.OnuID] = n
					}
					for id, it := range onuMap {
						if n, ok := namesMap[id]; ok {
							it.Name = n.Name
							it.SerialNumber = n.SerialNumber
						}
					}
				}
			}
		}

		items := make([]domain.ONUListItem, 0, len(onuMap))
		for _, it := range onuMap {
			items = append(items, *it)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].OnuID < items[j].OnuID })

		// tulis ke cache list: semua user dapat data segar tanpa walk ulang
		if s.cache != nil && len(items) > 0 {
			if data, mErr := json.Marshal(items); mErr == nil {
				ttl := adaptiveTTL(cache.TTLONUList, time.Since(start))
				if err := s.cache.SetONUListWithTTL(ctx, oltID, board, pon, data, ttl); err != nil {
					log.Warn().Err(err).Msg("Gagal cache hasil live")
				}
			}
		}

		log.Debug().Int("count", len(items)).Dur("dur", time.Since(start)).
			Msg("Live refresh selesai")
		return items, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]domain.ONUListItem), nil
}

// GetPONList retrieves list of PONs with descriptions for a specific board
func (s *ONUService) GetPONList(ctx context.Context, oltID string, board int) ([]domain.PONInfo, error) {
	if s.cache != nil {
		cacheData, err := s.cache.GetPONList(ctx, oltID, board)
		if err == nil {
			var pons []domain.PONInfo
			if json.Unmarshal(cacheData, &pons) == nil {
				log.Debug().Str("oltId", oltID).Int("board", board).Msg("PON list from cache")
				return pons, nil
			}
		} else if err != redis.Nil {
			log.Warn().Err(err).Msg("Cache read error")
		}
	}

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

	if s.cache != nil && len(descriptions) > 0 {
		if data, err := json.Marshal(descriptions); err == nil {
			if err := s.cache.SetPONList(ctx, oltID, board, data); err != nil {
				log.Warn().Err(err).Msg("Failed to cache PON list")
			}
		}
	}

	return descriptions, nil
}
