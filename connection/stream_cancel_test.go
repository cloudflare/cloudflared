package connection

import (
	"fmt"
	"testing"

	"github.com/pkg/errors"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

func TestIsBenignRemoteStreamCancel(t *testing.T) {
	t.Parallel()

	benign := &quic.StreamError{StreamID: 41, ErrorCode: 0, Remote: true}
	require.True(t, IsBenignRemoteStreamCancel(benign))
	require.True(t, IsBenignRemoteStreamCancel(fmt.Errorf("proxy: %w", benign)))
	require.True(t, IsBenignRemoteStreamCancel(errors.Wrap(benign, "proxyHTTPRequest")))

	require.False(t, IsBenignRemoteStreamCancel(nil))
	require.False(t, IsBenignRemoteStreamCancel(fmt.Errorf("other")))
	require.False(t, IsBenignRemoteStreamCancel(&quic.StreamError{StreamID: 41, ErrorCode: 0, Remote: false}))
	require.False(t, IsBenignRemoteStreamCancel(&quic.StreamError{StreamID: 41, ErrorCode: 1, Remote: true}))
	require.False(t, IsBenignRemoteStreamCancel(&quic.StreamError{StreamID: 41, ErrorCode: 0x100, Remote: true}))
}
