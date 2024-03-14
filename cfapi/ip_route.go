package cfapi

import (
	"encoding/json"
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

// Route is a mapping from customer's IP space to a tunnel.
// Each route allows the customer to route eyeballs in their corporate network
// to certain private IP ranges. Each Route represents an IP range in their
// network, and says that eyeballs can reach that route using the corresponding
// tunnel.
type Route struct {
	Network  CIDR      `json:"network"`
	TunnelID uuid.UUID `json:"tunnel_id"`
	// Optional field. When unset, it means the Route belongs to the default virtual network.
	VNetID    *uuid.UUID `json:"virtual_network_id,omitempty"`
	Comment   string     `json:"comment"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt time.Time  `json:"deleted_at"`
}

// CIDR is just a newtype wrapper around net.IPNet. It adds JSON unmarshalling.
type CIDR net.IPNet

func (c CIDR) String() string {
	n := net.IPNet(c)
	return n.String()
}

func (c CIDR) MarshalJSON() ([]byte, error) {
	str := c.String()
	json, err := json.Marshal(str)
	if err != nil {
		return nil, errors.Wrap(err, "error serializing CIDR into JSON")
	}
	return json, nil
}

// UnmarshalJSON parses a JSON string into net.IPNet
func (c *CIDR) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.Wrap(err, "error parsing cidr string")
	}
	_, network, err := net.ParseCIDR(s)
	if err != nil {
		return errors.Wrap(err, "error parsing invalid network from backend")
	}
	if network == nil {
		return fmt.Errorf("backend returned invalid network %s", s)
	}
	*c = CIDR(*network)
	return nil
}

// NewRoute has all the parameters necessary to add a new route to the table.
type NewRoute struct {
	Network  net.IPNet
	TunnelID uuid.UUID
	Comment  string
	// Optional field. If unset, backend will assume the default vnet for the account.
	VNetID *uuid.UUID
}

// MarshalJSON handles fields with non-JSON types (e.g. net.IPNet).
func (r NewRoute) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Network  string     `json:"network"`
		TunnelID uuid.UUID  `json:"tunnel_id"`
		Comment  string     `json:"comment"`
		VNetID   *uuid.UUID `json:"virtual_network_id,omitempty"`
	}{
		Network:  r.Network.String(),
		TunnelID: r.TunnelID,
		Comment:  r.Comment,
		VNetID:   r.VNetID,
	})
}

// DetailedRoute is just a Route with some extra fields, e.g. TunnelName.
type DetailedRoute struct {
	ID       uuid.UUID `json:"id"`
	Network  CIDR      `json:"network"`
	TunnelID uuid.UUID `json:"tunnel_id"`
	// Optional field. When unset, it means the DetailedRoute belongs to the default virtual network.
	VNetID     *uuid.UUID `json:"virtual_network_id,omitempty"`
	Comment    string     `json:"comment"`
	CreatedAt  time.Time  `json:"created_at"`
	DeletedAt  time.Time  `json:"deleted_at"`
	TunnelName string     `json:"tunnel_name"`
}

// IsZero checks if DetailedRoute is the zero value.
func (r *DetailedRoute) IsZero() bool {
	return r.TunnelID == uuid.Nil
}

// TableString outputs a table row summarizing the route, to be used
// when showing the user their routing table.
func (r DetailedRoute) TableString() string {
	deletedColumn := "-"
	if !r.DeletedAt.IsZero() {
		deletedColumn = r.DeletedAt.Format(time.RFC3339)
	}
	vnetColumn := "default"
	if r.VNetID != nil {
		vnetColumn = r.VNetID.String()
	}

	return fmt.Sprintf(
		"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t",
		r.ID,
		r.Network.String(),
		vnetColumn,
		r.Comment,
		r.TunnelID,
		r.TunnelName,
		r.CreatedAt.Format(time.RFC3339),
		deletedColumn,
	)
}

type GetRouteByIpParams struct {
	Ip net.IP
	// Optional field. If unset, backend will assume the default vnet for the account.
	VNetID *uuid.UUID
}

// ListRoutes calls the Tunnelstore GET endpoint for all routes under an account.
// Due to pagination on the server side it will call the endpoint multiple times if needed.
func (r *RESTClient) ListRoutes(filter *IpRouteFilter) ([]*DetailedRoute, error) {
	fetchFn := func(page int) (*http.Response, error) {
		endpoint := r.baseEndpoints.accountRoutes
		filter.Page(page)
		endpoint.RawQuery = filter.Encode()
		rsp, err := r.sendRequest("GET", endpoint, nil)

		if err != nil {
			return nil, errors.Wrap(err, "REST request failed")
		}
		if rsp.StatusCode != http.StatusOK {
			rsp.Body.Close()
			return nil, r.statusCodeToError("list routes", rsp)
		}
		return rsp, nil
	}
	return fetchExhaustively[DetailedRoute](fetchFn)
}

// AddRoute calls the Tunnelstore POST endpoint for a given route.
func (r *RESTClient) AddRoute(newRoute NewRoute) (Route, error) {
	endpoint := r.baseEndpoints.accountRoutes
	endpoint.Path = path.Join(endpoint.Path)
	resp, err := r.sendRequest("POST", endpoint, newRoute)
	if err != nil {
		return Route{}, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return parseRoute(resp.Body)
	}

	return Route{}, r.statusCodeToError("add route", resp)
}

// DeleteRoute calls the Tunnelstore DELETE endpoint for a given route.
func (r *RESTClient) DeleteRoute(id uuid.UUID) error {
	endpoint := r.baseEndpoints.accountRoutes
	endpoint.Path = path.Join(endpoint.Path, url.PathEscape(id.String()))

	resp, err := r.sendRequest("DELETE", endpoint, nil)
	if err != nil {
		return errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		_, err := parseRoute(resp.Body)
		return err
	}

	return r.statusCodeToError("delete route", resp)
}

// GetByIP checks which route will proxy a given IP.
func (r *RESTClient) GetByIP(params GetRouteByIpParams) (DetailedRoute, error) {
	endpoint := r.baseEndpoints.accountRoutes
	endpoint.Path = path.Join(endpoint.Path, "ip", url.PathEscape(params.Ip.String()))
	setVnetParam(&endpoint, params.VNetID)

	resp, err := r.sendRequest("GET", endpoint, nil)
	if err != nil {
		return DetailedRoute{}, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return parseDetailedRoute(resp.Body)
	}

	return DetailedRoute{}, r.statusCodeToError("get route by IP", resp)
}

func parseRoute(body io.ReadCloser) (Route, error) {
	var route Route
	err := parseResponse(body, &route)
	return route, err
}

func parseDetailedRoute(body io.ReadCloser) (DetailedRoute, error) {
	var route DetailedRoute
	err := parseResponse(body, &route)
	return route, err
}

// setVnetParam overwrites the URL's query parameters with a query param to scope the HostnameRoute action to a certain
// virtual network (if one is provided).
func setVnetParam(endpoint *url.URL, vnetID *uuid.UUID) {
	queryParams := url.Values{}
	if vnetID != nil {
		queryParams.Set("virtual_network_id", vnetID.String())
	}
	endpoint.RawQuery = queryParams.Encode()
}
