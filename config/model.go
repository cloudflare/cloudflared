package config

import (
	"crypto/sha256"
	"fmt"
	"io"
	"strings"

	"github.com/cloudflare/cloudflared/tunneldns"
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

// DNSResolver represents a client side DNS resolver
type DNSResolver struct {
	Enabled                bool     `json:"enabled"`
	Address                string   `json:"address,omitempty"`
	Port                   uint16   `json:"port,omitempty"`
	Upstreams              []string `json:"upstreams,omitempty"`
	Bootstraps             []string `json:"bootstraps,omitempty"`
	MaxUpstreamConnections int      `json:"max_upstream_connections,omitempty"`
}

// Root is the base options to configure the service
type Root struct {
	LogDirectory string      `json:"log_directory" yaml:"logDirectory,omitempty"`
	LogLevel     string      `json:"log_level" yaml:"logLevel,omitempty"`
	Forwarders   []Forwarder `json:"forwarders,omitempty" yaml:"forwarders,omitempty"`
	Tunnels      []Tunnel    `json:"tunnels,omitempty" yaml:"tunnels,omitempty"`
	Resolver     DNSResolver `json:"resolver,omitempty" yaml:"resolver,omitempty"`
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

// Hash returns the computed values to see if the forwarder values change
func (r *DNSResolver) Hash() string {
	h := sha256.New()
	_, _ = io.WriteString(h, r.Address)
	_, _ = io.WriteString(h, strings.Join(r.Bootstraps, ","))
	_, _ = io.WriteString(h, strings.Join(r.Upstreams, ","))
	_, _ = io.WriteString(h, fmt.Sprintf("%d", r.Port))
	_, _ = io.WriteString(h, fmt.Sprintf("%d", r.MaxUpstreamConnections))
	_, _ = io.WriteString(h, fmt.Sprintf("%v", r.Enabled))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// EnabledOrDefault returns the enabled property
func (r *DNSResolver) EnabledOrDefault() bool {
	return r.Enabled
}

// AddressOrDefault returns the address or returns the default if empty
func (r *DNSResolver) AddressOrDefault() string {
	if r.Address != "" {
		return r.Address
	}
	return "localhost"
}

// PortOrDefault return the port or returns the default if 0
func (r *DNSResolver) PortOrDefault() uint16 {
	if r.Port > 0 {
		return r.Port
	}
	return 53
}

// UpstreamsOrDefault returns the upstreams or returns the default if empty
func (r *DNSResolver) UpstreamsOrDefault() []string {
	if len(r.Upstreams) > 0 {
		return r.Upstreams
	}
	return []string{"https://1.1.1.1/dns-query", "https://1.0.0.1/dns-query"}
}

// BootstrapsOrDefault returns the bootstraps or returns the default if empty
func (r *DNSResolver) BootstrapsOrDefault() []string {
	if len(r.Bootstraps) > 0 {
		return r.Bootstraps
	}
	return []string{"https://162.159.36.1/dns-query", "https://162.159.46.1/dns-query", "https://[2606:4700:4700::1111]/dns-query", "https://[2606:4700:4700::1001]/dns-query"}
}

// MaxUpstreamConnectionsOrDefault return the max upstream connections or returns the default if negative
func (r *DNSResolver) MaxUpstreamConnectionsOrDefault() int {
	if r.MaxUpstreamConnections >= 0 {
		return r.MaxUpstreamConnections
	}
	return tunneldns.MaxUpstreamConnsDefault
}
