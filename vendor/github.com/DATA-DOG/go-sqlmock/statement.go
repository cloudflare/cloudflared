package sqlmock

import (
	"database/sql/driver"
)

type statement struct {
	conn  *sqlmock
	ex    *ExpectedPrepare
	query string
}

func (stmt *statement) Close() error {
	stmt.ex.wasClosed = true
	return stmt.ex.closeErr
}

func (stmt *statement) NumInput() int {
	return -1
}

func (stmt *statement) Exec(args []driver.Value) (driver.Result, error) {
	return stmt.conn.Exec(stmt.query, args)
}

func (stmt *statement) Query(args []driver.Value) (driver.Rows, error) {
	return stmt.conn.Query(stmt.query, args)
}
