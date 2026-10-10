package event

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// dedupWindow: event sama (ONU+jenis) dalam window ini dianggap duplikat.
// Menangkal: trap ganda dari OLT, duplikat syslog+trap, dan tabrakan dengan poller.
const dedupWindow = 60 * time.Second

// Bus: fan-out event ke subscriber dengan dedup.
//
// Desain: SATU goroutine worker membaca channel buffer. Subscriber dipanggil
// sekuensial — jadi subscriber WAJIB cepat & non-blocking (tulis Redis OK;
// kerja berat TIDAK OK di sini — alert engine punya worker+antrean sendiri).
// Bila buffer penuh, event baru dibuang (lebih baik kehilangan 1 event
// daripada OOM; poller tetap jadi jaring pengaman via reconcile).
type Bus struct {
	in   chan Event
	subs []func(Event)

	mu   sync.Mutex
	seen map[string]time.Time

	done chan struct{}
	wg   sync.WaitGroup
}

// NewBus buat bus dengan buffer sebesar size.
func NewBus(size int) *Bus {
	if size <= 0 {
		size = 1024
	}
	return &Bus{
		in:   make(chan Event, size),
		seen: make(map[string]time.Time),
		done: make(chan struct{}),
	}
}

// Subscribe daftarkan penerima event. Panggil sebelum Start.
func (b *Bus) Subscribe(fn func(Event)) {
	b.subs = append(b.subs, fn)
}

// Publish kirim event. Return false bila buffer penuh (event dibuang).
func (b *Bus) Publish(ev Event) bool {
	select {
	case b.in <- ev:
		return true
	default:
		log.Warn().Str("olt", ev.OLTID).Msg("Event bus penuh, event dibuang")
		return false
	}
}

// Start jalankan worker. Stop dengan Close.
func (b *Bus) Start() {
	b.wg.Add(1)
	go b.loop()
}

// Close hentikan worker.
func (b *Bus) Close() {
	close(b.done)
	b.wg.Wait()
}

func (b *Bus) loop() {
	defer b.wg.Done()
	tidy := time.NewTicker(5 * time.Minute)
	defer tidy.Stop()
	for {
		select {
		case <-b.done:
			return
		case ev := <-b.in:
			if b.dup(ev) {
				continue
			}
			for _, fn := range b.subs {
				fn(ev)
			}
		case <-tidy.C:
			b.gc()
		}
	}
}

func (b *Bus) dup(ev Event) bool {
	key := ev.dedupKey()
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	if t, ok := b.seen[key]; ok && now.Sub(t) < dedupWindow {
		return true
	}
	b.seen[key] = now
	return false
}

func (b *Bus) gc() {
	cutoff := time.Now().Add(-2 * dedupWindow)
	b.mu.Lock()
	for k, t := range b.seen {
		if t.Before(cutoff) {
			delete(b.seen, k)
		}
	}
	b.mu.Unlock()
}
