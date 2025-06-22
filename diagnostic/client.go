package diagnostic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	cfdflags "github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
)

type httpClient struct {
	http.Client
	baseURL *url.URL
}

func NewHTTPClient() *httpClient {
	httpTransport := http.Transport{
		TLSHandshakeTimeout:   defaultTimeout,
		ResponseHeaderTimeout: defaultTimeout,
	}

	return &httpClient{
		http.Client{
			Transport: &httpTransport,
			Timeout:   defaultTimeout,
		},
		nil,
	}
}

func (client *httpClient) SetBaseURL(baseURL *url.URL) {
	client.baseURL = baseURL
}

func (client *httpClient) GET(ctx context.Context, endpoint string) (*http.Response, error) {
	if client.baseURL == nil {
		return nil, ErrNoBaseURL
	}
	url := client.baseURL.JoinPath(endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
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
	response, err := client.GET(ctx, cliConfigurationEndpoint)
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

	logFile, exists := data[cfdflags.LogFile]
	if exists {
		return &LogConfiguration{logFile, "", uid}, nil
	}

	logDirectory, exists := data[cfdflags.LogDirectory]
	if exists {
		return &LogConfiguration{"", logDirectory, uid}, nil
	}

	// No log configured may happen when cloudflared is executed as a managed service or
	// when containerized
	return &LogConfiguration{"", "", uid}, nil
}

func (client *httpClient) GetMemoryDump(ctx context.Context, writer io.Writer) error {
	response, err := client.GET(ctx, memoryDumpEndpoint)
	if err != nil {
		return err
	}

	return copyToWriter(response, writer)
}

func (client *httpClient) GetGoroutineDump(ctx context.Context, writer io.Writer) error {
	response, err := client.GET(ctx, goroutineDumpEndpoint)
	if err != nil {
		return err
	}

	return copyToWriter(response, writer)
}

func (client *httpClient) GetTunnelState(ctx context.Context) (*TunnelState, error) {
	response, err := client.GET(ctx, tunnelStateEndpoint)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	var state TunnelState
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		return nil, fmt.Errorf("failed to decode body: %w", err)
	}

	return &state, nil
}

func (client *httpClient) GetSystemInformation(ctx context.Context, writer io.Writer) error {
	response, err := client.GET(ctx, systemInformationEndpoint)
	if err != nil {
		return err
	}

	return copyJSONToWriter(response, writer)
}

func (client *httpClient) GetMetrics(ctx context.Context, writer io.Writer) error {
	response, err := client.GET(ctx, metricsEndpoint)
	if err != nil {
		return err
	}

	return copyToWriter(response, writer)
}

func (client *httpClient) GetTunnelConfiguration(ctx context.Context, writer io.Writer) error {
	response, err := client.GET(ctx, tunnelConfigurationEndpoint)
	if err != nil {
		return err
	}

	return copyJSONToWriter(response, writer)
}

func (client *httpClient) GetCliConfiguration(ctx context.Context, writer io.Writer) error {
	response, err := client.GET(ctx, cliConfigurationEndpoint)
	if err != nil {
		return err
	}

	return copyJSONToWriter(response, writer)
}

func copyToWriter(response *http.Response, writer io.Writer) error {
	defer response.Body.Close()

	_, err := io.Copy(writer, response.Body)
	if err != nil {
		return fmt.Errorf("error writing response: %w", err)
	}

	return nil
}

func copyJSONToWriter(response *http.Response, writer io.Writer) error {
	defer response.Body.Close()

	var data interface{}

	decoder := json.NewDecoder(response.Body)

	err := decoder.Decode(&data)
	if err != nil {
		return fmt.Errorf("diagnostic client error whilst reading response: %w", err)
	}

	encoder := newFormattedEncoder(writer)

	err = encoder.Encode(data)
	if err != nil {
		return fmt.Errorf("diagnostic client error whilst writing json: %w", err)
	}

	return nil
}

type HTTPClient interface {
	GetLogConfiguration(ctx context.Context) (*LogConfiguration, error)
	GetMemoryDump(ctx context.Context, writer io.Writer) error
	GetGoroutineDump(ctx context.Context, writer io.Writer) error
	GetTunnelState(ctx context.Context) (*TunnelState, error)
	GetSystemInformation(ctx context.Context, writer io.Writer) error
	GetMetrics(ctx context.Context, writer io.Writer) error
	GetCliConfiguration(ctx context.Context, writer io.Writer) error
	GetTunnelConfiguration(ctx context.Context, writer io.Writer) error
}
