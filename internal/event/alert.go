package event

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"olt-monitor/internal/cache"
)

// Alert: satu notifikasi down yang sudah lolos anti-spam.
// Disimpan di Redis dan diekspos via GET /api/v1/alerts agar frontend
// (atau integrasi lain) bisa menampilkannya. Tanpa ketergantungan ke
// layanan pihak ketiga — backend murni.
type Alert struct {
	ID        string    `json:"id"`
	OLTID     string    `json:"oltId"`
	Board     int       `json:"board"`
	Pon       int       `json:"pon"`
	OnuID     int       `json:"onuId"` // 0 = mass-down satu PON port
	Kind      string    `json:"kind"`  // "down" | "mass-down"
	Cause     string    `json:"cause"`
	Count     int       `json:"count,omitempty"`
	Severity  string    `json:"severity"` // critical | warning | info
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

// AlertConfig konfigurasi mesin alert.
type AlertConfig struct {
	Cooldown      time.Duration // per ONU, default 60m
	AggWindow     time.Duration // default 90s
	MassThreshold int           // default 5
}

// VerifyFunc men-verify ulang apakah sebuah event down masih valid.
// Dipanggil di momen batch-flush, sebelum alert diterbitkan.
// Return true = ONU masih down (alert dilanjutkan), false = ONU sudah
// kembali online (alert dibuang — kemungkinan flapping syslog).
// Jalur event-driven tetap tidak membebani OLT: verifikasi ini hanya
// satu SNMP GET terarah per ONU yang akan di-alert, bukan polling.
// Kesalahan SNMP -> trust syslog (return true).
type VerifyFunc func(ev Event) bool

// Engine: subscriber bus yang mengubah event down menjadi Alert dengan
// anti-spam (prinsip: jangan banjiri teknisi):
//  1. Agregasi: down dikumpulkan AggWindow; satu PON port >= MassThreshold
//     dalam window -> SATU alert mass-down, bukan N alert.
//  2. Verifikasi ulang: di momen flush, setiap calon alert di-verify via
//     SNMP GET status ONU; yang sudah kembali online dibuang (anti false
//     alarm saat ONU flapping). Aktif bila SetVerifier dipasang.
//  3. Cooldown: 1 ONU max 1 alert down per Cooldown.
//  4. Severity: mass-down=critical, los/unknown=warning, power=info
//     (dying-gasp tetap tercatat, tapi tidak dianggap darurat).
type Engine struct {
	cfg   AlertConfig
	cache *cache.RedisCache
	in    chan Event

	verify VerifyFunc // nil = tanpa verifikasi ulang (perilaku lama)

	// onEmit dipanggil setiap alert diterbitkan; dipakai unit test
	// untuk menangkap alert tanpa Redis. Nil di produksi.
	onEmit func(Alert)

	mu       sync.Mutex
	buf      map[string][]Event // ponKey -> downs dalam window
	flushAt  time.Time
	lastSent map[string]time.Time

	done chan struct{}
	wg   sync.WaitGroup
}

// SetVerifier memasang fungsi verifikasi ulang yang dipanggil tepat
// sebelum alert diterbitkan. Boleh nil untuk menonaktifkan.
func (e *Engine) SetVerifier(v VerifyFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.verify = v
}

func NewEngine(cfg AlertConfig, c *cache.RedisCache) *Engine {
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 60 * time.Minute
	}
	if cfg.AggWindow <= 0 {
		cfg.AggWindow = 90 * time.Second
	}
	if cfg.MassThreshold <= 0 {
		cfg.MassThreshold = 5
	}
	return &Engine{
		cfg:      cfg,
		cache:    c,
		in:       make(chan Event, 512),
		buf:      make(map[string][]Event),
		lastSent: make(map[string]time.Time),
		done:     make(chan struct{}),
	}
}

// Handle penuhi signature subscriber Bus. Cepat: hanya enqueue.
func (e *Engine) Handle(ev Event) {
	if ev.Kind != KindDown {
		return
	}
	select {
	case e.in <- ev:
	default:
		log.Warn().Msg("Alert engine penuh, event dibuang")
	}
}

func (e *Engine) Start() {
	e.wg.Add(1)
	go e.loop()
}

func (e *Engine) Stop() {
	close(e.done)
	e.wg.Wait()
}

func (e *Engine) loop() {
	defer e.wg.Done()
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-e.done:
			e.flushAll()
			return
		case ev := <-e.in:
			e.add(ev)
		case <-tick.C:
			e.flushExpired()
		}
	}
}

func (e *Engine) add(ev Event) {
	key := ev.ponKey()
	var flushNow bool
	e.mu.Lock()
	e.buf[key] = append(e.buf[key], ev)
	if e.flushAt.IsZero() {
		e.flushAt = time.Now().Add(e.cfg.AggWindow)
	}
	flushNow = len(e.buf[key]) >= e.cfg.MassThreshold
	e.mu.Unlock()
	if flushNow {
		e.flushKey(key)
	}
}

