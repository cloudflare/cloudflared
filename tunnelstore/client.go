package tunnelstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
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
	ErrAPINoSuccess       = errors.New("API call failed")
)

type Tunnel struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	CreatedAt   time.Time    `json:"created_at"`
	DeletedAt   time.Time    `json:"deleted_at"`
	Connections []Connection `json:"connections"`
}

type Connection struct {
	ColoName           string    `json:"colo_name"`
	ID                 uuid.UUID `json:"id"`
	IsPendingReconnect bool      `json:"is_pending_reconnect"`
}

type Change = string

const (
	ChangeNew       = "new"
	ChangeUpdated   = "updated"
	ChangeUnchanged = "unchanged"
)

// Route represents a record type that can route to a tunnel
type Route interface {
	json.Marshaler
	RecordType() string
	UnmarshalResult(body io.Reader) (RouteResult, error)
}

type RouteResult interface {
	// SuccessSummary explains what will route to this tunnel when it's provisioned successfully
	SuccessSummary() string
}

type DNSRoute struct {
	userHostname string
}

type DNSRouteResult struct {
	route *DNSRoute
	CName Change `json:"cname"`
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

func (dr *DNSRoute) UnmarshalResult(body io.Reader) (RouteResult, error) {
	var result DNSRouteResult
	err := parseResponse(body, &result)
	result.route = dr
	return &result, err
}

func (dr *DNSRoute) RecordType() string {
	return "dns"
}

func (res *DNSRouteResult) SuccessSummary() string {
	var msgFmt string
	switch res.CName {
	case ChangeNew:
		msgFmt = "Added CNAME %s which will route to this tunnel"
	case ChangeUpdated: // this is not currently returned by tunnelsore
		msgFmt = "%s updated to route to your tunnel"
	case ChangeUnchanged:
		msgFmt = "%s is already configured to route to your tunnel"
	}
	return fmt.Sprintf(msgFmt, res.route.userHostname)
}

type LBRoute struct {
	lbName string
	lbPool string
}

type LBRouteResult struct {
	route        *LBRoute
	LoadBalancer Change `json:"load_balancer"`
	Pool         Change `json:"pool"`
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

func (lr *LBRoute) UnmarshalResult(body io.Reader) (RouteResult, error) {
	var result LBRouteResult
	err := parseResponse(body, &result)
	result.route = lr
	return &result, err
}

func (res *LBRouteResult) SuccessSummary() string {
	var msg string
	switch res.LoadBalancer + "," + res.Pool {
	case "new,new":
		msg = "Created load balancer %s and added a new pool %s with this tunnel as an origin"
	case "new,updated":
		msg = "Created load balancer %s with an existing pool %s which was updated to use this tunnel as an origin"
	case "new,unchanged":
		msg = "Created load balancer %s with an existing pool %s which already has this tunnel as an origin"
	case "updated,new":
		msg = "Added new pool %[2]s with this tunnel as an origin to load balancer %[1]s"
	case "updated,updated":
		msg = "Updated pool %[2]s to use this tunnel as an origin and added it to load balancer %[1]s"
	case "updated,unchanged":
		msg = "Added pool %[2]s, which already has this tunnel as an origin, to load balancer %[1]s"
	case "unchanged,updated":
		msg = "Added this tunnel as an origin in pool %[2]s which is already used by load balancer %[1]s"
	case "unchanged,unchanged":
		msg = "Load balancer %s already uses pool %s which has this tunnel as an origin"
	case "unchanged,new":
		// this state is not possible
		fallthrough
	default:
		msg = "Something went wrong: failed to modify load balancer %s with pool %s; please check traffic manager configuration in the dashboard"
	}

	return fmt.Sprintf(msg, res.route.lbName, res.route.lbPool)
}

type Client interface {
	CreateTunnel(name string, tunnelSecret []byte) (*Tunnel, error)
	GetTunnel(tunnelID uuid.UUID) (*Tunnel, error)
	DeleteTunnel(tunnelID uuid.UUID) error
	ListTunnels(filter *Filter) ([]*Tunnel, error)
	CleanupConnections(tunnelID uuid.UUID) error
	RouteTunnel(tunnelID uuid.UUID, route Route) (RouteResult, error)
}

type RESTClient struct {
	baseEndpoints *baseEndpoints
	authToken     string
	userAgent     string
	client        http.Client
	logger        logger.Service
}

type baseEndpoints struct {
	accountLevel url.URL
	zoneLevel    url.URL
}

var _ Client = (*RESTClient)(nil)

func NewRESTClient(baseURL, accountTag, zoneTag, authToken, userAgent string, logger logger.Service) (*RESTClient, error) {
	if strings.HasSuffix(baseURL, "/") {
		baseURL = baseURL[:len(baseURL)-1]
	}
	accountLevelEndpoint, err := url.Parse(fmt.Sprintf("%s/accounts/%s/tunnels", baseURL, accountTag))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create account level endpoint")
	}
	zoneLevelEndpoint, err := url.Parse(fmt.Sprintf("%s/zones/%s/tunnels", baseURL, zoneTag))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create account level endpoint")
	}
	return &RESTClient{
		baseEndpoints: &baseEndpoints{
			accountLevel: *accountLevelEndpoint,
			zoneLevel:    *zoneLevelEndpoint,
		},
		authToken: authToken,
		userAgent: userAgent,
		client: http.Client{
			Transport: &http.Transport{
				TLSHandshakeTimeout:   defaultTimeout,
				ResponseHeaderTimeout: defaultTimeout,
			},
			Timeout: defaultTimeout,
		},
		logger: logger,
	}, nil
}

