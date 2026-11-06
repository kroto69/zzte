package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	sfGroup singleflight.Group // deduplikasi request SNMP per OLT+board+pon
	telnet  *TelnetService     // nil = tanpa enrichment Telnet (detail tetap jalan via SNMP)
}

// NewONUService creates a new ONU service
func NewONUService(manager *OLTManager, cache *cache.RedisCache, telnet *TelnetService) *ONUService {
	return &ONUService{
		manager: manager,
		cache:   cache,
		telnet:  telnet,
	}
}

// GetONUList retrieves list of ONUs on a specific PON port
// Returns minimal data (ID, Name, Status) for fast response
// Use GetONUDetail for complete ONU information
func (s *ONUService) GetONUList(ctx context.Context, oltID string, board, pon int, forceFresh bool) ([]domain.ONUListItem, error) {
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
		// Existence check; walks below use isolated clients (see below).
		if _, err := s.manager.GetClient(oltID); err != nil {
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

		// The 4 walks are independent (every PDU carries its ONU id in the OID
		// suffix), so run them in parallel on isolated SNMP clients.
		// snmp.Client serializes internally, and the background pollers already
		// prove the OLT tolerates several concurrent SNMP sessions.
		// NOTE: Offline reason is intentionally not walked for list to keep it fast.
		adapter, _ := s.manager.GetAdapter(oltID)

		type onuWalk struct {
			key string
			oid string
		}
		walks := []onuWalk{
			{key: "status", oid: snmp.BuildWalkOID(snmp.OIDONUStatus, ifIndex)},
			{key: "name", oid: snmp.BuildWalkOID(snmp.OIDONUName, ifIndex)},
			{key: "sn", oid: snmp.BuildWalkOID(snmp.OIDONUSerialNumber, ifIndex)},
			{key: "rx", oid: snmp.BuildWalkOID(snmp.OIDONURXPower, ifIndex)},
		}
		walkOIDByKey := make(map[string]string, len(walks))
		for _, w := range walks {
			walkOIDByKey[w.key] = w.oid
		}

		type walkOutcome struct {
			key  string
			pdus []gosnmp.SnmpPDU
			err  error
		}
		outcomes := make([]walkOutcome, len(walks))
		var walkWg sync.WaitGroup
		for i, w := range walks {
			walkWg.Add(1)
			go func(i int, key, oid string) {
				defer walkWg.Done()
				walkClient, err := s.manager.GetNewClient(oltID)
				if err != nil {
					outcomes[i] = walkOutcome{key: key, err: err}
					return
				}
				if err := walkClient.Connect(); err != nil {
					outcomes[i] = walkOutcome{key: key, err: fmt.Errorf("SNMP connect failed: %w", err)}
					return
				}
				defer walkClient.Close()
				log.Debug().Str("walk", key).Str("oid", oid).Msg("Walking ONU data")
				pdus, err := walkClient.BulkWalk(oid)
				outcomes[i] = walkOutcome{key: key, pdus: pdus, err: err}
			}(i, w.key, w.oid)
		}
		walkWg.Wait()

		for _, outcome := range outcomes {
			if outcome.err != nil {
				// The status walk is the discovery source: its failure fails
				// the request. The other walks stay best-effort, as before.
				if outcome.key == "status" {
					log.Error().Err(outcome.err).Msg("Failed to walk ONU status")
					return nil, fmt.Errorf("SNMP walk failed: %w", outcome.err)
				}
				log.Warn().Err(outcome.err).Str("walk", outcome.key).Msg("ONU walk failed, continuing without it")
				continue
			}
			baseOID := walkOIDByKey[outcome.key]
			switch outcome.key {
			case "status":
				for _, pdu := range outcome.pdus {
					if id, err := extractONUID(pdu.Name, baseOID); err == nil {
						onu := getONU(id)
						onu.Status = snmp.ParseONUStatus(&pdu)
						onu.StatusCode = snmp.ParseStatusToInt(&pdu)
					}
				}
			case "name":
				for _, pdu := range outcome.pdus {
					if id, err := extractONUID(pdu.Name, baseOID); err == nil {
						getONU(id).Name = strings.TrimSpace(snmp.PduToString(&pdu))
					}
				}
			case "sn":
				for _, pdu := range outcome.pdus {
					if id, err := extractONUID(pdu.Name, baseOID); err == nil {
						getONU(id).SerialNumber = snmp.ParseSerialNumber(&pdu)
					}
				}
			case "rx":
				for _, pdu := range outcome.pdus {
					if id, err := extractONUID(pdu.Name, baseOID); err == nil {
						raw := snmp.GetPDUInt(&pdu)
						if adapter != nil {
							getONU(id).RXPower = adapter.ConvertPower(raw)
						} else {
							getONU(id).RXPower = domain.ConvertPower(raw)
						}
					}
				}
			}
		}

		if len(onuMap) == 0 {
			log.Debug().Msg("No ONUs found")
			return []domain.ONUListItem{}, nil
		}

		log.Debug().Int("count", len(onuMap)).Msg("ONUs discovered")

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

	// Batch GET — 1 SNMP round-trip вместо 10 sequential request
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
			onu.RXPower = domain.ConvertPower(raw)
		}
	}

	// Kolom .14 adalah daya pancar laser modem (positif, ~+2,4 dBm) —
	// BUKAN "TX Power" yang juragan pantau. Nilai laser disimpan terpisah
	// di LaserTxPower; TXPower diisi dari up Rx via Telnet di bawah.
	if pdu, ok := pduMap[oidTXPower]; ok {
		raw := snmp.GetPDUInt(pdu)
		if adapter != nil {
			onu.LaserTxPower = adapter.ConvertPower(raw)
		} else {
			onu.LaserTxPower = domain.ConvertPower(raw)
		}
	}

	onu.LastOnline = snmp.ParseTime(getString(oidLastOnline))
	onu.LastOffline = snmp.ParseTime(getString(oidLastOffline))

	if pdu, ok := pduMap[oidOfflineReason]; ok {
		onu.OfflineReason = snmp.ParseOfflineReason(pdu)
		onu.OfflineCode = snmp.ParseOfflineReasonToInt(pdu)
	}

	onu.WanIp = getString(oidWanIp)

	// TX Power (up Rx: level yang diterima OLT dari modem, negatif)
	// tidak ada di SNMP — diambil via Telnet (`show pon power attenuation`)
	// HANYA saat detail dibuka, dan hanya untuk ONU yang online-like
	// (ONU offline/LOS tidak ada sinyalnya, jadi tidak perlu di-dial).
	// Best-effort: gagal Telnet tidak menggagalkan detail (TXPower tetap
	// 0.0 dan detail tetap tampil dengan data SNMP).
	s.enrichUpRxPower(oltID, board, pon, onuID, onu)

	if s.cache != nil {
		if data, err := json.Marshal(onu); err == nil {
			ttl := adaptiveTTL(cache.TTLONUDetail, time.Since(start))
			s.cache.SetONUDetailWithTTL(ctx, oltID, board, pon, onuID, data, ttl)
		}
	}

	return onu, nil
}

