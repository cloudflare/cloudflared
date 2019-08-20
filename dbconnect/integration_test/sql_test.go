package dbconnect_test

import (
	"context"
	"log"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/dbconnect"
)

func TestIntegrationPostgreSQL(t *testing.T) {
	ctx, pq := helperNewSQLClient(t, "POSTGRESQL_URL")

	err := pq.Ping(ctx)
	assert.NoError(t, err)

	_, err = pq.Submit(ctx, &dbconnect.Command{
		Statement: "CREATE TABLE t (a TEXT, b UUID, c JSON, d INET[], e SERIAL);",
		Mode:      "exec",
	})
	assert.NoError(t, err)

	_, err = pq.Submit(ctx, &dbconnect.Command{
		Statement: "INSERT INTO t VALUES ($1, $2, $3, $4);",
		Mode:      "exec",
		Arguments: dbconnect.Arguments{
			Positional: []interface{}{
				"text",
				"6b8d686d-bd8e-43bc-b09a-cfcbbe702c10",
				"{\"bool\":true,\"array\":[\"a\", 1, 3.14],\"embed\":{\"num\":21}}",
				[]string{"1.1.1.1", "1.0.0.1"},
			},
		},
	})
	assert.NoError(t, err)

	_, err = pq.Submit(ctx, &dbconnect.Command{
		Statement: "UPDATE t SET b = NULL;",
		Mode:      "exec",
	})
	assert.NoError(t, err)

	res, err := pq.Submit(ctx, &dbconnect.Command{
		Statement: "SELECT * FROM t;",
		Mode:      "query",
	})
	assert.NoError(t, err)
	assert.IsType(t, make([]map[string]interface{}, 0), res)

	actual := res.([]map[string]interface{})[0]
	expected := map[string]interface{}{
		"a": "text",
		"b": nil,
		"c": map[string]interface{}{
			"bool":  true,
			"array": []interface{}{"a", float64(1), 3.14},
			"embed": map[string]interface{}{"num": float64(21)},
		},
		"d": "{1.1.1.1,1.0.0.1}",
		"e": int64(1),
	}
	assert.EqualValues(t, expected, actual)

	_, err = pq.Submit(ctx, &dbconnect.Command{
		Statement: "DROP TABLE t;",
		Mode:      "exec",
	})
	assert.NoError(t, err)
}

func TestIntegrationMySQL(t *testing.T) {
	ctx, my := helperNewSQLClient(t, "MYSQL_URL")

	err := my.Ping(ctx)
	assert.NoError(t, err)

	_, err = my.Submit(ctx, &dbconnect.Command{
		Statement: "CREATE TABLE t (a CHAR, b TINYINT, c FLOAT, d JSON, e YEAR);",
		Mode:      "exec",
	})
	assert.NoError(t, err)

	_, err = my.Submit(ctx, &dbconnect.Command{
		Statement: "INSERT INTO t VALUES (?, ?, ?, ?, ?);",
		Mode:      "exec",
		Arguments: dbconnect.Arguments{
			Positional: []interface{}{
				"a",
				10,
				3.14,
				"{\"bool\":true}",
				2000,
			},
		},
	})
	assert.NoError(t, err)

	_, err = my.Submit(ctx, &dbconnect.Command{
		Statement: "ALTER TABLE t ADD COLUMN f GEOMETRY;",
		Mode:      "exec",
	})
	assert.NoError(t, err)

	res, err := my.Submit(ctx, &dbconnect.Command{
		Statement: "SELECT * FROM t;",
		Mode:      "query",
	})
	assert.NoError(t, err)
	assert.IsType(t, make([]map[string]interface{}, 0), res)

	actual := res.([]map[string]interface{})[0]
	expected := map[string]interface{}{
		"a": "a",
		"b": float64(10),
		"c": 3.14,
		"d": map[string]interface{}{"bool": true},
		"e": float64(2000),
		"f": nil,
	}
	assert.EqualValues(t, expected, actual)

	_, err = my.Submit(ctx, &dbconnect.Command{
		Statement: "DROP TABLE t;",
		Mode:      "exec",
	})
	assert.NoError(t, err)
}

