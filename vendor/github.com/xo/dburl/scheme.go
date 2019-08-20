package dburl

import (
	"fmt"
	"sort"
)

// Proto are the allowed transport protocol types in a database URL scheme.
type Proto uint

// Proto types.
const (
	ProtoNone Proto = 0
	ProtoTCP  Proto = 1
	ProtoUDP  Proto = 2
	ProtoUnix Proto = 4
	ProtoAny  Proto = 8
)

// Scheme wraps information used for registering a URL scheme with
// Parse/Open.
type Scheme struct {
	// Driver is the name of the SQL driver that will set as the Scheme in
	// Parse'd URLs, and is the driver name expected by the standard sql.Open
	// calls.
	//
	// Note: a 2 letter alias will always be registered for the Driver as the
	// first 2 characters of the Driver, unless one of the Aliases includes an
	// alias that is 2 characters.
	Driver string

	// Generator is the func responsible for generating a DSN based on parsed
	// URL information.
	//
	// Note: this func should not modify the passed URL.
	Generator func(*URL) (string, error)

	// Proto are allowed protocol types for the scheme.
	Proto Proto

	// Opaque toggles Parse to not re-process URLs with an "opaque" component.
	Opaque bool

	// Aliases are any additional aliases for the scheme.
	Aliases []string

	// Override is the Go SQL driver to use instead of Driver.
	Override string
}

// BaseSchemes returns the supported base schemes.
func BaseSchemes() []Scheme {
	return []Scheme{
		// core databases
		{"mssql", GenSQLServer, 0, false, []string{"sqlserver"}, ""},
		{"mysql", GenMySQL, ProtoTCP | ProtoUDP | ProtoUnix, false, []string{"mariadb", "maria", "percona", "aurora"}, ""},
		{"ora", GenOracle, 0, false, []string{"oracle", "oci8", "oci"}, ""},
		{"postgres", GenPostgres, ProtoUnix, false, []string{"pg", "postgresql", "pgsql"}, ""},
		{"sqlite3", GenOpaque, 0, true, []string{"sqlite", "file"}, ""},

		// wire compatibles
		{"cockroachdb", GenFromURL("postgres://localhost:26257/?sslmode=disable"), 0, false, []string{"cr", "cockroach", "crdb", "cdb"}, "postgres"},
		{"memsql", GenMySQL, 0, false, nil, "mysql"},
		{"redshift", GenFromURL("postgres://localhost:5439/"), 0, false, []string{"rs"}, "postgres"},
		{"tidb", GenMySQL, 0, false, nil, "mysql"},
		{"vitess", GenMySQL, 0, false, []string{"vt"}, "mysql"},

		// testing
		{"spanner", GenScheme("spanner"), 0, false, []string{"gs", "google", "span"}, ""},

		// alternates implementations
		{"mymysql", GenMyMySQL, ProtoTCP | ProtoUDP | ProtoUnix, false, []string{"zm", "mymy"}, ""},
		{"pgx", GenScheme("postgres"), ProtoUnix, false, []string{"px"}, ""},

		// other databases
		{"adodb", GenADODB, 0, false, []string{"ado"}, ""},
		{"avatica", GenFromURL("http://localhost:8765/"), 0, false, []string{"phoenix"}, ""},
		{"cql", GenCassandra, 0, false, []string{"ca", "cassandra", "datastax", "scy", "scylla"}, ""},
		{"clickhouse", GenClickhouse, 0, false, []string{"ch"}, ""},
		{"firebirdsql", GenFirebird, 0, false, []string{"fb", "firebird"}, ""},
		{"hdb", GenScheme("hdb"), 0, false, []string{"sa", "saphana", "sap", "hana"}, ""},
		{"ignite", GenIgnite, 0, false, []string{"ig", "gridgain"}, ""},
		{"n1ql", GenFromURL("http://localhost:9000/"), 0, false, []string{"couchbase"}, ""},
		{"odbc", GenODBC, ProtoAny, false, nil, ""},
		{"oleodbc", GenOLEODBC, ProtoAny, false, []string{"oo", "ole"}, "adodb"},
		{"presto", GenPresto, 0, false, []string{"prestodb", "prestos", "prs", "prestodbs"}, ""},
		{"ql", GenOpaque, 0, true, []string{"ql", "cznic", "cznicql"}, ""},
		{"snowflake", GenSnowflake, 0, false, []string{"sf"}, ""},
		{"tds", GenFromURL("http://localhost:5000/"), 0, false, []string{"ax", "ase", "sapase"}, ""},
		{"voltdb", GenVoltDB, 0, false, []string{"volt", "vdb"}, ""},
	}
}

