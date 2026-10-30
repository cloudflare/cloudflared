package proxy

import (
	"bytes"
	"strings"
	"testing"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestLogRequestErrorBenignRemoteCancelUsesDebug(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf).Level(zerolog.DebugLevel)

	logRequestError(&logger, &quic.StreamError{StreamID: 41, ErrorCode: 0, Remote: true})
	out := buf.String()
	require.Contains(t, out, `"level":"debug"`)
	require.Contains(t, out, "request stream canceled by remote with NO_ERROR")
	require.NotContains(t, out, `"level":"error"`)
}

func TestLogRequestErrorRealErrorUsesError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf).Level(zerolog.DebugLevel)

	logRequestError(&logger, &quic.StreamError{StreamID: 41, ErrorCode: 1, Remote: true})
	out := buf.String()
	require.True(t, strings.Contains(out, `"level":"error"`), out)
}
