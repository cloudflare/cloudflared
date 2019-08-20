package clickhouse

import (
	"bufio"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"sync"
	"time"

	"github.com/kshvakov/clickhouse/lib/binary"
	"github.com/kshvakov/clickhouse/lib/column"
	"github.com/kshvakov/clickhouse/lib/data"
	"github.com/kshvakov/clickhouse/lib/protocol"
	"github.com/kshvakov/clickhouse/lib/types"
)

type (
	Date     = types.Date
	DateTime = types.DateTime
	UUID     = types.UUID
)

var (
	ErrInsertInNotBatchMode = errors.New("insert statement supported only in the batch mode (use begin/commit)")
	ErrLimitDataRequestInTx = errors.New("data request has already been prepared in transaction")
)

var (
	splitInsertRe = regexp.MustCompile(`(?i)\sVALUES\s*\(`)
)

type logger func(format string, v ...interface{})

type clickhouse struct {
	sync.Mutex
	data.ServerInfo
	data.ClientInfo
	logf          logger
	conn          *connect
	block         *data.Block
	buffer        *bufio.Writer
	decoder       *binary.Decoder
	encoder       *binary.Encoder
	compress      bool
	blockSize     int
	inTransaction bool
}

func (ch *clickhouse) Prepare(query string) (driver.Stmt, error) {
	return ch.prepareContext(context.Background(), query)
}

func (ch *clickhouse) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return ch.prepareContext(ctx, query)
}

func (ch *clickhouse) prepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	ch.logf("[prepare] %s", query)
	switch {
	case ch.conn.closed:
		return nil, driver.ErrBadConn
	case ch.block != nil:
		return nil, ErrLimitDataRequestInTx
	case isInsert(query):
		if !ch.inTransaction {
			return nil, ErrInsertInNotBatchMode
		}
		return ch.insert(query)
	}
	return &stmt{
		ch:       ch,
		query:    query,
		numInput: numInput(query),
	}, nil
}

func (ch *clickhouse) insert(query string) (_ driver.Stmt, err error) {
	if err := ch.sendQuery(splitInsertRe.Split(query, -1)[0] + " VALUES "); err != nil {
		return nil, err
	}
	if ch.block, err = ch.readMeta(); err != nil {
		return nil, err
	}
	return &stmt{
		ch:       ch,
		isInsert: true,
	}, nil
}

func (ch *clickhouse) Begin() (driver.Tx, error) {
	return ch.beginTx(context.Background(), txOptions{})
}

func (ch *clickhouse) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return ch.beginTx(ctx, txOptions{
		Isolation: int(opts.Isolation),
		ReadOnly:  opts.ReadOnly,
	})
}

type txOptions struct {
	Isolation int
	ReadOnly  bool
}

func (ch *clickhouse) beginTx(ctx context.Context, opts txOptions) (*clickhouse, error) {
	ch.logf("[begin] tx=%t, data=%t", ch.inTransaction, ch.block != nil)
	switch {
	case ch.inTransaction:
		return nil, sql.ErrTxDone
	case ch.conn.closed:
		return nil, driver.ErrBadConn
	}
	if finish := ch.watchCancel(ctx); finish != nil {
		defer finish()
	}
	ch.block = nil
	ch.inTransaction = true
	return ch, nil
}

func (ch *clickhouse) Commit() error {
	ch.logf("[commit] tx=%t, data=%t", ch.inTransaction, ch.block != nil)
	defer func() {
		if ch.block != nil {
			ch.block.Reset()
			ch.block = nil
		}
		ch.inTransaction = false
	}()
	switch {
	case !ch.inTransaction:
		return sql.ErrTxDone
	case ch.conn.closed:
		return driver.ErrBadConn
	}
	if ch.block != nil {
		if err := ch.writeBlock(ch.block); err != nil {
			return err
		}
		// Send empty block as marker of end of data.
		if err := ch.writeBlock(&data.Block{}); err != nil {
			return err
		}
		if err := ch.encoder.Flush(); err != nil {
			return err
		}
		return ch.process()
	}
	return nil
}

func (ch *clickhouse) Rollback() error {
	ch.logf("[rollback] tx=%t, data=%t", ch.inTransaction, ch.block != nil)
	if !ch.inTransaction {
		return sql.ErrTxDone
	}
	if ch.block != nil {
		ch.block.Reset()
	}
	ch.block = nil
	ch.buffer = nil
	ch.inTransaction = false
	return ch.conn.Close()
}

