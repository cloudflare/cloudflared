package client

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/features"
)

func TestGenerateConnectionOptions(t *testing.T) {
	version := "1234"
	arch := "linux_amd64"
	originIP := net.ParseIP("192.168.1.1")
	var previousAttempts uint8 = 4

	config, err := NewConfig(version, arch, &mockFeatureSelector{})
	require.NoError(t, err)
	require.Equal(t, version, config.Version)
	require.Equal(t, arch, config.Arch)

	// Validate ConnectionOptionsSnapshot fields
	connOptions := config.ConnectionOptionsSnapshot(originIP, previousAttempts)
	require.Equal(t, version, connOptions.client.Version)
	require.Equal(t, arch, connOptions.client.Arch)
	require.Equal(t, config.ConnectorID[:], connOptions.client.ClientID)

	// Vaidate snapshot feature fields against the connOptions generated
	snapshot := config.featureSelector.Snapshot()
	require.Equal(t, features.DatagramV3, snapshot.DatagramVersion)
	require.Equal(t, features.DatagramV3, connOptions.FeatureSnapshot.DatagramVersion)

	pogsConnOptions := connOptions.ConnectionOptions()
	require.Equal(t, connOptions.client, pogsConnOptions.Client)
	require.Equal(t, originIP, pogsConnOptions.OriginLocalIP)
	require.False(t, pogsConnOptions.ReplaceExisting)
	require.Equal(t, uint8(0), pogsConnOptions.CompressionQuality)
	require.Equal(t, previousAttempts, pogsConnOptions.NumPreviousAttempts)
}

type mockFeatureSelector struct{}

func (m *mockFeatureSelector) Snapshot() features.FeatureSnapshot {
	return features.FeatureSnapshot{
		PostQuantum:     features.PostQuantumPrefer,
		DatagramVersion: features.DatagramV3,
		FeaturesList:    []string{features.FeaturePostQuantum, features.FeatureDatagramV3_2},
	}
}
