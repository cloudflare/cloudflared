package dbconnect

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kshvakov/clickhouse"
	"github.com/lib/pq"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

func TestNewSQLClient(t *testing.T) {
	originURLs := []string{
		"postgres://localhost",
		"cockroachdb://localhost:1337",
		"postgresql://user:pass@127.0.0.1",
		"mysql://localhost",
		"clickhouse://127.0.0.1:9000/?debug",
		"sqlite3::memory:",
		"file:test.db?cache=shared",
	}

	for _, originURL := range originURLs {
		origin, _ := url.Parse(originURL)
		_, err := NewSQLClient(context.Background(), origin)

		assert.NoError(t, err, originURL)
	}

	originURLs = []string{
		"",
		"/",
		"http://localhost",
		"coolthing://user:pass@127.0.0.1",
	}

	for _, originURL := range originURLs {
		origin, _ := url.Parse(originURL)
		_, err := NewSQLClient(context.Background(), origin)

		assert.Error(t, err, originURL)
	}
}

func TestArgumentsSQL(t *testing.T) {
	args := []Arguments{
		Arguments{
			Positional: []interface{}{
				"val", 10, 3.14,
			},
		},
		Arguments{
			Named: map[string]interface{}{
				"key": time.Unix(0, 0),
			},
		},
	}

	var nameType sql.NamedArg
	for _, arg := range args {
		arg.sql("")
		for _, named := range arg.Positional {
			assert.IsType(t, nameType, named)
		}
	}
}

func TestSQLArg(t *testing.T) {
	tests := []struct {
		key     interface{}
		val     interface{}
		dialect string
		arg     sql.NamedArg
	}{
		{"key", "val", "mssql", sql.Named("key", "val")},
		{0, 1, "sqlite3", sql.Named("0", 1)},
		{1, []string{"a", "b", "c"}, "postgres", sql.Named("1", pq.Array([]string{"a", "b", "c"}))},
		{"in", []uint{0, 1}, "clickhouse", sql.Named("in", clickhouse.Array([]uint{0, 1}))},
		{"", time.Unix(0, 0), "", sql.Named("", time.Unix(0, 0))},
	}

	for _, test := range tests {
		arg := sqlArg(test.key, test.val, test.dialect)
		assert.Equal(t, test.arg, arg, test.key)
	}
}

func TestSQLIsolation(t *testing.T) {
	tests := []struct {
		str string
		iso sql.IsolationLevel
	}{
		{"", sql.LevelDefault},
		{"DEFAULT", sql.LevelDefault},
		{"read_UNcommitted", sql.LevelReadUncommitted},
		{"serializable", sql.LevelSerializable},
		{"none", sql.IsolationLevel(-1)},
		{"SNAP shot", -2},
		{"blah", -2},
	}

	for _, test := range tests {
		iso, err := sqlIsolation(test.str)

		if test.iso < -1 {
			assert.Error(t, err, test.str)
		} else {
			assert.NoError(t, err)
			assert.Equal(t, test.iso, iso, test.str)
		}
	}
}

func TestSQLMode(t *testing.T) {
	modes := []string{
		"query",
		"exec",
	}

	for _, mode := range modes {
		actual, err := sqlMode(mode)

		assert.NoError(t, err)
		assert.Equal(t, strings.ToLower(mode), actual, mode)
	}

	modes = []string{
		"",
		"blah",
	}

	for _, mode := range modes {
		_, err := sqlMode(mode)

		assert.Error(t, err)
	}
}

func helperRows(mockRows *sqlmock.Rows) *sql.Rows {
	db, mock, _ := sqlmock.New()
	mock.ExpectQuery("SELECT").WillReturnRows(mockRows)
	rows, _ := db.Query("SELECT")
	return rows
}

func TestSQLRows(t *testing.T) {
	actual, err := sqlRows(helperRows(sqlmock.NewRows(
		[]string{"name", "age", "dept"}).
		AddRow("alice", 19, "prod")))
	expected := []map[string]interface{}{map[string]interface{}{
		"name": "alice",
		"age":  int64(19),
		"dept": "prod"}}

	assert.NoError(t, err)
	assert.ElementsMatch(t, expected, actual)
}

func TestSQLValue(t *testing.T) {
	tests := []struct {
		input  interface{}
		output interface{}
	}{
		{"hello", "hello"},
		{1, 1},
		{false, false},
		{[]byte("random"), "random"},
		{[]byte("{\"json\":true}"), map[string]interface{}{"json": true}},
		{[]byte("[]"), []interface{}{}},
	}

	for _, test := range tests {
		assert.Equal(t, test.output, sqlValue(test.input, nil), test.input)
	}
}

