package diagnostic_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/diagnostic"
	"github.com/cloudflare/cloudflared/tunnelstate"
)

type SystemCollectorMock struct{}

const (
	systemInformationKey = "sikey"
	rawInformationKey    = "rikey"
	errorKey             = "errkey"
)

func newTrackerFromConns(t *testing.T, connections []tunnelstate.IndexedConnectionInfo) *tunnelstate.ConnTracker {
	t.Helper()

	log := zerolog.Nop()
	tracker := tunnelstate.NewConnTracker(&log)

	for _, conn := range connections {
		tracker.OnTunnelEvent(connection.Event{
			Index:       conn.Index,
			EventType:   connection.Connected,
			Protocol:    conn.Protocol,
			EdgeAddress: conn.EdgeAddress,
		})
	}

	return tracker
}

func setCtxValuesForSystemCollector(
	systemInfo *diagnostic.SystemInformation,
	rawInfo string,
	err error,
) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, systemInformationKey, systemInfo)
	ctx = context.WithValue(ctx, rawInformationKey, rawInfo)
	ctx = context.WithValue(ctx, errorKey, err)

	return ctx
}

func (*SystemCollectorMock) Collect(ctx context.Context) (*diagnostic.SystemInformation, string, error) {
	si, _ := ctx.Value(systemInformationKey).(*diagnostic.SystemInformation)
	ri, _ := ctx.Value(rawInformationKey).(string)
	err, _ := ctx.Value(errorKey).(error)
	return si, ri, err
}

func TestSystemHandler(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	tests := []struct {
		name       string
		systemInfo *diagnostic.SystemInformation
		rawInfo    string
		err        error
		statusCode int
	}{
		{
			name: "happy path",
			systemInfo: diagnostic.NewSystemInformation(
				0, 0, 0, 0,
				"string", "string", "string", "string",
				"string", "string", nil,
			),
			rawInfo:    "",
			err:        nil,
			statusCode: http.StatusOK,
		},
		{
			name: "on error and raw info", systemInfo: nil,
			rawInfo: "raw info", err: errors.New("an error"), statusCode: http.StatusOK,
		},
		{
			name: "on error and no raw info", systemInfo: nil,
			rawInfo: "", err: errors.New("an error"), statusCode: http.StatusInternalServerError,
		},
		{
			name: "malformed response", systemInfo: nil, rawInfo: "", err: nil, statusCode: http.StatusInternalServerError,
		},
	}

	for _, tCase := range tests {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			handler := diagnostic.NewDiagnosticHandler(&log, 0, &SystemCollectorMock{}, uuid.New(), uuid.New(), nil, map[string]string{}, nil)
			recorder := httptest.NewRecorder()
			ctx := setCtxValuesForSystemCollector(tCase.systemInfo, tCase.rawInfo, tCase.err)
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "/diag/syste,", nil)
			require.NoError(t, err)
			handler.SystemHandler(recorder, request)

			assert.Equal(t, tCase.statusCode, recorder.Code)
			if tCase.statusCode == http.StatusOK && tCase.systemInfo != nil {
				var response diagnostic.SystemInformation

				decoder := json.NewDecoder(recorder.Body)
				err = decoder.Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, tCase.systemInfo, &response)
			} else if tCase.statusCode == http.StatusOK && tCase.rawInfo != "" {
				rawBytes, err := io.ReadAll(recorder.Body)
				require.NoError(t, err)
				assert.Equal(t, tCase.rawInfo, string(rawBytes))
			}
		})
	}
}

func TestTunnelStateHandler(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	tests := []struct {
		name        string
		tunnelID    uuid.UUID
		clientID    uuid.UUID
		connections []tunnelstate.IndexedConnectionInfo
		icmpSources []string
	}{
		{
			name:     "case1",
			tunnelID: uuid.New(),
			clientID: uuid.New(),
		},
		{
			name:        "case2",
			tunnelID:    uuid.New(),
			clientID:    uuid.New(),
			icmpSources: []string{"172.17.0.3", "::1"},
			connections: []tunnelstate.IndexedConnectionInfo{{
				ConnectionInfo: tunnelstate.ConnectionInfo{
					IsConnected: true,
					Protocol:    connection.QUIC,
					EdgeAddress: net.IPv4(100, 100, 100, 100),
				},
				Index: 0,
			}},
		},
	}

	for _, tCase := range tests {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			tracker := newTrackerFromConns(t, tCase.connections)
			handler := diagnostic.NewDiagnosticHandler(
				&log,
				0,
				nil,
				tCase.tunnelID,
				tCase.clientID,
				tracker,
				map[string]string{},
				tCase.icmpSources,
			)
			recorder := httptest.NewRecorder()
			handler.TunnelStateHandler(recorder, nil)
			decoder := json.NewDecoder(recorder.Body)

			var response diagnostic.TunnelState
			err := decoder.Decode(&response)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, tCase.tunnelID, response.TunnelID)
			assert.Equal(t, tCase.clientID, response.ConnectorID)
			assert.Equal(t, tCase.connections, response.Connections)
			assert.Equal(t, tCase.icmpSources, response.ICMPSources)
		})
	}
}

func TestConfigurationHandler(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()

	tests := []struct {
		name     string
		flags    map[string]string
		expected map[string]string
	}{
		{
			name:  "empty cli",
			flags: make(map[string]string),
			expected: map[string]string{
				"uid": "0",
			},
		},
		{
			name: "cli with flags",
			flags: map[string]string{
				"b":   "a",
				"c":   "a",
				"d":   "a",
				"uid": "0",
			},
			expected: map[string]string{
				"b":   "a",
				"c":   "a",
				"d":   "a",
				"uid": "0",
			},
		},
	}

	for _, tCase := range tests {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()

			var response map[string]string

			handler := diagnostic.NewDiagnosticHandler(&log, 0, nil, uuid.New(), uuid.New(), nil, tCase.flags, nil)
			recorder := httptest.NewRecorder()
			handler.ConfigurationHandler(recorder, nil)
			decoder := json.NewDecoder(recorder.Body)
			err := decoder.Decode(&response)
			require.NoError(t, err)
			_, ok := response["uid"]
			assert.True(t, ok)
			delete(tCase.expected, "uid")
			delete(response, "uid")
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, tCase.expected, response)
		})
	}
}
