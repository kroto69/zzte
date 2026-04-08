package service

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/ziutek/telnet"

	"olt-monitor/internal/domain"
)

// TelnetService handles Telnet interactions with OLTs
type TelnetService struct{}

const (
	telnetDialTimeout    = 3 * time.Second
	telnetLoginTimeout   = 5 * time.Second
	telnetCommandTimeout = 15 * time.Second
	telnetBatchTimeout   = 30 * time.Second
)

// NewTelnetService creates a new TelnetService
func NewTelnetService() *TelnetService {
	return &TelnetService{}
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
