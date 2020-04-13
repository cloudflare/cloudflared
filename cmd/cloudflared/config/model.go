package config

import (
	"crypto/md5"
	"fmt"
	"io"
)

// Forwarder represents a client side listener to forward traffic to the edge
type Forwarder struct {
	URL      string `json:"url"`
	Listener string `json:"listener"`
}

// Tunnel represents a tunnel that should be started
type Tunnel struct {
	URL          string `json:"url"`
	Origin       string `json:"origin"`
	ProtocolType string `json:"type"`
}

// Root is the base options to configure the service
type Root struct {
	OrgKey          string      `json:"org_key"`
	ConfigType      string      `json:"type"`
	CheckinInterval int         `json:"checkin_interval"`
	Forwarders      []Forwarder `json:"forwarders,omitempty"`
	Tunnels         []Tunnel    `json:"tunnels,omitempty"`
}

// Hash returns the computed values to see if the forwarder values change
func (f *Forwarder) Hash() string {
	h := md5.New()
	io.WriteString(h, f.URL)
	io.WriteString(h, f.Listener)
	return fmt.Sprintf("%x", h.Sum(nil))
}
