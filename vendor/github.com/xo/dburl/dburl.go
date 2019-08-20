// Package dburl provides a standard, URL style mechanism for parsing and
// opening SQL database connection strings.
//
// Database URL Connection Strings
//
// Supported database URLs are of the form:
//
//   protocol+transport://user:pass@host/dbname?opt1=a&opt2=b
//   protocol:/path/to/file
//
// Where:
//
//   protocol  - driver name or alias (see below)
//   transport - "tcp", "udp", "unix" or driver name (odbc/oleodbc)                                  |
//   user      - username
//   pass      - password
//   host      - host
//   dbname*   - database, instance, or service name/id to connect to
//   ?opt1=... - additional database driver options
//                 (see respective SQL driver for available options)
//
// * for Microsoft SQL Server, the syntax to supply an instance and database
// name is /instance/dbname, where /instance is optional. For Oracle databases,
// /dbname is the unique database ID (SID). Please see below for examples.
//
// Quickstart
//
// URLs in the above format can be parsed with Parse as such:
//
//   u, err := dburl.Parse("postgresql://user:pass@localhost/mydatabase/?sslmode=disable")
//   if err != nil { /* ... */ }
//
// Additionally, a simple helper func, Open, is available to quickly parse,
// open, and return a standard SQL database connection:
//
//   db, err := dburl.Open("sqlite:mydatabase.sqlite3?loc=auto")
//   if err != nil { /* ... */ }
//
// Example URLs
//
// The following are URLs that can be handled with a call to Open or Parse:
//
//   postgres://user:pass@localhost/dbname
//   pg://user:pass@localhost/dbname?sslmode=disable
//   mysql://user:pass@localhost/dbname
//   mysql:/var/run/mysqld/mysqld.sock
//   sqlserver://user:pass@remote-host.com/dbname
//   mssql://user:pass@remote-host.com/instance/dbname
//   ms://user:pass@remote-host.com:port/instance/dbname?keepAlive=10
//   oracle://user:pass@somehost.com/oracledb
//   sap://user:pass@localhost/dbname
//   sqlite:/path/to/file.db
//   file:myfile.sqlite3?loc=auto
//   odbc+postgres://user:pass@localhost:port/dbname?option1=
//
// Protocol Schemes and Aliases
//
// The following protocols schemes (ie, driver) and their associated aliases
// are supported out of the box:
//
//   Database (scheme/driver)     | Protocol Aliases [real driver]
//   -----------------------------|-------------------------------------------
//   Microsoft SQL Server (mssql) | ms, sqlserver
//   MySQL (mysql)                | my, mariadb, maria, percona, aurora
//   Oracle (ora)                 | or, oracle, oci8, oci
//   PostgreSQL (postgres)        | pg, postgresql, pgsql
//   SQLite3 (sqlite3)            | sq, sqlite, file
//   -----------------------------|-------------------------------------------
//   Amazon Redshift (redshift)   | rs [postgres]
//   CockroachDB (cockroachdb)    | cr, cockroach, crdb, cdb [postgres]
//   MemSQL (memsql)              | me [mysql]
//   TiDB (tidb)                  | ti [mysql]
//   Vitess (vitess)              | vt [mysql]
//   -----------------------------|-------------------------------------------
//   Google Spanner (spanner)     | gs, google, span (not yet public)
//   -----------------------------|-------------------------------------------
//   MySQL (mymysql)              | zm, mymy
//   PostgreSQL (pgx)             | px
//   -----------------------------|-------------------------------------------
//   Apache Avatica (avatica)     | av, phoenix
//   Apache Ignite (ignite)       | ig, gridgain
//   Cassandra (cql)              | ca, cassandra, datastax, scy, scylla
//   ClickHouse (clickhouse)      | ch
//   Couchbase (n1ql)             | n1, couchbase
//   Cznic QL (ql)                | ql, cznic, cznicql
//   Firebird SQL (firebirdsql)   | fb, firebird
//   Microsoft ADODB (adodb)      | ad, ado
//   ODBC (odbc)                  | od
//   OLE ODBC (oleodbc)           | oo, ole, oleodbc [adodb]
//   Presto (presto)              | pr, prestodb, prestos, prs, prestodbs
//   SAP ASE (tds)                | ax, ase, sapase
//   SAP HANA (hdb)               | sa, saphana, sap, hana
//   Snowflake (snowflake)        | sf
//   VoltDB (voltdb)              | vo, volt, vdb
//
// Any protocol scheme alias:// can be used in place of protocol://, and will
// work identically with Parse/Open.
//
// Usage
//
// Please note that the dburl package does not import actual SQL drivers, and
// only provides a standard way to parse/open respective database connection URLs.
//
// For reference, these are the following "expected" SQL drivers that would need
// to be imported:
//
//   Database (scheme/driver)     | Package
//   -----------------------------|-------------------------------------------------
//   Microsoft SQL Server (mssql) | github.com/denisenkom/go-mssqldb
//   MySQL (mysql)                | github.com/go-sql-driver/mysql
//   Oracle (ora)                 | gopkg.in/rana/ora.v4
//   PostgreSQL (postgres)        | github.com/lib/pq
//   SQLite3 (sqlite3)            | github.com/mattn/go-sqlite3
//   -----------------------------|-------------------------------------------------
//   Amazon Redshift (redshift)   | github.com/lib/pq
//   CockroachDB (cockroachdb)    | github.com/lib/pq
//   MemSQL (memsql)              | github.com/go-sql-driver/mysql
//   TiDB (tidb)                  | github.com/go-sql-driver/mysql
//   Vitess (vitess)              | github.com/go-sql-driver/mysql
//   -----------------------------|-------------------------------------------------
//   Google Spanner (spanner)     | github.com/xo/spanner (not yet public)
//   -----------------------------|-------------------------------------------------
//   MySQL (mymysql)              | github.com/ziutek/mymysql/godrv
//   PostgreSQL (pgx)             | github.com/jackc/pgx/stdlib
//   -----------------------------|-------------------------------------------------
//   Apache Avatica (avatica)     | github.com/Boostport/avatica
//   Apache Ignite (ignite)       | github.com/amsokol/ignite-go-client/sql
//   Cassandra (cql)              | github.com/MichaelS11/go-cql-driver
//   ClickHouse (clickhouse)      | github.com/kshvakov/clickhouse
//   Couchbase (n1ql)             | github.com/couchbase/go_n1ql
//   Cznic QL (ql)                | github.com/cznic/ql
//   Firebird SQL (firebirdsql)   | github.com/nakagami/firebirdsql
//   Microsoft ADODB (adodb)      | github.com/mattn/go-adodb
//   ODBC (odbc)                  | github.com/alexbrainman/odbc
//   OLE ODBC (oleodbc)*          | github.com/mattn/go-adodb
//   Presto (presto)              | github.com/prestodb/presto-go-client
//   SAP ASE (tds)                | github.com/thda/tds
//   SAP HANA (hdb)               | github.com/SAP/go-hdb/driver
//   Snowflake (snowflake)        | github.com/snowflakedb/gosnowflake
//   VoltDB (voltdb)              | github.com/VoltDB/voltdb-client-go/voltdbclient
//
// * OLE ODBC is not an actual protocol, but instead is an alias for using the
//   "MSDASQL.1" OLE provider with the ADODB driver, and the DSN will be an
//   ADODB DSN, but with "Extended Properties" for the respective ODBC
//   parameters, including the underlying transport prootocol. As such,
//   "oleodbc+protocol://user:pass@host/dbname" URLs are equivalent to
//   "adodb://MSDASQL.1/?Extended+Properties=...". Please see the
//   documentation for GenOLEODBC for information regarding how URL components
//   are mapped/passed to ADODB's Extended Properties parameter.
//
// URL Parsing Rules
//
// Parse and Open rely heavily on the standard net/url.URL type, as such
// parsing rules have the same conventions/semantics as any URL parsed by the
// standard library's net/url.Parse.
//
// Related Projects
//
// This package was written mainly to support xo (https://github.com/xo/xo)
// and usql (https://github.com/xo/usql).
package dburl

