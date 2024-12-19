package connection

import "net"

// Event is something that happened to a connection, e.g. disconnection or registration.
type Event struct {
	Index       uint8
	EventType   Status
	Location    string
	Protocol    Protocol
	URL         string
	EdgeAddress net.IP
}

// Status is the status of a connection.
type Status int

const (
	// Disconnected means the connection to the edge was broken.
	Disconnected Status = iota
	// Connected means the connection to the edge was successfully established.
	Connected
	// Reconnecting means the connection to the edge is being re-established.
	Reconnecting
	// SetURL means this connection's tunnel was given a URL by the edge. Used for quick tunnels.
	SetURL
	// RegisteringTunnel means the non-named tunnel is registering its connection.
	RegisteringTunnel
	// We're unregistering tunnel from the edge in preparation for a disconnect
	Unregistering
)