func TestIntegrationMSSQL(t *testing.T) {
	ctx, ms := helperNewSQLClient(t, "MSSQL_URL")

	err := ms.Ping(ctx)
	assert.NoError(t, err)

	_, err = ms.Submit(ctx, &dbconnect.Command{
		Statement: "CREATE TABLE t (a BIT, b DECIMAL, c MONEY, d TEXT);",
		Mode:      "exec"})
	assert.NoError(t, err)

	_, err = ms.Submit(ctx, &dbconnect.Command{
		Statement: "INSERT INTO t VALUES (?, ?, ?, ?);",
		Mode:      "exec",
		Arguments: dbconnect.Arguments{
			Positional: []interface{}{
				0,
				3,
				"$0.99",
				"text",
			},
		},
	})
	assert.NoError(t, err)

	_, err = ms.Submit(ctx, &dbconnect.Command{
		Statement: "UPDATE t SET d = NULL;",
		Mode:      "exec",
	})
	assert.NoError(t, err)

	res, err := ms.Submit(ctx, &dbconnect.Command{
		Statement: "SELECT * FROM t;",
		Mode:      "query",
	})
	assert.NoError(t, err)
	assert.IsType(t, make([]map[string]interface{}, 0), res)

	actual := res.([]map[string]interface{})[0]
	expected := map[string]interface{}{
		"a": false,
		"b": float64(3),
		"c": float64(0.99),
		"d": nil,
	}
	assert.EqualValues(t, expected, actual)

	_, err = ms.Submit(ctx, &dbconnect.Command{
		Statement: "DROP TABLE t;",
		Mode:      "exec",
	})
	assert.NoError(t, err)
}

func TestIntegrationClickhouse(t *testing.T) {
	ctx, ch := helperNewSQLClient(t, "CLICKHOUSE_URL")

	err := ch.Ping(ctx)
	assert.NoError(t, err)

	_, err = ch.Submit(ctx, &dbconnect.Command{
		Statement: "CREATE TABLE t (a UUID, b String, c Float64, d UInt32, e Int16, f Array(Enum8('a'=1, 'b'=2, 'c'=3))) engine=Memory;",
		Mode:      "exec",
	})
	assert.NoError(t, err)

	_, err = ch.Submit(ctx, &dbconnect.Command{
		Statement: "INSERT INTO t VALUES (?, ?, ?, ?, ?, ?);",
		Mode:      "exec",
		Arguments: dbconnect.Arguments{
			Positional: []interface{}{
				"ec65f626-6f50-4c86-9628-6314ef1edacd",
				"",
				3.14,
				314,
				-144,
				[]string{"a", "b", "c"},
			},
		},
	})
	assert.NoError(t, err)

	res, err := ch.Submit(ctx, &dbconnect.Command{
		Statement: "SELECT * FROM t;",
		Mode:      "query",
	})
	assert.NoError(t, err)
	assert.IsType(t, make([]map[string]interface{}, 0), res)

	actual := res.([]map[string]interface{})[0]
	expected := map[string]interface{}{
		"a": "ec65f626-6f50-4c86-9628-6314ef1edacd",
		"b": "",
		"c": float64(3.14),
		"d": uint32(314),
		"e": int16(-144),
		"f": []string{"a", "b", "c"},
	}
	assert.EqualValues(t, expected, actual)

	_, err = ch.Submit(ctx, &dbconnect.Command{
		Statement: "DROP TABLE t;",
		Mode:      "exec",
	})
	assert.NoError(t, err)
}

func helperNewSQLClient(t *testing.T, env string) (context.Context, dbconnect.Client) {
	_, ok := os.LookupEnv("DBCONNECT_INTEGRATION_TEST")
	if ok {
		t.Helper()
	} else {
		t.SkipNow()
	}

	val, ok := os.LookupEnv(env)
	if !ok {
		log.Fatalf("must provide database url as environment variable: %s", env)
	}

	parsed, err := url.Parse(val)
	if err != nil {
		log.Fatalf("cannot provide invalid database url: %s=%s", env, val)
	}

	ctx := context.Background()
	client, err := dbconnect.NewSQLClient(ctx, parsed)
	if err != nil {
		log.Fatalf("could not start test client: %s", err)
	}

	return ctx, client
}
