package pogs

import (
	"context"
	"time"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/google/uuid"

	log "github.com/sirupsen/logrus"
	"zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/pogs"
	"zombiezen.com/go/capnproto2/rpc"
	"zombiezen.com/go/capnproto2/server"
)

type Authentication struct {
	Key         string
	Email       string
	OriginCAKey string
}

func MarshalAuthentication(s tunnelrpc.Authentication, p *Authentication) error {
	return pogs.Insert(tunnelrpc.Authentication_TypeID, s.Struct, p)
}

func UnmarshalAuthentication(s tunnelrpc.Authentication) (*Authentication, error) {
	p := new(Authentication)
	err := pogs.Extract(p, tunnelrpc.Authentication_TypeID, s.Struct)
	return p, err
}

type TunnelRegistration struct {
	Err              string
	Url              string
	LogLines         []string
	PermanentFailure bool
	TunnelID         string `capnp:"tunnelID"`
}

func MarshalTunnelRegistration(s tunnelrpc.TunnelRegistration, p *TunnelRegistration) error {
	return pogs.Insert(tunnelrpc.TunnelRegistration_TypeID, s.Struct, p)
}

func UnmarshalTunnelRegistration(s tunnelrpc.TunnelRegistration) (*TunnelRegistration, error) {
	p := new(TunnelRegistration)
	err := pogs.Extract(p, tunnelrpc.TunnelRegistration_TypeID, s.Struct)
	return p, err
}

type RegistrationOptions struct {
	ClientID             string `capnp:"clientId"`
	Version              string
	OS                   string `capnp:"os"`
	ExistingTunnelPolicy tunnelrpc.ExistingTunnelPolicy
	PoolName             string `capnp:"poolName"`
	Tags                 []Tag
	ConnectionID         uint8  `capnp:"connectionId"`
	OriginLocalIP        string `capnp:"originLocalIp"`
	IsAutoupdated        bool   `capnp:"isAutoupdated"`
	RunFromTerminal      bool   `capnp:"runFromTerminal"`
	CompressionQuality   uint64 `capnp:"compressionQuality"`
	UUID                 string `capnp:"uuid"`
}

func MarshalRegistrationOptions(s tunnelrpc.RegistrationOptions, p *RegistrationOptions) error {
	return pogs.Insert(tunnelrpc.RegistrationOptions_TypeID, s.Struct, p)
}

func UnmarshalRegistrationOptions(s tunnelrpc.RegistrationOptions) (*RegistrationOptions, error) {
	p := new(RegistrationOptions)
	err := pogs.Extract(p, tunnelrpc.RegistrationOptions_TypeID, s.Struct)
	return p, err
}

type ServerHello struct {
	ConnectResult *ConnectResult
	CloudflaredID uuid.UUID
}

// CapnpServerHello is ServerHello respresented in Cap'n Proto build-in types
type CapnpServerHello struct {
	ConnectResult *ConnectResult
	CloudflaredID []byte
}

func MarshalServerHello(s tunnelrpc.CapnpServerHello, p *ServerHello) error {
	cloudflaredIDBytes, err := p.CloudflaredID.MarshalBinary()
	if err != nil {
		return err
	}
	capnpServerHello := &CapnpServerHello{
		ConnectResult: p.ConnectResult,
		CloudflaredID: cloudflaredIDBytes,
	}
	return pogs.Insert(tunnelrpc.CapnpServerHello_TypeID, s.Struct, capnpServerHello)
}

func UnmarshalServerHello(s tunnelrpc.CapnpServerHello) (*ServerHello, error) {
	p := new(CapnpServerHello)
	err := pogs.Extract(p, tunnelrpc.CapnpServerHello_TypeID, s.Struct)
	if err != nil {
		return nil, err
	}
	cloudflaredID, err := uuid.FromBytes(p.CloudflaredID)
	if err != nil {
		log.Errorf("fail to unmarshal %+v", p.CloudflaredID)
		return nil, err
	}
	return &ServerHello{
		ConnectResult: p.ConnectResult,
		CloudflaredID: cloudflaredID,
	}, nil
}

type ConnectResult struct {
	Err        *ConnectError
	ServerInfo ServerInfo
}

func MarshalConnectResult(s tunnelrpc.ConnectResult, p *ConnectResult) error {
	return pogs.Insert(tunnelrpc.ConnectResult_TypeID, s.Struct, p)
}

func UnmarshalConnectResult(s tunnelrpc.ConnectResult) (*ConnectResult, error) {
	p := new(ConnectResult)
	err := pogs.Extract(p, tunnelrpc.ConnectResult_TypeID, s.Struct)
	return p, err
}

