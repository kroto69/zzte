package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"olt-monitor/internal/cache"
	"olt-monitor/internal/domain"
)

// ActivityService manages audit log entries
type ActivityService struct {
	cache *cache.RedisCache
	mu    sync.Mutex
	mem   []domain.Activity
	max   int
}

// NewActivityService creates a new activity service
func NewActivityService(redisCache *cache.RedisCache) *ActivityService {
	return &ActivityService{
		cache: redisCache,
		max:   cache.MaxActivityEntries,
	}
}

// Log stores an activity entry
func (s *ActivityService) Log(ctx context.Context, entry domain.Activity) {
	if entry.Time.IsZero() {
		entry.Time = time.Now().UTC()
	}
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	if s.cache != nil {
		if data, err := json.Marshal(entry); err == nil {
			_ = s.cache.AppendActivity(ctx, data)
			return
		}
	}

	// Fallback to in-memory buffer
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mem = append([]domain.Activity{entry}, s.mem...)
	if s.max > 0 && len(s.mem) > s.max {
		s.mem = s.mem[:s.max]
	}
}

// List returns the latest activity entries
func (s *ActivityService) List(ctx context.Context, limit int) ([]domain.Activity, error) {
	if limit <= 0 || limit > s.max {
		limit = s.max
	}

	if s.cache != nil {
		items, err := s.cache.ListActivities(ctx, limit)
		if err == nil {
			activities := make([]domain.Activity, 0, len(items))
			for _, item := range items {
				var a domain.Activity
				if err := json.Unmarshal(item, &a); err == nil {
					activities = append(activities, a)
				}
			}
			return activities, nil
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if limit > len(s.mem) {
		limit = len(s.mem)
	}
	result := make([]domain.Activity, 0, limit)
	result = append(result, s.mem[:limit]...)
	return result, nil
}
