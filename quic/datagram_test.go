package quic

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var (
	testSessionID = uuid.New()
)

func TestSuffixThenRemoveSessionID(t *testing.T) {
	msg := []byte(t.Name())
	msgWithID, err := SuffixSessionID(testSessionID, msg)
	require.NoError(t, err)
	require.Len(t, msgWithID, len(msg)+sessionIDLen)

	sessionID, msgWithoutID, err := ExtractSessionID(msgWithID)
	require.NoError(t, err)
	require.Equal(t, msg, msgWithoutID)
	require.Equal(t, testSessionID, sessionID)
}

func TestRemoveSessionIDError(t *testing.T) {
	// message is too short to contain session ID
	msg := []byte("test")
	_, _, err := ExtractSessionID(msg)
	require.Error(t, err)
}

func TestSuffixSessionIDError(t *testing.T) {
	msg := make([]byte, MaxDatagramFrameSize-sessionIDLen)
	_, err := SuffixSessionID(testSessionID, msg)
	require.NoError(t, err)

	msg = make([]byte, MaxDatagramFrameSize-sessionIDLen+1)
	_, err = SuffixSessionID(testSessionID, msg)
	require.Error(t, err)
}