type ConnectError struct {
	Cause      string
	RetryAfter time.Duration
}

func MarshalConnectError(s tunnelrpc.ConnectError, p *ConnectError) error {
	return pogs.Insert(tunnelrpc.ConnectError_TypeID, s.Struct, p)
}

func UnmarshalConnectError(s tunnelrpc.ConnectError) (*ConnectError, error) {
	p := new(ConnectError)
	err := pogs.Extract(p, tunnelrpc.ConnectError_TypeID, s.Struct)
	return p, err
}

func (e *ConnectError) Error() string {
	return e.Cause
}

type Tag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ServerInfo struct {
	LocationName string
}

func MarshalServerInfo(s tunnelrpc.ServerInfo, p *ServerInfo) error {
	return pogs.Insert(tunnelrpc.ServerInfo_TypeID, s.Struct, p)
}

func UnmarshalServerInfo(s tunnelrpc.ServerInfo) (*ServerInfo, error) {
	p := new(ServerInfo)
	err := pogs.Extract(p, tunnelrpc.ServerInfo_TypeID, s.Struct)
	return p, err
}

type HelloParameters struct {
	OriginCert          []byte
	Tags                []Tag
	NumPreviousAttempts uint8
}

func MarshalHelloParameters(s tunnelrpc.HelloParameters, p *HelloParameters) error {
	return pogs.Insert(tunnelrpc.HelloParameters_TypeID, s.Struct, p)
}

func UnmarshalHelloParameters(s tunnelrpc.HelloParameters) (*HelloParameters, error) {
	p := new(HelloParameters)
	err := pogs.Extract(p, tunnelrpc.HelloParameters_TypeID, s.Struct)
	return p, err
}

type ConnectParameters struct {
	OriginCert          []byte
	CloudflaredID       uuid.UUID
	NumPreviousAttempts uint8
}

// CapnpConnectParameters is ConnectParameters represented in Cap'n Proto build-in types
type CapnpConnectParameters struct {
	OriginCert          []byte
	CloudflaredID       []byte
	NumPreviousAttempts uint8
}

func MarshalConnectParameters(s tunnelrpc.CapnpConnectParameters, p *ConnectParameters) error {
	cloudflaredIDBytes, err := p.CloudflaredID.MarshalBinary()
	if err != nil {
		return err
	}
	capnpConnectParameters := &CapnpConnectParameters{
		OriginCert:          p.OriginCert,
		CloudflaredID:       cloudflaredIDBytes,
		NumPreviousAttempts: p.NumPreviousAttempts,
	}
	return pogs.Insert(tunnelrpc.CapnpConnectParameters_TypeID, s.Struct, capnpConnectParameters)
}

func UnmarshalConnectParameters(s tunnelrpc.CapnpConnectParameters) (*ConnectParameters, error) {
	p := new(CapnpConnectParameters)
	err := pogs.Extract(p, tunnelrpc.CapnpConnectParameters_TypeID, s.Struct)
	if err != nil {
		return nil, err
	}
	cloudflaredID, err := uuid.FromBytes(p.CloudflaredID)
	if err != nil {
		return nil, err
	}
	return &ConnectParameters{
		OriginCert:          p.OriginCert,
		CloudflaredID:       cloudflaredID,
		NumPreviousAttempts: p.NumPreviousAttempts,
	}, nil
}

type TunnelServer interface {
	RegisterTunnel(ctx context.Context, originCert []byte, hostname string, options *RegistrationOptions) (*TunnelRegistration, error)
	GetServerInfo(ctx context.Context) (*ServerInfo, error)
	UnregisterTunnel(ctx context.Context, gracePeriodNanoSec int64) error
	Hello(ctx context.Context, parameters *HelloParameters) (*ServerHello, error)
	Connect(ctx context.Context, paramaters *ConnectParameters) (*ConnectResult, error)
}

func TunnelServer_ServerToClient(s TunnelServer) tunnelrpc.TunnelServer {
	return tunnelrpc.TunnelServer_ServerToClient(TunnelServer_PogsImpl{s})
}

type TunnelServer_PogsImpl struct {
	impl TunnelServer
}

func (i TunnelServer_PogsImpl) RegisterTunnel(p tunnelrpc.TunnelServer_registerTunnel) error {
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
	registration, err := i.impl.RegisterTunnel(p.Ctx, originCert, hostname, pogsOptions)
	if err != nil {
		return err
	}
	result, err := p.Results.NewResult()
	if err != nil {
		return err
	}
	log.Info(registration.TunnelID)
	return MarshalTunnelRegistration(result, registration)
}

