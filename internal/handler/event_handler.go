package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"olt-monitor/internal/cache"
)

// EventHandler: akses event up/down yang masuk via syslog + alert.
//
//  GET /api/v1/events/stream        SSE — dorong event & alert realtime
//  GET /api/v1/events/recent?limit= event log terakhir (JSON)
//  GET /api/v1/alerts?limit=        alert down yang lolos anti-spam (JSON)
type EventHandler struct {
	cache *cache.RedisCache
}

func NewEventHandler(c *cache.RedisCache) *EventHandler {
	return &EventHandler{cache: c}
}

// Stream: Server-Sent Events. Tanpa dependency websocket — stdlib cukup.
func (h *EventHandler) Stream(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		InternalError(w, "cache tidak tersedia")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		InternalError(w, "streaming tidak didukung")
		return
	}

	ch, closeSub := h.cache.SubscribeEvents(r.Context())
	defer closeSub()
	ach, closeAlert := h.cache.SubscribeAlerts(r.Context())
	defer closeAlert()

	// Kirim status awal agar client tahu stream hidup.
	fmt.Fprintf(w, "event: hello\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: onu\ndata: %s\n\n", msg)
			flusher.Flush()
		case msg, ok := <-ach:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: alert\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// Alerts: alert down yang lolos anti-spam. Untuk ditampilkan frontend.
func (h *EventHandler) Alerts(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		InternalError(w, "cache tidak tersedia")
		return
	}
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	raw, err := h.cache.RecentAlerts(r.Context(), limit)
	if err != nil {
		log.Error().Err(err).Msg("Gagal baca alert log")
		InternalError(w, "gagal baca alert")
		return
	}
	out := make([]json.RawMessage, 0, len(raw))
	for _, b := range raw {
		out = append(out, json.RawMessage(b))
	}
	Success(w, out)
}

// Recent: event log terakhir.
func (h *EventHandler) Recent(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		InternalError(w, "cache tidak tersedia")
		return
	}
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	raw, err := h.cache.RecentEvents(r.Context(), limit)
	if err != nil {
		log.Error().Err(err).Msg("Gagal baca event log")
		InternalError(w, "gagal baca event log")
		return
	}
	out := make([]json.RawMessage, 0, len(raw))
	for _, b := range raw {
		out = append(out, json.RawMessage(b))
	}
	Success(w, out)
}
