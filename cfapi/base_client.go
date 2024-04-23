package cfapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/net/http2"
)

const (
	defaultTimeout  = 15 * time.Second
	jsonContentType = "application/json"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrBadRequest   = errors.New("incorrect request parameters")
	ErrNotFound     = errors.New("not found")
	ErrAPINoSuccess = errors.New("API call failed")
)

type RESTClient struct {
	baseEndpoints *baseEndpoints
	authToken     string
	userAgent     string
	client        http.Client
	log           *zerolog.Logger
}

type baseEndpoints struct {
	accountLevel  url.URL
	zoneLevel     url.URL
	accountRoutes url.URL
	accountVnets  url.URL
}

var _ Client = (*RESTClient)(nil)

func NewRESTClient(baseURL, accountTag, zoneTag, authToken, userAgent string, log *zerolog.Logger) (*RESTClient, error) {
	if strings.HasSuffix(baseURL, "/") {
		baseURL = baseURL[:len(baseURL)-1]
	}
	accountLevelEndpoint, err := url.Parse(fmt.Sprintf("%s/accounts/%s/cfd_tunnel", baseURL, accountTag))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create account level endpoint")
	}
	accountRoutesEndpoint, err := url.Parse(fmt.Sprintf("%s/accounts/%s/teamnet/routes", baseURL, accountTag))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create route account-level endpoint")
	}
	accountVnetsEndpoint, err := url.Parse(fmt.Sprintf("%s/accounts/%s/teamnet/virtual_networks", baseURL, accountTag))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create virtual network account-level endpoint")
	}
	zoneLevelEndpoint, err := url.Parse(fmt.Sprintf("%s/zones/%s/tunnels", baseURL, zoneTag))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create account level endpoint")
	}
	httpTransport := http.Transport{
		TLSHandshakeTimeout:   defaultTimeout,
		ResponseHeaderTimeout: defaultTimeout,
	}
	http2.ConfigureTransport(&httpTransport)
	return &RESTClient{
		baseEndpoints: &baseEndpoints{
			accountLevel:  *accountLevelEndpoint,
			zoneLevel:     *zoneLevelEndpoint,
			accountRoutes: *accountRoutesEndpoint,
			accountVnets:  *accountVnetsEndpoint,
		},
		authToken: authToken,
		userAgent: userAgent,
		client: http.Client{
			Transport: &httpTransport,
			Timeout:   defaultTimeout,
		},
		log: log,
	}, nil
}

func (r *RESTClient) sendRequest(method string, url url.URL, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		if bodyBytes, err := json.Marshal(body); err != nil {
			return nil, errors.Wrap(err, "failed to serialize json body")
		} else {
			bodyReader = bytes.NewBuffer(bodyBytes)
		}
	}

	req, err := http.NewRequest(method, url.String(), bodyReader)
	if err != nil {
		return nil, errors.Wrapf(err, "can't create %s request", method)
	}
	req.Header.Set("User-Agent", r.userAgent)
	if bodyReader != nil {
		req.Header.Set("Content-Type", jsonContentType)
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.authToken))
	req.Header.Add("Accept", "application/json;version=1")
	return r.client.Do(req)
}

func parseResponseEnvelope(reader io.Reader) (*response, error) {
	// Schema for Tunnelstore responses in the v1 API.
	// Roughly, it's a wrapper around a particular result that adds failures/errors/etc
	var result response
	// First, parse the wrapper and check the API call succeeded
	if err := json.NewDecoder(reader).Decode(&result); err != nil {
		return nil, errors.Wrap(err, "failed to decode response")
	}
	if err := result.checkErrors(); err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, ErrAPINoSuccess
	}

	return &result, nil
}

func parseResponse(reader io.Reader, data interface{}) error {
	result, err := parseResponseEnvelope(reader)
	if err != nil {
		return err
	}

	return parseResponseBody(result, data)
}

func parseResponseBody(result *response, data interface{}) error {
	// At this point we know the API call succeeded, so, parse out the inner
	// result into the datatype provided as a parameter.
	if err := json.Unmarshal(result.Result, &data); err != nil {
		return errors.Wrap(err, "the Cloudflare API response was an unexpected type")
	}
	return nil
}

func fetchExhaustively[T any](requestFn func(int) (*http.Response, error)) ([]*T, error) {
	page := 0
	var fullResponse []*T

	for {
		page += 1
		envelope, parsedBody, err := fetchPage[T](requestFn, page)

		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("Error Parsing page %d", page))
		}

		fullResponse = append(fullResponse, parsedBody...)
		if envelope.Pagination.Count < envelope.Pagination.PerPage || len(fullResponse) >= envelope.Pagination.TotalCount {
			break
		}

	}
	return fullResponse, nil
}

func fetchPage[T any](requestFn func(int) (*http.Response, error), page int) (*response, []*T, error) {
	pageResp, err := requestFn(page)
	if err != nil {
		return nil, nil, errors.Wrap(err, "REST request failed")
	}
	defer pageResp.Body.Close()
	if pageResp.StatusCode == http.StatusOK {
		envelope, err := parseResponseEnvelope(pageResp.Body)
		if err != nil {
			return nil, nil, err
		}
		var parsedRspBody []*T
		return envelope, parsedRspBody, parseResponseBody(envelope, &parsedRspBody)

	}
	return nil, nil, errors.New(fmt.Sprintf("Failed to fetch page. Server returned: %d", pageResp.StatusCode))
}

type response struct {
	Success    bool            `json:"success,omitempty"`
	Errors     []apiErr        `json:"errors,omitempty"`
	Messages   []string        `json:"messages,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Pagination Pagination      `json:"result_info,omitempty"`
}

type Pagination struct {
	Count      int `json:"count,omitempty"`
	Page       int `json:"page,omitempty"`
	PerPage    int `json:"per_page,omitempty"`
	TotalCount int `json:"total_count,omitempty"`
}

func (r *response) checkErrors() error {
	if len(r.Errors) == 0 {
		return nil
	}
	if len(r.Errors) == 1 {
		return r.Errors[0]
	}
	var messages string
	for _, e := range r.Errors {
		messages += fmt.Sprintf("%s; ", e)
	}
	return fmt.Errorf("API errors: %s", messages)
}

type apiErr struct {
	Code    json.Number `json:"code,omitempty"`
	Message string      `json:"message,omitempty"`
}

func (e apiErr) Error() string {
	return fmt.Sprintf("code: %v, reason: %s", e.Code, e.Message)
}

func (r *RESTClient) statusCodeToError(op string, resp *http.Response) error {
	if resp.Header.Get("Content-Type") == "application/json" {
		var errorsResp response
		if json.NewDecoder(resp.Body).Decode(&errorsResp) == nil {
			if err := errorsResp.checkErrors(); err != nil {
				return errors.Errorf("Failed to %s: %s", op, err)
			}
		}
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusBadRequest:
		return ErrBadRequest
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	case http.StatusNotFound:
		return ErrNotFound
	}
	return errors.Errorf("API call to %s failed with status %d: %s", op,
		resp.StatusCode, http.StatusText(resp.StatusCode))
}
