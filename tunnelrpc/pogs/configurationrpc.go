package pogs

import (
	"context"
	"fmt"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	capnp "zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/rpc"
	"zombiezen.com/go/capnproto2/server"
)

type ConfigurationManager interface {
	UpdateConfiguration(ctx context.Context, version int32, config []byte) *UpdateConfigurationResponse
}

type ConfigurationManager_PogsImpl struct {
	impl ConfigurationManager
}

func ConfigurationManager_ServerToClient(c ConfigurationManager) tunnelrpc.ConfigurationManager {
	return tunnelrpc.ConfigurationManager_ServerToClient(ConfigurationManager_PogsImpl{c})
}

func (i ConfigurationManager_PogsImpl) UpdateConfiguration(p tunnelrpc.ConfigurationManager_updateConfiguration) error {
	server.Ack(p.Options)

	version := p.Params.Version()
	config, err := p.Params.Config()
	if err != nil {
		return err
	}

	result, err := p.Results.NewResult()
	if err != nil {
		return err
	}

	updateResp := i.impl.UpdateConfiguration(p.Ctx, version, config)
	return updateResp.Marshal(result)
}

type ConfigurationManager_PogsClient struct {
	Client capnp.Client
	Conn   *rpc.Conn
}

func (c ConfigurationManager_PogsClient) Close() error {
	c.Client.Close()
	return c.Conn.Close()
}

func (c ConfigurationManager_PogsClient) UpdateConfiguration(ctx context.Context, version int32, config []byte) (*UpdateConfigurationResponse, error) {
	client := tunnelrpc.ConfigurationManager{Client: c.Client}
	promise := client.UpdateConfiguration(ctx, func(p tunnelrpc.ConfigurationManager_updateConfiguration_Params) error {
		p.SetVersion(version)
		return p.SetConfig(config)
	})
	result, err := promise.Result().Struct()
	if err != nil {
		return nil, wrapRPCError(err)
	}
	response := new(UpdateConfigurationResponse)

	err = response.Unmarshal(result)
	if err != nil {
		return nil, err
	}
	return response, nil
}

type UpdateConfigurationResponse struct {
	LastAppliedVersion int32 `json:"lastAppliedVersion"`
	Err                error `json:"err"`
}

func (p *UpdateConfigurationResponse) Marshal(s tunnelrpc.UpdateConfigurationResponse) error {
	s.SetLatestAppliedVersion(p.LastAppliedVersion)
	if p.Err != nil {
		return s.SetErr(p.Err.Error())
	}
	return nil
}

func (p *UpdateConfigurationResponse) Unmarshal(s tunnelrpc.UpdateConfigurationResponse) error {
	p.LastAppliedVersion = s.LatestAppliedVersion()
	respErr, err := s.Err()
	if err != nil {
		return err
	}
	if respErr != "" {
		p.Err = fmt.Errorf(respErr)
	}
	return nil
}
