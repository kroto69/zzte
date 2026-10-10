package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"olt-monitor/internal/domain"
)

// TTL constants for different cache types
const (
	TTLOLTInfo    = 5 * time.Minute
	TTLONUList    = 60 * time.Second
	TTLONUDetail  = 2 * time.Minute
	TTLONUNames   = 10 * time.Hour
	TTLHealth     = 5 * time.Minute
	TTLPONList    = 5 * time.Minute
	MaxActivityEntries = 500
)

// Cache key patterns
const (
	KeyOLTInfo     = "olt:%s:info"
	KeyOLTFirmware = "olt:%s:firmware"
	KeyONUList     = "olt:%s:board:%d:pon:%d:list"
	KeyONUNames    = "olt:%s:board:%d:pon:%d:names"
	KeyONUDetail   = "olt:%s:onu:%d:%d:%d"
	KeyOLTHealth   = "olt:%s:health"
	KeyPONList     = "olt:%s:board:%d:pon:list"
	KeyOLTPattern  = "olt:%s:*"
	KeyActivityLog = "activity:log"
	// PATCH(oltwtch-event): kunci event-driven
	KeyEventBus  = "zzte:events"     // pub/sub channel
	KeyEventLog  = "zzte:events:log" // list terbatas
	KeyAlertBus  = "zzte:alerts"     // pub/sub channel alert
	KeyAlertLog  = "zzte:alerts:log" // list alert terbatas
	MaxEventEntries = 500
	MaxAlertEntries = 500
)

// RedisCache wraps Redis client with OLT-specific operations
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache creates a new Redis cache instance
func NewRedisCache(addr, password string, db int) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log.Info().Str("addr", addr).Msg("Connected to Redis")
	return &RedisCache{client: client}, nil
}

// Close closes the Redis connection
func (c *RedisCache) Close() error {
	return c.client.Close()
}

// --- OLT Info ---

// GetOLTInfo retrieves OLT info from cache
func (c *RedisCache) GetOLTInfo(ctx context.Context, oltID string) ([]byte, error) {
	key := fmt.Sprintf(KeyOLTInfo, oltID)
	return c.client.Get(ctx, key).Bytes()
}

// SetOLTInfo stores OLT info in cache
func (c *RedisCache) SetOLTInfo(ctx context.Context, oltID string, data []byte) error {
	key := fmt.Sprintf(KeyOLTInfo, oltID)
	return c.client.Set(ctx, key, data, TTLOLTInfo).Err()
}

// --- ONU List ---

// GetONUList retrieves ONU list from cache
func (c *RedisCache) GetONUList(ctx context.Context, oltID string, board, pon int) ([]byte, error) {
	key := fmt.Sprintf(KeyONUList, oltID, board, pon)
	return c.client.Get(ctx, key).Bytes()
}

// SetONUList stores ONU list in cache
func (c *RedisCache) SetONUList(ctx context.Context, oltID string, board, pon int, data []byte) error {
	key := fmt.Sprintf(KeyONUList, oltID, board, pon)
	return c.client.Set(ctx, key, data, TTLONUList).Err()
}

// SetONUListWithTTL stores ONU list with a custom TTL
func (c *RedisCache) SetONUListWithTTL(ctx context.Context, oltID string, board, pon int, data []byte, ttl time.Duration) error {
	key := fmt.Sprintf(KeyONUList, oltID, board, pon)
	if ttl <= 0 {
		ttl = TTLONUList
	}
	return c.client.Set(ctx, key, data, ttl).Err()
}

// --- ONU Names ---

func (c *RedisCache) GetONUNames(ctx context.Context, oltID string, board, pon int) ([]byte, error) {
	key := fmt.Sprintf(KeyONUNames, oltID, board, pon)
	return c.client.Get(ctx, key).Bytes()
}

func (c *RedisCache) SetONUNames(ctx context.Context, oltID string, board, pon int, data []byte) error {
	key := fmt.Sprintf(KeyONUNames, oltID, board, pon)
	return c.client.Set(ctx, key, data, TTLONUNames).Err()
}

// --- ONU Detail ---

// GetONUDetail retrieves single ONU detail from cache
func (c *RedisCache) GetONUDetail(ctx context.Context, oltID string, board, pon, onuID int) ([]byte, error) {
	key := fmt.Sprintf(KeyONUDetail, oltID, board, pon, onuID)
	return c.client.Get(ctx, key).Bytes()
}

// SetONUDetail stores single ONU detail in cache
func (c *RedisCache) SetONUDetail(ctx context.Context, oltID string, board, pon, onuID int, data []byte) error {
	key := fmt.Sprintf(KeyONUDetail, oltID, board, pon, onuID)
	return c.client.Set(ctx, key, data, TTLONUDetail).Err()
}

