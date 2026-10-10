package event

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rs/zerolog/log"

	"olt-monitor/internal/cache"
	"olt-monitor/internal/domain"
)

// Sink: subscriber bus yang menulis event ke Redis.
//
// Tiga hal dilakukan per event (semuanya cepat & non-blocking):
//  1. Patch status ONU di cache list PON (read-modify-write 1 item) —
//     dashboard yang baca cache langsung lihat status baru tanpa walk SNMP.
//  2. Publish ke Redis pub/sub "zzte:events" — untuk dashboard realtime (SSE).
//  3. Append ke event log terbatas (500) — untuk API /events/recent.
type Sink struct {
	cache *cache.RedisCache
}

func NewSink(c *cache.RedisCache) *Sink { return &Sink{cache: c} }

// Handle penuhi signature subscriber Bus.
func (s *Sink) Handle(ev Event) {
	if s.cache == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 1. patch status di cache list
	status, code := statusFor(ev)
	if err := s.cache.PatchONUStatus(ctx, ev.OLTID, ev.Board, ev.Pon, ev.OnuID, status, code); err != nil {
		log.Debug().Err(err).Msg("Patch status cache gagal (mungkin cache kosong)")
	}
	// invalidate detail ONU itu — biar baca berikutnya segar
	_ = s.cache.InvalidateONUDetail(ctx, ev.OLTID, ev.Board, ev.Pon, ev.OnuID)

	// 2. pub/sub realtime
	if data, err := json.Marshal(ev); err == nil {
		if err := s.cache.PublishEvent(ctx, data); err != nil {
			log.Warn().Err(err).Msg("Publish event gagal")
		}
		// 3. event log
		if err := s.cache.AppendEventLog(ctx, data); err != nil {
			log.Warn().Err(err).Msg("Append event log gagal")
		}
	}
}

// statusFor samakan kosakata dengan ParseONUStatus (SNMP).
func statusFor(ev Event) (string, int) {
	if ev.Kind == KindUp {
		return "Online", domain.ONUStatusOnline
	}
	switch ev.Cause {
	case CauseLOS:
		return "LOS", domain.ONUStatusLOS
	case CausePower:
		return "DyingGasp", domain.ONUStatusDyingGasp
	default:
		return "Offline", domain.ONUStatusOffline
	}
}
