package pogs

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/google/uuid"
	capnp "zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/pogs"
	"zombiezen.com/go/capnproto2/rpc"
	"zombiezen.com/go/capnproto2/server"

	"github.com/cloudflare/cloudflared/tunnelrpc"
)

type RegistrationServer interface {
	RegisterConnection(ctx context.Context, auth TunnelAuth, tunnelID uuid.UUID, connIndex byte, options *ConnectionOptions) (*ConnectionDetails, error)
	UnregisterConnection(ctx context.Context)
	UpdateLocalConfiguration(ctx context.Context, config []byte) error
}

type RegistrationServer_PogsImpl struct {
	impl RegistrationServer
}

func RegistrationServer_ServerToClient(s RegistrationServer) tunnelrpc.RegistrationServer {
	return tunnelrpc.RegistrationServer_ServerToClient(RegistrationServer_PogsImpl{s})
}

func (i RegistrationServer_PogsImpl) RegisterConnection(p tunnelrpc.RegistrationServer_registerConnection) error {
	server.Ack(p.Options)

	auth, err := p.Params.Auth()
	if err != nil {
		return err
	}
	var pogsAuth TunnelAuth
	err = pogsAuth.UnmarshalCapnproto(auth)
	if err != nil {
		return err
	}
	uuidBytes, err := p.Params.TunnelId()
	if err != nil {
		return err
	}
	tunnelID, err := uuid.FromBytes(uuidBytes)
	if err != nil {
		return err
	}
	connIndex := p.Params.ConnIndex()
	options, err := p.Params.Options()
	if err != nil {
		return err
	}
	var pogsOptions ConnectionOptions
	err = pogsOptions.UnmarshalCapnproto(options)
	if err != nil {
		return err
	}

	connDetails, callError := i.impl.RegisterConnection(p.Ctx, pogsAuth, tunnelID, connIndex, &pogsOptions)

	resp, err := p.Results.NewResult()
	if err != nil {
		return err
	}

	if callError != nil {
		if connError, err := resp.Result().NewError(); err != nil {
			return err
		} else {
			return MarshalError(connError, callError)
		}
	}

	if details, err := resp.Result().NewConnectionDetails(); err != nil {
		return err
	} else {
		return connDetails.MarshalCapnproto(details)
	}
}

func (i RegistrationServer_PogsImpl) UnregisterConnection(p tunnelrpc.RegistrationServer_unregisterConnection) error {
	server.Ack(p.Options)

	i.impl.UnregisterConnection(p.Ctx)
	return nil
}

func (i RegistrationServer_PogsImpl) UpdateLocalConfiguration(c tunnelrpc.RegistrationServer_updateLocalConfiguration) error {
	server.Ack(c.Options)

	configBytes, err := c.Params.Config()
	if err != nil {
		return err
	}

	return i.impl.UpdateLocalConfiguration(c.Ctx, configBytes)
}

type RegistrationServer_PogsClient struct {
	Client capnp.Client
	Conn   *rpc.Conn
}

func (c RegistrationServer_PogsClient) Close() error {
	c.Client.Close()
	return c.Conn.Close()
}

func (c RegistrationServer_PogsClient) RegisterConnection(ctx context.Context, auth TunnelAuth, tunnelID uuid.UUID, connIndex byte, options *ConnectionOptions) (*ConnectionDetails, error) {
	client := tunnelrpc.TunnelServer{Client: c.Client}
	promise := client.RegisterConnection(ctx, func(p tunnelrpc.RegistrationServer_registerConnection_Params) error {
		tunnelAuth, err := p.NewAuth()
		if err != nil {
			return err
		}
		if err = auth.MarshalCapnproto(tunnelAuth); err != nil {
			return err
		}
		err = p.SetAuth(tunnelAuth)
		if err != nil {
			return err
		}
		err = p.SetTunnelId(tunnelID[:])
		if err != nil {
			return err
		}
		p.SetConnIndex(connIndex)
		connectionOptions, err := p.NewOptions()
		if err != nil {
			return err
		}
		err = options.MarshalCapnproto(connectionOptions)
		if err != nil {
			return err
		}
		return nil
	})
	response, err := promise.Result().Struct()
	if err != nil {
		return nil, wrapRPCError(err)
	}
	result := response.Result()
	switch result.Which() {
	case tunnelrpc.ConnectionResponse_result_Which_error:
		resultError, err := result.Error()
		if err != nil {
			return nil, wrapRPCError(err)
		}
		cause, err := resultError.Cause()
		if err != nil {
			return nil, wrapRPCError(err)
		}
		err = errors.New(cause)
		if resultError.ShouldRetry() {
			err = RetryErrorAfter(err, time.Duration(resultError.RetryAfter()))
		}
		return nil, err

	case tunnelrpc.ConnectionResponse_result_Which_connectionDetails:
		connDetails, err := result.ConnectionDetails()
		if err != nil {
			return nil, wrapRPCError(err)
		}
		details := new(ConnectionDetails)
		if err = details.UnmarshalCapnproto(connDetails); err != nil {
			return nil, wrapRPCError(err)
		}
		return details, nil
	}

	return nil, newRPCError("unknown result which %d", result.Which())
}

