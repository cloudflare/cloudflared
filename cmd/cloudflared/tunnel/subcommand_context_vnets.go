package tunnel

import (
	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/cfapi"
)

func (sc *subcommandContext) addVirtualNetwork(newVnet cfapi.NewVirtualNetwork) (cfapi.VirtualNetwork, error) {
	client, err := sc.client()
	if err != nil {
		return cfapi.VirtualNetwork{}, errors.Wrap(err, noClientMsg)
	}
	return client.CreateVirtualNetwork(newVnet)
}

func (sc *subcommandContext) listVirtualNetworks(filter *cfapi.VnetFilter) ([]*cfapi.VirtualNetwork, error) {
	client, err := sc.client()
	if err != nil {
		return nil, errors.Wrap(err, noClientMsg)
	}
	return client.ListVirtualNetworks(filter)
}

func (sc *subcommandContext) deleteVirtualNetwork(vnetId uuid.UUID, force bool) error {
	client, err := sc.client()
	if err != nil {
		return errors.Wrap(err, noClientMsg)
	}
	return client.DeleteVirtualNetwork(vnetId, force)
}

func (sc *subcommandContext) updateVirtualNetwork(vnetId uuid.UUID, updates cfapi.UpdateVirtualNetwork) error {
	client, err := sc.client()
	if err != nil {
		return errors.Wrap(err, noClientMsg)
	}
	return client.UpdateVirtualNetwork(vnetId, updates)
}
