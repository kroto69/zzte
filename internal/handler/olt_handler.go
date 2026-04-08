package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"olt-monitor/internal/config"
	"olt-monitor/internal/domain"
	"olt-monitor/internal/service"
)

// OLTHandler handles OLT-related HTTP requests
type OLTHandler struct {
	manager  *service.OLTManager
	indexer  *service.IndexerService
	cfg      *config.Config
	telnet   *service.TelnetService
	activity *service.ActivityService
}

// NewOLTHandler creates a new OLT handler
func NewOLTHandler(manager *service.OLTManager, indexer *service.IndexerService, cfg *config.Config, telnet *service.TelnetService, activity *service.ActivityService) *OLTHandler {
	return &OLTHandler{
		manager:  manager,
		indexer:  indexer,
		cfg:      cfg,
		telnet:   telnet,
		activity: activity,
	}
}

// TestConnection tests connection to an OLT
// POST /api/v1/olt/test-connection
func (h *OLTHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	var req domain.OLTTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequest(w, "Invalid request body")
		return
	}

	// Validate required fields
	if req.Host == "" {
		BadRequest(w, "Host is required")
		return
	}
	if req.Port == 0 {
		req.Port = 161
	}
	if req.Community == "" {
		req.Community = "public"
	}

	config := domain.SNMPConfig{
		Host:      req.Host,
		Port:      req.Port,
		Community: req.Community,
		Timeout:   5,
		Retries:   2,
	}

	result, err := h.manager.TestConnection(r.Context(), config)
	if err != nil {
		log.Error().Err(err).Str("host", req.Host).Msg("Test connection failed")
		InternalError(w, "Failed to test OLT connection")
		return
	}

	Success(w, result)
}

// CreateOLT registers a new OLT
// POST /api/v1/olt
func (h *OLTHandler) CreateOLT(w http.ResponseWriter, r *http.Request) {
	var req domain.OLTCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequest(w, "Invalid request body")
		return
	}

	// Validate required fields
	if req.ID == "" {
		BadRequest(w, "ID is required")
		return
	}
	if req.SNMP.Host == "" {
		BadRequest(w, "SNMP host is required")
		return
	}

	applyOLTRequestDefaults(&req)
	instance := buildOLTInstance(req)

	result, err := h.manager.RegisterOLT(r.Context(), instance)
	if err != nil {
		if errors.Is(err, domain.ErrOLTAlreadyExists) {
			Conflict(w, "OLT with this ID already exists") // Changed from BadRequest
			return
		}
		log.Error().Err(err).Str("id", req.ID).Msg("Failed to register OLT")
		InternalError(w, "Failed to register OLT")
		return
	}

	// Persist to config
	if h.cfg.OLTs == nil {
		h.cfg.OLTs = make(map[string]config.OLTConfig)
	}
	h.cfg.OLTs[req.ID] = sanitizePersistedOLTConfig(req, nil)
	if err := h.cfg.Save(); err != nil {
		log.Error().Err(err).Msg("Failed to save configuration after adding OLT")
	}

	h.refreshVLANProfiles(req.ID, instance.Config)

	// Trigger search index update
	if h.indexer != nil {
		h.indexer.TriggerSync()
	}

	logActivity(h.activity, r, "olt.create", req.ID, map[string]interface{}{
		"name": req.Name,
		"host": req.SNMP.Host,
		"port": req.SNMP.Port,
	})

	Created(w, sanitizeOLTPointer(result))
}

// ListOLTs returns all registered OLTs
// GET /api/v1/olts
func (h *OLTHandler) ListOLTs(w http.ResponseWriter, r *http.Request) {
	olts := h.manager.ListOLTs()
	Success(w, sanitizeOLTInstances(olts))
}

// GetOLT returns a single OLT by ID
// GET /api/v1/olt/{oltId}
func (h *OLTHandler) GetOLT(w http.ResponseWriter, r *http.Request) {
	oltID := chi.URLParam(r, "oltId")
	if oltID == "" {
		BadRequest(w, "OLT ID is required")
		return
	}

	olt, err := h.manager.GetOLT(oltID)
	if err != nil {
		if errors.Is(err, domain.ErrOLTNotFound) {
			NotFound(w, "OLT not found")
			return
		}
		log.Error().Err(err).Str("oltId", oltID).Msg("Failed to load OLT")
		InternalError(w, "Failed to load OLT")
		return
	}

	Success(w, sanitizeOLTPointer(olt))
}

// DeleteOLT unregisters an OLT and clears its cache
// DELETE /api/v1/olt/{oltId}
func (h *OLTHandler) DeleteOLT(w http.ResponseWriter, r *http.Request) {
	oltID := chi.URLParam(r, "oltId")
	if oltID == "" {
		BadRequest(w, "OLT ID is required")
		return
	}

	err := h.manager.UnregisterOLT(r.Context(), oltID)
	if err != nil {
		if errors.Is(err, domain.ErrOLTNotFound) {
			NotFound(w, "OLT not found")
			return
		}
		log.Error().Err(err).Str("oltId", oltID).Msg("Failed to delete OLT")
		InternalError(w, "Failed to delete OLT")
		return
	}

	// Remove from config and save
	delete(h.cfg.OLTs, oltID)
	if err := h.cfg.Save(); err != nil {
		log.Error().Err(err).Msg("Failed to save configuration after deleting OLT")
	}

	logActivity(h.activity, r, "olt.delete", oltID, nil)

	MessageResponse(w, "OLT deleted successfully")
}

