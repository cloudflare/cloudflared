package clickhouse

import (
	"bytes"
	"context"
	"database/sql/driver"
	"unicode"

	"github.com/kshvakov/clickhouse/lib/data"
)

type stmt struct {
	ch       *clickhouse
	query    string
	counter  int
	numInput int
	isInsert bool
}

var emptyResult = &result{}

func (stmt *stmt) NumInput() int {
	switch {
	case stmt.ch.block != nil:
		return len(stmt.ch.block.Columns)
	case stmt.numInput < 0:
		return 0
	}
	return stmt.numInput
}

func (stmt *stmt) Exec(args []driver.Value) (driver.Result, error) {
	return stmt.execContext(context.Background(), args)
}

func (stmt *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	dargs := make([]driver.Value, len(args))
	for i, nv := range args {
		dargs[i] = nv.Value
	}
	return stmt.execContext(ctx, dargs)
}

func (stmt *stmt) execContext(ctx context.Context, args []driver.Value) (driver.Result, error) {
	if stmt.isInsert {
		stmt.counter++
		if err := stmt.ch.block.AppendRow(args); err != nil {
			return nil, err
		}
		if (stmt.counter % stmt.ch.blockSize) == 0 {
			stmt.ch.logf("[exec] flush block")
			if err := stmt.ch.writeBlock(stmt.ch.block); err != nil {
				return nil, err
			}
			if err := stmt.ch.encoder.Flush(); err != nil {
				return nil, err
			}
		}
		return emptyResult, nil
	}
	if err := stmt.ch.sendQuery(stmt.bind(convertOldArgs(args))); err != nil {
		return nil, err
	}
	if err := stmt.ch.process(); err != nil {
		return nil, err
	}
	return emptyResult, nil
}

func (stmt *stmt) Query(args []driver.Value) (driver.Rows, error) {
	return stmt.queryContext(context.Background(), convertOldArgs(args))
}

func (stmt *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return stmt.queryContext(ctx, args)
}

func (stmt *stmt) queryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	finish := stmt.ch.watchCancel(ctx)
	if err := stmt.ch.sendQuery(stmt.bind(args)); err != nil {
		finish()
		return nil, err
	}
	meta, err := stmt.ch.readMeta()
	if err != nil {
		finish()
		return nil, err
	}
	rows := rows{
		ch:           stmt.ch,
		finish:       finish,
		stream:       make(chan *data.Block, 50),
		columns:      meta.ColumnNames(),
		blockColumns: meta.Columns,
	}
	go rows.receiveData()
	return &rows, nil
}

func (stmt *stmt) Close() error {
	stmt.ch.logf("[stmt] close")
	return nil
}

func (stmt *stmt) bind(args []driver.NamedValue) string {
	var (
		buf       bytes.Buffer
		index     int
		keyword   bool
		inBetween bool
		like      = newMatcher("like")
		limit     = newMatcher("limit")
		between   = newMatcher("between")
		and       = newMatcher("and")
	)
	switch {
	case stmt.NumInput() != 0:
		reader := bytes.NewReader([]byte(stmt.query))
		for {
			if char, _, err := reader.ReadRune(); err == nil {
				switch char {
				case '@':
					if param := paramParser(reader); len(param) != 0 {
						for _, v := range args {
							if len(v.Name) != 0 && v.Name == param {
								buf.WriteString(quote(v.Value))
							}
						}
					}
				case '?':
					if keyword && index < len(args) && len(args[index].Name) == 0 {
						buf.WriteString(quote(args[index].Value))
						index++
					} else {
						buf.WriteRune(char)
					}
				default:
					switch {
					case
						char == '=',
						char == '<',
						char == '>',
						char == '(',
						char == ',',
						char == '+',
						char == '-',
						char == '*',
						char == '/',
						char == '[':
						keyword = true
					default:
						if limit.matchRune(char) || like.matchRune(char) {
							keyword = true
						} else if between.matchRune(char) {
							keyword = true
							inBetween = true
						} else if inBetween && and.matchRune(char) {
							keyword = true
							inBetween = false
						} else {
							keyword = keyword && unicode.IsSpace(char)
						}
					}
					buf.WriteRune(char)
				}
			} else {
				break
			}
		}
	default:
		buf.WriteString(stmt.query)
	}
	return buf.String()
}

func convertOldArgs(args []driver.Value) []driver.NamedValue {
	dargs := make([]driver.NamedValue, len(args))
	for i, v := range args {
		dargs[i] = driver.NamedValue{
			Ordinal: i + 1,
			Value:   v,
		}
	}
	return dargs
}
