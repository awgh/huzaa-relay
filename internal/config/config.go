package config

import (
	"encoding/json"
	"os"
)

// TurnUser is one allowed bot credential (username + secret).
type TurnUser struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

// RelayConfig is the configuration for the relay bot (runs on IRC server).
type RelayConfig struct {
	TURNListen  string     `json:"turn_listen"`
	TURNSecret  string     `json:"turn_secret,omitempty"`
	TurnUsers   []TurnUser `json:"turn_users,omitempty"`
	DCCPortMin  int        `json:"dcc_port_min"`
	DCCPortMax  int        `json:"dcc_port_max"`
	RelayHost   string     `json:"relay_host"`
	TLSCertFile string     `json:"tls_cert_file"`
	TLSKeyFile  string     `json:"tls_key_file"`
	MaxSessions int        `json:"max_sessions,omitempty"`
}

// LoadRelayConfig loads a single relay config from a JSON file.
func LoadRelayConfig(path string) (*RelayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c RelayConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
