package service

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/ziutek/telnet"

	"olt-monitor/internal/domain"
)

// TelnetService handles Telnet interactions with OLTs.
// Satu instance dipakai bersama (dibuat sekali di main) supaya semaphore
// per-OLT mengikat semua pemakai Telnet (detail, reboot, provisioning).
type TelnetService struct {
	mu    sync.Mutex
	slots map[string]chan struct{} // semaphore per host OLT
}

const (
	telnetDialTimeout    = 3 * time.Second
	telnetLoginTimeout   = 5 * time.Second
	telnetCommandTimeout = 15 * time.Second
	telnetBatchTimeout   = 30 * time.Second

	// maxTelnetSessionsPerOLT membatasi session Telnet bersamaan per OLT
	// (host). OLT ZTE umumnya hanya menyediakan ~5 VTY; 2 session untuk
	// zzte menyisakan ruang bagi CLI juragan/teknisi.
	maxTelnetSessionsPerOLT = 2
	// telnetSlotAcquireTimeout: berapa lama menunggu giliran session
	// Telnet sebelum menyerah (dan pemanggil degrade gracefully).
	telnetSlotAcquireTimeout = 3 * time.Second
)

// NewTelnetService creates a new TelnetService
func NewTelnetService() *TelnetService {
	return &TelnetService{
		slots: make(map[string]chan struct{}),
	}
}

// acquireTelnetSlot mengambil satu slot session Telnet untuk host OLT.
// Mengembalikan fungsi release + true bila berhasil; false bila semua slot
// penuh selama telnetSlotAcquireTimeout (pemanggil harus degrade gracefully,
// bukan menunggu tanpa batas).
func (s *TelnetService) acquireTelnetSlot(host string) (func(), bool) {
	s.mu.Lock()
	if s.slots == nil {
		s.slots = make(map[string]chan struct{})
	}
	ch, ok := s.slots[host]
	if !ok {
		ch = make(chan struct{}, maxTelnetSessionsPerOLT)
		s.slots[host] = ch
	}
	s.mu.Unlock()

	select {
	case ch <- struct{}{}:
		return func() { <-ch }, true
	case <-time.After(telnetSlotAcquireTimeout):
		return nil, false
	}
}

// ONUPowerAttenuation adalah hasil `show pon power attenuation` untuk satu ONU.
// Semua field pointer: nil berarti "no signal" / "N/A" (ONU tidak ada sinyal
// optik), bukan 0 dBm.
type ONUPowerAttenuation struct {
	OnuRx   *float64 // down Rx: daya yang diterima ONU dari OLT (dBm)
	OnuTx   *float64 // up Tx: daya pancar laser modem (dBm)
	OltRx   *float64 // up Rx: daya yang diterima OLT dari modem (dBm) — "TX Power" juragan
	OltTx   *float64 // down Tx: daya pancar OLT (dBm)
	AttUp   *float64 // atenuasi upstream (dB)
	AttDown *float64 // atenuasi downstream (dB)
}

var (
	// `Rx :-28.358(dbm)` / `Tx:2.468(dbm)` / `Tx:N/A` / `Rx :no signal`
	powerTokenRe = regexp.MustCompile(`(?i)\b(rx|tx)\s*:?\s*([^\s]+)`)
	// `30.826(dB)` — butuh `)` tepat setelah dB supaya `(dbm)` tidak ikut cocok.
	attenuationRe = regexp.MustCompile(`(?i)(-?\d+(?:\.\d+)?)\s*\(db\)`)
)

// ParsePowerAttenuation mem-parse output `show pon power attenuation gpon-onu_...`.
// Contoh yang dikenali (dari OLT live):
//
//	up      Rx :-28.358(dbm)      Tx:2.468(dbm)        30.826(dB)
//	down    Tx :6.033(dbm)        Rx:-23.468(dbm)      29.501(dB)
//
// Varian tanpa sinyal (`Rx :no signal`, `Tx:N/A`) menghasilkan nil.
func ParsePowerAttenuation(raw string) ONUPowerAttenuation {
	var att ONUPowerAttenuation
	for _, line := range strings.Split(raw, "\n") {
		low := strings.ToLower(strings.TrimSpace(line))
		var isUp, isDown bool
		if strings.HasPrefix(low, "up") {
			isUp = true
		} else if strings.HasPrefix(low, "down") {
			isDown = true
		} else {
			continue
		}

		var rx, tx *float64
		for _, m := range powerTokenRe.FindAllStringSubmatch(low, -1) {
			v, err := parsePowerToken(m[2])
			if err != nil {
				continue // "no signal" / "N/A" -> nil
			}
			if m[1] == "rx" {
				rx = v
			} else {
				tx = v
			}
		}

		var attenuation *float64
		if m := attenuationRe.FindStringSubmatch(low); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				attenuation = &v
			}
		}

		if isUp {
			att.OltRx, att.OnuTx, att.AttUp = rx, tx, attenuation
		} else if isDown {
			att.OltTx, att.OnuRx, att.AttDown = tx, rx, attenuation
		}
	}
	return att
}

