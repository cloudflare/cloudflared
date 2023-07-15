//go:build !windows
// +build !windows

package proxy

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/config"
)

func TestUnixSocketOrigin(t *testing.T) {
	file, err := os.CreateTemp("", "unix.sock")
	require.NoError(t, err)
	os.Remove(file.Name()) // remove the file since binding the socket expects to create it

	l, err := net.Listen("unix", file.Name())
	require.NoError(t, err)
	defer l.Close()
	defer os.Remove(file.Name())

	api := &httptest.Server{
		Listener: l,
		Config:   &http.Server{Handler: mockAPI{}},
	}
	api.Start()
	defer api.Close()

	unvalidatedIngress := []config.UnvalidatedIngressRule{
		{
			Hostname: "unix.example.com",
			Service:  "unix:" + file.Name(),
		},
		{
			Hostname: "*",
			Service:  "http_status:404",
		},
	}

	tests := []MultipleIngressTest{
		{
			url:            "http://unix.example.com",
			expectedStatus: http.StatusCreated,
			expectedBody:   []byte("Created"),
		},
	}

	runIngressTestScenarios(t, unvalidatedIngress, tests)
}
