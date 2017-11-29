package origin

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlattenServiceIPs(t *testing.T) {
	result := FlattenServiceIPs([][]*net.TCPAddr{
		[]*net.TCPAddr{
			&net.TCPAddr{Port: 1},
			&net.TCPAddr{Port: 2},
			&net.TCPAddr{Port: 3},
			&net.TCPAddr{Port: 4},
		},
		[]*net.TCPAddr{
			&net.TCPAddr{Port: 10},
			&net.TCPAddr{Port: 12},
			&net.TCPAddr{Port: 13},
		},
		[]*net.TCPAddr{
			&net.TCPAddr{Port: 21},
			&net.TCPAddr{Port: 22},
			&net.TCPAddr{Port: 23},
			&net.TCPAddr{Port: 24},
			&net.TCPAddr{Port: 25},
		},
	})
	assert.EqualValues(t, []*net.TCPAddr{
		&net.TCPAddr{Port: 1},
		&net.TCPAddr{Port: 10},
		&net.TCPAddr{Port: 21},
		&net.TCPAddr{Port: 2},
		&net.TCPAddr{Port: 12},
		&net.TCPAddr{Port: 22},
		&net.TCPAddr{Port: 3},
		&net.TCPAddr{Port: 13},
		&net.TCPAddr{Port: 23},
		&net.TCPAddr{Port: 4},
		&net.TCPAddr{Port: 24},
		&net.TCPAddr{Port: 25},
	}, result)
}