func (ch *clickhouse) CheckNamedValue(nv *driver.NamedValue) error {
	switch nv.Value.(type) {
	case column.IP, column.UUID:
		return nil
	case nil, []byte, int8, int16, int32, int64, uint8, uint16, uint32, uint64, float32, float64, string, time.Time:
		return nil
	}
	switch v := nv.Value.(type) {
	case
		[]int, []int8, []int16, []int32, []int64,
		[]uint, []uint8, []uint16, []uint32, []uint64,
		[]float32, []float64,
		[]string:
		return nil
	case net.IP:
		return nil
	case driver.Valuer:
		value, err := v.Value()
		if err != nil {
			return err
		}
		nv.Value = value
	default:
		switch value := reflect.ValueOf(nv.Value); value.Kind() {
		case reflect.Slice:
			return nil
		case reflect.Bool:
			nv.Value = uint8(0)
			if value.Bool() {
				nv.Value = uint8(1)
			}
		case reflect.Int8:
			nv.Value = int8(value.Int())
		case reflect.Int16:
			nv.Value = int16(value.Int())
		case reflect.Int32:
			nv.Value = int32(value.Int())
		case reflect.Int64:
			nv.Value = value.Int()
		case reflect.Uint8:
			nv.Value = uint8(value.Uint())
		case reflect.Uint16:
			nv.Value = uint16(value.Uint())
		case reflect.Uint32:
			nv.Value = uint32(value.Uint())
		case reflect.Uint64:
			nv.Value = uint64(value.Uint())
		case reflect.Float32:
			nv.Value = float32(value.Float())
		case reflect.Float64:
			nv.Value = float64(value.Float())
		case reflect.String:
			nv.Value = value.String()
		}
	}
	return nil
}

func (ch *clickhouse) Close() error {
	ch.block = nil
	return ch.conn.Close()
}

func (ch *clickhouse) process() error {
	packet, err := ch.decoder.Uvarint()
	if err != nil {
		return err
	}
	for {
		switch packet {
		case protocol.ServerPong:
			ch.logf("[process] <- pong")
			return nil
		case protocol.ServerException:
			ch.logf("[process] <- exception")
			return ch.exception()
		case protocol.ServerProgress:
			progress, err := ch.progress()
			if err != nil {
				return err
			}
			ch.logf("[process] <- progress: rows=%d, bytes=%d, total rows=%d",
				progress.rows,
				progress.bytes,
				progress.totalRows,
			)
		case protocol.ServerProfileInfo:
			profileInfo, err := ch.profileInfo()
			if err != nil {
				return err
			}
			ch.logf("[process] <- profiling: rows=%d, bytes=%d, blocks=%d", profileInfo.rows, profileInfo.bytes, profileInfo.blocks)
		case protocol.ServerData:
			block, err := ch.readBlock()
			if err != nil {
				return err
			}
			ch.logf("[process] <- data: packet=%d, columns=%d, rows=%d", packet, block.NumColumns, block.NumRows)
		case protocol.ServerEndOfStream:
			ch.logf("[process] <- end of stream")
			return nil
		default:
			ch.conn.Close()
			return fmt.Errorf("[process] unexpected packet [%d] from server", packet)
		}
		if packet, err = ch.decoder.Uvarint(); err != nil {
			return err
		}
	}
}

func (ch *clickhouse) cancel() error {
	ch.logf("[cancel request]")
	// even if we fail to write the cancel, we still need to close
	err := ch.encoder.Uvarint(protocol.ClientCancel)
	if err == nil {
		err = ch.encoder.Flush()
	}
	// return the close error if there was one, otherwise return the write error
	if cerr := ch.conn.Close(); cerr != nil {
		return cerr
	}
	return err
}

func (ch *clickhouse) watchCancel(ctx context.Context) func() {
	if done := ctx.Done(); done != nil {
		finished := make(chan struct{})
		go func() {
			select {
			case <-done:
				ch.cancel()
				finished <- struct{}{}
				ch.logf("[cancel] <- done")
			case <-finished:
				ch.logf("[cancel] <- finished")
			}
		}()
		return func() {
			select {
			case <-finished:
			case finished <- struct{}{}:
			}
		}
	}
	return func() {}
}
