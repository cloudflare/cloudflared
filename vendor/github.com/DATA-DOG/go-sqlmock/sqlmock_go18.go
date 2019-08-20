// +build go1.8

package sqlmock

import (
	"context"
	"database/sql/driver"
	"errors"
	"time"
)

// ErrCancelled defines an error value, which can be expected in case of
// such cancellation error.
var ErrCancelled = errors.New("canceling query due to user request")

// Implement the "QueryerContext" interface
func (c *sqlmock) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	namedArgs := make([]namedValue, len(args))
	for i, nv := range args {
		namedArgs[i] = namedValue(nv)
	}

	ex, err := c.query(query, namedArgs)
	if ex != nil {
		select {
		case <-time.After(ex.delay):
			if err != nil {
				return nil, err
			}
			return ex.rows, nil
		case <-ctx.Done():
			return nil, ErrCancelled
		}
	}

	return nil, err
}

// Implement the "ExecerContext" interface
func (c *sqlmock) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	namedArgs := make([]namedValue, len(args))
	for i, nv := range args {
		namedArgs[i] = namedValue(nv)
	}

	ex, err := c.exec(query, namedArgs)
	if ex != nil {
		select {
		case <-time.After(ex.delay):
			if err != nil {
				return nil, err
			}
			return ex.result, nil
		case <-ctx.Done():
			return nil, ErrCancelled
		}
	}

	return nil, err
}

// Implement the "ConnBeginTx" interface
func (c *sqlmock) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	ex, err := c.begin()
	if ex != nil {
		select {
		case <-time.After(ex.delay):
			if err != nil {
				return nil, err
			}
			return c, nil
		case <-ctx.Done():
			return nil, ErrCancelled
		}
	}

	return nil, err
}

// Implement the "ConnPrepareContext" interface
func (c *sqlmock) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	ex, err := c.prepare(query)
	if ex != nil {
		select {
		case <-time.After(ex.delay):
			if err != nil {
				return nil, err
			}
			return &statement{c, ex, query}, nil
		case <-ctx.Done():
			return nil, ErrCancelled
		}
	}

	return nil, err
}

// Implement the "Pinger" interface
// for now we do not have a Ping expectation
// may be something for the future
func (c *sqlmock) Ping(ctx context.Context) error {
	return nil
}

// Implement the "StmtExecContext" interface
func (stmt *statement) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return stmt.conn.ExecContext(ctx, stmt.query, args)
}

// Implement the "StmtQueryContext" interface
func (stmt *statement) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return stmt.conn.QueryContext(ctx, stmt.query, args)
}

// @TODO maybe add ExpectedBegin.WithOptions(driver.TxOptions)

// CheckNamedValue meets https://golang.org/pkg/database/sql/driver/#NamedValueChecker
func (c *sqlmock) CheckNamedValue(nv *driver.NamedValue) (err error) {
	nv.Value, err = c.converter.ConvertValue(nv.Value)
	return err
}
