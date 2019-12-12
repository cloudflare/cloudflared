package column

import (
	"net"

	"github.com/kshvakov/clickhouse/lib/binary"
)

type IPv6 struct {
	base
}

func (*IPv6) Read(decoder *binary.Decoder) (interface{}, error) {
	v, err := decoder.Fixed(16)
	if err != nil {
		return nil, err
	}
	return net.IP(v), nil
}

func (ip *IPv6) Write(encoder *binary.Encoder, v interface{}) error {
	netIP, ok := v.(net.IP)
	if !ok {
		return &ErrUnexpectedType{
			T:      v,
			Column: ip,
		}
	}
	if _, err := encoder.Write([]byte(netIP.To16())); err != nil {
		return err
	}
	return nil
}
