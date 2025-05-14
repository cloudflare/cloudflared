package client

import (
	"fmt"
	"net"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/features"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// Config captures the local client runtime configuration.
type Config struct {
	ConnectorID uuid.UUID
	Version     string
	Arch        string

	featureSelector features.FeatureSelector
}

func NewConfig(version string, arch string, featureSelector features.FeatureSelector) (*Config, error) {
	connectorID, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("unable to generate a connector UUID: %w", err)
	}
	return &Config{
		ConnectorID:     connectorID,
		Version:         version,
		Arch:            arch,
		featureSelector: featureSelector,
	}, nil
}

// ConnectionOptionsSnapshot is a snapshot of the current client information used to initialize a connection.
//
// The FeatureSnapshot is the features that are available for this connection. At the client level they may
// change, but they will not change within the scope of this struct.
type ConnectionOptionsSnapshot struct {
	client              pogs.ClientInfo
	originLocalIP       net.IP
	numPreviousAttempts uint8
	FeatureSnapshot     features.FeatureSnapshot
}

func (c *Config) ConnectionOptionsSnapshot(originIP net.IP, previousAttempts uint8) *ConnectionOptionsSnapshot {
	snapshot := c.featureSelector.Snapshot()
	return &ConnectionOptionsSnapshot{
		client: pogs.ClientInfo{
			ClientID: c.ConnectorID[:],
			Version:  c.Version,
			Arch:     c.Arch,
			Features: snapshot.FeaturesList,
		},
		originLocalIP:       originIP,
		numPreviousAttempts: previousAttempts,
		FeatureSnapshot:     snapshot,
	}
}

func (c ConnectionOptionsSnapshot) ConnectionOptions() *pogs.ConnectionOptions {
	return &pogs.ConnectionOptions{
		Client:              c.client,
		OriginLocalIP:       c.originLocalIP,
		ReplaceExisting:     false,
		CompressionQuality:  0,
		NumPreviousAttempts: c.numPreviousAttempts,
	}
}

func (c ConnectionOptionsSnapshot) LogFields(event *zerolog.Event) *zerolog.Event {
	return event.Strs("features", c.client.Features)
}
