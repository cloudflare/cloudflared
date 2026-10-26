package config

import (
	"crypto/sha256"
	"fmt"
	"io"
)

// Forwarder represents a client side listener to forward traffic to the edge
type Forwarder struct {
	URL           string `json:"url"`
	Listener      string `json:"listener"`
	TokenClientID string `json:"service_token_id" yaml:"serviceTokenID"`
	TokenSecret   string `json:"secret_token_id" yaml:"serviceTokenSecret"`
	Destination   string `json:"destination"`
	IsFedramp     bool   `json:"is_fedramp" yaml:"isFedramp"`
}

// Tunnel represents a tunnel that should be started
type Tunnel struct {
	URL          string `json:"url"`
	Origin       string `json:"origin"`
	ProtocolType string `json:"type"`
}

// Root is the base options to configure the service.
type Root struct {
	LogDirectory string      `json:"log_directory" yaml:"logDirectory,omitempty"`
	LogLevel     string      `json:"log_level" yaml:"logLevel,omitempty"`
	Forwarders   []Forwarder `json:"forwarders,omitempty" yaml:"forwarders,omitempty"`
	Tunnels      []Tunnel    `json:"tunnels,omitempty" yaml:"tunnels,omitempty"`
	// `resolver` key is reserved for a removed feature (proxy-dns) and should not be used.
}

// Hash returns the computed values to see if the forwarder values change
func (f *Forwarder) Hash() string {
	h := sha256.New()
	_, _ = io.WriteString(h, f.URL)
	_, _ = io.WriteString(h, f.Listener)
	_, _ = io.WriteString(h, f.TokenClientID)
	_, _ = io.WriteString(h, f.TokenSecret)
	_, _ = io.WriteString(h, f.Destination)
	return fmt.Sprintf("%x", h.Sum(nil))
}
