package ingress

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/cloudflare/cloudflared/ingress/middleware"
)

// Rule routes traffic from a hostname/path on the public internet to the
// service running on the given URL.
type Rule struct {
	// Requests for this hostname will be proxied to this rule's service.
	Hostname string `json:"hostname"`

	// punycodeHostname is an additional optional hostname converted to punycode.
	punycodeHostname string

	// Path is an optional regex that can specify path-driven ingress rules.
	Path *Regexp `json:"path"`

	// A (probably local) address. Requests for a hostname which matches this
	// rule's hostname pattern will be proxied to the service running on this
	// address.
	Service OriginService `json:"service"`

	// Handlers is a list of functions that acts as a middleware during ProxyHTTP
	Handlers []middleware.Handler

	// Configure the request cloudflared sends to this specific origin.
	Config OriginRequestConfig `json:"originRequest"`
}

// MultiLineString is for outputting rules in a human-friendly way when Cloudflared
// is used as a CLI tool (not as a daemon).
func (r Rule) MultiLineString() string {
	var out strings.Builder
	if r.Hostname != "" {
		out.WriteString("\thostname: ")
		out.WriteString(r.Hostname)
		out.WriteRune('\n')
	}
	if r.Path != nil && r.Path.Regexp != nil {
		out.WriteString("\tpath: ")
		out.WriteString(r.Path.Regexp.String())
		out.WriteRune('\n')
	}
	out.WriteString("\tservice: ")
	out.WriteString(r.Service.String())
	return out.String()
}

// Matches checks if the rule matches a given hostname/path combination.
func (r *Rule) Matches(hostname, path string) bool {
	hostMatch := false
	if r.Hostname == "" || r.Hostname == "*" {
		hostMatch = true
	} else {
		hostMatch = matchHost(r.Hostname, hostname)
	}
	punycodeHostMatch := false
	if r.punycodeHostname != "" {
		punycodeHostMatch = matchHost(r.punycodeHostname, hostname)
	}
	pathMatch := r.Path == nil || r.Path.Regexp == nil || r.Path.Regexp.MatchString(path)
	return (hostMatch || punycodeHostMatch) && pathMatch
}

// Regexp adds unmarshalling from json for regexp.Regexp
type Regexp struct {
	*regexp.Regexp
}

func (r *Regexp) MarshalJSON() ([]byte, error) {
	if r.Regexp == nil {
		return json.Marshal(nil)
	}
	return json.Marshal(r.Regexp.String())
}