type newTunnel struct {
	Name         string `json:"name"`
	TunnelSecret []byte `json:"tunnel_secret"`
}

func (r *RESTClient) CreateTunnel(name string, tunnelSecret []byte) (*Tunnel, error) {
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
		return unmarshalTunnel(resp.Body)
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

func (r *RESTClient) DeleteTunnel(tunnelID uuid.UUID) error {
	endpoint := r.baseEndpoints.accountLevel
	endpoint.Path = path.Join(endpoint.Path, fmt.Sprintf("%v", tunnelID))
	resp, err := r.sendRequest("DELETE", endpoint, nil)
	if err != nil {
		return errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	return r.statusCodeToError("delete tunnel", resp)
}

func (r *RESTClient) ListTunnels(filter *Filter) ([]*Tunnel, error) {
	endpoint := r.baseEndpoints.accountLevel
	endpoint.RawQuery = filter.encode()
	resp, err := r.sendRequest("GET", endpoint, nil)
	if err != nil {
		return nil, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return parseListTunnels(resp.Body)
	}

	return nil, r.statusCodeToError("list tunnels", resp)
}

func parseListTunnels(body io.ReadCloser) ([]*Tunnel, error) {
	var tunnels []*Tunnel
	err := parseResponse(body, &tunnels)
	return tunnels, err
}

func (r *RESTClient) CleanupConnections(tunnelID uuid.UUID) error {
	endpoint := r.baseEndpoints.accountLevel
	endpoint.Path = path.Join(endpoint.Path, fmt.Sprintf("%v/connections", tunnelID))
	resp, err := r.sendRequest("DELETE", endpoint, nil)
	if err != nil {
		return errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	return r.statusCodeToError("cleanup connections", resp)
}

func (r *RESTClient) RouteTunnel(tunnelID uuid.UUID, route Route) (RouteResult, error) {
	endpoint := r.baseEndpoints.zoneLevel
	endpoint.Path = path.Join(endpoint.Path, fmt.Sprintf("%v/routes", tunnelID))
	resp, err := r.sendRequest("PUT", endpoint, route)
	if err != nil {
		return nil, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return route.UnmarshalResult(resp.Body)
	}

	return nil, r.statusCodeToError("add route", resp)
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
	req.Header.Add("X-Auth-User-Service-Key", r.authToken)
	req.Header.Add("Accept", "application/json;version=1")
	return r.client.Do(req)
}

func parseResponse(reader io.Reader, data interface{}) error {
	// Schema for Tunnelstore responses in the v1 API.
	// Roughly, it's a wrapper around a particular result that adds failures/errors/etc
	var result response
	// First, parse the wrapper and check the API call succeeded
	if err := json.NewDecoder(reader).Decode(&result); err != nil {
		return errors.Wrap(err, "failed to decode response")
	}
	if err := result.checkErrors(); err != nil {
		return err
	}
	if !result.Success {
		return ErrAPINoSuccess
	}
	// At this point we know the API call succeeded, so, parse out the inner
	// result into the datatype provided as a parameter.
	return json.Unmarshal(result.Result, &data)
}

func unmarshalTunnel(reader io.Reader) (*Tunnel, error) {
	var tunnel Tunnel
	err := parseResponse(reader, &tunnel)
	return &tunnel, err
}

type response struct {
	Success  bool            `json:"success,omitempty"`
	Errors   []apiErr        `json:"errors,omitempty"`
	Messages []string        `json:"messages,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
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
