package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"olt-monitor/internal/cache"
	"olt-monitor/internal/domain"
	"olt-monitor/internal/service"
)

// SearchHandler handles global search requests
type SearchHandler struct {
	cache   *cache.RedisCache
	indexer *service.IndexerService
}

// NewSearchHandler creates a new search handler
func NewSearchHandler(cache *cache.RedisCache, indexer *service.IndexerService) *SearchHandler {
	return &SearchHandler{
		cache:   cache,
		indexer: indexer,
	}
}

// ForceSync triggers a manual background sync
// POST /api/v1/search/sync
func (h *SearchHandler) ForceSync(w http.ResponseWriter, r *http.Request) {
	if h.indexer == nil {
		InternalError(w, "Indexer service not available")
		return
	}
	h.indexer.TriggerSync()
	MessageResponse(w, "Background sync triggered")
}

// GetConfig returns the current search configuration
// GET /api/v1/search/config
func (h *SearchHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	if h.indexer == nil {
		InternalError(w, "Indexer service not available")
		return
	}

	config := map[string]interface{}{
		"enabled": h.indexer.IsEnabled(),
		// "interval": h.indexer.GetInterval(), // To be added if needed
	}
	Success(w, config)
}

// UpdateConfig updates the search configuration
// POST /api/v1/search/config
func (h *SearchHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	if h.indexer == nil {
		InternalError(w, "Indexer service not available")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequest(w, "Invalid request body")
		return
	}

	if err := h.indexer.SetEnabled(req.Enabled); err != nil {
		log.Error().Err(err).Msg("Failed to update search config")
		InternalError(w, "Failed to save configuration")
		return
	}

	MessageResponse(w, "Search configuration updated")
}

// GetStats returns basic stats for the search index
// GET /api/v1/search/stats
func (h *SearchHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		InternalError(w, "Search is unavailable (Cache disabled)")
		return
	}

	data, err := h.cache.GetGlobalIndex(r.Context())
	if err != nil {
		// Keep consistent with Search: return zero stats if index is missing
		Success(w, map[string]int{
			"total":   0,
			"online":  0,
			"offline": 0,
			"los":     0,
		})
		return
	}

	var allItems []domain.SearchItem
	if err := json.Unmarshal(data, &allItems); err != nil {
		InternalError(w, "Index corruption detected")
		return
	}

	online := 0
	offline := 0
	los := 0
	for _, item := range allItems {
		status := strings.ToLower(item.Status)
		if strings.Contains(status, "online") {
			online++
		} else if strings.Contains(status, "los") {
			los++
		} else {
			offline++
		}
	}

	Success(w, map[string]int{
		"total":   len(allItems),
		"online":  online,
		"offline": offline,
		"los":     los,
	})
}

// Search performs a global search on the indexed data
// GET /api/v1/search?q=query
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(r.URL.Query().Get("q"))
	if query == "" || len(query) < 2 {
		BadRequest(w, "Query must be at least 2 characters")
		return
	}

	if h.cache == nil {
		InternalError(w, "Search is unavailable (Cache disabled)")
		return
	}

	// Fetch full index from Redis
	data, err := h.cache.GetGlobalIndex(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to get search index")
		// Fallback: return empty list instead of error
		Success(w, []domain.SearchItem{})
		return
	}

	var allItems []domain.SearchItem
	if err := json.Unmarshal(data, &allItems); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal search index")
		InternalError(w, "Index corruption detected")
		return
	}

	// Filter in memory (fast for < 100k items)
	var results []domain.SearchItem
	count := 0
	limit := 50 // limit results

	for _, item := range allItems {
		match := false

		// Search by Name
		if strings.Contains(strings.ToLower(item.Name), query) {
			match = true
		}
		// Search by SN
		if !match && strings.Contains(strings.ToLower(item.SerialNumber), query) {
			match = true
		}
		// Search by ONU ID
		if !match && strings.Contains(strings.ToLower(item.OltID), query) {
			match = true
		}
		// TODO: Search by Status?

		if match {
			results = append(results, item)
			count++
			if count >= limit {
				break
			}
		}
	}

	Success(w, results)
}
