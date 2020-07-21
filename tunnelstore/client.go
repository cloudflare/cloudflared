package tunnelstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/logger"
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
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	CreatedAt   time.Time    `json:"created_at"`
	DeletedAt   time.Time    `json:"deleted_at"`
	Connections []Connection `json:"connections"`
}

type Connection struct {
	ColoName string    `json:"colo_name"`
	ID       uuid.UUID `json:"uuid"`
}

// Route represents a record type that can route to a tunnel
type Route interface {
	json.Marshaler
	RecordType() string
}

type DNSRoute struct {
	userHostname string
}

func NewDNSRoute(userHostname string) Route {
	return &DNSRoute{
		userHostname: userHostname,
	}
}

func (dr *DNSRoute) MarshalJSON() ([]byte, error) {
	s := struct {
		Type         string `json:"type"`
		UserHostname string `json:"user_hostname"`
	}{
		Type:         dr.RecordType(),
		UserHostname: dr.userHostname,
	}
	return json.Marshal(&s)
}

func (dr *DNSRoute) RecordType() string {
	return "dns"
}

type LBRoute struct {
	lbName string
	lbPool string
}

func NewLBRoute(lbName, lbPool string) Route {
	return &LBRoute{
		lbName: lbName,
		lbPool: lbPool,
	}
}

func (lr *LBRoute) MarshalJSON() ([]byte, error) {
	s := struct {
		Type   string `json:"type"`
		LBName string `json:"lb_name"`
		LBPool string `json:"lb_pool"`
	}{
		Type:   lr.RecordType(),
		LBName: lr.lbName,
		LBPool: lr.lbPool,
	}
	return json.Marshal(&s)
}

func (lr *LBRoute) RecordType() string {
	return "lb"
}

type Client interface {
	CreateTunnel(name string, tunnelSecret []byte) (*Tunnel, error)
	GetTunnel(tunnelID uuid.UUID) (*Tunnel, error)
	DeleteTunnel(tunnelID uuid.UUID) error
	ListTunnels() ([]Tunnel, error)
	CleanupConnections(tunnelID uuid.UUID) error
	RouteTunnel(tunnelID uuid.UUID, route Route) error
}

type RESTClient struct {
	baseEndpoints *baseEndpoints
	authToken     string
	client        http.Client
	logger        logger.Service
}

type baseEndpoints struct {
	accountLevel string
	zoneLevel    string
}

var _ Client = (*RESTClient)(nil)

func NewRESTClient(baseURL string, accountTag, zoneTag string, authToken string, logger logger.Service) *RESTClient {
	if strings.HasSuffix(baseURL, "/") {
		baseURL = baseURL[:len(baseURL)-1]
	}
	return &RESTClient{
		baseEndpoints: &baseEndpoints{
			accountLevel: fmt.Sprintf("%s/accounts/%s/tunnels", baseURL, accountTag),
			zoneLevel:    fmt.Sprintf("%s/zones/%s/tunnels", baseURL, zoneTag),
		},
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
	Name         string `json:"name"`
	TunnelSecret []byte `json:"tunnel_secret"`
}

func (r *RESTClient) CreateTunnel(name string, tunnelSecret []byte) (*Tunnel, error) {
	if name == "" {
		return nil, errors.New("tunnel name required")
	}
	body := &newTunnel{
		Name:         name,
		TunnelSecret: tunnelSecret,
	}

	resp, err := r.sendRequest("POST", r.baseEndpoints.accountLevel, body)
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

	return nil, r.statusCodeToError("create tunnel", resp)
}

func (r *RESTClient) GetTunnel(tunnelID uuid.UUID) (*Tunnel, error) {
	endpoint := fmt.Sprintf("%s/%v", r.baseEndpoints.accountLevel, tunnelID)
	resp, err := r.sendRequest("GET", endpoint, nil)
	if err != nil {
		return nil, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return unmarshalTunnel(resp.Body)
	}

	return nil, r.statusCodeToError("get tunnel", resp)
}

func (r *RESTClient) DeleteTunnel(tunnelID uuid.UUID) error {
	endpoint := fmt.Sprintf("%s/%v", r.baseEndpoints.accountLevel, tunnelID)
	resp, err := r.sendRequest("DELETE", endpoint, nil)
	if err != nil {
		return errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	return r.statusCodeToError("delete tunnel", resp)
}

func (r *RESTClient) ListTunnels() ([]Tunnel, error) {
	resp, err := r.sendRequest("GET", r.baseEndpoints.accountLevel, nil)
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

	return nil, r.statusCodeToError("list tunnels", resp)
}

func (r *RESTClient) CleanupConnections(tunnelID uuid.UUID) error {
	endpoint := fmt.Sprintf("%s/%v/connections", r.baseEndpoints.accountLevel, tunnelID)
	resp, err := r.sendRequest("DELETE", endpoint, nil)
	if err != nil {
		return errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	return r.statusCodeToError("cleanup connections", resp)
}

func (r *RESTClient) RouteTunnel(tunnelID uuid.UUID, route Route) error {
	endpoint := fmt.Sprintf("%s/%v/routes", r.baseEndpoints.zoneLevel, tunnelID)
	resp, err := r.sendRequest("PUT", endpoint, route)
	if err != nil {
		return errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	return r.statusCodeToError("add route", resp)
}

func (r *RESTClient) sendRequest(method string, url string, body interface{}) (*http.Response, error) {
	r.logger.Debugf("%s %s", method, url)

	var bodyReader io.Reader
	if body != nil {
		if bodyBytes, err := json.Marshal(body); err != nil {
			return nil, errors.Wrap(err, "failed to serialize json body")
		} else {
			bodyReader = bytes.NewBuffer(bodyBytes)
		}
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, errors.Wrapf(err, "can't create %s request", method)
	}
	if bodyReader != nil {
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

func (r *RESTClient) statusCodeToError(op string, resp *http.Response) error {
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
