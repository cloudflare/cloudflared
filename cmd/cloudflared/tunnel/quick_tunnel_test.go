package tunnel

import (
	"errors"
	"flag"
	"fmt"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v2"
	"net/http"
	"net/http/httptest"
	"testing"
)

func makeContext(i int, serverUrl string) *cli.Context {
	flagSet := flag.NewFlagSet(fmt.Sprintf("test%d", i), flag.PanicOnError)
	flagSet.String("edge-ip-version", "", "")
	flagSet.String("protocol", "", "")
	flagSet.String("url", "", "")
	flagSet.String("quick-service", "", "")

	c := cli.NewContext(cli.NewApp(), flagSet, nil)
	_ = c.Set("edge-ip-version", "auto")
	_ = c.Set("protocol", "quic")
	_ = c.Set("url", "http://localhost:8080")
	_ = c.Set("quick-service", serverUrl)
	return c
}

// @noinspection SpellCheckingInspection
func TestQuickTunnel(t *testing.T) {
	var tests = []struct {
		name        string
		statusCode  int
		response    string
		wantErr     bool
		expectedErr error
	}{
		{
			name:       "200 OK response from server, valid response",
			statusCode: http.StatusOK,
			response:   `{"success":true,"result":{"id":"0347c3ea-504b-47bc-8e2c-339961e6ea3e","name":"qt-not-a-real-name","hostname":"not-a-real-hostname.trycloudflare.com","account_tag":"not-an-account-tag","secret":"notreallyasecret"},"errors":[]}`,
		},
		{
			name:        "200 OK response from server, bad tunnel ID",
			statusCode:  http.StatusOK,
			response:    `{"success":true,"result":{"id":"not-a-uuid","name":"qt-not-a-real-name","hostname":"not-a-real-hostname.trycloudflare.com","account_tag":"not-an-account-tag","secret":"notreallyasecret"},"errors":[]}`,
			wantErr:     true,
			expectedErr: errors.New("failed to parse quick Tunnel ID: invalid UUID length: 10"),
		},
		{
			name:        "200 OK response from server, bad JSON",
			statusCode:  http.StatusOK,
			response:    `This is not JSON!`,
			wantErr:     true,
			expectedErr: errors.New("failed to unmarshal quick Tunnel: invalid character 'T' looking for beginning of value"),
		},
		{
			name:        "429 Too Many Requests response from server",
			statusCode:  http.StatusTooManyRequests,
			response:    `error`,
			wantErr:     true,
			expectedErr: errors.New("rate limit exceeded; wait a while and try again"),
		},
		{
			name:        "400 Bad Request response from server",
			statusCode:  http.StatusBadRequest,
			response:    `error`,
			wantErr:     true,
			expectedErr: errors.New("HTTP error 400"),
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize tunnel subcommand
			bInfo := cliutil.GetBuildInfo("", "DEV")
			graceShutdownC := make(chan struct{})
			Init(bInfo, graceShutdownC)

			// Create a test HTTP server to act in place of the Cloudflare service
			serverReceivedRequest := false
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				// All requests should be to /tunnel
				assert.Equal(t, "/tunnel", req.URL.String())

				serverReceivedRequest = true

				rw.WriteHeader(tt.statusCode)
				_, _ = rw.Write([]byte(tt.response))
			}))
			defer server.Close()

			if !tt.wantErr {
				// Close the shutdown channel now so that the tunnel subcommand doesn't proceed to the quic negotiation
				close(graceShutdownC)
			}

			err := TunnelCommand(makeContext(i, server.URL))

			if tt.wantErr {
				assert.Equal(t, tt.expectedErr.Error(), err.Error())
			} else {
				assert.Nil(t, err)
			}
			assert.True(t, serverReceivedRequest)
		})
	}
}
