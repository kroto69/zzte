package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"olt-monitor/internal/domain"
	"olt-monitor/internal/service"
)

// ONUHandler handles ONU-related HTTP requests
type ONUHandler struct {
	onuService *service.ONUService
}

// NewONUHandler creates a new ONU handler
func NewONUHandler(onuService *service.ONUService) *ONUHandler {
	return &ONUHandler{onuService: onuService}
}

// GetONUList returns list of ONUs on a PON port
// GET /api/v1/olt/{oltId}/board/{board}/pon/{pon}
func (h *ONUHandler) GetONUList(w http.ResponseWriter, r *http.Request) {
	oltID := chi.URLParam(r, "oltId")
	boardStr := chi.URLParam(r, "board")
	ponStr := chi.URLParam(r, "pon")

	if oltID == "" {
		BadRequest(w, "OLT ID is required")
		return
	}

	board, err := strconv.Atoi(boardStr)
	if err != nil || board < 1 {
		BadRequest(w, "Invalid board number")
		return
	}

	pon, err := strconv.Atoi(ponStr)
	if err != nil || pon < 1 {
		BadRequest(w, "Invalid PON number")
		return
	}

	log.Debug().
		Str("oltId", oltID).
		Int("board", board).
		Int("pon", pon).
		Msg("Getting ONU list")

	fresh := r.URL.Query().Get("fresh")
	force := fresh == "1" || strings.ToLower(fresh) == "true"

	onus, err := h.onuService.GetONUList(r.Context(), oltID, board, pon, force)
	if err != nil {
		if errors.Is(err, domain.ErrOLTNotFound) {
			NotFound(w, "OLT not found")
			return
		}
		log.Error().Err(err).Msg("Failed to get ONU list")
		InternalError(w, "Failed to get ONU list: "+err.Error())
		return
	}

	Success(w, onus)
}

// GetONUDetail returns detailed info for a single ONU
// GET /api/v1/olt/{oltId}/board/{board}/pon/{pon}/onu/{onuId}
func (h *ONUHandler) GetONUDetail(w http.ResponseWriter, r *http.Request) {
	oltID := chi.URLParam(r, "oltId")
	boardStr := chi.URLParam(r, "board")
	ponStr := chi.URLParam(r, "pon")
	onuIDStr := chi.URLParam(r, "onuId")

	if oltID == "" {
		BadRequest(w, "OLT ID is required")
		return
	}

	board, err := strconv.Atoi(boardStr)
	if err != nil || board < 1 {
		BadRequest(w, "Invalid board number")
		return
	}

	pon, err := strconv.Atoi(ponStr)
	if err != nil || pon < 1 {
		BadRequest(w, "Invalid PON number")
		return
	}

	onuID, err := strconv.Atoi(onuIDStr)
	if err != nil || onuID < 1 {
		BadRequest(w, "Invalid ONU ID")
		return
	}

	log.Debug().
		Str("oltId", oltID).
		Int("board", board).
		Int("pon", pon).
		Int("onuId", onuID).
		Msg("Getting ONU detail")

	fresh := r.URL.Query().Get("fresh")
	force := fresh == "1" || strings.ToLower(fresh) == "true"

	onu, err := h.onuService.GetONUDetail(r.Context(), oltID, board, pon, onuID, force)
	if err != nil {
		if errors.Is(err, domain.ErrOLTNotFound) {
			NotFound(w, "OLT not found")
			return
		}
		if errors.Is(err, domain.ErrONUNotFound) {
			NotFound(w, "ONU not found")
			return
		}
		log.Error().Err(err).Msg("Failed to get ONU detail")
		InternalError(w, "Failed to get ONU detail: "+err.Error())
		return
	}

	Success(w, onu)
}

// GetPONList returns list of PONs with descriptions
// GET /api/v1/olt/{oltId}/board/{board}/pon
func (h *ONUHandler) GetPONList(w http.ResponseWriter, r *http.Request) {
	oltID := chi.URLParam(r, "oltId")
	boardStr := chi.URLParam(r, "board")

	board, err := strconv.Atoi(boardStr)
	if err != nil {
		BadRequest(w, "Invalid board ID")
		return
	}

	pons, err := h.onuService.GetPONList(r.Context(), oltID, board)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get PON list")
		InternalError(w, err.Error())
		return
	}

	Success(w, pons)
}
