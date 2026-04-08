package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"

	"olt-monitor/internal/cache"
	"olt-monitor/internal/domain"
	"olt-monitor/internal/snmp"
)

// OLTManager manages multiple OLT connections
type OLTManager struct {
	olts  map[string]*OLTConnection
	cache *cache.RedisCache
	mu    sync.RWMutex
	opLocks map[string]chan struct{}
}

// OLTConnection holds SNMP client and adapter for an OLT
type OLTConnection struct {
	OLT     domain.OLTInstance
	Client  *snmp.Client
	Adapter snmp.FirmwareAdapter
}

// Global OLT manager instance
var (
	manager     *OLTManager
	managerOnce sync.Once
)

// GetOLTManager returns the singleton OLT manager
func GetOLTManager() *OLTManager {
	return manager
}

// InitOLTManager initializes the OLT manager singleton
func InitOLTManager(cache *cache.RedisCache) *OLTManager {
	managerOnce.Do(func() {
		manager = &OLTManager{
			olts:    make(map[string]*OLTConnection),
			cache:   cache,
			opLocks: make(map[string]chan struct{}),
		}
	})
	return manager
}

// RegisterOLT registers a new OLT with auto-detected firmware
func (m *OLTManager) RegisterOLT(ctx context.Context, olt domain.OLTInstance) (*domain.OLTInstance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if _, exists := m.olts[olt.ID]; exists {
		return nil, domain.ErrOLTAlreadyExists
	}

	// Create SNMP client
	client, err := snmp.NewClient(olt.SNMP)
	if err != nil {
		return nil, fmt.Errorf("failed to create SNMP client: %w", err)
	}

	// Connect to OLT
	if err := client.Connect(); err != nil {
		log.Warn().Err(err).Str("oltId", olt.ID).Msg("Failed to connect to OLT during registration (will retry later)")
		// Don't fail here, allow offline registration
	}

	// Detect firmware version
	adapter, err := snmp.DetectFirmware(client)
	if err != nil {
		log.Warn().Err(err).Str("oltId", olt.ID).Msg("Failed to detect firmware, using default")
		adapter = snmp.NewAdapterFromVersion("v1", "Unknown")
	}

	// Update OLT with detected firmware (Temporary: FirmwareVersion field removed from OLTInstance)
	// olt.FirmwareVersion = adapter.GetVersion()
	// olt.FullVersion = adapter.GetFullVersion()

	// Store connection
	m.olts[olt.ID] = &OLTConnection{
		OLT:     olt,
		Client:  client,
		Adapter: adapter,
	}

	log.Info().
		Str("oltId", olt.ID).
		Str("name", olt.Name).
		Str("host", olt.SNMP.Host).
		// Str("firmware", olt.FirmwareVersion).
		Msg("OLT registered")

	return &olt, nil
}

// UnregisterOLT removes an OLT and clears its cache
func (m *OLTManager) UnregisterOLT(ctx context.Context, oltID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, exists := m.olts[oltID]
	if !exists {
		return domain.ErrOLTNotFound
	}

	// Close SNMP connection
	if conn.Client != nil {
		conn.Client.Close()
	}

	// Remove from map
	delete(m.olts, oltID)
	delete(m.opLocks, oltID)

	// Invalidate cache
	if m.cache != nil {
		if err := m.cache.InvalidateOLT(ctx, oltID); err != nil {
			log.Warn().Err(err).Str("oltId", oltID).Msg("Failed to invalidate cache")
		}
	}

	log.Info().Str("oltId", oltID).Msg("OLT unregistered")
	return nil
}