func (e *Engine) flushExpired() {
	e.mu.Lock()
	if e.flushAt.IsZero() || time.Now().Before(e.flushAt) {
		e.mu.Unlock()
		return
	}
	keys := make([]string, 0, len(e.buf))
	for key := range e.buf {
		keys = append(keys, key)
	}
	e.mu.Unlock()
	for _, key := range keys {
		e.flushKey(key)
	}
	e.mu.Lock()
	e.flushAt = time.Time{}
	e.mu.Unlock()
}

func (e *Engine) flushAll() {
	e.mu.Lock()
	keys := make([]string, 0, len(e.buf))
	for key := range e.buf {
		keys = append(keys, key)
	}
	e.mu.Unlock()
	for _, key := range keys {
		e.flushKey(key)
	}
}

// flushKey menarik buffer satu PON port, memverifikasi ulang status ONU
// yang masih down, lalu menerbitkan alert. Verifikasi dilakukan di luar
// lock (ada I/O jaringan) agar intake event tidak terhambat.
func (e *Engine) flushKey(key string) {
	e.mu.Lock()
	evs := e.buf[key]
	delete(e.buf, key)
	e.mu.Unlock()

	if len(evs) == 0 {
		return
	}
	evs = e.reverify(evs)
	if len(evs) == 0 {
		log.Debug().Str("pon", key).Msg("Alert dibuang: semua ONU sudah kembali online")
		return
	}
	if len(evs) >= e.cfg.MassThreshold {
		e0 := evs[0]
		e.emit(Alert{
			OLTID:    e0.OLTID,
			Board:    e0.Board,
			Pon:      e0.Pon,
			Kind:     "mass-down",
			Cause:    CauseMass,
			Count:    len(evs),
			Severity: "critical",
			Message: "MASS DOWN: " + itoa(len(evs)) + " ONU down di " +
				e0.OLTID + " PON " + itoa(e0.Board) + "/" + itoa(e0.Pon) +
				". Kemungkinan feeder/splitter/OLT — cek infrastruktur dulu.",
		})
		e.mu.Lock()
		now := time.Now()
		for _, ev := range evs {
			e.lastSent[ev.dedupKey()] = now
		}
		e.mu.Unlock()
		return
	}
	e.emitSingles(evs)
}

// reverify membuang event yang ONU-nya sudah kembali online di momen
// flush. Tanpa verifier (nil), semua event dianggap masih valid.
func (e *Engine) reverify(evs []Event) []Event {
	if e == nil || len(evs) == 0 {
		return evs
	}
	verify := e.verify
	if verify == nil {
		return evs
	}
	kept := evs[:0]
	for _, ev := range evs {
		if verify(ev) {
			kept = append(kept, ev)
			continue
		}
		log.Debug().
			Str("olt", ev.OLTID).
			Int("board", ev.Board).Int("pon", ev.Pon).Int("onu", ev.OnuID).
			Msg("Alert dibuang: ONU kembali online sebelum flush")
	}
	return kept
}

// emitSingles menerbitkan alert per-ONU dengan cooldown.
func (e *Engine) emitSingles(evs []Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	for _, ev := range evs {
		dk := ev.dedupKey()
		if last, ok := e.lastSent[dk]; ok && now.Sub(last) < e.cfg.Cooldown {
			continue // cooldown
		}
		e.lastSent[dk] = now
		severity := "warning"
		causeMsg := "penyebab belum diketahui"
		switch ev.Cause {
		case CausePower:
			severity = "info"
			causeMsg = "kemungkinan listrik/adaptor di sisi pelanggan"
		case CauseLOS:
			causeMsg = "sinyal optik hilang — cek fiber/konektor"
		}
		e.emit(Alert{
			OLTID:    ev.OLTID,
			Board:    ev.Board,
			Pon:      ev.Pon,
			OnuID:    ev.OnuID,
			Kind:     "down",
			Cause:    ev.Cause,
			Severity: severity,
			Message: "DOWN: " + ev.OLTID + " " + itoa(ev.Board) + "/" +
				itoa(ev.Pon) + ":" + itoa(ev.OnuID) + " — " + causeMsg,
		})
	}
}

func (e *Engine) emit(a Alert) {
	a.ID = newID()
	a.CreatedAt = time.Now()
	if e.onEmit != nil {
		e.onEmit(a)
	}
	if e.cache == nil {
		return
	}
	data, err := json.Marshal(a)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := e.cache.AppendAlert(ctx, data); err != nil {
		log.Warn().Err(err).Msg("Append alert gagal")
	}
	if err := e.cache.PublishAlert(ctx, data); err != nil {
		log.Warn().Err(err).Msg("Publish alert gagal")
	}
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
