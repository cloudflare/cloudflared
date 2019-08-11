package pogs

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/google/uuid"

	log "github.com/sirupsen/logrus"
	capnp "zombiezen.com/go/capnproto2"
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
	Cause       string
	RetryAfter  time.Duration
	ShouldRetry bool
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

//go-sumtype:decl Scope
type Scope interface {
	Value() string
	PostgresType() string
	GraphQLType() string
	isScope()
}

type SystemName struct {
	systemName string
}

func NewSystemName(systemName string) *SystemName {
	return &SystemName{systemName: systemName}
}

func (s *SystemName) Value() string        { return s.systemName }
func (_ *SystemName) PostgresType() string { return "system_name" }
func (_ *SystemName) GraphQLType() string { return "SYSTEM_NAME" }

func (_ *SystemName) isScope() {}

type Group struct {
	group string
}

func NewGroup(group string) *Group {
	return &Group{group: group}
}

func (g *Group) Value() string        { return g.group }
func (_ *Group) PostgresType() string { return "group" }
func (_ *Group) GraphQLType() string { return "GROUP" }

func (_ *Group) isScope() {}

func MarshalScope(s tunnelrpc.Scope, p Scope) error {
	ss := s.Value()
	switch scope := p.(type) {
	case *SystemName:
		ss.SetSystemName(scope.systemName)
	case *Group:
		ss.SetGroup(scope.group)
	default:
		return fmt.Errorf("unexpected Scope value: %v", p)
	}
	return nil
}

func UnmarshalScope(s tunnelrpc.Scope) (Scope, error) {
	ss := s.Value()
	switch ss.Which() {
	case tunnelrpc.Scope_value_Which_systemName:
		systemName, err := ss.SystemName()
		if err != nil {
			return nil, err
		}
		return NewSystemName(systemName), nil
	case tunnelrpc.Scope_value_Which_group:
		group, err := ss.Group()
		if err != nil {
			return nil, err
		}
		return NewGroup(group), nil
	default:
		return nil, fmt.Errorf("unexpected Scope tag: %v", ss.Which())
	}
}

type ConnectParameters struct {
	OriginCert          []byte
	CloudflaredID       uuid.UUID
	NumPreviousAttempts uint8
	Tags                []Tag
	CloudflaredVersion  string
	Scope               Scope
}

func MarshalConnectParameters(s tunnelrpc.CapnpConnectParameters, p *ConnectParameters) error {
	if err := s.SetOriginCert(p.OriginCert); err != nil {
		return err
	}
	cloudflaredIDBytes, err := p.CloudflaredID.MarshalBinary()
	if err != nil {
		return err
	}
	if err := s.SetCloudflaredID(cloudflaredIDBytes); err != nil {
		return err
	}
	s.SetNumPreviousAttempts(p.NumPreviousAttempts)
	if len(p.Tags) > 0 {
		tagsCapnpList, err := s.NewTags(int32(len(p.Tags)))
		if err != nil {
			return err
		}
		for i, tag := range p.Tags {
			tagCapnp := tagsCapnpList.At(i)
			if err := tagCapnp.SetName(tag.Name); err != nil {
				return err
			}
			if err := tagCapnp.SetValue(tag.Value); err != nil {
				return err
			}
		}
	}
	if err := s.SetCloudflaredVersion(p.CloudflaredVersion); err != nil {
		return err
	}
	scope, err := s.NewScope()
	if err != nil {
		return err
	}
	return MarshalScope(scope, p.Scope)
}

func UnmarshalConnectParameters(s tunnelrpc.CapnpConnectParameters) (*ConnectParameters, error) {
	originCert, err := s.OriginCert()
	if err != nil {
		return nil, err
	}

	cloudflaredIDBytes, err := s.CloudflaredID()
	if err != nil {
		return nil, err
	}
	cloudflaredID, err := uuid.FromBytes(cloudflaredIDBytes)
	if err != nil {
		return nil, err
	}

	tagsCapnpList, err := s.Tags()
	if err != nil {
		return nil, err
	}
	var tags []Tag
	for i := 0; i < tagsCapnpList.Len(); i++ {
		tagCapnp := tagsCapnpList.At(i)
		name, err := tagCapnp.Name()
		if err != nil {
			return nil, err
		}
		value, err := tagCapnp.Value()
		if err != nil {
			return nil, err
		}
		tags = append(tags, Tag{Name: name, Value: value})
	}

	cloudflaredVersion, err := s.CloudflaredVersion()
	if err != nil {
		return nil, err
	}

	scopeCapnp, err := s.Scope()
	if err != nil {
		return nil, err
	}
	scope, err := UnmarshalScope(scopeCapnp)
	if err != nil {
		return nil, err
	}

	return &ConnectParameters{
		OriginCert:          originCert,
		CloudflaredID:       cloudflaredID,
		NumPreviousAttempts: s.NumPreviousAttempts(),
		Tags:                tags,
		CloudflaredVersion:  cloudflaredVersion,
		Scope:               scope,
	}, nil
}

type TunnelServer interface {
	RegisterTunnel(ctx context.Context, originCert []byte, hostname string, options *RegistrationOptions) (*TunnelRegistration, error)
	GetServerInfo(ctx context.Context) (*ServerInfo, error)
	UnregisterTunnel(ctx context.Context, gracePeriodNanoSec int64) error
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
