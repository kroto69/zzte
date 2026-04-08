package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"olt-monitor/internal/cache"
	"olt-monitor/internal/config"
	"olt-monitor/internal/domain"
	"olt-monitor/internal/snmp"
)

// IndexerService manages background indexing of ONUs
type IndexerService struct {
	manager *OLTManager
	cache   *cache.RedisCache
	cfg     *config.Config
	mu      sync.RWMutex
}

// NewIndexerService creates a new indexer service
func NewIndexerService(manager *OLTManager, cache *cache.RedisCache, cfg *config.Config) *IndexerService {
	return &IndexerService{
		manager: manager,
		cache:   cache,
		cfg:     cfg,
	}
}

// StartBackgroundSync starts the periodic synchronization
func (s *IndexerService) StartBackgroundSync(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		if s.IsEnabled() {
			log.Info().Dur("interval", interval).Msg("Starting background search indexer (Enabled)")
			// Initial sync should run if enabled
			if err := s.SyncAll(context.Background()); err != nil {
				log.Error().Err(err).Msg("Initial sync failed")
			}
		} else {
			log.Info().Msg("Background search indexer is DISABLED at startup")
		}

		for range ticker.C {
			if s.IsEnabled() {
				if err := s.SyncAll(context.Background()); err != nil {
					log.Error().Err(err).Msg("Scheduled sync failed")
				}
			}
		}
	}()
}

// IsEnabled checks if background sync is enabled
func (s *IndexerService) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Search.Enabled
}

// SetEnabled updates the enabled status
func (s *IndexerService) SetEnabled(enabled bool) error {
	s.mu.Lock()
	s.cfg.Search.Enabled = enabled
	s.mu.Unlock()

	// Persist to file
	return s.cfg.Save()
}

// TriggerSync forces an immediate sync (async)
func (s *IndexerService) TriggerSync() {
	go func() {
		if err := s.SyncAll(context.Background()); err != nil {
			log.Error().Err(err).Msg("Triggered sync failed")
		}
	}()
}

// SyncAll performs a full sync of all registered OLTs
func (s *IndexerService) SyncAll(ctx context.Context) error {
	log.Info().Msg("Starting global index sync")
	start := time.Now()

	olts := s.manager.ListOLTs()
	var allItems []domain.SearchItem
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Process OLTs in parallel
	for _, olt := range olts {
		wg.Add(1)
		go func(olt domain.OLTInstance) {
			defer wg.Done()
			items, err := s.WalkOLT(ctx, olt.ID)
			if err != nil {
				log.Error().Err(err).Str("oltId", olt.ID).Msg("Failed to index OLT")
				return
			}
			mu.Lock()
			allItems = append(allItems, items...)
			mu.Unlock()
		}(olt) // Pass by value is fine given small struct
	}

	wg.Wait()

	// Store in Cache
	if s.cache != nil {
		data, err := json.Marshal(allItems)
		if err != nil {
			return err
		}
		if err := s.cache.SetGlobalIndex(ctx, data); err != nil {
			return err
		}
	}

	log.Info().
		Int("items", len(allItems)).
		Dur("duration", time.Since(start)).
		Msg("Global index sync completed")
	return nil
}

// WalkOLT retrieves all ONUs from an OLT using optimized bulk walks
func (s *IndexerService) WalkOLT(ctx context.Context, oltID string) ([]domain.SearchItem, error) {
	release, err := s.manager.AcquireSNMPLock(ctx, oltID)
	if err != nil {
		return nil, err
	}
	defer release()

	// Use a dedicated client for indexing to avoid blocking the dashboard
	client, err := s.manager.GetNewClient(oltID)
	if err != nil {
		return nil, err
	}

	// Connect
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect for indexing: %w", err)
	}
	defer client.Close()

	log.Debug().Str("oltId", oltID).Msg("Walking OLT for indexing")

	// We need 3 main pieces of info: Status, Name, SerialNumber
	// We'll use maps to correlate by ifIndex + onuID

	type tempONU struct {
		Board  int
		Pon    int
		OnuID  int
		Status string
		Name   string
		SN     string
	}

	onuMap := make(map[string]*tempONU) // Key: "ifIndex.onuID"

	getOrCreate := func(ifIndex, onuID int) *tempONU {
		key := fmt.Sprintf("%d.%d", ifIndex, onuID)
		if _, exists := onuMap[key]; !exists {
			_, slot, port := snmp.ParseIfIndex(ifIndex)
			onuMap[key] = &tempONU{
				Board: slot,
				Pon:   port,
				OnuID: onuID,
			}
		}
		return onuMap[key]
	}

	// 1. Walk Status (Base for discovery)
	results, err := client.Walk(snmp.OIDONUStatus)
	if err != nil {
		return nil, fmt.Errorf("failed walking status: %w", err)
	}

	for _, pdu := range results {
		// OID: ...1012.3.28.2.1.4.{ifIndex}.{onuId}
		// Extract ifIndex and onuId
		// Suffix is ifIndex.onuId
		suffix := strings.TrimPrefix(pdu.Name, snmp.OIDONUStatus+".")
		parts := strings.Split(suffix, ".")
		if len(parts) >= 2 {
			ifIndex, _ := strconv.Atoi(parts[0])
			onuID, _ := strconv.Atoi(parts[1])

			onu := getOrCreate(ifIndex, onuID)
			onu.Status = snmp.ParseONUStatus(&pdu)
		}
	}

	// 2. Walk Names
	// Optimized: Only walk validation matching OIDs to avoid large mismatch if possible,
	// but standard walk is safe.
	nameResults, err := client.Walk(snmp.OIDONUName)
	if err == nil {
		for _, pdu := range nameResults {
			suffix := strings.TrimPrefix(pdu.Name, snmp.OIDONUName+".")
			parts := strings.Split(suffix, ".")
			if len(parts) >= 2 {
				ifIndex, _ := strconv.Atoi(parts[0])
				onuID, _ := strconv.Atoi(parts[1])

				// Only update if exists (from status walk)
				// Or add new? Better to rely on Status as source of truth for "Active enough to be indexed"?
				// Actually, Status OID is unreliable for some states? No, it's the specific Status OID.
				// Let's assume Status is primary.
				key := fmt.Sprintf("%d.%d", ifIndex, onuID)
				if val, ok := onuMap[key]; ok {
					val.Name = snmp.PduToString(&pdu)
				}
			}
		}
	}

	// 3. Walk Serial Numbers
	snResults, err := client.Walk(snmp.OIDONUSerialNumber)
	if err == nil {
		for _, pdu := range snResults {
			suffix := strings.TrimPrefix(pdu.Name, snmp.OIDONUSerialNumber+".")
			parts := strings.Split(suffix, ".")
			if len(parts) >= 2 {
				ifIndex, _ := strconv.Atoi(parts[0])
				onuID, _ := strconv.Atoi(parts[1])

				key := fmt.Sprintf("%d.%d", ifIndex, onuID)
				if val, ok := onuMap[key]; ok {
					val.SN = snmp.ParseSerialNumber(&pdu)
				}
			}
		}
	}

	// Convert to SearchItems
	items := make([]domain.SearchItem, 0, len(onuMap))
	for _, o := range onuMap {
		items = append(items, domain.SearchItem{
			OltID:        oltID,
			Board:        o.Board,
			Pon:          o.Pon,
			OnuID:        o.OnuID,
			Name:         o.Name,
			SerialNumber: o.SN,
			Status:       o.Status,
		})
	}

	return items, nil
}
