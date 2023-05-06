package handshake

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/quicvarint"
)

const sessionTicketRevision = 2

type sessionTicket struct {
	Parameters *wire.TransportParameters
	RTT        time.Duration // to be encoded in mus
}

func (t *sessionTicket) Marshal() []byte {
	b := make([]byte, 0, 256)
	b = quicvarint.Append(b, sessionTicketRevision)
	b = quicvarint.Append(b, uint64(t.RTT.Microseconds()))
	return t.Parameters.MarshalForSessionTicket(b)
}

func (t *sessionTicket) Unmarshal(b []byte) error {
	r := bytes.NewReader(b)
	rev, err := quicvarint.Read(r)
	if err != nil {
		return errors.New("failed to read session ticket revision")
	}
	if rev != sessionTicketRevision {
		return fmt.Errorf("unknown session ticket revision: %d", rev)
	}
	rtt, err := quicvarint.Read(r)
	if err != nil {
		return errors.New("failed to read RTT")
	}
	var tp wire.TransportParameters
	if err := tp.UnmarshalFromSessionTicket(r); err != nil {
		return fmt.Errorf("unmarshaling transport parameters from session ticket failed: %s", err.Error())
	}
	t.Parameters = &tp
	t.RTT = time.Duration(rtt) * time.Microsecond
	return nil
}