// SetONUDetailWithTTL stores ONU detail with a custom TTL
func (c *RedisCache) SetONUDetailWithTTL(ctx context.Context, oltID string, board, pon, onuID int, data []byte, ttl time.Duration) error {
	key := fmt.Sprintf(KeyONUDetail, oltID, board, pon, onuID)
	if ttl <= 0 {
		ttl = TTLONUDetail
	}
	return c.client.Set(ctx, key, data, ttl).Err()
}

// --- PON List ---

func (c *RedisCache) GetPONList(ctx context.Context, oltID string, board int) ([]byte, error) {
	key := fmt.Sprintf(KeyPONList, oltID, board)
	return c.client.Get(ctx, key).Bytes()
}

func (c *RedisCache) SetPONList(ctx context.Context, oltID string, board int, data []byte) error {
	key := fmt.Sprintf(KeyPONList, oltID, board)
	return c.client.Set(ctx, key, data, TTLPONList).Err()
}

func (c *RedisCache) SetPONListWithTTL(ctx context.Context, oltID string, board int, data []byte, ttl time.Duration) error {
	key := fmt.Sprintf(KeyPONList, oltID, board)
	if ttl <= 0 {
		ttl = TTLPONList
	}
	return c.client.Set(ctx, key, data, ttl).Err()
}

// --- Invalidation ---

// InvalidateOLT removes all cache entries for an OLT
func (c *RedisCache) InvalidateOLT(ctx context.Context, oltID string) error {
	pattern := fmt.Sprintf(KeyOLTPattern, oltID)
	return c.deleteByPattern(ctx, pattern)
}

// InvalidateONUList removes ONU list cache for a specific PON
func (c *RedisCache) InvalidateONUList(ctx context.Context, oltID string, board, pon int) error {
	key := fmt.Sprintf(KeyONUList, oltID, board, pon)
	return c.client.Del(ctx, key).Err()
}

// InvalidateONUDetail removes a single ONU detail cache entry
func (c *RedisCache) InvalidateONUDetail(ctx context.Context, oltID string, board, pon, onuID int) error {
	key := fmt.Sprintf(KeyONUDetail, oltID, board, pon, onuID)
	return c.client.Del(ctx, key).Err()
}

// AppendActivity stores an activity entry (JSON) in Redis list
func (c *RedisCache) AppendActivity(ctx context.Context, data []byte) error {
	pipe := c.client.TxPipeline()
	pipe.LPush(ctx, KeyActivityLog, data)
	pipe.LTrim(ctx, KeyActivityLog, 0, MaxActivityEntries-1)
	_, err := pipe.Exec(ctx)
	return err
}

// ListActivities returns a list of activity entries (JSON bytes)
func (c *RedisCache) ListActivities(ctx context.Context, limit int) ([][]byte, error) {
	if limit <= 0 || limit > MaxActivityEntries {
		limit = MaxActivityEntries
	}
	values, err := c.client.LRange(ctx, KeyActivityLog, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	result := make([][]byte, 0, len(values))
	for _, v := range values {
		result = append(result, []byte(v))
	}
	return result, nil
}

// deleteByPattern deletes all keys matching a pattern
func (c *RedisCache) deleteByPattern(ctx context.Context, pattern string) error {
	var cursor uint64
	for {
		var keys []string
		var err error
		keys, cursor, err = c.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}

		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}

		if cursor == 0 {
			break
		}
	}
	return nil
}

// --- Global Search Index ---

// GetGlobalIndex retrieves the full search index
func (c *RedisCache) GetGlobalIndex(ctx context.Context) ([]byte, error) {
	return c.client.Get(ctx, "search:index").Bytes()
}

// SetGlobalIndex stores the full search index
func (c *RedisCache) SetGlobalIndex(ctx context.Context, data []byte) error {
	return c.client.Set(ctx, "search:index", data, 0).Err() // No expiration, updated by sync
}

// --- Health Check ---

// GetHealth retrieves health status from cache
func (c *RedisCache) GetHealth(ctx context.Context, oltID string) ([]byte, error) {
	key := fmt.Sprintf(KeyOLTHealth, oltID)
	return c.client.Get(ctx, key).Bytes()
}

// SetHealth stores health status in cache
func (c *RedisCache) SetHealth(ctx context.Context, oltID string, data []byte) error {
	key := fmt.Sprintf(KeyOLTHealth, oltID)
	return c.client.Set(ctx, key, data, TTLHealth).Err()
}

// --- Generic helpers ---