func (c RegistrationServer_PogsClient) SendLocalConfiguration(ctx context.Context, config []byte) error {
	client := tunnelrpc.TunnelServer{Client: c.Client}
	promise := client.UpdateLocalConfiguration(ctx, func(p tunnelrpc.RegistrationServer_updateLocalConfiguration_Params) error {
		if err := p.SetConfig(config); err != nil {
			return err
		}

		return nil
	})

	_, err := promise.Struct()
	if err != nil {
		return wrapRPCError(err)
	}

	return nil
}

func (c RegistrationServer_PogsClient) UnregisterConnection(ctx context.Context) error {
	client := tunnelrpc.TunnelServer{Client: c.Client}
	promise := client.UnregisterConnection(ctx, func(p tunnelrpc.RegistrationServer_unregisterConnection_Params) error {
		return nil
	})
	_, err := promise.Struct()
	if err != nil {
		return wrapRPCError(err)
	}
	return nil
}

type ClientInfo struct {
	ClientID []byte `capnp:"clientId"` // must be a slice for capnp compatibility
	Features []string
	Version  string
	Arch     string
}

type ConnectionOptions struct {
	Client              ClientInfo
	OriginLocalIP       net.IP `capnp:"originLocalIp"`
	ReplaceExisting     bool
	CompressionQuality  uint8
	NumPreviousAttempts uint8
}

type TunnelAuth struct {
	AccountTag   string
	TunnelSecret []byte
}

func (p *ConnectionOptions) MarshalCapnproto(s tunnelrpc.ConnectionOptions) error {
	return pogs.Insert(tunnelrpc.ConnectionOptions_TypeID, s.Struct, p)
}

func (p *ConnectionOptions) UnmarshalCapnproto(s tunnelrpc.ConnectionOptions) error {
	return pogs.Extract(p, tunnelrpc.ConnectionOptions_TypeID, s.Struct)
}

func (a *TunnelAuth) MarshalCapnproto(s tunnelrpc.TunnelAuth) error {
	return pogs.Insert(tunnelrpc.TunnelAuth_TypeID, s.Struct, a)
}

func (a *TunnelAuth) UnmarshalCapnproto(s tunnelrpc.TunnelAuth) error {
	return pogs.Extract(a, tunnelrpc.TunnelAuth_TypeID, s.Struct)
}

type ConnectionDetails struct {
	UUID                    uuid.UUID
	Location                string
	TunnelIsRemotelyManaged bool
}

func (details *ConnectionDetails) MarshalCapnproto(s tunnelrpc.ConnectionDetails) error {
	if err := s.SetUuid(details.UUID[:]); err != nil {
		return err
	}
	if err := s.SetLocationName(details.Location); err != nil {
		return err
	}
	s.SetTunnelIsRemotelyManaged(details.TunnelIsRemotelyManaged)

	return nil
}

func (details *ConnectionDetails) UnmarshalCapnproto(s tunnelrpc.ConnectionDetails) error {
	uuidBytes, err := s.Uuid()
	if err != nil {
		return err
	}
	details.UUID, err = uuid.FromBytes(uuidBytes)
	if err != nil {
		return err
	}
	details.Location, err = s.LocationName()
	if err != nil {
		return err
	}
	details.TunnelIsRemotelyManaged = s.TunnelIsRemotelyManaged()

	return err
}

func MarshalError(s tunnelrpc.ConnectionError, err error) error {
	if err := s.SetCause(err.Error()); err != nil {
		return err
	}
	if retryableErr, ok := err.(*RetryableError); ok {
		s.SetShouldRetry(true)
		s.SetRetryAfter(int64(retryableErr.Delay))
	}

	return nil
}