func init() {
	schemes := BaseSchemes()
	schemeMap = make(map[string]*Scheme, len(schemes))

	// register
	for _, scheme := range schemes {
		Register(scheme)
	}
}

// schemeMap is the map of registered schemes.
var schemeMap map[string]*Scheme

// registerAlias registers a alias for an already registered Scheme.
func registerAlias(name, alias string, doSort bool) {
	scheme, ok := schemeMap[name]
	if !ok {
		panic(fmt.Sprintf("scheme %s not registered", name))
	}

	if doSort && has(scheme.Aliases, alias) {
		panic(fmt.Sprintf("scheme %s already has alias %s", name, alias))
	}

	if _, ok := schemeMap[alias]; ok {
		panic(fmt.Sprintf("scheme %s already registered", alias))
	}

	scheme.Aliases = append(scheme.Aliases, alias)
	if doSort {
		sort.Sort(ss(scheme.Aliases))
	}

	schemeMap[alias] = scheme
}

// Register registers a Scheme.
func Register(scheme Scheme) {
	if scheme.Generator == nil {
		panic("must specify Generator when registering Scheme")
	}

	if scheme.Opaque && scheme.Proto&ProtoUnix != 0 {
		panic("scheme must support only Opaque or Unix protocols, not both")
	}

	// register
	if _, ok := schemeMap[scheme.Driver]; ok {
		panic(fmt.Sprintf("scheme %s already registered", scheme.Driver))
	}

	sz := &Scheme{
		Driver:    scheme.Driver,
		Generator: scheme.Generator,
		Proto:     scheme.Proto,
		Opaque:    scheme.Opaque,
		Override:  scheme.Override,
	}

	schemeMap[scheme.Driver] = sz

	// add aliases
	var hasShort bool
	for _, alias := range scheme.Aliases {
		if len(alias) == 2 {
			hasShort = true
		}
		if scheme.Driver != alias {
			registerAlias(scheme.Driver, alias, false)
		}
	}

	if !hasShort && len(scheme.Driver) > 2 {
		registerAlias(scheme.Driver, scheme.Driver[:2], false)
	}

	// ensure always at least one alias, and that if Driver is 2 characters,
	// that it gets added as well
	if len(sz.Aliases) == 0 || len(scheme.Driver) == 2 {
		sz.Aliases = append(sz.Aliases, scheme.Driver)
	}

	// sort
	sort.Sort(ss(sz.Aliases))
}

// Unregister unregisters a Scheme and all associated aliases.
func Unregister(name string) *Scheme {
	scheme, ok := schemeMap[name]
	if ok {
		for _, alias := range scheme.Aliases {
			delete(schemeMap, alias)
		}
		delete(schemeMap, name)
		return scheme
	}
	return nil
}

// RegisterAlias registers a alias for an already registered Scheme.h
func RegisterAlias(name, alias string) {
	registerAlias(name, alias, true)
}

// has is a util func to determine if a contains b.
func has(a []string, b string) bool {
	for _, s := range a {
		if s == b {
			return true
		}
	}

	return false
}

// SchemeDriverAndAliases returns the registered driver and aliases for a
// database scheme.
func SchemeDriverAndAliases(name string) (string, []string) {
	if scheme, ok := schemeMap[name]; ok {
		driver := scheme.Driver
		if scheme.Override != "" {
			driver = scheme.Override
		}

		var aliases []string
		for _, alias := range scheme.Aliases {
			if alias == driver {
				continue
			}
			aliases = append(aliases, alias)
		}

		sort.Sort(ss(aliases))

		return driver, aliases
	}

	return "", nil
}

// ss is a util type to provide sorting of a string slice (used for sorting
// aliases).
type ss []string

func (s ss) Len() int      { return len(s) }
func (s ss) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s ss) Less(i, j int) bool {
	if len(s[i]) <= len(s[j]) {
		return true
	}

	if len(s[j]) < len(s[i]) {
		return false
	}

	return s[i] < s[j]
}
