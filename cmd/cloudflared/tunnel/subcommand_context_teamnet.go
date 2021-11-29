package tunnel

import (
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

func (sc *subcommandContext) deleteRoute(params teamnet.DeleteRouteParams) error {
	client, err := sc.client()
	if err != nil {
		return errors.Wrap(err, noClientMsg)
	}
	return client.DeleteRoute(params)
}

func (sc *subcommandContext) getRouteByIP(params teamnet.GetRouteByIpParams) (teamnet.DetailedRoute, error) {
	client, err := sc.client()
	if err != nil {
		return teamnet.DetailedRoute{}, errors.Wrap(err, noClientMsg)
	}
	return client.GetByIP(params)
}
