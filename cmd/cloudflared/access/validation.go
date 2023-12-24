package access

import (
	"errors"
	"fmt"
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
