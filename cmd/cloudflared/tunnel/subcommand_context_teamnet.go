package tunnel

import (
	"github.com/cloudflare/cloudflared/teamnet"
)

func (sc *subcommandContext) listRoutes(filter *teamnet.Filter) ([]*teamnet.Route, error) {
	client, err := sc.client()
	if err != nil {
		return nil, err
	}
	return client.ListRoutes(filter)
}

func (sc *subcommandContext) addRoute(newRoute teamnet.NewRoute) (teamnet.Route, error) {
	client, err := sc.client()
	if err != nil {
		return teamnet.Route{}, err
	}
	return client.AddRoute(newRoute)
}
