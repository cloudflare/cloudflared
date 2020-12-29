package tunnel

import (
	"net"

	"github.com/cloudflare/cloudflared/teamnet"
	"github.com/pkg/errors"
)

const noClientMsg = "error while creating backend client"

func (sc *subcommandContext) listRoutes(filter *teamnet.Filter) ([]*teamnet.Route, error) {
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
