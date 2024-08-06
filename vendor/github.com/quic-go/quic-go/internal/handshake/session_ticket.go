package handshake

import (
	"errors"
	"fmt"
	"time"

	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/quicvarint"
)

const sessionTicketRevision = 4

type sessionTicket struct {
	Parameters *wire.TransportParameters
	RTT        time.Duration // to be encoded in mus
}

func (t *sessionTicket) Marshal() []byte {
	b := make([]byte, 0, 256)
	b = quicvarint.Append(b, sessionTicketRevision)
	b = quicvarint.Append(b, uint64(t.RTT.Microseconds()))
	if t.Parameters == nil {
		return b
	}
	return t.Parameters.MarshalForSessionTicket(b)
}

func (t *sessionTicket) Unmarshal(b []byte, using0RTT bool) error {
	rev, l, err := quicvarint.Parse(b)
	if err != nil {
		return errors.New("failed to read session ticket revision")
	}
	b = b[l:]
	if rev != sessionTicketRevision {
		return fmt.Errorf("unknown session ticket revision: %d", rev)
	}
	rtt, l, err := quicvarint.Parse(b)
	if err != nil {
		return errors.New("failed to read RTT")
	}
	b = b[l:]
	if using0RTT {
		var tp wire.TransportParameters
		if err := tp.UnmarshalFromSessionTicket(b); err != nil {
			return fmt.Errorf("unmarshaling transport parameters from session ticket failed: %s", err.Error())
		}
		t.Parameters = &tp
	} else if len(b) > 0 {
		return fmt.Errorf("the session ticket has more bytes than expected")
	}
	t.RTT = time.Duration(rtt) * time.Microsecond
	return nil
}
