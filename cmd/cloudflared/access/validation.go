package access

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/http/httpguts"
)

// parseRequestHeaders will take user-provided header values as strings "Content-Type: application/json" and create
// a http.Header object.
func parseRequestHeaders(values []string) http.Header {
	headers := make(http.Header)
	for _, valuePair := range values {
		header, value, found := strings.Cut(valuePair, ":")
		if found {
			headers.Add(strings.TrimSpace(header), strings.TrimSpace(value))
		}
	}
	return headers
}

// bracketBareIPv6 wraps bare IPv6 addresses in a URL with square brackets.
// Go 1.26 tightened net/url parsing to strictly require RFC 3986 bracket syntax
// for IPv6 addresses in URLs. Before Go 1.26, bare forms like "http://::1" were
// accepted; now they are rejected. This function detects bare IPv6 in the host
// portion and brackets it so that url.ParseRequestURI can parse it correctly.
func bracketBareIPv6(input string) string {
	prefix := input[:strings.Index(input, "://")+3]
	rest := input[len(prefix):]
	host := rest
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		host = rest[:i]
	}
	if net.ParseIP(host) != nil && strings.Contains(host, ":") {
		return prefix + "[" + host + "]" + rest[len(host):]
	}
	return input
}

// parseHostname will attempt to convert a user provided URL string into a string with some light error checking on
// certain expectations from the URL.
// Will convert all HTTP URLs to HTTPS
func parseURL(input string) (*url.URL, error) {
	if input == "" {
		return nil, errors.New("no input provided")
	}
	if !strings.HasPrefix(input, "https://") && !strings.HasPrefix(input, "http://") {
		input = fmt.Sprintf("https://%s", input)
	}
	input = bracketBareIPv6(input)
	url, err := url.ParseRequestURI(input)
	if err != nil {
		return nil, fmt.Errorf("failed to parse as URL: %w", err)
	}
	if url.Scheme != "https" {
		url.Scheme = "https"
	}
	if url.Host == "" {
		return nil, errors.New("failed to parse Host")
	}
	host, err := httpguts.PunycodeHostPort(url.Host)
	if err != nil || host == "" {
		return nil, err
	}
	if !httpguts.ValidHostHeader(host) {
		return nil, errors.New("invalid Host provided")
	}
	url.Host = host
	return url, nil
}
