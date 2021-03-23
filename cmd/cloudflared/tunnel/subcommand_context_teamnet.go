package tunnel

import (
	"net"

	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/teamnet"
)

const noClientMsg = "error while creating backend client"

func (sc *subcommandContext) listRoutes(filter *teamnet.Filter) ([]*teamnet.DetailedRoute, error) {
	client, err := sc.client()
	if err != nil {
		return nil, errors.Wrap(err, noClientMsg)
	}
	return client.ListRoutes(filter)
}

func (sc *subcommandContext) addRoute(newRoute teamnet.NewRoute) (teamnet.Route, error) {
	client, err := sc.client()
	if err != nil {
		return teamnet.Route{}, errors.Wrap(err, noClientMsg)
	}
	return client.AddRoute(newRoute)
}

func (sc *subcommandContext) deleteRoute(network net.IPNet) error {
	client, err := sc.client()
	if err != nil {
		return errors.Wrap(err, noClientMsg)
	}
	return client.DeleteRoute(network)
}

func (sc *subcommandContext) getRouteByIP(ip net.IP) (teamnet.DetailedRoute, error) {
	client, err := sc.client()
	if err != nil {
		return teamnet.DetailedRoute{}, errors.Wrap(err, noClientMsg)
	}
	return client.GetByIP(ip)
}
