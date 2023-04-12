package cfapi

import (
	"github.com/google/uuid"
)

type TunnelClient interface {
	CreateTunnel(name string, tunnelSecret []byte) (*TunnelWithToken, error)
	GetTunnel(tunnelID uuid.UUID) (*Tunnel, error)
	GetTunnelToken(tunnelID uuid.UUID) (string, error)
	GetManagementToken(tunnelID uuid.UUID) (string, error)
	DeleteTunnel(tunnelID uuid.UUID) error
	ListTunnels(filter *TunnelFilter) ([]*Tunnel, error)
	ListActiveClients(tunnelID uuid.UUID) ([]*ActiveClient, error)
	CleanupConnections(tunnelID uuid.UUID, params *CleanupParams) error
}

type HostnameClient interface {
	RouteTunnel(tunnelID uuid.UUID, route HostnameRoute) (HostnameRouteResult, error)
}

type IPRouteClient interface {
	ListRoutes(filter *IpRouteFilter) ([]*DetailedRoute, error)
	AddRoute(newRoute NewRoute) (Route, error)
	DeleteRoute(params DeleteRouteParams) error
	GetByIP(params GetRouteByIpParams) (DetailedRoute, error)
}

type VnetClient interface {
	CreateVirtualNetwork(newVnet NewVirtualNetwork) (VirtualNetwork, error)
	ListVirtualNetworks(filter *VnetFilter) ([]*VirtualNetwork, error)
	DeleteVirtualNetwork(id uuid.UUID, force bool) error
	UpdateVirtualNetwork(id uuid.UUID, updates UpdateVirtualNetwork) error
}

type Client interface {
	TunnelClient
	HostnameClient
	IPRouteClient
	VnetClient
}
