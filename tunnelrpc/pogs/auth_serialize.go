package pogs

import (
	"context"

	"zombiezen.com/go/capnproto2/pogs"
	"zombiezen.com/go/capnproto2/server"

	"github.com/cloudflare/cloudflared/tunnelrpc"
)

func (i TunnelServer_PogsImpl) Authenticate(p tunnelrpc.TunnelServer_authenticate) error {
	originCert, err := p.Params.OriginCert()
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
	resp, err := i.impl.Authenticate(p.Ctx, originCert, hostname, pogsOptions)
	if err != nil {
		return err
	}
	result, err := p.Results.NewResult()
	if err != nil {
		return err
	}
	return MarshalAuthenticateResponse(result, resp)
}

func MarshalAuthenticateResponse(s tunnelrpc.AuthenticateResponse, p *AuthenticateResponse) error {
	return pogs.Insert(tunnelrpc.AuthenticateResponse_TypeID, s.Struct, p)
}

func (c TunnelServer_PogsClient) Authenticate(ctx context.Context, originCert []byte, hostname string, options *RegistrationOptions) (*AuthenticateResponse, error) {
	client := tunnelrpc.TunnelServer{Client: c.Client}
	promise := client.Authenticate(ctx, func(p tunnelrpc.TunnelServer_authenticate_Params) error {
		err := p.SetOriginCert(originCert)
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
		return nil, err
	}
	return UnmarshalAuthenticateResponse(retval)
}

func UnmarshalAuthenticateResponse(s tunnelrpc.AuthenticateResponse) (*AuthenticateResponse, error) {
	p := new(AuthenticateResponse)
	err := pogs.Extract(p, tunnelrpc.AuthenticateResponse_TypeID, s.Struct)
	return p, err
}
