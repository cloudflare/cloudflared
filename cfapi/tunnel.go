package cfapi

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

var ErrTunnelNameConflict = errors.New("tunnel with name already exists")

type Tunnel struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	CreatedAt   time.Time    `json:"created_at"`
	DeletedAt   time.Time    `json:"deleted_at"`
	Connections []Connection `json:"connections"`
}

type TunnelWithToken struct {
	Tunnel
	Token string `json:"token"`
}

type Connection struct {
	ColoName           string    `json:"colo_name"`
	ID                 uuid.UUID `json:"id"`
	IsPendingReconnect bool      `json:"is_pending_reconnect"`
	OriginIP           net.IP    `json:"origin_ip"`
	OpenedAt           time.Time `json:"opened_at"`
}

type ActiveClient struct {
	ID          uuid.UUID    `json:"id"`
	Features    []string     `json:"features"`
	Version     string       `json:"version"`
	Arch        string       `json:"arch"`
	RunAt       time.Time    `json:"run_at"`
	Connections []Connection `json:"conns"`
}

type newTunnel struct {
	Name         string `json:"name"`
	TunnelSecret []byte `json:"tunnel_secret"`
}

type managementRequest struct {
	Resources []string `json:"resources"`
}

type CleanupParams struct {
	queryParams url.Values
}

func NewCleanupParams() *CleanupParams {
	return &CleanupParams{
		queryParams: url.Values{},
	}
}

func (cp *CleanupParams) ForClient(clientID uuid.UUID) {
	cp.queryParams.Set("client_id", clientID.String())
}

func (cp CleanupParams) encode() string {
	return cp.queryParams.Encode()
}

func (r *RESTClient) CreateTunnel(name string, tunnelSecret []byte) (*TunnelWithToken, error) {
	if name == "" {
		return nil, errors.New("tunnel name required")
	}
	if _, err := uuid.Parse(name); err == nil {
		return nil, errors.New("you cannot use UUIDs as tunnel names")
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
		var tunnel TunnelWithToken
		if serdeErr := parseResponse(resp.Body, &tunnel); serdeErr != nil {
			return nil, serdeErr
		}
		return &tunnel, nil
	case http.StatusConflict:
		return nil, ErrTunnelNameConflict
	}

	return nil, r.statusCodeToError("create tunnel", resp)
}

func (r *RESTClient) GetTunnel(tunnelID uuid.UUID) (*Tunnel, error) {
	endpoint := r.baseEndpoints.accountLevel
	endpoint.Path = path.Join(endpoint.Path, fmt.Sprintf("%v", tunnelID))
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

func (r *RESTClient) GetTunnelToken(tunnelID uuid.UUID) (token string, err error) {
	endpoint := r.baseEndpoints.accountLevel
	endpoint.Path = path.Join(endpoint.Path, fmt.Sprintf("%v/token", tunnelID))
	resp, err := r.sendRequest("GET", endpoint, nil)
	if err != nil {
		return "", errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		err = parseResponse(resp.Body, &token)
		return token, err
	}

	return "", r.statusCodeToError("get tunnel token", resp)
}

func (r *RESTClient) GetManagementToken(tunnelID uuid.UUID) (token string, err error) {
	endpoint := r.baseEndpoints.accountLevel
	endpoint.Path = path.Join(endpoint.Path, fmt.Sprintf("%v/management", tunnelID))

	body := &managementRequest{
		Resources: []string{"logs"},
	}

	resp, err := r.sendRequest("POST", endpoint, body)
	if err != nil {
		return "", errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		err = parseResponse(resp.Body, &token)
		return token, err
	}

	return "", r.statusCodeToError("get tunnel token", resp)
}

func (r *RESTClient) DeleteTunnel(tunnelID uuid.UUID, cascade bool) error {
	endpoint := r.baseEndpoints.accountLevel
	endpoint.Path = path.Join(endpoint.Path, fmt.Sprintf("%v", tunnelID))
	// Cascade will delete all tunnel dependencies (connections, routes, etc.) that
	// are linked to the deleted tunnel.
	if cascade {
		endpoint.RawQuery = "cascade=true"
	}
	resp, err := r.sendRequest("DELETE", endpoint, nil)
	if err != nil {
		return errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	return r.statusCodeToError("delete tunnel", resp)
}

func (r *RESTClient) ListTunnels(filter *TunnelFilter) ([]*Tunnel, error) {
	fetchFn := func(page int) (*http.Response, error) {
		endpoint := r.baseEndpoints.accountLevel
		filter.Page(page)
		endpoint.RawQuery = filter.encode()
		rsp, err := r.sendRequest("GET", endpoint, nil)
		if err != nil {
			return nil, errors.Wrap(err, "REST request failed")
		}
		if rsp.StatusCode != http.StatusOK {
			rsp.Body.Close()
			return nil, r.statusCodeToError("list tunnels", rsp)
		}
		return rsp, nil
	}

	return fetchExhaustively[Tunnel](fetchFn)
}

func (r *RESTClient) ListActiveClients(tunnelID uuid.UUID) ([]*ActiveClient, error) {
	endpoint := r.baseEndpoints.accountLevel
	endpoint.Path = path.Join(endpoint.Path, fmt.Sprintf("%v/connections", tunnelID))
	resp, err := r.sendRequest("GET", endpoint, nil)
	if err != nil {
		return nil, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return parseConnectionsDetails(resp.Body)
	}

	return nil, r.statusCodeToError("list connection details", resp)
}

func parseConnectionsDetails(reader io.Reader) ([]*ActiveClient, error) {
	var clients []*ActiveClient
	err := parseResponse(reader, &clients)
	return clients, err
}

func (r *RESTClient) CleanupConnections(tunnelID uuid.UUID, params *CleanupParams) error {
	endpoint := r.baseEndpoints.accountLevel
	endpoint.RawQuery = params.encode()
	endpoint.Path = path.Join(endpoint.Path, fmt.Sprintf("%v/connections", tunnelID))
	resp, err := r.sendRequest("DELETE", endpoint, nil)
	if err != nil {
		return errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	return r.statusCodeToError("cleanup connections", resp)
}

func unmarshalTunnel(reader io.Reader) (*Tunnel, error) {
	var tunnel Tunnel
	err := parseResponse(reader, &tunnel)
	return &tunnel, err
}
