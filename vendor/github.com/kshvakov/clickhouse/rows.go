package clickhouse

import (
	"database/sql/driver"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/kshvakov/clickhouse/lib/column"
	"github.com/kshvakov/clickhouse/lib/data"
	"github.com/kshvakov/clickhouse/lib/protocol"
)

type rows struct {
	ch           *clickhouse
	err          error
	mutex        sync.RWMutex
	finish       func()
	offset       int
	block        *data.Block
	totals       *data.Block
	extremes     *data.Block
	stream       chan *data.Block
	columns      []string
	blockColumns []column.Column
}

func (rows *rows) Columns() []string {
	return rows.columns
}

func (rows *rows) ColumnTypeScanType(idx int) reflect.Type {
	return rows.blockColumns[idx].ScanType()
}

func (rows *rows) ColumnTypeDatabaseTypeName(idx int) string {
	return rows.blockColumns[idx].CHType()
}

func (rows *rows) Next(dest []driver.Value) error {
	if rows.block == nil || int(rows.block.NumRows) <= rows.offset {
		switch block, ok := <-rows.stream; true {
		case !ok:
			if err := rows.error(); err != nil {
				return err
			}
			return io.EOF
		default:
			rows.block = block
			rows.offset = 0
		}
	}
	for i := range dest {
		dest[i] = rows.block.Values[i][rows.offset]
	}
	rows.offset++
	return nil
}

func (rows *rows) HasNextResultSet() bool {
	return rows.totals != nil || rows.extremes != nil
}

func (rows *rows) NextResultSet() error {
	switch {
	case rows.totals != nil:
		rows.block = rows.totals
		rows.offset = 0
		rows.totals = nil
	case rows.extremes != nil:
		rows.block = rows.extremes
		rows.offset = 0
		rows.extremes = nil
	default:
		return io.EOF
	}
	return nil
}

func (rows *rows) receiveData() error {
	defer close(rows.stream)
	var (
		err         error
		packet      uint64
		progress    *progress
		profileInfo *profileInfo
	)
	for {
		if packet, err = rows.ch.decoder.Uvarint(); err != nil {
			return rows.setError(err)
		}
		switch packet {
		case protocol.ServerException:
			rows.ch.logf("[rows] <- exception")
			return rows.setError(rows.ch.exception())
		case protocol.ServerProgress:
			if progress, err = rows.ch.progress(); err != nil {
				return rows.setError(err)
			}
			rows.ch.logf("[rows] <- progress: rows=%d, bytes=%d, total rows=%d",
				progress.rows,
				progress.bytes,
				progress.totalRows,
			)
		case protocol.ServerProfileInfo:
			if profileInfo, err = rows.ch.profileInfo(); err != nil {
				return rows.setError(err)
			}
			rows.ch.logf("[rows] <- profiling: rows=%d, bytes=%d, blocks=%d", profileInfo.rows, profileInfo.bytes, profileInfo.blocks)
		case protocol.ServerData, protocol.ServerTotals, protocol.ServerExtremes:
			var (
				block *data.Block
				begin = time.Now()
			)
			if block, err = rows.ch.readBlock(); err != nil {
				return rows.setError(err)
			}
			rows.ch.logf("[rows] <- data: packet=%d, columns=%d, rows=%d, elapsed=%s", packet, block.NumColumns, block.NumRows, time.Since(begin))
			if block.NumRows == 0 {
				continue
			}
			switch packet {
			case protocol.ServerData:
				rows.stream <- block
			case protocol.ServerTotals:
				rows.totals = block
			case protocol.ServerExtremes:
				rows.extremes = block
			}
		case protocol.ServerEndOfStream:
			rows.ch.logf("[rows] <- end of stream")
			return nil
		default:
			rows.ch.conn.Close()
			rows.ch.logf("[rows] unexpected packet [%d]", packet)
			return rows.setError(fmt.Errorf("[rows] unexpected packet [%d] from server", packet))
		}
	}
}

func (rows *rows) Close() error {
	rows.ch.logf("[rows] close")
	rows.columns = nil
	for range rows.stream {
	}
	rows.finish()
	return nil
}

func (rows *rows) error() error {
	rows.mutex.RLock()
	defer rows.mutex.RUnlock()
	return rows.err
}

func (rows *rows) setError(err error) error {
	rows.mutex.Lock()
	rows.err = err
	rows.mutex.Unlock()
	return err
}