// GetJSON retrieves and unmarshals JSON from cache
func (c *RedisCache) GetJSON(ctx context.Context, key string, dest interface{}) error {
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// SetJSON marshals and stores JSON in cache
func (c *RedisCache) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key, data, ttl).Err()
}

// Ping checks Redis connectivity
func (c *RedisCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// ---- PATCH(oltwtch-event): method untuk jalur event-driven ----

// PublishEvent publikasikan event ke pub/sub untuk dashboard realtime (SSE).
func (c *RedisCache) PublishEvent(ctx context.Context, data []byte) error {
	return c.client.Publish(ctx, KeyEventBus, data).Err()
}

// SubscribeEvents kembalikan channel pub/sub. Tutup via close().
func (c *RedisCache) SubscribeEvents(ctx context.Context) (<-chan []byte, func()) {
	sub := c.client.Subscribe(ctx, KeyEventBus)
	ch := make(chan []byte, 128)
	go func() {
		defer close(ch)
		for msg := range sub.Channel() {
			select {
			case ch <- []byte(msg.Payload):
			default:
			}
		}
	}()
	return ch, func() { sub.Close() }
}

// AppendEventLog simpan event ke log terbatas (LPush + LTrim).
func (c *RedisCache) AppendEventLog(ctx context.Context, data []byte) error {
	pipe := c.client.Pipeline()
	pipe.LPush(ctx, KeyEventLog, data)
	pipe.LTrim(ctx, KeyEventLog, 0, MaxEventEntries-1)
	_, err := pipe.Exec(ctx)
	return err
}

// RecentEvents ambil N event terakhir (terbaru dulu).
func (c *RedisCache) RecentEvents(ctx context.Context, limit int) ([][]byte, error) {
	if limit <= 0 || limit > MaxEventEntries {
		limit = 50
	}
	strs, err := c.client.LRange(ctx, KeyEventLog, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	out := make([][]byte, 0, len(strs))
	for _, s := range strs {
		out = append(out, []byte(s))
	}
	return out, nil
}

// PublishAlert publikasikan alert ke pub/sub untuk dashboard realtime.
func (c *RedisCache) PublishAlert(ctx context.Context, data []byte) error {
	return c.client.Publish(ctx, KeyAlertBus, data).Err()
}

// SubscribeAlerts kembalikan channel pub/sub alert. Tutup via close().
func (c *RedisCache) SubscribeAlerts(ctx context.Context) (<-chan []byte, func()) {
	sub := c.client.Subscribe(ctx, KeyAlertBus)
	ch := make(chan []byte, 128)
	go func() {
		defer close(ch)
		for msg := range sub.Channel() {
			select {
			case ch <- []byte(msg.Payload):
			default:
			}
		}
	}()
	return ch, func() { sub.Close() }
}

// AppendAlert simpan alert ke log terbatas (LPush + LTrim).
func (c *RedisCache) AppendAlert(ctx context.Context, data []byte) error {
	pipe := c.client.Pipeline()
	pipe.LPush(ctx, KeyAlertLog, data)
	pipe.LTrim(ctx, KeyAlertLog, 0, MaxAlertEntries-1)
	_, err := pipe.Exec(ctx)
	return err
}

// RecentAlerts ambil N alert terakhir (terbaru dulu).
func (c *RedisCache) RecentAlerts(ctx context.Context, limit int) ([][]byte, error) {
	if limit <= 0 || limit > MaxAlertEntries {
		limit = 50
	}
	strs, err := c.client.LRange(ctx, KeyAlertLog, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	out := make([][]byte, 0, len(strs))
	for _, s := range strs {
		out = append(out, []byte(s))
	}
	return out, nil
}

// PatchONUStatus update status 1 ONU di cache list PON (read-modify-write).
// Dipakai jalur event agar dashboard langsung segar tanpa walk SNMP.
// Bila cache kosong, kembalikan redis.Nil (poller yang akan mengisi).
func (c *RedisCache) PatchONUStatus(ctx context.Context, oltID string, board, pon, onuID int, status string, statusCode int) error {
	key := fmt.Sprintf(KeyONUList, oltID, board, pon)
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	var items []domain.ONUListItem
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	found := false
	for i := range items {
		if items[i].OnuID == onuID {
			items[i].Status = status
			items[i].StatusCode = statusCode
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("onu %d tidak ada di cache list", onuID)
	}
	ttl, err := c.client.TTL(ctx, key).Result()
	if err != nil || ttl <= 0 {
		ttl = TTLONUList
	}
	return c.SetONUListWithTTL(ctx, oltID, board, pon, mustJSON(items), ttl)
}

func mustJSON(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}