// AcquireSNMPLock serializes SNMP operations per OLT to avoid contention
func (m *OLTManager) AcquireSNMPLock(ctx context.Context, oltID string) (func(), error) {
	m.mu.Lock()
	lock, ok := m.opLocks[oltID]
	if !ok {
		lock = make(chan struct{}, 1)
		m.opLocks[oltID] = lock
	}
	m.mu.Unlock()

	select {
	case lock <- struct{}{}:
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// InvalidateONUCache clears cached ONU list/detail for a specific PON/ONU
func (m *OLTManager) InvalidateONUCache(ctx context.Context, oltID string, board, pon, onuID int) {
	if m.cache == nil {
		return
	}
	if err := m.cache.InvalidateONUList(ctx, oltID, board, pon); err != nil {
		log.Warn().Err(err).Str("oltId", oltID).Msg("Failed to invalidate ONU list cache")
	}
	if onuID > 0 {
		if err := m.cache.InvalidateONUDetail(ctx, oltID, board, pon, onuID); err != nil {
			log.Warn().Err(err).Str("oltId", oltID).Msg("Failed to invalidate ONU detail cache")
		}
	}
}

// GetClient returns the SNMP client for an OLT
func (m *OLTManager) GetClient(oltID string) (*snmp.Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.olts[oltID]
	if !exists {
		return nil, domain.ErrOLTNotFound
	}
	return conn.Client, nil
}

// GetNewClient creates and returns a new isolated SNMP client for an OLT
// Useful for background tasks that shouldn't block the main client
func (m *OLTManager) GetNewClient(oltID string) (*snmp.Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.olts[oltID]
	if !exists {
		return nil, domain.ErrOLTNotFound
	}

	// Create new client from existing config
	client, err := conn.Client.Clone()
	if err != nil {
		return nil, err
	}

	return client, nil
}

// GetAdapter returns the firmware adapter for an OLT
func (m *OLTManager) GetAdapter(oltID string) (snmp.FirmwareAdapter, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.olts[oltID]
	if !exists {
		return nil, domain.ErrOLTNotFound
	}
	return conn.Adapter, nil
}

// GetOLT returns a single OLT instance
func (m *OLTManager) GetOLT(oltID string) (*domain.OLTInstance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.olts[oltID]
	if !exists {
		return nil, domain.ErrOLTNotFound
	}
	return &conn.OLT, nil
}

// ListOLTs returns all registered OLTs
func (m *OLTManager) ListOLTs() []domain.OLTInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	olts := make([]domain.OLTInstance, 0, len(m.olts))
	for _, conn := range m.olts {
		olts = append(olts, conn.OLT)
	}
	return olts
}

// TestConnection tests connection to an OLT without registering
func (m *OLTManager) TestConnection(ctx context.Context, config domain.SNMPConfig) (*domain.OLTTestResponse, error) {
	// Create temporary client
	client, err := snmp.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create SNMP client: %w", err)
	}

	// Connect
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	// Detect firmware
	adapter, err := snmp.DetectFirmware(client)
	if err != nil {
		return nil, fmt.Errorf("failed to detect firmware: %w", err)
	}

	return &domain.OLTTestResponse{
		FirmwareVersion: adapter.GetVersion(),
		FullVersion:     adapter.GetFullVersion(),
	}, nil
}

// GetConnection returns full connection info for an OLT
func (m *OLTManager) GetConnection(oltID string) (*OLTConnection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.olts[oltID]
	if !exists {
		return nil, domain.ErrOLTNotFound
	}
	return conn, nil
}

// UpdateOLT updates an existing OLT configuration
func (m *OLTManager) UpdateOLT(ctx context.Context, olt domain.OLTInstance) (*domain.OLTInstance, error) {
	// Unregister existing
	if err := m.UnregisterOLT(ctx, olt.ID); err != nil && err != domain.ErrOLTNotFound {
		return nil, err
	}

	// Re-register with new config
	return m.RegisterOLT(ctx, olt)
}

// UpdateVLANProfiles updates VLAN profiles mapping for an OLT in memory
func (m *OLTManager) UpdateVLANProfiles(oltID string, profiles map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, exists := m.olts[oltID]
	if !exists {
		return domain.ErrOLTNotFound
	}

	conn.OLT.Config.VlanProfiles = profiles
	return nil
}
