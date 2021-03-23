package pogs

import (
	"context"
	"fmt"

	capnp "zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/pogs"
	"zombiezen.com/go/capnproto2/rpc"
	"zombiezen.com/go/capnproto2/server"

	"github.com/cloudflare/cloudflared/tunnelrpc"
)

const (
	defaultRetryAfterSeconds = 15
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
	SuccessfulTunnelRegistration
	Err               string
	PermanentFailure  bool
	RetryAfterSeconds uint16
}

type SuccessfulTunnelRegistration struct {
	Url         string
	LogLines    []string
	TunnelID    string `capnp:"tunnelID"`
	EventDigest []byte
	ConnDigest  []byte
}

func NewSuccessfulTunnelRegistration(
	url string,
	logLines []string,
	tunnelID string,
	eventDigest []byte,
	connDigest []byte,
) *TunnelRegistration {
	// Marshal nil will result in an error
	if logLines == nil {
		logLines = []string{}
	}
	return &TunnelRegistration{
		SuccessfulTunnelRegistration: SuccessfulTunnelRegistration{
			Url:         url,
			LogLines:    logLines,
			TunnelID:    tunnelID,
			EventDigest: eventDigest,
			ConnDigest:  connDigest,
		},
	}
}

// Not calling this function Error() to avoid confusion with implementing error interface
func (tr TunnelRegistration) DeserializeError() TunnelRegistrationError {
	if tr.Err != "" {
		err := fmt.Errorf(tr.Err)
		if tr.PermanentFailure {
			return NewPermanentRegistrationError(err)
		}
		retryAfterSeconds := tr.RetryAfterSeconds
		if retryAfterSeconds < defaultRetryAfterSeconds {
			retryAfterSeconds = defaultRetryAfterSeconds
		}
		return NewRetryableRegistrationError(err, retryAfterSeconds)
	}
	return nil
}

type TunnelRegistrationError interface {
	error
	Serialize() *TunnelRegistration
	IsPermanent() bool
}

type PermanentRegistrationError struct {
	err string
}

func NewPermanentRegistrationError(err error) TunnelRegistrationError {
	return &PermanentRegistrationError{
		err: err.Error(),
	}
}

func (pre *PermanentRegistrationError) Error() string {
	return pre.err
}

func (pre *PermanentRegistrationError) Serialize() *TunnelRegistration {
	return &TunnelRegistration{
		Err:              pre.err,
		PermanentFailure: true,
	}
}

func (*PermanentRegistrationError) IsPermanent() bool {
	return true
}

type RetryableRegistrationError struct {
	err               string
	retryAfterSeconds uint16
}

func NewRetryableRegistrationError(err error, retryAfterSeconds uint16) TunnelRegistrationError {
	return &RetryableRegistrationError{
		err:               err.Error(),
		retryAfterSeconds: retryAfterSeconds,
	}
}

func (rre *RetryableRegistrationError) Error() string {
	return rre.err
}

func (rre *RetryableRegistrationError) Serialize() *TunnelRegistration {
	return &TunnelRegistration{
		Err:               rre.err,
		PermanentFailure:  false,
		RetryAfterSeconds: rre.retryAfterSeconds,
	}
}

func (*RetryableRegistrationError) IsPermanent() bool {
	return false
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
	NumPreviousAttempts  uint8
	Features             []string
}

func MarshalRegistrationOptions(s tunnelrpc.RegistrationOptions, p *RegistrationOptions) error {
	return pogs.Insert(tunnelrpc.RegistrationOptions_TypeID, s.Struct, p)
}

func UnmarshalRegistrationOptions(s tunnelrpc.RegistrationOptions) (*RegistrationOptions, error) {
	p := new(RegistrationOptions)
	err := pogs.Extract(p, tunnelrpc.RegistrationOptions_TypeID, s.Struct)
	return p, err
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

type TunnelServer interface {
	RegistrationServer
	RegisterTunnel(ctx context.Context, originCert []byte, hostname string, options *RegistrationOptions) *TunnelRegistration
	GetServerInfo(ctx context.Context) (*ServerInfo, error)
	UnregisterTunnel(ctx context.Context, gracePeriodNanoSec int64) error
	Authenticate(ctx context.Context, originCert []byte, hostname string, options *RegistrationOptions) (*AuthenticateResponse, error)
	ReconnectTunnel(ctx context.Context, jwt, eventDigest, connDigest []byte, hostname string, options *RegistrationOptions) (*TunnelRegistration, error)
}

func TunnelServer_ServerToClient(s TunnelServer) tunnelrpc.TunnelServer {
	return tunnelrpc.TunnelServer_ServerToClient(TunnelServer_PogsImpl{RegistrationServer_PogsImpl{s}, s})
}

type TunnelServer_PogsImpl struct {
	RegistrationServer_PogsImpl
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
	registration := i.impl.RegisterTunnel(p.Ctx, originCert, hostname, pogsOptions)

	result, err := p.Results.NewResult()
	if err != nil {
		return err
	}
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

func (i TunnelServer_PogsImpl) ObsoleteDeclarativeTunnelConnect(p tunnelrpc.TunnelServer_obsoleteDeclarativeTunnelConnect) error {
	return fmt.Errorf("RPC to create declarative tunnel connection has been deprecated")
}

type TunnelServer_PogsClient struct {
	RegistrationServer_PogsClient
	Client capnp.Client
	Conn   *rpc.Conn
}

func (c TunnelServer_PogsClient) Close() error {
	c.Client.Close()
	return c.Conn.Close()
}

func (c TunnelServer_PogsClient) RegisterTunnel(ctx context.Context, originCert []byte, hostname string, options *RegistrationOptions) *TunnelRegistration {
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
		return NewRetryableRegistrationError(err, defaultRetryAfterSeconds).Serialize()
	}
	registration, err := UnmarshalTunnelRegistration(retval)
	if err != nil {
		return NewRetryableRegistrationError(err, defaultRetryAfterSeconds).Serialize()
	}
	return registration
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
