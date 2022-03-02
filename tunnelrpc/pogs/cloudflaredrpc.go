package pogs

import (
	"github.com/cloudflare/cloudflared/tunnelrpc"
	capnp "zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/rpc"
)

type CloudflaredServer interface {
	SessionManager
	ConfigurationManager
}

type CloudflaredServer_PogsImpl struct {
	SessionManager_PogsImpl
	ConfigurationManager_PogsImpl
}

func CloudflaredServer_ServerToClient(s SessionManager, c ConfigurationManager) tunnelrpc.CloudflaredServer {
	return tunnelrpc.CloudflaredServer_ServerToClient(CloudflaredServer_PogsImpl{
		SessionManager_PogsImpl:       SessionManager_PogsImpl{s},
		ConfigurationManager_PogsImpl: ConfigurationManager_PogsImpl{c},
	})
}

type CloudflaredServer_PogsClient struct {
	SessionManager_PogsClient
	ConfigurationManager_PogsClient
	Client capnp.Client
	Conn   *rpc.Conn
}

func NewCloudflaredServer_PogsClient(client capnp.Client, conn *rpc.Conn) CloudflaredServer_PogsClient {
	sessionManagerClient := SessionManager_PogsClient{
		Client: client,
		Conn:   conn,
	}
	configManagerClient := ConfigurationManager_PogsClient{
		Client: client,
		Conn:   conn,
	}
	return CloudflaredServer_PogsClient{
		SessionManager_PogsClient:       sessionManagerClient,
		ConfigurationManager_PogsClient: configManagerClient,
		Client:                          client,
		Conn:                            conn,
	}
}

func (c CloudflaredServer_PogsClient) Close() error {
	c.Client.Close()
	return c.Conn.Close()
}
