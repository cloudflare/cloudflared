package dburl

import (
	"net/url"
	"os"
	stdpath "path"
	"sort"
	"strings"
)

// contains code taken from go1.8, for purposes of backwards compatability with
// older go versions.

// hostname returns u.Host, without any port number.
//
// If Host is an IPv6 literal with a port number, Hostname returns the
// IPv6 literal without the square brackets. IPv6 literals may include
// a zone identifier.
func hostname(hostport string) string {
	colon := strings.IndexByte(hostport, ':')
	if colon == -1 {
		return hostport
	}
	if i := strings.IndexByte(hostport, ']'); i != -1 {
		return strings.TrimPrefix(hostport[:i], "[")
	}
	return hostport[:colon]
}

// hostport returns the port part of u.Host, without the leading colon.
// If u.Host doesn't contain a port, Port returns an empty string.
func hostport(hostport string) string {
	colon := strings.IndexByte(hostport, ':')
	if colon == -1 {
		return ""
	}
	if i := strings.Index(hostport, "]:"); i != -1 {
		return hostport[i+len("]:"):]
	}
	if strings.Contains(hostport, "]") {
		return ""
	}
	return hostport[colon+len(":"):]
}

// genOptions takes URL values and generates options, joining together with
// joiner, and separated by sep, with any multi URL values joined by valSep,
// ignoring any values with keys in ignore.
//
// For example, to build a "ODBC" style connection string, use like the following:
//     genOptions(u.Query(), "", "=", ";", ",")
func genOptions(q url.Values, joiner, assign, sep, valSep string, skipWhenEmpty bool, ignore ...string) string {
	qlen := len(q)
	if qlen == 0 {
		return ""
	}

	// make ignore map
	ig := make(map[string]bool, len(ignore))
	for _, v := range ignore {
		ig[strings.ToLower(v)] = true
	}

	// sort keys
	s := make([]string, len(q))
	var i int
	for k := range q {
		s[i] = k
		i++
	}
	sort.Strings(s)

	var opts []string
	for _, k := range s {
		if !ig[strings.ToLower(k)] {
			val := strings.Join(q[k], valSep)
			if !skipWhenEmpty || val != "" {
				if val != "" {
					val = assign + val
				}
				opts = append(opts, k+val)
			}
		}
	}

	if len(opts) != 0 {
		return joiner + strings.Join(opts, sep)
	}

	return ""
}

// genOptionsODBC is a util wrapper around genOptions that uses the fixed settings
// for ODBC style connection strings.
func genOptionsODBC(q url.Values, skipWhenEmpty bool, ignore ...string) string {
	return genOptions(q, "", "=", ";", ",", skipWhenEmpty, ignore...)
}

// genQueryOptions generates standard query options.
func genQueryOptions(q url.Values) string {
	if s := q.Encode(); s != "" {
		return "?" + s
	}
	return ""
}

// convertOptions converts an option value based on name, value pairs.
func convertOptions(q url.Values, pairs ...string) url.Values {
	n := make(url.Values)
	for k, v := range q {
		x := make([]string, len(v))
		for i, z := range v {
			for j := 0; j < len(pairs); j += 2 {
				if pairs[j] == z {
					z = pairs[j+1]
				}
			}
			x[i] = z
		}
		n[k] = x
	}

	return n
}

// mode returns the mode of the path.
func mode(path string) os.FileMode {
	if fi, err := os.Stat(path); err == nil {
		return fi.Mode()
	}

	return 0
}

// resolveSocket tries to resolve a path to a Unix domain socket based on the
// form "/path/to/socket/dbname" returning either the original path and the
// empty string, or the components "/path/to/socket" and "dbname", when
// /path/to/socket/dbname is reported by os.Stat as a socket.
//
// Used for MySQL DSNs.
func resolveSocket(path string) (string, string) {
	dir, dbname := path, ""
	for dir != "" && dir != "/" && dir != "." {
		if m := mode(dir); m&os.ModeSocket != 0 {
			return dir, dbname
		}
		dir, dbname = stdpath.Dir(dir), stdpath.Base(dir)
	}

	return path, ""
}

// resolveDir resolves a directory with a :port list.
//
// Used for PostgreSQL DSNs.
func resolveDir(path string) (string, string, string) {
	dir := path
	for dir != "" && dir != "/" && dir != "." {
		port := ""
		i, j := strings.LastIndex(dir, ":"), strings.LastIndex(dir, "/")
		if i != -1 && i > j {
			port = dir[i+1:]
			dir = dir[:i]
		}

		if mode(dir)&os.ModeDir != 0 {
			rest := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(path, dir), ":"+port), "/")
			return dir, port, rest
		}

		if j != -1 {
			dir = dir[:j]
		} else {
			dir = ""
		}
	}

	return path, "", ""
}
