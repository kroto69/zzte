package service

import (
	"fmt"
	"sync"
	"time"
)

// HotTracker mencatat PON port mana yang sedang aktif dibuka user.
//
// Ini jawaban atas "polling terus atau on-demand?": keduanya —
//   - background poller TETAP jalan (wajib untuk history/tren redaman),
//     tapi port "panas" (baru dibuka user) dipoll tiap siklus,
//     port dingin cukup sesekali;
//   - data real-time saat troubleshooting diambil via endpoint /live (on-demand).
type HotTracker struct {
	mu   sync.RWMutex
	last map[string]time.Time
}

func NewHotTracker() *HotTracker {
	return &HotTracker{last: make(map[string]time.Time)}
}

func hotKey(oltID string, board, pon int) string {
	return fmt.Sprintf("%s/%d/%d", oltID, board, pon)
}

// Touch dipanggil setiap user membuka data suatu PON port. Aman dipanggil
// dengan receiver nil (jadi no-op) agar tracker bersifat opsional.
func (h *HotTracker) Touch(oltID string, board, pon int) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.last[hotKey(oltID, board, pon)] = time.Now()
	h.mu.Unlock()
}

// IsHot true bila port disentuh dalam window terakhir.
// Tanpa tracker (nil): anggap semua panas = perilaku lama.
func (h *HotTracker) IsHot(oltID string, board, pon int, window time.Duration) bool {
	if h == nil {
		return true
	}
	h.mu.RLock()
	t, ok := h.last[hotKey(oltID, board, pon)]
	h.mu.RUnlock()
	return ok && time.Since(t) < window
}
