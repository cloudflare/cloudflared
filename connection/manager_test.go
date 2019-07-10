package connection

import (
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/buildinfo"
	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
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

type mockStreamHandler struct {
}

func (msh *mockStreamHandler) ServeStream(*h2mux.MuxedStream) error {
	return nil
}

func mockEdgeManager() *EdgeManager {
	return NewEdgeManager(
		&mockStreamHandler{},
		configurable,
		[]byte{},
		nil,
		&mockEdgeServiceDiscoverer{},
		cloudflaredConfig,
		logrus.New(),
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
