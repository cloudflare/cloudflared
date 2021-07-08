package quic

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectRequestData(t *testing.T) {
	var tests = []struct {
		name           string
		hostname       string
		connectionType ConnectionType
		metadata       []Metadata
	}{
		{
			name:           "Signature verified and request metadata is unmarshaled and read correctly",
			hostname:       "tunnel.com",
			connectionType: ConnectionTypeHTTP,
			metadata: []Metadata{
				Metadata{
					Key: "key",
					Val: "1234",
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := &bytes.Buffer{}
			err := WriteConnectRequestData(b, test.hostname, test.connectionType, test.metadata...)
			require.NoError(t, err)
			reqMeta, err := ReadConnectRequestData(b)
			require.NoError(t, err)

			assert.Equal(t, test.metadata, reqMeta.Metadata)
			assert.Equal(t, test.hostname, reqMeta.Dest)
			assert.Equal(t, test.connectionType, reqMeta.Type)
		})
	}
}

func TestConnectResponseMeta(t *testing.T) {
	var tests = []struct {
		name     string
		err      error
		metadata []Metadata
	}{
		{
			name: "Signature verified and response metadata is unmarshaled and read correctly",
			metadata: []Metadata{
				Metadata{
					Key: "key",
					Val: "1234",
				},
			},
		},
		{
			name: "If error is not empty, other fields should be blank",
			err:  errors.New("something happened"),
			metadata: []Metadata{
				Metadata{
					Key: "key",
					Val: "1234",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := &bytes.Buffer{}
			err := WriteConnectResponseData(b, test.err, test.metadata...)
			require.NoError(t, err)
			respMeta, err := ReadConnectResponseData(b)
			require.NoError(t, err)

			if respMeta.Error == "" {
				assert.Equal(t, test.metadata, respMeta.Metadata)
			} else {
				assert.Equal(t, 0, len(respMeta.Metadata))
			}
		})
	}
}