func TestSQLResultFrom(t *testing.T) {
	res := sqlResultFrom(sqlmock.NewResult(1, 2))
	assert.Equal(t, sqlResult{1, 2}, res)

	res = sqlResultFrom(sqlmock.NewErrorResult(fmt.Errorf("")))
	assert.Equal(t, sqlResult{-1, -1}, res)
}

func helperSQLite3(t *testing.T) (context.Context, Client) {
	t.Helper()

	ctx := context.Background()
	url, _ := url.Parse("file::memory:?cache=shared")

	sqlite3, err := NewSQLClient(ctx, url)
	assert.NoError(t, err)

	return ctx, sqlite3
}

func TestPing(t *testing.T) {
	ctx, sqlite3 := helperSQLite3(t)
	err := sqlite3.Ping(ctx)

	assert.NoError(t, err)
}

func TestSubmit(t *testing.T) {
	ctx, sqlite3 := helperSQLite3(t)

	res, err := sqlite3.Submit(ctx, &Command{
		Statement: "CREATE TABLE t (a INTEGER, b FLOAT, c TEXT, d BLOB);",
		Mode:      "exec",
	})
	assert.NoError(t, err)
	assert.Equal(t, sqlResult{0, 0}, res)

	res, err = sqlite3.Submit(ctx, &Command{
		Statement: "SELECT * FROM t;",
		Mode:      "query",
	})
	assert.NoError(t, err)
	assert.Empty(t, res)

	res, err = sqlite3.Submit(ctx, &Command{
		Statement: "INSERT INTO t VALUES (?, ?, ?, ?);",
		Mode:      "exec",
		Arguments: Arguments{
			Positional: []interface{}{
				1,
				3.14,
				"text",
				"blob",
			},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, sqlResult{1, 1}, res)

	res, err = sqlite3.Submit(ctx, &Command{
		Statement: "UPDATE t SET c = NULL;",
		Mode:      "exec",
	})
	assert.NoError(t, err)
	assert.Equal(t, sqlResult{1, 1}, res)

	res, err = sqlite3.Submit(ctx, &Command{
		Statement: "SELECT * FROM t WHERE a = ?;",
		Mode:      "query",
		Arguments: Arguments{
			Positional: []interface{}{1},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, res, 1)

	resf, ok := res.([]map[string]interface{})
	assert.True(t, ok)
	assert.EqualValues(t, map[string]interface{}{
		"a": int64(1),
		"b": 3.14,
		"c": nil,
		"d": "blob",
	}, resf[0])

	res, err = sqlite3.Submit(ctx, &Command{
		Statement: "DROP TABLE t;",
		Mode:      "exec",
	})
	assert.NoError(t, err)
	assert.Equal(t, sqlResult{1, 1}, res)
}

func TestSubmitTransaction(t *testing.T) {
	ctx, sqlite3 := helperSQLite3(t)

	res, err := sqlite3.Submit(ctx, &Command{
		Statement: "BEGIN;",
		Mode:      "exec",
	})
	assert.Error(t, err)
	assert.Empty(t, res)

	res, err = sqlite3.Submit(ctx, &Command{
		Statement: "BEGIN; CREATE TABLE tt (a INT); COMMIT;",
		Mode:      "exec",
		Isolation: "none",
	})
	assert.NoError(t, err)
	assert.Equal(t, sqlResult{0, 0}, res)

	rows, err := sqlite3.Submit(ctx, &Command{
		Statement: "SELECT * FROM tt;",
		Mode:      "query",
		Isolation: "repeatable_read",
	})
	assert.NoError(t, err)
	assert.Empty(t, rows)
}

func TestSubmitTimeout(t *testing.T) {
	ctx, sqlite3 := helperSQLite3(t)

	res, err := sqlite3.Submit(ctx, &Command{
		Statement: "SELECT * FROM t;",
		Mode:      "query",
		Timeout:   1 * time.Nanosecond,
	})
	assert.Error(t, err)
	assert.Empty(t, res)
}

func TestSubmitMode(t *testing.T) {
	ctx, sqlite3 := helperSQLite3(t)

	res, err := sqlite3.Submit(ctx, &Command{
		Statement: "SELECT * FROM t;",
		Mode:      "notanoption",
	})
	assert.Error(t, err)
	assert.Empty(t, res)
}

func TestSubmitEmpty(t *testing.T) {
	ctx, sqlite3 := helperSQLite3(t)

	res, err := sqlite3.Submit(ctx, &Command{
		Statement: "; ; ; ;",
		Mode:      "query",
	})
	assert.Error(t, err)
	assert.Empty(t, res)
}
