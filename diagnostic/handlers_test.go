package diagnostic_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/diagnostic"
)

type SystemCollectorMock struct{}

const (
	systemInformationKey = "sikey"
	rawInformationKey    = "rikey"
	errorKey             = "errkey"
)

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
			handler := diagnostic.NewDiagnosticHandler(&log, 0, &SystemCollectorMock{})
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
