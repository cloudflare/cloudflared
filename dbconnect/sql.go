package dbconnect

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"github.com/xo/dburl"

	// SQL drivers self-register with the database/sql package.
	// https://github.com/golang/go/wiki/SQLDrivers
	_ "github.com/denisenkom/go-mssqldb"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/kshvakov/clickhouse"
	"github.com/lib/pq"
)

// SQLClient is a Client that talks to a SQL database.
type SQLClient struct {
	Dialect string
	driver  *sqlx.DB
}

// NewSQLClient creates a SQL client based on its URL scheme.
func NewSQLClient(ctx context.Context, originURL *url.URL) (Client, error) {
	res, err := dburl.Parse(originURL.String())
	if err != nil {
		helpText := fmt.Sprintf("supported drivers: %+q, see documentation for more details: %s", sql.Drivers(), "https://godoc.org/github.com/xo/dburl")
		return nil, fmt.Errorf("could not parse sql database url '%s': %s\n%s", originURL, err.Error(), helpText)
	}

	// Establishes the driver, but does not test the connection.
	driver, err := sqlx.Open(res.Driver, res.DSN)
	if err != nil {
		return nil, fmt.Errorf("could not open sql driver %s: %s\n%s", res.Driver, err.Error(), res.DSN)
	}

	// Closes the driver, will occur when the context finishes.
	go func() {
		<-ctx.Done()
		driver.Close()
	}()

	return &SQLClient{driver.DriverName(), driver}, nil
}

// Ping verifies a connection to the database is still alive.
func (client *SQLClient) Ping(ctx context.Context) error {
	return client.driver.PingContext(ctx)
}

// Submit queries or executes a command to the SQL database.
func (client *SQLClient) Submit(ctx context.Context, cmd *Command) (interface{}, error) {
	txx, err := cmd.ValidateSQL(client.Dialect)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, cmd.Timeout)
	defer cancel()

	var res interface{}

	// Get the next available sql.Conn and submit the Command.
	err = sqlConn(ctx, client.driver, txx, func(conn *sql.Conn) error {
		stmt := cmd.Statement
		args := cmd.Arguments.Positional

		if cmd.Mode == "query" {
			res, err = sqlQuery(ctx, conn, stmt, args)
		} else {
			res, err = sqlExec(ctx, conn, stmt, args)
		}

		return err
	})

	return res, err
}

// ValidateSQL extends the contract of Command for SQL dialects:
// mode is conformed, arguments are []sql.NamedArg, and isolation is a sql.IsolationLevel.
//
// When the command should not be wrapped in a transaction, *sql.TxOptions and error will both be nil.
func (cmd *Command) ValidateSQL(dialect string) (*sql.TxOptions, error) {
	err := cmd.Validate()
	if err != nil {
		return nil, err
	}

	mode, err := sqlMode(cmd.Mode)
	if err != nil {
		return nil, err
	}

	// Mutates Arguments to only use positional arguments with the type sql.NamedArg.
	// This is a required by the sql.Driver before submitting arguments.
	cmd.Arguments.sql(dialect)

	iso, err := sqlIsolation(cmd.Isolation)
	if err != nil {
		return nil, err
	}

	// When isolation is out-of-range, this is indicative that no
	// transaction should be executed and sql.TxOptions should be nil.
	if iso < sql.LevelDefault {
		return nil, nil
	}

	// In query mode, execute the transaction in read-only, unless it's Microsoft SQL
	// which does not support that type of transaction.
	readOnly := mode == "query" && dialect != "mssql"

	return &sql.TxOptions{Isolation: iso, ReadOnly: readOnly}, nil
}