// parsePowerToken mengubah token seperti "-28.358(dbm)" / "2.468(dbm)"
// menjadi *float64. Token "no" / "n/a" (dari "no signal" / "N/A")
// mengembalikan error supaya jadi nil.
func parsePowerToken(tok string) (*float64, error) {
	tok = strings.TrimSuffix(tok, "(dbm)")
	v, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// FetchONUPowerAttenuation menjalankan `show pon power attenuation`
// untuk satu ONU via Telnet dan mem-parse hasilnya.
// Dipakai HANYA dari GetONUDetail (saat user membuka detail ONU) —
// bukan dari list/poller — supaya beban Telnet tetap kecil.
// Gagal (slot penuh, OLT tidak menjawab) mengembalikan error; pemanggil
// harus degrade gracefully (detail tetap tampil dengan data SNMP,
// TX Power 0.0).
func (s *TelnetService) FetchONUPowerAttenuation(config domain.OLTConfig, board, pon, onuID int) (*ONUPowerAttenuation, error) {
	host := config.SNMP.Host
	release, ok := s.acquireTelnetSlot(host)
	if !ok {
		return nil, fmt.Errorf("slot Telnet penuh untuk OLT %s (maks %d session bersamaan)", host, maxTelnetSessionsPerOLT)
	}
	defer release()

	sess, err := s.Connect(config)
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	cmd := fmt.Sprintf("show pon power attenuation gpon-onu_1/%d/%d:%d", board, pon, onuID)
	raw, err := sess.SendCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("gagal menjalankan %q: %w", cmd, err)
	}

	att := ParsePowerAttenuation(raw)
	return &att, nil
}

// TelnetSession wraps a telnet connection
type TelnetSession struct {
	conn *telnet.Conn
}

// Connect establishes a Telnet connection
func (s *TelnetService) Connect(config domain.OLTConfig) (*TelnetSession, error) {
	addr := fmt.Sprintf("%s:%d", config.SNMP.Host, config.Telnet.Port)
	if config.Telnet.Port == 0 {
		addr = fmt.Sprintf("%s:23", config.SNMP.Host)
	}

	// Connect with timeout for faster failure on unreachable hosts
	netConn, err := net.DialTimeout("tcp", addr, telnetDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to dial telnet: %w", err)
	}
	t, err := telnet.NewConn(netConn)
	if err != nil {
		netConn.Close()
		return nil, fmt.Errorf("failed to create telnet connection: %w", err)
	}

	// Set generic deadline for login
	t.SetDeadline(time.Now().Add(telnetLoginTimeout))

	session := &TelnetSession{conn: t}

	// Login
	if err := session.login(config.Telnet.User, config.Telnet.Password); err != nil {
		t.Close()
		return nil, err
	}

	return session, nil
}

// Close closes the session
func (s *TelnetSession) Close() error {
	return s.conn.Close()
}

