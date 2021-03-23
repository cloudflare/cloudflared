package pogs

import (
	"context"

	"zombiezen.com/go/capnproto2/server"

	"github.com/cloudflare/cloudflared/tunnelrpc"
)

func (i TunnelServer_PogsImpl) ReconnectTunnel(p tunnelrpc.TunnelServer_reconnectTunnel) error {
	jwt, err := p.Params.Jwt()
	if err != nil {
		return err
	}
	eventDigest, err := p.Params.EventDigest()
	if err != nil {
		return err
	}
	connDigest, err := p.Params.ConnDigest()
	if err != nil {
		return err
	}
	hostname, err := p.Params.Hostname()
	if err != nil {
		return err
	}
	options, err := p.Params.Options()
	if err != nil {
		return err
	}
	pogsOptions, err := UnmarshalRegistrationOptions(options)
	if err != nil {
		return err
	}
	server.Ack(p.Options)
	registration, err := i.impl.ReconnectTunnel(p.Ctx, jwt, eventDigest, connDigest, hostname, pogsOptions)
	if err != nil {
		return err
	}
	result, err := p.Results.NewResult()
	if err != nil {
		return err
	}
	return MarshalTunnelRegistration(result, registration)
}

func (c TunnelServer_PogsClient) ReconnectTunnel(
	ctx context.Context,
	jwt,
	eventDigest []byte,
	connDigest []byte,
	hostname string,
	options *RegistrationOptions,
) *TunnelRegistration {
	client := tunnelrpc.TunnelServer{Client: c.Client}
	promise := client.ReconnectTunnel(ctx, func(p tunnelrpc.TunnelServer_reconnectTunnel_Params) error {
		err := p.SetJwt(jwt)
		if err != nil {
			return err
		}
		err = p.SetEventDigest(eventDigest)
		if err != nil {
			return err
		}
		err = p.SetConnDigest(connDigest)
		if err != nil {
			return err
		}
		err = p.SetHostname(hostname)
		if err != nil {
			return err
		}
		registrationOptions, err := p.NewOptions()
		if err != nil {
			return err
		}
		err = MarshalRegistrationOptions(registrationOptions, options)
		if err != nil {
			return err
		}
		return nil
	})
	retval, err := promise.Result().Struct()
	if err != nil {
		return NewRetryableRegistrationError(err, defaultRetryAfterSeconds).Serialize()
	}
	registration, err := UnmarshalTunnelRegistration(retval)
	if err != nil {
		return NewRetryableRegistrationError(err, defaultRetryAfterSeconds).Serialize()
	}
	return registration
}
