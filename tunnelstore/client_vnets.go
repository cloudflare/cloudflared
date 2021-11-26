package tunnelstore

import (
	"io"
	"net/http"
	"net/url"
	"path"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/vnet"
)

func (r *RESTClient) CreateVirtualNetwork(newVnet vnet.NewVirtualNetwork) (vnet.VirtualNetwork, error) {
	resp, err := r.sendRequest("POST", r.baseEndpoints.accountVnets, newVnet)
	if err != nil {
		return vnet.VirtualNetwork{}, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return parseVnet(resp.Body)
	}

	return vnet.VirtualNetwork{}, r.statusCodeToError("add virtual network", resp)
}

func (r *RESTClient) ListVirtualNetworks(filter *vnet.Filter) ([]*vnet.VirtualNetwork, error) {
	endpoint := r.baseEndpoints.accountVnets
	endpoint.RawQuery = filter.Encode()
	resp, err := r.sendRequest("GET", endpoint, nil)
	if err != nil {
		return nil, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return parseListVnets(resp.Body)
	}

	return nil, r.statusCodeToError("list virtual networks", resp)
}

func (r *RESTClient) DeleteVirtualNetwork(id uuid.UUID) error {
	endpoint := r.baseEndpoints.accountVnets
	endpoint.Path = path.Join(endpoint.Path, url.PathEscape(id.String()))
	resp, err := r.sendRequest("DELETE", endpoint, nil)
	if err != nil {
		return errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		_, err := parseVnet(resp.Body)
		return err
	}

	return r.statusCodeToError("delete virtual network", resp)
}

func (r *RESTClient) UpdateVirtualNetwork(id uuid.UUID, updates vnet.UpdateVirtualNetwork) error {
	endpoint := r.baseEndpoints.accountVnets
	endpoint.Path = path.Join(endpoint.Path, url.PathEscape(id.String()))
	resp, err := r.sendRequest("PATCH", endpoint, updates)
	if err != nil {
		return errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		_, err := parseVnet(resp.Body)
		return err
	}

	return r.statusCodeToError("update virtual network", resp)
}

func parseListVnets(body io.ReadCloser) ([]*vnet.VirtualNetwork, error) {
	var vnets []*vnet.VirtualNetwork
	err := parseResponse(body, &vnets)
	return vnets, err
}

func parseVnet(body io.ReadCloser) (vnet.VirtualNetwork, error) {
	var vnet vnet.VirtualNetwork
	err := parseResponse(body, &vnet)
	return vnet, err
}