// enrichUpRxPower mengisi onu.TXPower dengan up Rx (level yang diterima OLT
// dari modem) via Telnet. Dipanggil hanya dari GetONUDetail — jadi TX hanya
// diambil saat user membuka detail ONU, tidak pernah dari list/poller.
// Gagal (Telnet tidak terkonfigurasi, slot penuh, OLT tidak menjawab) hanya
// dicatat di log; TXPower tetap 0.0 dan detail tetap tampil dengan data SNMP.
func (s *ONUService) enrichUpRxPower(oltID string, board, pon, onuID int, onu *domain.ONU) {
	if s.telnet == nil || !isOnlineLikeStatus(onu.StatusCode) {
		return
	}

	olt, err := s.manager.GetOLT(oltID)
	if err != nil {
		log.Warn().Err(err).Str("oltId", oltID).Msg("Gagal ambil config OLT untuk Telnet up Rx")
		return
	}
	// Tanpa kredensial Telnet, jangan coba dial sama sekali.
	if strings.TrimSpace(olt.Config.Telnet.User) == "" || strings.TrimSpace(olt.Config.Telnet.Password) == "" {
		return
	}

	att, err := s.telnet.FetchONUPowerAttenuation(olt.Config, board, pon, onuID)
	if err != nil {
		log.Warn().Err(err).Str("oltId", oltID).Int("board", board).Int("pon", pon).Int("onuId", onuID).Msg("Telnet up Rx gagal, TX Power tidak tersedia")
		return
	}
	if att != nil && att.OltRx != nil {
		onu.TXPower = domain.SanitizePower(*att.OltRx)
	}
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

// GetBoards returns the distinct slot numbers that have PON ports,
// discovered from a single SNMP walk of the PON description table.
// This reflects the OLT's actual populated slots (e.g. [1, 2] on a
// 2-slot C320) instead of a hardcoded range. A slot with no PON
// description entries is not reported.
func (s *ONUService) GetBoards(ctx context.Context, oltID string) ([]int, error) {
	client, err := s.manager.GetClient(oltID)
	if err != nil {
		return nil, err
	}

	results, err := client.Walk(snmp.OIDPONDescription)
	if err != nil {
		return nil, fmt.Errorf("SNMP walk failed: %w", err)
	}

	seen := make(map[int]struct{})
	for _, pdu := range results {
		suffix := strings.TrimPrefix(pdu.Name, snmp.OIDPONDescription+".")
		if suffix == pdu.Name {
			continue
		}
		ifIndex, err := strconv.Atoi(suffix)
		if err != nil {
			continue
		}
		_, slot, _ := snmp.ParseIfIndex(ifIndex)
		seen[slot] = struct{}{}
	}

	boards := make([]int, 0, len(seen))
	for slot := range seen {
		boards = append(boards, slot)
	}
	sort.Ints(boards)
	return boards, nil
}