import (
	"database/sql"
)

// Error is a dburl error.
type Error string

// Error satisfies the error interface.
func (err Error) Error() string {
	return string(err)
}

const (
	// ErrInvalidDatabaseScheme is the invalid database scheme error.
	ErrInvalidDatabaseScheme Error = "invalid database scheme"

	// ErrUnknownDatabaseScheme is the unknown database type error.
	ErrUnknownDatabaseScheme Error = "unknown database scheme"

	// ErrInvalidTransportProtocol is the invalid transport protocol error.
	ErrInvalidTransportProtocol Error = "invalid transport protocol"

	// ErrRelativePathNotSupported is the relative paths not supported error.
	ErrRelativePathNotSupported Error = "relative path not supported"

	// ErrMissingHost is the missing host error.
	ErrMissingHost Error = "missing host"

	// ErrMissingPath is the missing path error.
	ErrMissingPath Error = "missing path"
)

// Open takes a urlstr like "protocol+transport://user:pass@host/dbname?option1=a&option2=b"
// and creates a standard sql.DB connection.
//
// See Parse for information on formatting URLs to work properly with Open.
func Open(urlstr string) (*sql.DB, error) {
	u, err := Parse(urlstr)
	if err != nil {
		return nil, err
	}

	return sql.Open(u.Driver, u.DSN)
}
