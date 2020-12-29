package tunnelstore

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"path"

	"github.com/cloudflare/cloudflared/teamnet"
	"github.com/pkg/errors"
)

// ListRoutes calls the Tunnelstore GET endpoint for all routes under an account.
func (r *RESTClient) ListRoutes(filter *teamnet.Filter) ([]*teamnet.Route, error) {
	endpoint := r.baseEndpoints.accountRoutes
	endpoint.RawQuery = filter.Encode()
	resp, err := r.sendRequest("GET", endpoint, nil)
	if err != nil {
		return nil, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return parseListRoutes(resp.Body)
	}

	return nil, r.statusCodeToError("list routes", resp)
}

// AddRoute calls the Tunnelstore POST endpoint for a given route.
func (r *RESTClient) AddRoute(newRoute teamnet.NewRoute) (teamnet.Route, error) {
	endpoint := r.baseEndpoints.accountRoutes
	endpoint.Path = path.Join(endpoint.Path, url.PathEscape(newRoute.Network.String()))
	resp, err := r.sendRequest("POST", endpoint, newRoute)
	if err != nil {
		return teamnet.Route{}, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return parseRoute(resp.Body)
	}

	return teamnet.Route{}, r.statusCodeToError("add route", resp)
}

// DeleteRoute calls the Tunnelstore DELETE endpoint for a given route.
func (r *RESTClient) DeleteRoute(network net.IPNet) error {
	endpoint := r.baseEndpoints.accountRoutes
	endpoint.Path = path.Join(endpoint.Path, url.PathEscape(network.String()))
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

func parseListRoutes(body io.ReadCloser) ([]*teamnet.Route, error) {
	var routes []*teamnet.Route
	err := parseResponse(body, &routes)
	return routes, err
}

func parseRoute(body io.ReadCloser) (teamnet.Route, error) {
	var route teamnet.Route
	err := parseResponse(body, &route)
	return route, err
}
