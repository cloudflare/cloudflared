package diagnostic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cloudflare/cloudflared/logger"
)

const configurationEndpoint = "diag/configuration"

type httpClient struct {
	http.Client
	baseURL url.URL
}

func NewHTTPClient(baseURL url.URL) *httpClient {
	httpTransport := http.Transport{
		TLSHandshakeTimeout:   defaultTimeout,
		ResponseHeaderTimeout: defaultTimeout,
	}

	return &httpClient{
		http.Client{
			Transport: &httpTransport,
			Timeout:   defaultTimeout,
		},
		baseURL,
	}
}

func (client *httpClient) GET(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating GET request: %w", err)
	}

	req.Header.Add("Accept", "application/json;version=1")

	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error GET request: %w", err)
	}

	return response, nil
}

type LogConfiguration struct {
	logFile      string
	logDirectory string
	uid          int // the uid of the user that started cloudflared
}

func (client *httpClient) GetLogConfiguration(ctx context.Context) (*LogConfiguration, error) {
	endpoint, err := url.JoinPath(client.baseURL.String(), configurationEndpoint)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL: %w", err)
	}

	response, err := client.GET(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	var data map[string]string
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode body: %w", err)
	}

	uidStr, exists := data[configurationKeyUID]
	if !exists {
		return nil, ErrKeyNotFound
	}

	uid, err := strconv.Atoi(uidStr)
	if err != nil {
		return nil, fmt.Errorf("error convertin pid to int: %w", err)
	}

	logFile, exists := data[logger.LogFileFlag]
	if exists {
		return &LogConfiguration{logFile, "", uid}, nil
	}

	logDirectory, exists := data[logger.LogDirectoryFlag]
	if exists {
		return &LogConfiguration{"", logDirectory, uid}, nil
	}

	return nil, ErrKeyNotFound
}

type HTTPClient interface {
	GetLogConfiguration(ctx context.Context) (LogConfiguration, error)
}
