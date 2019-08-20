package clickhouse

import (
	"context"
	"database/sql/driver"

	"github.com/kshvakov/clickhouse/lib/protocol"
)

func (ch *clickhouse) Ping(ctx context.Context) error {
	return ch.ping(ctx)
}

func (ch *clickhouse) ping(ctx context.Context) error {
	if ch.conn.closed {
		return driver.ErrBadConn
	}
	ch.logf("-> ping")
	finish := ch.watchCancel(ctx)
	defer finish()
	if err := ch.encoder.Uvarint(protocol.ClientPing); err != nil {
		return err
	}
	if err := ch.encoder.Flush(); err != nil {
		return err
	}
	return ch.process()
}
