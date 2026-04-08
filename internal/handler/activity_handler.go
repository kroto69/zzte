package handler

import (
	"net/http"
	"strconv"

	"olt-monitor/internal/service"
)

// ActivityHandler handles audit log requests
type ActivityHandler struct {
	service *service.ActivityService
}

// NewActivityHandler creates a new activity handler
func NewActivityHandler(service *service.ActivityService) *ActivityHandler {
	return &ActivityHandler{service: service}
}

// List returns recent activity logs
// GET /api/v1/activity?limit=100
func (h *ActivityHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if val := r.URL.Query().Get("limit"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			limit = parsed
		}
	}

	items, err := h.service.List(r.Context(), limit)
	if err != nil {
		InternalError(w, err.Error())
		return
	}

	Success(w, items)
}
