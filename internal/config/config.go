package config

import (
	"fmt"
	"strings"

	"olt-monitor/internal/domain"

	"github.com/spf13/viper"
)

// Config holds all application configuration
type Config struct {
	Server ServerConfig           `mapstructure:"server"`
	Redis  RedisConfig            `mapstructure:"redis"`
	Search SearchConfig           `mapstructure:"search"`
	OLTs   map[string]OLTConfig   `mapstructure:"olts"`
	Users  map[string]UserAccount `mapstructure:"users"`
	v      *viper.Viper
}

// ServerConfig holds application settings
type ServerConfig struct {
	Port      int    `mapstructure:"port"`
	Host      string `mapstructure:"host"`
	LogLevel  string `mapstructure:"log_level"`
	JWTSecret string `mapstructure:"jwt_secret"`
}

// RedisConfig holds Redis settings
type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// SearchConfig holds Search Indexer settings
type SearchConfig struct {
	Enabled  bool `mapstructure:"enabled"`
	Interval int  `mapstructure:"interval"`
}

// OLTConfig holds OLT connection settings
type OLTConfig struct {
	Name         string            `mapstructure:"name"`
	Host         string            `mapstructure:"host"`
	Port         int               `mapstructure:"port"`
	Community    string            `mapstructure:"community"`
	Timeout      int               `mapstructure:"timeout"`
	Retries      int               `mapstructure:"retries"`
	Telnet       TelnetConfig      `mapstructure:"telnet"`
	VlanProfiles map[string]string `mapstructure:"vlan_profiles"`
}

// TelnetConfig holds Telnet credentials for config
type TelnetConfig struct {
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Port     int    `mapstructure:"port"`
}

// UserAccount holds user credentials and role
type UserAccount struct {
	Password string `mapstructure:"password"`
	Role     string `mapstructure:"role"`
}

// Load loads configuration from file
func Load(path string) (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("server.port", 8081)
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.log_level", "info")
	v.SetDefault("redis.host", "localhost")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.db", 0)
	v.SetDefault("search.enabled", true)
	v.SetDefault("search.interval", 10)

	// Config file
	v.SetConfigName("olt_config")
	v.SetConfigType("yaml")
	v.AddConfigPath(path)
	v.AddConfigPath(".")
	v.AddConfigPath("config")

	// Env vars
	v.SetEnvPrefix("OLT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if users := parseUsers(v); len(users) > 0 {
		cfg.Users = users
	}

	// Set defaults for OLTs
	for id, olt := range cfg.OLTs {
		if olt.Port == 0 {
			olt.Port = 161
		}
		if olt.Community == "" {
			olt.Community = "public"
		}
		if olt.Timeout == 0 {
			olt.Timeout = 5
		}
		if olt.Retries == 0 {
			olt.Retries = 2
		}
		if olt.Telnet.Port == 0 {
			olt.Telnet.Port = 23
		}

		cfg.OLTs[id] = olt
	}

	if len(cfg.Users) == 0 {
		defaultPassword, err := HashPassword("admin")
		if err != nil {
			return nil, fmt.Errorf("hash default admin password: %w", err)
		}
		cfg.Users = map[string]UserAccount{
			"admin": {Password: defaultPassword, Role: "superadmin"},
		}
	}

	if err := NormalizeUsers(cfg.Users); err != nil {
		return nil, fmt.Errorf("normalize users: %w", err)
	}

	cfg.v = v

	return &cfg, nil
}

// Save persists the current configuration to file
func (c *Config) Save() error {
	c.v.Set("olts", c.OLTs)
	c.v.Set("users", c.Users)

	if err := c.v.WriteConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok || strings.Contains(err.Error(), "Not Found") {
			return c.v.WriteConfigAs("config/olt_config.yaml")
		}
		return err
	}
	return nil
}

// ToOLTInstance converts OLTConfig to domain.OLTInstance
func (c *OLTConfig) ToOLTInstance(id string) domain.OLTInstance {
	snmp := c.ResolvedSNMPConfig(id)
	telnet := c.ResolvedTelnetConfig(id)

	return domain.OLTInstance{
		ID:   id,
		Name: c.Name,
		Config: domain.OLTConfig{
			ID:           id,
			Name:         c.Name,
			SNMP:         snmp,
			Telnet:       telnet,
			VlanProfiles: c.VlanProfiles,
		},
		SNMP:   snmp,
		Telnet: telnet,
	}
}

// GetRedisAddr returns Redis address string
func (c *RedisConfig) GetRedisAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func parseUsers(v *viper.Viper) map[string]UserAccount {
	raw := v.Get("users")
	if raw == nil {
		return nil
	}

	users := make(map[string]UserAccount)
	switch u := raw.(type) {
	case map[string]interface{}:
		for username, val := range u {
			role := "superadmin"
			switch vv := val.(type) {
			case string:
				users[username] = UserAccount{Password: vv, Role: role}
			case map[string]interface{}:
				pass, _ := vv["password"].(string)
				role, _ = vv["role"].(string)
				if role == "" {
					role = "superadmin"
				}
				if pass != "" {
					users[username] = UserAccount{Password: pass, Role: strings.ToLower(role)}
				}
			}
		}
	case map[string]string:
		for username, pass := range u {
			users[username] = UserAccount{Password: pass, Role: "superadmin"}
		}
	}

	return users
}
