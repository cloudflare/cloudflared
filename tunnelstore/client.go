package tunnelstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudflare/cloudflared/logger"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

const (
	defaultTimeout  = 15 * time.Second
	jsonContentType = "application/json"
)

var (
	ErrTunnelNameConflict = errors.New("tunnel with name already exists")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrBadRequest         = errors.New("incorrect request parameters")
	ErrNotFound           = errors.New("not found")
)

type Tunnel struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	CreatedAt   time.Time    `json:"created_at"`
	Connections []Connection `json:"connections"`
}

type Connection struct {
	ColoName string    `json:"colo_name"`
	ID       uuid.UUID `json:"uuid"`
}

type Client interface {
	CreateTunnel(name string) (*Tunnel, error)
	GetTunnel(id string) (*Tunnel, error)
	DeleteTunnel(id string) error
	ListTunnels() ([]Tunnel, error)
}

type RESTClient struct {
	baseURL   string
	authToken string
	client    http.Client
	logger    logger.Service
}

var _ Client = (*RESTClient)(nil)

func NewRESTClient(baseURL string, accountTag string, authToken string, logger logger.Service) *RESTClient {
	if strings.HasSuffix(baseURL, "/") {
		baseURL = baseURL[:len(baseURL)-1]
	}
	url := fmt.Sprintf("%s/accounts/%s/tunnels", baseURL, accountTag)
	return &RESTClient{
		baseURL:   url,
		authToken: authToken,
		client: http.Client{
			Transport: &http.Transport{
				TLSHandshakeTimeout:   defaultTimeout,
				ResponseHeaderTimeout: defaultTimeout,
			},
			Timeout: defaultTimeout,
		},
		logger: logger,
	}
}

type newTunnel struct {
	Name string `json:"name"`
}

func (r *RESTClient) CreateTunnel(name string) (*Tunnel, error) {
	if name == "" {
		return nil, errors.New("tunnel name required")
	}
	body, err := json.Marshal(&newTunnel{
		Name: name,
	})
	if err != nil {
		return nil, errors.Wrap(err, "Failed to serialize new tunnel request")
	}

	resp, err := r.sendRequest("POST", "", bytes.NewBuffer(body))
	if err != nil {
		return nil, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return unmarshalTunnel(resp.Body)
	case http.StatusConflict:
		return nil, ErrTunnelNameConflict
	}

	return nil, statusCodeToError("create", resp)
}

func (r *RESTClient) GetTunnel(id string) (*Tunnel, error) {
	resp, err := r.sendRequest("GET", id, nil)
	if err != nil {
		return nil, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return unmarshalTunnel(resp.Body)
	}

	return nil, statusCodeToError("read", resp)
}

func (r *RESTClient) DeleteTunnel(id string) error {
	resp, err := r.sendRequest("DELETE", id, nil)
	if err != nil {
		return errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	return statusCodeToError("delete", resp)
}

func (r *RESTClient) ListTunnels() ([]Tunnel, error) {
	resp, err := r.sendRequest("GET", "", nil)
	if err != nil {
		return nil, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var tunnels []Tunnel
		if err := json.NewDecoder(resp.Body).Decode(&tunnels); err != nil {
			return nil, errors.Wrap(err, "failed to decode response")
		}
		return tunnels, nil
	}

	return nil, statusCodeToError("list", resp)
}

func (r *RESTClient) resolve(target string) string {
	if target != "" {
		return r.baseURL + "/" + target
	}
	return r.baseURL
}

func (r *RESTClient) sendRequest(method string, target string, body io.Reader) (*http.Response, error) {
	url := r.resolve(target)
	r.logger.Debugf("%s %s", method, url)
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, errors.Wrapf(err, "can't create %s request", method)
	}
	if body != nil {
		req.Header.Set("Content-Type", jsonContentType)
	}
	req.Header.Add("X-Auth-User-Service-Key", r.authToken)
	return r.client.Do(req)
}

func unmarshalTunnel(reader io.Reader) (*Tunnel, error) {
	var tunnel Tunnel
	if err := json.NewDecoder(reader).Decode(&tunnel); err != nil {
		return nil, errors.Wrap(err, "failed to decode response")
	}
	return &tunnel, nil
}

func statusCodeToError(op string, resp *http.Response) error {
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
	return errors.Errorf("API call to %s tunnel failed with status %d: %s", op,
		resp.StatusCode, http.StatusText(resp.StatusCode))
}
