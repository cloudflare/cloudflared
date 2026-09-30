package connection

import (
	"errors"

	"github.com/quic-go/quic-go"
)

// IsBenignRemoteStreamCancel reports whether err (possibly wrapped) is a remote
// QUIC stream cancellation with error code 0 (NO_ERROR).
//
// Per RFC 9113 §7, HTTP/2 RST_STREAM with NO_ERROR is an intentional,
// non-erroneous stream close. Cloudflare's edge surfaces that to cloudflared as
// a quic.StreamError with Remote=true and ErrorCode=0 (for example when a
// browser tab closes a long-lived SSE or WebSocket). Logging those at ERR
// creates false positives in monitoring; treat them as debug instead.
func IsBenignRemoteStreamCancel(err error) bool {
	var se *quic.StreamError
	if !errors.As(err, &se) {
		return false
	}
	return se.Remote && se.ErrorCode == 0
}
