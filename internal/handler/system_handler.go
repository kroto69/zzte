package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"olt-monitor/internal/service"
)

// SystemHandler handles system-related HTTP requests
type SystemHandler struct {
	systemService *service.SystemService
}

// NewSystemHandler creates a new system handler
func NewSystemHandler(systemService *service.SystemService) *SystemHandler {
	return &SystemHandler{
		systemService: systemService,
	}
}

// GetAllSystemInfo returns system info for all OLTs
// GET /api/v1/system/olts
func (h *SystemHandler) GetAllSystemInfo(w http.ResponseWriter, r *http.Request) {
	infos, err := h.systemService.GetAllSystemInfo(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	Success(w, infos)
}

// GetSystemInfo returns system info for a specific OLT
// GET /api/v1/system/olt/{oltId}
func (h *SystemHandler) GetSystemInfo(w http.ResponseWriter, r *http.Request) {
	oltID := chi.URLParam(r, "oltId")
	if oltID == "" {
		Error(w, http.StatusBadRequest, "OLT ID required")
		return
	}

	info, err := h.systemService.GetSystemInfo(r.Context(), oltID)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}
	Success(w, info)
}
