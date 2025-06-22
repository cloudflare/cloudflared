package cfapi

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

type NewVirtualNetwork struct {
	Name      string `json:"name"`
	Comment   string `json:"comment"`
	IsDefault bool   `json:"is_default_network"`
}

type VirtualNetwork struct {
	ID        uuid.UUID `json:"id"`
	Comment   string    `json:"comment"`
	Name      string    `json:"name"`
	IsDefault bool      `json:"is_default_network"`
	CreatedAt time.Time `json:"created_at"`
	DeletedAt time.Time `json:"deleted_at"`
}

type UpdateVirtualNetwork struct {
	Name      *string `json:"name,omitempty"`
	Comment   *string `json:"comment,omitempty"`
	IsDefault *bool   `json:"is_default_network,omitempty"`
}

func (virtualNetwork VirtualNetwork) TableString() string {
	deletedColumn := "-"
	if !virtualNetwork.DeletedAt.IsZero() {
		deletedColumn = virtualNetwork.DeletedAt.Format(time.RFC3339)
	}
	return fmt.Sprintf(
		"%s\t%s\t%s\t%s\t%s\t%s\t",
		virtualNetwork.ID,
		virtualNetwork.Name,
		strconv.FormatBool(virtualNetwork.IsDefault),
		virtualNetwork.Comment,
		virtualNetwork.CreatedAt.Format(time.RFC3339),
		deletedColumn,
	)
}

func (r *RESTClient) CreateVirtualNetwork(newVnet NewVirtualNetwork) (VirtualNetwork, error) {
	resp, err := r.sendRequest("POST", r.baseEndpoints.accountVnets, newVnet)
	if err != nil {
		return VirtualNetwork{}, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return parseVnet(resp.Body)
	}

	return VirtualNetwork{}, r.statusCodeToError("add virtual network", resp)
}

func (r *RESTClient) ListVirtualNetworks(filter *VnetFilter) ([]*VirtualNetwork, error) {
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

func (r *RESTClient) DeleteVirtualNetwork(id uuid.UUID, force bool) error {
	endpoint := r.baseEndpoints.accountVnets
	endpoint.Path = path.Join(endpoint.Path, url.PathEscape(id.String()))

	queryParams := url.Values{}
	if force {
		queryParams.Set("force", strconv.FormatBool(force))
	}
	endpoint.RawQuery = queryParams.Encode()

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

func (r *RESTClient) UpdateVirtualNetwork(id uuid.UUID, updates UpdateVirtualNetwork) error {
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

func parseListVnets(body io.ReadCloser) ([]*VirtualNetwork, error) {
	var vnets []*VirtualNetwork
	err := parseResponse(body, &vnets)
	return vnets, err
}

func parseVnet(body io.ReadCloser) (VirtualNetwork, error) {
	var vnet VirtualNetwork
	err := parseResponse(body, &vnet)
	return vnet, err
}
