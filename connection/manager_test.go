package connection

import (
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/buildinfo"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/streamhandler"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

var (
	configurable = &EdgeManagerConfigurable{
		[]h2mux.TunnelHostname{
			"http.example.com",
			"ws.example.com",
			"hello.example.com",
		},
		&pogs.EdgeConnectionConfig{
			NumHAConnections:    1,
			HeartbeatInterval:   1 * time.Second,
			Timeout:             5 * time.Second,
			MaxFailedHeartbeats: 3,
			UserCredentialPath:  "/etc/cloudflared/cert.pem",
		},
	}
	cloudflaredConfig = &CloudflaredConfig{
		CloudflaredID: uuid.New(),
		Tags: []pogs.Tag{
			{Name: "pool", Value: "east-6"},
		},
		BuildInfo: &buildinfo.BuildInfo{
			GoOS:               "linux",
			GoVersion:          "1.12",
			GoArch:             "amd64",
			CloudflaredVersion: "2019.6.0",
		},
	}
)

func mockEdgeManager() *EdgeManager {
	newConfigChan := make(chan<- *pogs.ClientConfig)
	useConfigResultChan := make(<-chan *pogs.UseConfigurationResult)
	logger := logrus.New()
	edge := edgediscovery.MockEdge(logger, []*net.TCPAddr{})
	return NewEdgeManager(
		streamhandler.NewStreamHandler(newConfigChan, useConfigResultChan, logger),
		configurable,
		[]byte{},
		nil,
		edge,
		cloudflaredConfig,
		logger,
	)
}

func TestUpdateConfigurable(t *testing.T) {
	m := mockEdgeManager()
	newConfigurable := &EdgeManagerConfigurable{
		[]h2mux.TunnelHostname{
			"second.example.com",
		},
		&pogs.EdgeConnectionConfig{
			NumHAConnections: 2,
		},
	}
	m.UpdateConfigurable(newConfigurable)

	assert.Equal(t, newConfigurable, m.state.getConfigurable())
}
