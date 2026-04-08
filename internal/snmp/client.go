package snmp

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/rs/zerolog/log"

	"olt-monitor/internal/domain"
)

// Client wraps GoSNMP for thread-safe SNMP operations
type Client struct {
	config domain.SNMPConfig
	snmp   *gosnmp.GoSNMP
	mu     sync.Mutex
}

// NewClient creates a new SNMP client
func NewClient(config domain.SNMPConfig) (*Client, error) {
	// Set defaults
	if config.Port == 0 {
		config.Port = 161
	}
	if config.Timeout == 0 {
		config.Timeout = 5
	}
	if config.Retries == 0 {
		config.Retries = 2
	}
	if config.Community == "" {
		config.Community = "public"
	}

	client := &Client{
		config: config,
		snmp: &gosnmp.GoSNMP{
			Target:    config.Host,
			Port:      uint16(config.Port),
			Community: config.Community,
			Version:   gosnmp.Version2c,
			Timeout:   time.Duration(config.Timeout) * time.Second,
			Retries:   config.Retries,
		},
	}

	return client, nil
}

// Clone creates a new independent client with the same configuration
func (c *Client) Clone() (*Client, error) {
	return NewClient(c.config)
}

// Connect establishes connection to SNMP target
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snmp.Connect()
}

// Close closes the SNMP connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snmp.Conn.Close()
}

// Get retrieves a single OID value
func (c *Client) Get(oid string) (*gosnmp.SnmpPDU, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	result, err := c.snmp.Get([]string{oid})
	if err != nil {
		return nil, fmt.Errorf("SNMP GET failed for %s: %w", oid, err)
	}

	if len(result.Variables) == 0 {
		return nil, fmt.Errorf("no result for OID %s", oid)
	}

	return &result.Variables[0], nil
}

// GetMultiple retrieves multiple OID values
func (c *Client) GetMultiple(oids []string) ([]gosnmp.SnmpPDU, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	result, err := c.snmp.Get(oids)
	if err != nil {
		return nil, fmt.Errorf("SNMP GET failed: %w", err)
	}

	return result.Variables, nil
}

// Walk performs SNMP walk on a base OID
func (c *Client) Walk(baseOID string) ([]gosnmp.SnmpPDU, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var results []gosnmp.SnmpPDU
	err := c.snmp.Walk(baseOID, func(pdu gosnmp.SnmpPDU) error {
		results = append(results, pdu)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("SNMP WALK failed for %s: %w", baseOID, err)
	}

	return results, nil
}

// BulkWalk performs SNMP bulk walk on a base OID
func (c *Client) BulkWalk(baseOID string) ([]gosnmp.SnmpPDU, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var results []gosnmp.SnmpPDU
	err := c.snmp.BulkWalk(baseOID, func(pdu gosnmp.SnmpPDU) error {
		results = append(results, pdu)
		return nil
	})
	if err != nil {
		// Fallback to regular walk if bulk walk fails
		log.Warn().Err(err).Str("oid", baseOID).Msg("BulkWalk failed, falling back to Walk")
		return c.walkUnlocked(baseOID)
	}

	return results, nil
}

// walkUnlocked performs walk without acquiring lock (for internal use)
func (c *Client) walkUnlocked(baseOID string) ([]gosnmp.SnmpPDU, error) {
	var results []gosnmp.SnmpPDU
	err := c.snmp.Walk(baseOID, func(pdu gosnmp.SnmpPDU) error {
		results = append(results, pdu)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("SNMP WALK failed for %s: %w", baseOID, err)
	}
	return results, nil
}

// GetString retrieves a string value from OID
func (c *Client) GetString(oid string) (string, error) {
	pdu, err := c.Get(oid)
	if err != nil {
		return "", err
	}
	return pduToString(pdu), nil
}

// GetInt retrieves an integer value from OID
func (c *Client) GetInt(oid string) (int, error) {
	pdu, err := c.Get(oid)
	if err != nil {
		return 0, err
	}
	return pduToInt(pdu), nil
}

// PduToString converts PDU value to string
func PduToString(pdu *gosnmp.SnmpPDU) string {
	return pduToString(pdu)
}

// pduToString converts PDU value to string (internal)
func pduToString(pdu *gosnmp.SnmpPDU) string {
	// Handle nil or error types
	if pdu.Value == nil {
		return ""
	}

	switch pdu.Type {
	case gosnmp.NoSuchObject, gosnmp.NoSuchInstance, gosnmp.Null:
		return ""
	case gosnmp.OctetString:
		if b, ok := pdu.Value.([]byte); ok {
			return string(b)
		}
		return ""
	case gosnmp.Integer, gosnmp.Counter32, gosnmp.Counter64, gosnmp.Gauge32, gosnmp.TimeTicks:
		return fmt.Sprintf("%v", pdu.Value)
	default:
		if pdu.Value == nil {
			return ""
		}
		s := fmt.Sprintf("%v", pdu.Value)
		if s == "<nil>" {
			return ""
		}
		return s
	}
}

// pduToInt converts PDU value to int
func pduToInt(pdu *gosnmp.SnmpPDU) int {
	switch v := pdu.Value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint64:
		return int(v)
	case uint32:
		return int(v)
	default:
		return 0
	}
}

// ExtractONUIDFromOID extracts ONU ID from a full OID path
// OID format: {baseOID}.{onuId} or {baseOID}.{onuId}.1
func ExtractONUIDFromOID(fullOID string, baseOID string, ifIndex int) (int, error) {
	// Remove base OID prefix
	suffix := strings.TrimPrefix(fullOID, baseOID+".")
	if suffix == fullOID {
		return 0, fmt.Errorf("OID doesn't match base: %s", fullOID)
	}

	// Parse the remaining parts
	// For Name: suffix = "1" (just ONU ID)
	// For Power: suffix = "1.1" (ONU ID + channel)
	parts := strings.Split(suffix, ".")
	if len(parts) < 1 {
		return 0, fmt.Errorf("invalid OID format: %s", fullOID)
	}

	// ONU ID is the FIRST number after baseOID
	var onuID int
	fmt.Sscanf(parts[0], "%d", &onuID)

	return onuID, nil
}
