package domain

import (
	"time"
)

// SNMPConfig holds SNMP connection parameters for an OLT
type SNMPConfig struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Community string `json:"community"`
	Timeout   int    `json:"timeout,omitempty"` // seconds, default 5
	Retries   int    `json:"retries,omitempty"` // default 2
}

// TelnetConfig holds Telnet credentials
type TelnetConfig struct {
	User     string `json:"user" mapstructure:"user"`
	Password string `json:"password" mapstructure:"password"`
	Port     int    `json:"port" mapstructure:"port"` // Default 23
}

// OLTSystemInfo contains system metrics
type OLTSystemInfo struct {
	CPUUsage    float64 `json:"cpuUsage"`
	MemoryUsage float64 `json:"memoryUsage"`
	Uptime      string  `json:"uptime"`
	SysName     string  `json:"sysName"`
	SysDescr    string  `json:"sysDescr"`
}

// PONInfo contains PON port details
type PONInfo struct {
	Board       int    `json:"board"`
	Pon         int    `json:"pon"`
	Description string `json:"description"`
}

// OLTConfig represents OLT configuration
type OLTConfig struct {
	ID           string            `mapstructure:"id" json:"id"`
	Name         string            `mapstructure:"name" json:"name"`
	SNMP         SNMPConfig        `mapstructure:"snmp" json:"snmp"`
	Telnet       TelnetConfig      `mapstructure:"telnet" json:"telnet"`
	VlanProfiles map[string]string `mapstructure:"vlan_profiles" json:"vlanProfiles,omitempty"`
	PollInterval int              `mapstructure:"poll_interval" json:"pollInterval,omitempty"`
	IsOnline     bool              `json:"isOnline"`
	LastCheck    time.Time         `json:"lastCheck"`
}

// OLTInstance represents a connected OLT
type OLTInstance struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Config    OLTConfig      `json:"config"`
	SNMP      SNMPConfig     `json:"snmp"`
	Telnet    TelnetConfig   `json:"telnet"`
	System    *OLTSystemInfo `json:"system,omitempty"`
	IsOnline  bool           `json:"isOnline"`
	LastCheck time.Time      `json:"lastCheck"`
}

// OLTCreateRequest is the request body for creating a new OLT
type OLTCreateRequest struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	SNMP   SNMPConfig   `json:"snmp"`
	Telnet TelnetConfig `json:"telnet"`
}

// OLTTestRequest is the request body for testing OLT connection
type OLTTestRequest struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Community string `json:"community"`
}

// OLTTestResponse is the response for test connection
type OLTTestResponse struct {
	FirmwareVersion string `json:"firmwareVersion"`
	FullVersion     string `json:"fullVersion"`
}
