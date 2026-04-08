package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"olt-monitor/internal/domain"
	"olt-monitor/internal/service"
)

type ProvisioningHandler struct {
	service *service.ProvisioningService
	manager *service.OLTManager
	activity *service.ActivityService
}

func NewProvisioningHandler(service *service.ProvisioningService, manager *service.OLTManager, activity *service.ActivityService) *ProvisioningHandler {
	return &ProvisioningHandler{
		service:  service,
		manager:  manager,
		activity: activity,
	}
}

// GetUnconfiguredONUs handles GET /api/v1/provisioning/unconfigured
func (h *ProvisioningHandler) GetUnconfiguredONUs(w http.ResponseWriter, r *http.Request) {
	oltID := r.URL.Query().Get("olt_id")
	if oltID == "" {
		BadRequest(w, "olt_id is required")
		return
	}

	onus, err := h.service.GetUnconfiguredONUs(oltID)
	if err != nil {
		InternalError(w, err.Error())
		return
	}

	Success(w, onus)
}

// ProvisionONU handles POST /api/v1/provisioning/execute
func (h *ProvisioningHandler) ProvisionONU(w http.ResponseWriter, r *http.Request) {
	var req domain.ProvisioningRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequest(w, "Invalid request payload")
		return
	}

	// Basic Validation
	if req.OLTID == "" || req.SN == "" || req.TemplateID == "" {
		BadRequest(w, "Missing required fields (oltId, sn, templateId)")
		return
	}

	resp, err := h.service.ProvisionONU(req)
	if err != nil {
		InternalError(w, err.Error())
		return
	}

	if resp != nil && resp.Success {
		if h.manager != nil {
			h.manager.InvalidateONUCache(r.Context(), req.OLTID, req.Board, req.PON, req.ONUID)
		}
		logActivity(h.activity, r, "onu.provision", req.OLTID, map[string]interface{}{
			"board":      req.Board,
			"pon":        req.PON,
			"onuId":      req.ONUID,
			"sn":         req.SN,
			"templateId": req.TemplateID,
		})
	}

	Success(w, resp)
}

// PreviewProvisioning handles POST /api/v1/provisioning/preview
func (h *ProvisioningHandler) PreviewProvisioning(w http.ResponseWriter, r *http.Request) {
	var req domain.ProvisioningRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequest(w, "Invalid request payload")
		return
	}

	// Basic Validation
	if req.OLTID == "" || req.SN == "" || req.TemplateID == "" {
		BadRequest(w, "Missing required fields (oltId, sn, templateId)")
		return
	}

	cmds, err := h.service.PreviewProvisioning(req)
	if err != nil {
		InternalError(w, err.Error())
		return
	}

	script := ""
	if len(cmds) > 0 {
		script = strings.Join(cmds, "\n")
	}

	Success(w, map[string]interface{}{
		"commands": cmds,
		"script":   script,
	})
}