// SendCommand sends a command and returns the output
func (s *TelnetSession) SendCommand(cmd string) (string, error) {
	s.conn.SetDeadline(time.Now().Add(telnetCommandTimeout))

	if _, err := s.conn.Write([]byte(cmd + "\n")); err != nil {
		return "", err
	}

	// Read until prompt or reasonable amount of data
	// Simple approach: read until '#', '>', or timeout
	// In complex scenarios, we might need smart prompt detection
	data, err := s.conn.ReadUntil("#", ">", "Confirm")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SendCommands sends multiple commands in a pipeline (Paste mode)
// It writes all commands first, then reads the expected number of prompts.
// This significantly reduces RTT latency compared to one-by-one execution.
func (s *TelnetSession) SendCommands(cmds []string) (string, error) {
	s.conn.SetDeadline(time.Now().Add(telnetBatchTimeout))

	var bigCmd strings.Builder
	for _, cmd := range cmds {
		bigCmd.WriteString(cmd + "\n")
	}

	// Write all at once
	if _, err := s.conn.Write([]byte(bigCmd.String())); err != nil {
		return "", err
	}

	var fullOutput strings.Builder
	// Read expected number of prompts (one per command)
	for i := 0; i < len(cmds); i++ {
		data, err := s.conn.ReadUntil("#", ">", "Confirm")
		if err != nil {
			// If we timeout/error but got some data, return what we got
			return fullOutput.String(), fmt.Errorf("read error after command %d/%d: %w", i+1, len(cmds), err)
		}
		fullOutput.Write(data)
	}

	return fullOutput.String(), nil
}

// Internal login helper
func (s *TelnetSession) login(user, pass string) error {
	// 1. Wait for Username prompt
	_, err := s.conn.ReadUntil("Username:", "Login:", "login:")
	if err != nil {
		return fmt.Errorf("failed waiting for username prompt: %w", err)
	}
	if _, err := s.conn.Write([]byte(user + "\n")); err != nil {
		return err
	}

	// 2. Wait for Password prompt
	_, err = s.conn.ReadUntil("Password:", "password:")
	if err != nil {
		return fmt.Errorf("failed waiting for password prompt: %w", err)
	}
	if _, err := s.conn.Write([]byte(pass + "\n")); err != nil {
		return err
	}

	// 3. Wait for Shell prompt
	_, err = s.conn.ReadUntil(">", "#")
	if err != nil {
		return fmt.Errorf("login failed or prompt not found: %w", err)
	}
	return nil
}

// RebootONU reboots a specific ONU via Telnet
func (s *TelnetService) RebootONU(config domain.OLTConfig, board, pon, onuID int) error {
	sess, err := s.Connect(config)
	if err != nil {
		return err
	}
	defer sess.Close()

	// 1. Enter Config Mode
	if _, err := sess.SendCommand("config t"); err != nil {
		return err
	}

	// 2. Select Interface
	ifaceCmd := fmt.Sprintf("pon-onu-mng gpon-onu_%d/%d/%d:%d", 1, board, pon, onuID)
	if _, err := sess.SendCommand(ifaceCmd); err != nil {
		return err
	}

	// 3. Send Reboot Command
	log.Info().Msg("Sending 'reboot' command...")
	// Send manually to handle confirmation logic inside session if needed,
	// or just use SendCommand which expects prompt

	// Issue: reboot might ask for confirmation "Confirm to reboot? (y/n)[n]:"
	// SendCommand reads until # or >, so if it asks confirmation, it might timeout or catch it if "Confirm" is added to logic
	// Let's rely on specific logic here like before, or relax SendCommand

	sess.conn.Write([]byte("reboot\n"))

	// Handle confirmation
	data, err := sess.conn.ReadUntil("confirms", "Confirm", "y/n", "#", ">")
	if err == nil {
		out := string(data)
		if len(out) > 0 && (strings.Contains(out, "Confirm") || strings.Contains(out, "y/n")) {
			sess.conn.Write([]byte("y\n"))
		}
	}

	// Cleanup
	sess.conn.Write([]byte("exit\n"))
	sess.conn.Write([]byte("exit\n"))

	return nil
}

// FetchVLANProfiles retrieves VLAN profile mapping via Telnet.
// Key: CVLAN as string, Value: profile name.
func (s *TelnetService) FetchVLANProfiles(config domain.OLTConfig) (map[string]string, error) {
	sess, err := s.Connect(config)
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	output, err := sess.SendCommand("show gpon onu profile vlan")
	if err != nil {
		return nil, err
	}

	return parseVLANProfileOutput(output), nil
}

func parseVLANProfileOutput(raw string) map[string]string {
	profiles := make(map[string]string)
	currentName := ""

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Profile name:") {
			currentName = strings.TrimSpace(strings.TrimPrefix(line, "Profile name:"))
			continue
		}
		if strings.HasPrefix(line, "CVLAN:") {
			vlan := strings.TrimSpace(strings.TrimPrefix(line, "CVLAN:"))
			if vlan != "" && currentName != "" {
				profiles[vlan] = currentName
			}
		}
	}

	return profiles
}