// UpdateOLT updates an existing OLT
// PUT /api/v1/olt/{oltId}
func (h *OLTHandler) UpdateOLT(w http.ResponseWriter, r *http.Request) {
	oltID := chi.URLParam(r, "oltId")
	if oltID == "" {
		BadRequest(w, "OLT ID is required")
		return
	}

	var req domain.OLTCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequest(w, "Invalid request body")
		return
	}

	// Validate (ID in body must match URL or be ignored/consistent)
	req.ID = oltID // Force ID from URL

	if req.SNMP.Host == "" {
		BadRequest(w, "SNMP host is required")
		return
	}

	applyOLTRequestDefaults(&req)
	instance := buildOLTInstance(req)

	// Check if OLT exists
	_, err := h.manager.GetOLT(oltID)
	if err != nil {
		if errors.Is(err, domain.ErrOLTNotFound) {
			NotFound(w, "OLT not found")
			return
		}
		log.Error().Err(err).Str("oltId", oltID).Msg("Failed to load OLT before update")
		InternalError(w, "Failed to load OLT")
		return
	}

	updated, err := h.manager.UpdateOLT(r.Context(), instance)
	if err != nil {
		if errors.Is(err, domain.ErrOLTNotFound) {
			NotFound(w, "OLT not found")
			return
		}
		log.Error().Err(err).Msg("Failed to update OLT")
		InternalError(w, "Failed to update OLT")
		return
	}

	// Update config persistence
	if h.cfg.OLTs == nil {
		h.cfg.OLTs = make(map[string]config.OLTConfig)
	}
	existingCfg := h.cfg.OLTs[oltID]
	h.cfg.OLTs[oltID] = sanitizePersistedOLTConfig(req, existingCfg.VlanProfiles)
	if err := h.cfg.Save(); err != nil {
		log.Error().Err(err).Msg("Failed to save configuration after updating OLT")
	}

	h.refreshVLANProfiles(oltID, updated.Config)

	Success(w, sanitizeOLTPointer(updated))

	logActivity(h.activity, r, "olt.update", oltID, map[string]interface{}{
		"name": updated.Name,
		"host": updated.SNMP.Host,
		"port": updated.SNMP.Port,
	})
}

func applyOLTRequestDefaults(req *domain.OLTCreateRequest) {
	if req.SNMP.Port == 0 {
		req.SNMP.Port = 161
	}
	if req.SNMP.Community == "" {
		req.SNMP.Community = "public"
	}
	if req.SNMP.Timeout == 0 {
		req.SNMP.Timeout = 5
	}
	if req.SNMP.Retries == 0 {
		req.SNMP.Retries = 2
	}
	if req.Telnet.Port == 0 {
		req.Telnet.Port = 23
	}
}

func buildOLTInstance(req domain.OLTCreateRequest) domain.OLTInstance {
	return domain.OLTInstance{
		ID:     req.ID,
		Name:   req.Name,
		SNMP:   req.SNMP,
		Telnet: req.Telnet,
		Config: domain.OLTConfig{
			ID:     req.ID,
			Name:   req.Name,
			SNMP:   req.SNMP,
			Telnet: req.Telnet,
		},
	}
}

func sanitizePersistedOLTConfig(req domain.OLTCreateRequest, profiles map[string]string) config.OLTConfig {
	return config.OLTConfig{
		Name:         req.Name,
		Host:         req.SNMP.Host,
		Port:         req.SNMP.Port,
		Community:    "",
		Timeout:      req.SNMP.Timeout,
		Retries:      req.SNMP.Retries,
		Telnet:       config.TelnetConfig{Port: req.Telnet.Port},
		VlanProfiles: cloneVLANProfiles(profiles),
	}
}

func cloneVLANProfiles(profiles map[string]string) map[string]string {
	if len(profiles) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(profiles))
	for vlan, profile := range profiles {
		cloned[vlan] = profile
	}

	return cloned
}

func shouldFetchVLANProfiles(telnet domain.TelnetConfig) bool {
	return strings.TrimSpace(telnet.User) != "" && strings.TrimSpace(telnet.Password) != ""
}

func (h *OLTHandler) refreshVLANProfiles(oltID string, runtimeConfig domain.OLTConfig) {
	if h.telnet == nil || !shouldFetchVLANProfiles(runtimeConfig.Telnet) {
		return
	}

	profiles, err := h.telnet.FetchVLANProfiles(runtimeConfig)
	if err != nil {
		log.Warn().Err(err).Str("oltId", oltID).Msg("Failed to fetch VLAN profiles")
		return
	}
	if len(profiles) == 0 {
		return
	}

	cfg := h.cfg.OLTs[oltID]
	cfg.VlanProfiles = cloneVLANProfiles(profiles)
	h.cfg.OLTs[oltID] = cfg
	if err := h.cfg.Save(); err != nil {
		log.Error().Err(err).Msg("Failed to save VLAN profiles")
	}
	if err := h.manager.UpdateVLANProfiles(oltID, profiles); err != nil {
		log.Warn().Err(err).Str("oltId", oltID).Msg("Failed to update VLAN profiles in memory")
	}
}

func sanitizeOLTPointer(olt *domain.OLTInstance) domain.OLTInstance {
	if olt == nil {
		return domain.OLTInstance{}
	}

	return sanitizeOLTInstance(*olt)
}

func sanitizeOLTInstances(olts []domain.OLTInstance) []domain.OLTInstance {
	if len(olts) == 0 {
		return nil
	}

	sanitized := make([]domain.OLTInstance, 0, len(olts))
	for _, olt := range olts {
		sanitized = append(sanitized, sanitizeOLTInstance(olt))
	}

	return sanitized
}

func sanitizeOLTInstance(olt domain.OLTInstance) domain.OLTInstance {
	olt.SNMP.Community = ""
	olt.Telnet.User = ""
	olt.Telnet.Password = ""
	olt.Config.SNMP.Community = ""
	olt.Config.Telnet.User = ""
	olt.Config.Telnet.Password = ""

	return olt
}
