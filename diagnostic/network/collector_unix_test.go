//go:build darwin || linux

package diagnostic_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	diagnostic "github.com/cloudflare/cloudflared/diagnostic/network"
)

func TestDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		text         string
		expectedHops []*diagnostic.Hop
	}{
		{
			"repeated hop index parse failure",
			`1  172.68.101.121 (172.68.101.121)  12.874 ms  15.517 ms  15.311 ms
2  172.68.101.121 (172.68.101.121)  12.874 ms  15.517 ms  15.311 ms
someletters * * *
4  172.68.101.121 (172.68.101.121)  12.874 ms  15.517 ms  15.311 ms `,
			[]*diagnostic.Hop{
				diagnostic.NewHop(
					uint8(1),
					"172.68.101.121 (172.68.101.121)",
					[]time.Duration{
						time.Duration(12874),
						time.Duration(15517),
						time.Duration(15311),
					},
				),
				diagnostic.NewHop(
					uint8(2),
					"172.68.101.121 (172.68.101.121)",
					[]time.Duration{
						time.Duration(12874),
						time.Duration(15517),
						time.Duration(15311),
					},
				),
				diagnostic.NewHop(
					uint8(4),
					"172.68.101.121 (172.68.101.121)",
					[]time.Duration{
						time.Duration(12874),
						time.Duration(15517),
						time.Duration(15311),
					},
				),
			},
		},
		{
			"hop index parse failure",
			`1  172.68.101.121 (172.68.101.121)  12.874 ms  15.517 ms  15.311 ms
2  172.68.101.121 (172.68.101.121)  12.874 ms  15.517 ms  15.311 ms
someletters 8.8.8.8 8.8.8.9 abc ms 0.456 ms 0.789 ms`,
			[]*diagnostic.Hop{
				diagnostic.NewHop(
					uint8(1),
					"172.68.101.121 (172.68.101.121)",
					[]time.Duration{
						time.Duration(12874),
						time.Duration(15517),
						time.Duration(15311),
					},
				),
				diagnostic.NewHop(
					uint8(2),
					"172.68.101.121 (172.68.101.121)",
					[]time.Duration{
						time.Duration(12874),
						time.Duration(15517),
						time.Duration(15311),
					},
				),
			},
		},
		{
			"missing rtt",
			`1  172.68.101.121 (172.68.101.121)  12.874 ms  15.517 ms  15.311 ms
2 * 8.8.8.8 8.8.8.9 0.456 ms 0.789 ms`,
			[]*diagnostic.Hop{
				diagnostic.NewHop(
					uint8(1),
					"172.68.101.121 (172.68.101.121)",
					[]time.Duration{
						time.Duration(12874),
						time.Duration(15517),
						time.Duration(15311),
					},
				),
				diagnostic.NewHop(
					uint8(2),
					"8.8.8.8 8.8.8.9",
					[]time.Duration{
						time.Duration(456),
						time.Duration(789),
					},
				),
			},
		},
		{
			"simple example ipv4",
			`1  172.68.101.121 (172.68.101.121)  12.874 ms  15.517 ms  15.311 ms
2  172.68.101.121 (172.68.101.121)  12.874 ms  15.517 ms  15.311 ms
3  * * *`,
			[]*diagnostic.Hop{
				diagnostic.NewHop(
					uint8(1),
					"172.68.101.121 (172.68.101.121)",
					[]time.Duration{
						time.Duration(12874),
						time.Duration(15517),
						time.Duration(15311),
					},
				),
				diagnostic.NewHop(
					uint8(2),
					"172.68.101.121 (172.68.101.121)",
					[]time.Duration{
						time.Duration(12874),
						time.Duration(15517),
						time.Duration(15311),
					},
				),
				diagnostic.NewTimeoutHop(uint8(3)),
			},
		},
		{
			"simple example ipv6",
			` 1  2400:cb00:107:1024::ac44:6550  12.780 ms  9.118 ms  10.046 ms
 2  2a09:bac1::  9.945 ms  10.033 ms  11.562 ms`,
			[]*diagnostic.Hop{
				diagnostic.NewHop(
					uint8(1),
					"2400:cb00:107:1024::ac44:6550",
					[]time.Duration{
						time.Duration(12780),
						time.Duration(9118),
						time.Duration(10046),
					},
				),
				diagnostic.NewHop(
					uint8(2),
					"2a09:bac1::",
					[]time.Duration{
						time.Duration(9945),
						time.Duration(10033),
						time.Duration(11562),
					},
				),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			hops, err := diagnostic.Decode(strings.NewReader(test.text), diagnostic.DecodeLine)
			require.NoError(t, err)
			assert.Equal(t, test.expectedHops, hops)
		})
	}
}
