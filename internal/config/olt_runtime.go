package config

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"olt-monitor/internal/domain"
)

func (c *OLTConfig) ResolvedSNMPConfig(id string) domain.SNMPConfig {
	community := c.Community
	if envCommunity := readOLTEnv(id, "SNMP_COMMUNITY"); envCommunity != "" {
		community = envCommunity
	}

	return domain.SNMPConfig{
		Host:      c.Host,
		Port:      c.Port,
		Community: community,
		Timeout:   c.Timeout,
		Retries:   c.Retries,
	}
}

func (c *OLTConfig) ResolvedTelnetConfig(id string) domain.TelnetConfig {
	user := c.Telnet.User
	if envUser := readOLTEnv(id, "TELNET_USER"); envUser != "" {
		user = envUser
	}

	password := c.Telnet.Password
	if envPassword := readOLTEnv(id, "TELNET_PASSWORD"); envPassword != "" {
		password = envPassword
	}

	return domain.TelnetConfig{
		User:     user,
		Password: password,
		Port:     c.Telnet.Port,
	}
}

func readOLTEnv(id, suffix string) string {
	return strings.TrimSpace(os.Getenv(fmt.Sprintf("OLT_%s_%s", normalizeOLTEnvSegment(id), suffix)))
}

func normalizeOLTEnvSegment(id string) string {
	var builder strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(id)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}

	normalized := strings.Trim(builder.String(), "_")
	if normalized == "" {
		return "DEFAULT"
	}

	return normalized
}
