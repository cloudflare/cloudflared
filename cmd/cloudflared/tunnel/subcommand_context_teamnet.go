package tunnel

import (
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

func (sc *subcommandContext) deleteRoute(params cfapi.DeleteRouteParams) error {
	client, err := sc.client()
	if err != nil {
		return errors.Wrap(err, noClientMsg)
	}
	return client.DeleteRoute(params)
}

func (sc *subcommandContext) getRouteByIP(params cfapi.GetRouteByIpParams) (cfapi.DetailedRoute, error) {
	client, err := sc.client()
	if err != nil {
		return cfapi.DetailedRoute{}, errors.Wrap(err, noClientMsg)
	}
	return client.GetByIP(params)
}