func (i TunnelServer_PogsImpl) GetServerInfo(p tunnelrpc.TunnelServer_getServerInfo) error {
	server.Ack(p.Options)
	serverInfo, err := i.impl.GetServerInfo(p.Ctx)
	if err != nil {
		return err
	}
	result, err := p.Results.NewResult()
	if err != nil {
		return err
	}
	return MarshalServerInfo(result, serverInfo)
}

func (i TunnelServer_PogsImpl) UnregisterTunnel(p tunnelrpc.TunnelServer_unregisterTunnel) error {
	gracePeriodNanoSec := p.Params.GracePeriodNanoSec()
	server.Ack(p.Options)
	return i.impl.UnregisterTunnel(p.Ctx, gracePeriodNanoSec)
}

func (i TunnelServer_PogsImpl) Hello(p tunnelrpc.TunnelServer_hello) error {
	parameters, err := p.Params.Parameters()
	if err != nil {
		return err
	}
	pogsParameters, err := UnmarshalHelloParameters(parameters)
	if err != nil {
		return err
	}
	server.Ack(p.Options)
	serverHello, err := i.impl.Hello(p.Ctx, pogsParameters)
	if err != nil {
		return err
	}
	result, err := p.Results.NewResult()
	if err != nil {
		return err
	}
	return MarshalServerHello(result, serverHello)
}

func (i TunnelServer_PogsImpl) Connect(p tunnelrpc.TunnelServer_connect) error {
	paramaters, err := p.Params.Parameters()
	if err != nil {
		return err
	}
	pogsParameters, err := UnmarshalConnectParameters(paramaters)
	if err != nil {
		return err
	}
	server.Ack(p.Options)
	connectResult, err := i.impl.Connect(p.Ctx, pogsParameters)
	if err != nil {
		return err
	}
	result, err := p.Results.NewResult()
	if err != nil {
		return err
	}
	return MarshalConnectResult(result, connectResult)
}

type TunnelServer_PogsClient struct {
	Client capnp.Client
	Conn   *rpc.Conn
}

func (c TunnelServer_PogsClient) Close() error {
	return c.Conn.Close()
}

func (c TunnelServer_PogsClient) RegisterTunnel(ctx context.Context, originCert []byte, hostname string, options *RegistrationOptions) (*TunnelRegistration, error) {
	client := tunnelrpc.TunnelServer{Client: c.Client}
	promise := client.RegisterTunnel(ctx, func(p tunnelrpc.TunnelServer_registerTunnel_Params) error {
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
	return UnmarshalTunnelRegistration(retval)
}

func (c TunnelServer_PogsClient) GetServerInfo(ctx context.Context) (*ServerInfo, error) {
	client := tunnelrpc.TunnelServer{Client: c.Client}
	promise := client.GetServerInfo(ctx, func(p tunnelrpc.TunnelServer_getServerInfo_Params) error {
		return nil
	})
	retval, err := promise.Result().Struct()
	if err != nil {
		return nil, err
	}
	return UnmarshalServerInfo(retval)
}

func (c TunnelServer_PogsClient) UnregisterTunnel(ctx context.Context, gracePeriodNanoSec int64) error {
	client := tunnelrpc.TunnelServer{Client: c.Client}
	promise := client.UnregisterTunnel(ctx, func(p tunnelrpc.TunnelServer_unregisterTunnel_Params) error {
		p.SetGracePeriodNanoSec(gracePeriodNanoSec)
		return nil
	})
	_, err := promise.Struct()
	return err
}

func (c TunnelServer_PogsClient) Hello(ctx context.Context,
	parameters *HelloParameters,
) (*ServerHello, error) {
	client := tunnelrpc.TunnelServer{Client: c.Client}
	promise := client.Hello(ctx, func(p tunnelrpc.TunnelServer_hello_Params) error {
		helloParameters, err := p.NewParameters()
		if err != nil {
			return err
		}
		err = MarshalHelloParameters(helloParameters, parameters)
		if err != nil {
			return err
		}
		return nil
	})
	retval, err := promise.Result().Struct()
	if err != nil {
		return nil, err
	}
	return UnmarshalServerHello(retval)
}

func (c TunnelServer_PogsClient) Connect(ctx context.Context,
	parameters *ConnectParameters,
) (*ConnectResult, error) {
	client := tunnelrpc.TunnelServer{Client: c.Client}
	promise := client.Connect(ctx, func(p tunnelrpc.TunnelServer_connect_Params) error {
		connectParameters, err := p.NewParameters()
		if err != nil {
			return err
		}
		err = MarshalConnectParameters(connectParameters, parameters)
		if err != nil {
			return err
		}
		return nil
	})
	retval, err := promise.Result().Struct()
	if err != nil {
		return nil, err
	}
	return UnmarshalConnectResult(retval)
}
