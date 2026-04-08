package handler

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"

	"olt-monitor/internal/service"
)

type ControlHandler struct {
	oltManager    *service.OLTManager
	telnetService *service.TelnetService
	activity      *service.ActivityService
}

func NewControlHandler(oltManager *service.OLTManager, telnetService *service.TelnetService, activity *service.ActivityService) *ControlHandler {
	return &ControlHandler{
		oltManager:    oltManager,
		telnetService: telnetService,
		activity:      activity,
	}
}

type RebootRequest struct {
	OLTID string `json:"olt_id"`
	Board int    `json:"board"`
	Pon   int    `json:"pon"`
	ONUID int    `json:"onu_id"`
}

// RebootONU handles the API request to reboot an ONU
func (h *ControlHandler) RebootONU(w http.ResponseWriter, r *http.Request) {
	var req RebootRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequest(w, "Invalid request body")
		return
	}

	if req.OLTID == "" || req.Board < 1 || req.Pon < 1 || req.ONUID < 1 {
		BadRequest(w, "Invalid parameters")
		return
	}

	log.Info().
		Str("oltId", req.OLTID).
		Int("board", req.Board).
		Int("pon", req.Pon).
		Int("onuId", req.ONUID).
		Msg("Received Reboot Request")

	// Get OLT Config
	olt, err := h.oltManager.GetOLT(req.OLTID)
	if err != nil {
		NotFound(w, "OLT not found")
		return
	}

	// Check if Telnet credentials are set
	if olt.Config.Telnet.User == "" || olt.Config.Telnet.Password == "" {
		BadRequest(w, "Telnet credentials not configured for this OLT")
		return
	}

	// Trigger Reboot (Sync or Async?)
	// User agreed to "return Success immediately", but Go allows us to just run it.
	// If it takes ~5 seconds, we can probably wait. If 30s, async is better.
	// The user flow says "Wait 30 seconds" is AFTER execution.
	// The execution itself (Telnet login -> reboot) takes maybe 2-5 seconds.
	// So we can do it synchronously to ensure the command was actually SENT.

	err = h.telnetService.RebootONU(olt.Config, req.Board, req.Pon, req.ONUID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to reboot ONU via Telnet")
		logActivity(h.activity, r, "onu.reboot.failed", req.OLTID, map[string]interface{}{
			"board":  req.Board,
			"pon":    req.Pon,
			"onuId":  req.ONUID,
			"error":  err.Error(),
		})
		InternalError(w, "Failed to send reboot command: "+err.Error())
		return
	}

	h.oltManager.InvalidateONUCache(r.Context(), req.OLTID, req.Board, req.Pon, req.ONUID)
	logActivity(h.activity, r, "onu.reboot", req.OLTID, map[string]interface{}{
		"board": req.Board,
		"pon":   req.Pon,
		"onuId": req.ONUID,
	})

	Success(w, map[string]string{
		"status":  "success",
		"message": "Reboot command sent successfully. ONU will reconnect shortly.",
	})
}
