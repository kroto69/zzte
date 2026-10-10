package event

import (
	"sync"

	"olt-monitor/internal/service"
)

// Mapper: IP pengirim syslog -> OLTID. Wajib untuk multi-OLT.
//
// Urutan resolve:
//  1. Config eksplisit: event.syslog.sources = { "192.168.1.100": "olt-jkt" }
//  2. Fallback otomatis: cocokkan dengan host SNMP OLT yang terdaftar
//     (umumnya syslog dikirim dari IP yang sama dengan IP SNMP OLT).
type Mapper struct {
	mu      sync.RWMutex
	sources map[string]string // ip -> oltID (config)

	manager *service.OLTManager // untuk fallback host SNMP
}

// NewMapper buat mapper. manager boleh nil (fallback nonaktif).
func NewMapper(manager *service.OLTManager) *Mapper {
	return &Mapper{
		sources: make(map[string]string),
		manager: manager,
	}
}

// SetSources isi/overwrite mapping dari config.
func (m *Mapper) SetSources(src map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ip, id := range src {
		m.sources[ip] = id
	}
}

// Resolve IP -> OLTID. ok=false bila tidak dikenal.
func (m *Mapper) Resolve(ip string) (oltID string, ok bool) {
	if ip == "" {
		return "", false
	}
	m.mu.RLock()
	id, ok := m.sources[ip]
	m.mu.RUnlock()
	if ok {
		return id, true
	}
	// Fallback: samakan dengan host SNMP OLT terdaftar.
	if m.manager != nil {
		for _, olt := range m.manager.ListOLTs() {
			if olt.SNMP.Host == ip {
				return olt.ID, true
			}
		}
	}
	return "", false
}
