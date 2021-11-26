package tunnel

import (
	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/vnet"
)

func (sc *subcommandContext) addVirtualNetwork(newVnet vnet.NewVirtualNetwork) (vnet.VirtualNetwork, error) {
	client, err := sc.client()
	if err != nil {
		return vnet.VirtualNetwork{}, errors.Wrap(err, noClientMsg)
	}
	return client.CreateVirtualNetwork(newVnet)
}

func (sc *subcommandContext) listVirtualNetworks(filter *vnet.Filter) ([]*vnet.VirtualNetwork, error) {
	client, err := sc.client()
	if err != nil {
		return nil, errors.Wrap(err, noClientMsg)
	}
	return client.ListVirtualNetworks(filter)
}

func (sc *subcommandContext) deleteVirtualNetwork(vnetId uuid.UUID) error {
	client, err := sc.client()
	if err != nil {
		return errors.Wrap(err, noClientMsg)
	}
	return client.DeleteVirtualNetwork(vnetId)
}

func (sc *subcommandContext) updateVirtualNetwork(vnetId uuid.UUID, updates vnet.UpdateVirtualNetwork) error {
	client, err := sc.client()
	if err != nil {
		return errors.Wrap(err, noClientMsg)
	}
	return client.UpdateVirtualNetwork(vnetId, updates)
}