// sqlConn gets the next available sql.Conn in the connection pool and runs a function to use it.
//
// If the transaction options are nil, run the useIt function outside a transaction.
// This is potentially an unsafe operation if the command does not clean up its state.
func sqlConn(ctx context.Context, driver *sqlx.DB, txx *sql.TxOptions, useIt func(*sql.Conn) error) error {
	conn, err := driver.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// If transaction options are specified, begin and defer a rollback to catch errors.
	var tx *sql.Tx
	if txx != nil {
		tx, err = conn.BeginTx(ctx, txx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
	}

	err = useIt(conn)

	// Check if useIt was successful and a transaction exists before committing.
	if err == nil && tx != nil {
		err = tx.Commit()
	}

	return err
}

// sqlQuery queries rows on a sql.Conn and returns an array of result objects.
func sqlQuery(ctx context.Context, conn *sql.Conn, stmt string, args []interface{}) ([]map[string]interface{}, error) {
	rows, err := conn.QueryContext(ctx, stmt, args...)
	if err == nil {
		return sqlRows(rows)
	}
	return nil, err
}

// sqlExec executes a command on a sql.Conn and returns the result of the operation.
func sqlExec(ctx context.Context, conn *sql.Conn, stmt string, args []interface{}) (sqlResult, error) {
	exec, err := conn.ExecContext(ctx, stmt, args...)
	if err == nil {
		return sqlResultFrom(exec), nil
	}
	return sqlResult{}, err
}

// sql mutates Arguments to contain a positional []sql.NamedArg.
//
// The actual return type is []interface{} due to the native Golang
// function signatures for sql.Exec and sql.Query being generic.
func (args *Arguments) sql(dialect string) {
	result := args.Positional

	for i, val := range result {
		result[i] = sqlArg("", val, dialect)
	}

	for key, val := range args.Named {
		result = append(result, sqlArg(key, val, dialect))
	}

	args.Positional = result
	args.Named = map[string]interface{}{}
}

// sqlArg creates a sql.NamedArg from a key-value pair and an optional dialect.
//
// Certain dialects will need to wrap objects, such as arrays, to conform its driver requirements.
func sqlArg(key, val interface{}, dialect string) sql.NamedArg {
	switch reflect.ValueOf(val).Kind() {

	// PostgreSQL and Clickhouse require arrays to be wrapped before
	// being inserted into the driver interface.
	case reflect.Slice, reflect.Array:
		switch dialect {
		case "postgres":
			val = pq.Array(val)
		case "clickhouse":
			val = clickhouse.Array(val)
		}
	}

	return sql.Named(fmt.Sprint(key), val)
}

// sqlIsolation tries to match a string to a sql.IsolationLevel.
func sqlIsolation(str string) (sql.IsolationLevel, error) {
	if str == "none" {
		return sql.IsolationLevel(-1), nil
	}

	for iso := sql.LevelDefault; ; iso++ {
		if iso > sql.LevelLinearizable {
			return -1, fmt.Errorf("cannot provide an invalid sql isolation level: '%s'", str)
		}

		if str == "" || strings.EqualFold(iso.String(), strings.ReplaceAll(str, "_", " ")) {
			return iso, nil
		}
	}
}

// sqlMode tries to match a string to a command mode: 'query' or 'exec' for now.
func sqlMode(str string) (string, error) {
	switch str {
	case "query", "exec":
		return str, nil
	default:
		return "", fmt.Errorf("cannot provide invalid sql mode: '%s'", str)
	}
}

// sqlRows scans through a SQL result set and returns an array of objects.
func sqlRows(rows *sql.Rows) ([]map[string]interface{}, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, errors.Wrap(err, "could not extract columns from result")
	}
	defer rows.Close()

	types, err := rows.ColumnTypes()
	if err != nil {
		// Some drivers do not support type extraction, so fail silently and continue.
		types = make([]*sql.ColumnType, len(columns))
	}

	values := make([]interface{}, len(columns))
	pointers := make([]interface{}, len(columns))

	var results []map[string]interface{}
	for rows.Next() {
		for i := range columns {
			pointers[i] = &values[i]
		}
		rows.Scan(pointers...)

		// Convert a row, an array of values, into an object where
		// each key is the name of its respective column.
		entry := make(map[string]interface{})
		for i, col := range columns {
			entry[col] = sqlValue(values[i], types[i])
		}
		results = append(results, entry)
	}

	return results, nil
}

// sqlValue handles special cases where sql.Rows does not return a "human-readable" object.
func sqlValue(val interface{}, col *sql.ColumnType) interface{} {
	bytes, ok := val.([]byte)
	if ok {
		// Opportunistically check for embedded JSON and convert it to a first-class object.
		var embedded interface{}
		if json.Unmarshal(bytes, &embedded) == nil {
			return embedded
		}

		// STOR-604: investigate a way to coerce PostgreSQL arrays '{a, b, ...}' into JSON.
		// Although easy with strings, it becomes more difficult with special types like INET[].

		return string(bytes)
	}

	return val
}

// sqlResult is a thin wrapper around sql.Result.
type sqlResult struct {
	LastInsertId int64 `json:"last_insert_id"`
	RowsAffected int64 `json:"rows_affected"`
}

// sqlResultFrom converts sql.Result into a JSON-marshable sqlResult.
func sqlResultFrom(res sql.Result) sqlResult {
	insertID, errID := res.LastInsertId()
	rowsAffected, errRows := res.RowsAffected()

	// If an error occurs when extracting the result, it is because the
	// driver does not support that specific field. Instead of passing this
	// to the user, omit the field in the response.
	if errID != nil {
		insertID = -1
	}
	if errRows != nil {
		rowsAffected = -1
	}

	return sqlResult{insertID, rowsAffected}
}
