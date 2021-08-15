package ingress

import (
	"regexp"
	"strings"
)

// Rule routes traffic from a hostname/path on the public internet to the
// service running on the given URL.
type Rule struct {
	// Requests for this hostname will be proxied to this rule's service.
	Hostname string

	// Path is an optional regex that can specify path-driven ingress rules.
	Path *regexp.Regexp

	// A (probably local) address. Requests for a hostname which matches this
	// rule's hostname pattern will be proxied to the service running on this
	// address.
	Service OriginService

	// Configure the request cloudflared sends to this specific origin.
	Config OriginRequestConfig
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
	if r.Path != nil {
		out.WriteString("\tpath: ")
		out.WriteString(r.Path.String())
		out.WriteRune('\n')
	}
	out.WriteString("\tservice: ")
	out.WriteString(r.Service.String())
	return out.String()
}

// Matches checks if the rule matches a given hostname/path combination.
func (r *Rule) Matches(hostname, path string) bool {
	hostMatch := r.Hostname == "" || r.Hostname == "*" || matchHost(r.Hostname, hostname)
	pathMatch := r.Path == nil || r.Path.MatchString(path)
	return hostMatch && pathMatch
}
