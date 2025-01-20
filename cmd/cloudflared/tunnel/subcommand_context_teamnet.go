package tunnel

import (
	"net"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/cfapi"
)

const noClientMsg = "error while creating backend client"

func (sc *subcommandContext) listRoutes(filter *cfapi.IpRouteFilter) ([]*cfapi.DetailedRoute, error) {
	client, err := sc.client()
	if err != nil {
		return nil, errors.Wrap(err, noClientMsg)
	}
	return client.ListRoutes(filter)
}

func (sc *subcommandContext) addRoute(newRoute cfapi.NewRoute) (cfapi.Route, error) {
	client, err := sc.client()
	if err != nil {
		return cfapi.Route{}, errors.Wrap(err, noClientMsg)
	}
	return client.AddRoute(newRoute)
}

func (sc *subcommandContext) deleteRoute(id uuid.UUID) error {
	client, err := sc.client()
	if err != nil {
		return errors.Wrap(err, noClientMsg)
	}
	return client.DeleteRoute(id)
}

func (sc *subcommandContext) getRouteByIP(params cfapi.GetRouteByIpParams) (cfapi.DetailedRoute, error) {
	client, err := sc.client()
	if err != nil {
		return cfapi.DetailedRoute{}, errors.Wrap(err, noClientMsg)
	}
	return client.GetByIP(params)
}

func (sc *subcommandContext) getRouteId(network net.IPNet, vnetId *uuid.UUID) (uuid.UUID, error) {
	filters := cfapi.NewIPRouteFilter()
	filters.NotDeleted()
	filters.NetworkIsSubsetOf(network)
	filters.NetworkIsSupersetOf(network)

	if vnetId != nil {
		filters.VNetID(*vnetId)
	}

	result, err := sc.listRoutes(filters)
	if err != nil {
		return uuid.Nil, err
	}

	if len(result) != 1 {
		return uuid.Nil, errors.New("unable to find route for provided network and vnet")
	}

	return result[0].ID, nil
}
