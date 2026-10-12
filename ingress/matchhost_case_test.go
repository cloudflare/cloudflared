package ingress

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Hostnames are case-insensitive (RFC 4343, RFC 3986 §3.2.2). cloudflared
// accepts mixed-case hostnames in ingress config and the request Host header
// can be mixed-case, so rule matching must ignore case.
func TestMatchHostCaseInsensitive(t *testing.T) {
	tests := []struct {
		rule, req string
		want      bool
	}{
		{"myapp.example.com", "MyApp.example.com", true}, // mixed-case request Host
		{"MyApp.example.com", "myapp.example.com", true}, // uppercase rule hostname
		{"*.example.com", "foo.EXAMPLE.com", true},       // wildcard, mixed-case request
		{"*.Example.com", "foo.example.com", true},       // wildcard, uppercase rule
		{"a.example.com", "b.example.com", false},        // still a non-match
		{"*.example.com", "example.com", false},          // wildcard does not match apex
	}
	for _, test := range tests {
		assert.Equalf(t, test.want, matchHost(test.rule, test.req),
			"matchHost(%q, %q)", test.rule, test.req)
	}
}

// End-to-end: a mixed-case request Host resolves to the correct rule rather
// than falling through to the catch-all.
func TestFindMatchingRuleCaseInsensitive(t *testing.T) {
	ingress := Ingress{
		Rules: []Rule{
			{Hostname: "tunnel-a.example.com"},
			{Hostname: "*.wild.example.com"},
			{Hostname: "*"}, // catch-all
		},
	}
	tests := []struct {
		host          string
		wantRuleIndex int
	}{
		{"Tunnel-A.example.com", 0},
		{"TUNNEL-A.EXAMPLE.COM", 0},
		{"Foo.Wild.Example.com", 1},
		{"other.example.com", 2},
	}
	for _, test := range tests {
		_, ruleIndex := ingress.FindMatchingRule(test.host, "/")
		assert.Equalf(t, test.wantRuleIndex, ruleIndex, "host=%s", test.host)
	}
}
