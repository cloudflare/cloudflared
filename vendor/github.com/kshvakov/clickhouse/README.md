# ClickHouse [![Build Status](https://travis-ci.org/kshvakov/clickhouse.svg?branch=master)](https://travis-ci.org/kshvakov/clickhouse) [![Go Report Card](https://goreportcard.com/badge/github.com/kshvakov/clickhouse)](https://goreportcard.com/report/github.com/kshvakov/clickhouse) [![codecov](https://codecov.io/gh/kshvakov/clickhouse/branch/master/graph/badge.svg)](https://codecov.io/gh/kshvakov/clickhouse)

Golang SQL database driver for [Yandex ClickHouse](https://clickhouse.yandex/) 

## Key features

* Uses native ClickHouse tcp client-server protocol
* Compatibility with `database/sql`
* Round Robin load-balancing
* Bulk write support :  `begin->prepare->(in loop exec)->commit`
* LZ4 compression support (default to use pure go lz4, switch to use cgo lz4 by turn clz4 build tags on)

## DSN 

* username/password - auth credentials
* database - select the current default database
* read_timeout/write_timeout - timeout in second 
* no_delay   - disable/enable the Nagle Algorithm for tcp socket (default is 'true' - disable)
* alt_hosts  - comma separated list of single address host for load-balancing
* connection_open_strategy - random/in_order (default random). 
    * random   - choose random server from set 
    * in_order - first live server is choosen in specified order
* block_size - maximum rows in block (default is 1000000). If the rows are larger then the data will be split into several blocks to send them to the server
* pool size - maximum amount of preallocated byte chunks used in queries (default is 100). Decrease this if you experience memory problems at the expense of more GC pressure and vice versa.
* debug - enable debug output (boolean value)

SSL/TLS parameters:

* secure - establish secure connection (default is false)
* skip_verify - skip certificate verification (default is false)
* tls_config - name of a TLS config with client certificates, registered using `clickhouse.RegisterTLSConfig()`; implies secure to be true, unless explicitly specified

example:
```
tcp://host1:9000?username=user&password=qwerty&database=clicks&read_timeout=10&write_timeout=20&alt_hosts=host2:9000,host3:9000
```

## Supported data types

* UInt8, UInt16, UInt32, UInt64, Int8, Int16, Int32, Int64
* Float32, Float64
* String
* FixedString(N)
* Date 
* DateTime
* IPv4
* IPv6
* Enum
* UUID
* Nullable(T)
* [Array(T) (one-dimensional)](https://clickhouse.yandex/reference_en.html#Array(T)) [godoc](https://godoc.org/github.com/kshvakov/clickhouse#Array)

## TODO

* Support other compression methods(zstd ...)

## Install
```
go get -u github.com/kshvakov/clickhouse
```

## Example
```go 
package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/kshvakov/clickhouse"
)

func main() {
	connect, err := sql.Open("clickhouse", "tcp://127.0.0.1:9000?debug=true")
	if err != nil {
		log.Fatal(err)
	}
	if err := connect.Ping(); err != nil {
		if exception, ok := err.(*clickhouse.Exception); ok {
			fmt.Printf("[%d] %s \n%s\n", exception.Code, exception.Message, exception.StackTrace)
		} else {
			fmt.Println(err)
		}
		return
	}

	_, err = connect.Exec(`
		CREATE TABLE IF NOT EXISTS example (
			country_code FixedString(2),
			os_id        UInt8,
			browser_id   UInt8,
			categories   Array(Int16),
			action_day   Date,
			action_time  DateTime
		) engine=Memory
	`)

	if err != nil {
		log.Fatal(err)
	}
	var (
		tx, _   = connect.Begin()
		stmt, _ = tx.Prepare("INSERT INTO example (country_code, os_id, browser_id, categories, action_day, action_time) VALUES (?, ?, ?, ?, ?, ?)")
	)
	defer stmt.Close()

	for i := 0; i < 100; i++ {
		if _, err := stmt.Exec(
			"RU",
			10+i,
			100+i,
			clickhouse.Array([]int16{1, 2, 3}),
			time.Now(),
			time.Now(),
		); err != nil {
			log.Fatal(err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}

	rows, err := connect.Query("SELECT country_code, os_id, browser_id, categories, action_day, action_time FROM example")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			country               string
			os, browser           uint8
			categories            []int16
			actionDay, actionTime time.Time
		)
		if err := rows.Scan(&country, &os, &browser, &categories, &actionDay, &actionTime); err != nil {
			log.Fatal(err)
		}
		log.Printf("country: %s, os: %d, browser: %d, categories: %v, action_day: %s, action_time: %s", country, os, browser, categories, actionDay, actionTime)
	}

	if _, err := connect.Exec("DROP TABLE example"); err != nil {
		log.Fatal(err)
	}
}
```

Use [sqlx](https://github.com/jmoiron/sqlx)

```go
package main

import (
	"log"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/kshvakov/clickhouse"
)

func main() {
	connect, err := sqlx.Open("clickhouse", "tcp://127.0.0.1:9000?debug=true")
	if err != nil {
		log.Fatal(err)
	}
	var items []struct {
		CountryCode string    `db:"country_code"`
		OsID        uint8     `db:"os_id"`
		BrowserID   uint8     `db:"browser_id"`
		Categories  []int16   `db:"categories"`
		ActionTime  time.Time `db:"action_time"`
	}

	if err := connect.Select(&items, "SELECT country_code, os_id, browser_id, categories, action_time FROM example"); err != nil {
		log.Fatal(err)
	}

	for _, item := range items {
		log.Printf("country: %s, os: %d, browser: %d, categories: %v, action_time: %s", item.CountryCode, item.OsID, item.BrowserID, item.Categories, item.ActionTime)
	}
}
```
