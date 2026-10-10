package event

import (
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// Server menerima syslog via UDP dan meneruskannya ke Bus sebagai Event.
//
// Catatan beban OLT: server ini PASIF. OLT yang mengirim saat ada kejadian;
// tidak ada satu pun paket yang dikirim ke OLT dari jalur ini. Satu-satunya
// "beban" di OLT adalah 1 baris config: arahkan syslog ke IP:port ini.
type Server struct {
	conn   *net.UDPConn
	bus    *Bus
	mapper *Mapper
	port   int

	dropped atomic.Uint64 // datagram dibuang karena antrian penuh
	unknown atomic.Uint64 // baris tak dikenal (untuk kalibrasi)
}

// NewServer buat server syslog. bus dan mapper wajib non-nil.
func NewServer(bus *Bus, mapper *Mapper, port int) *Server {
	return &Server{bus: bus, mapper: mapper, port: port}
}

// Serve blokir sampai ctx selesai. Panggil sebagai goroutine.
func (s *Server) Serve(stop <-chan struct{}) error {
	addr, err := net.ResolveUDPAddr("udp", "0.0.0.0:"+itoa(s.port))
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	s.conn = conn
	defer conn.Close()

	// Buffer socket besar agar burst trap (mass-down) tidak terbuang di kernel.
	_ = conn.SetReadBuffer(4 << 20)

	log.Info().Int("port", s.port).Msg("Syslog server dengar (event-driven, nol beban polling)")

	buf := make([]byte, 65535)
	for {
		select {
		case <-stop:
			return nil
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			log.Warn().Err(err).Msg("Syslog read error")
			continue
		}
		s.handle(buf[:n], src)
	}
}

func (s *Server) handle(data []byte, src *net.UDPAddr) {
	line := strings.TrimSpace(string(data))
	if line == "" {
		return
	}
	// Syslog bisa multi-baris; proses per baris.
	for _, l := range strings.Split(line, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if len(l) > 500 {
			l = l[:500]
		}
		p, ok := ParseLine(l)
		if !ok {
			s.unknown.Add(1)
			log.Debug().Str("line", l).Msg("Syslog tak dikenal (kalibrasi pola?)")
			continue
		}
		srcIP := ""
		if src != nil {
			srcIP = src.IP.String()
		}
		oltID, ok := s.mapper.Resolve(srcIP)
		if !ok {
			log.Warn().Str("src", srcIP).Str("line", l).
				Msg("Syslog dari IP tak dikenal — daftarkan di event.syslog.sources atau sebagai OLT")
			continue
		}
		ev := Event{
			OLTID:      oltID,
			Board:      p.Board,
			Pon:        p.Pon,
			OnuID:      p.OnuID,
			Kind:       p.Kind,
			Cause:      p.Cause,
			Raw:        l,
			SourceIP:   srcIP,
			ReceivedAt: time.Now(),
		}
		if !s.bus.Publish(ev) {
			s.dropped.Add(1)
		}
	}
}

// Stats untuk endpoint monitoring.
func (s *Server) Stats() (dropped, unknown uint64) {
	return s.dropped.Load(), s.unknown.Load()
}
