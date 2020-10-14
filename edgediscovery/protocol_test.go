package edgediscovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTTP2Percentage(t *testing.T) {
	_, err := HTTP2Percentage()
	assert.NoError(t, err)
}

func TestParseHTTP2Precentage(t *testing.T) {
	tests := []struct {
		record     string
		percentage int32
		wantErr    bool
	}{
		{
			record:     "http2=-1",
			percentage: -1,
			wantErr:    false,
		},
		{
			record:     "http2=0",
			percentage: 0,
			wantErr:    false,
		},
		{
			record:     "http2=50",
			percentage: 50,
			wantErr:    false,
		},
		{
			record:     "http2=100",
			percentage: 100,
			wantErr:    false,
		},
		{
			record:     "http2=1000",
			percentage: 1000,
			wantErr:    false,
		},
		{
			record:  "http2=10.5",
			wantErr: true,
		},
		{
			record:  "http2=10 h2mux=90",
			wantErr: true,
		},
		{
			record:  "http2=ten",
			wantErr: true,
		},

		{
			record:  "h2mux=100",
			wantErr: true,
		},
		{
			record:  "http2",
			wantErr: true,
		},
		{
			record:  "http2=",
			wantErr: true,
		},
	}

	for _, test := range tests {
		p, err := parseHTTP2Precentage(test.record)
		if test.wantErr {
			assert.Error(t, err)
		} else {
			assert.Equal(t, test.percentage, p)
		}
	}
}
