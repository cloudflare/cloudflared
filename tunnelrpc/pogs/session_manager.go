package pogs

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	capnp "zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/rpc"
	"zombiezen.com/go/capnproto2/server"

	"github.com/cloudflare/cloudflared/tunnelrpc/metrics"
	"github.com/cloudflare/cloudflared/tunnelrpc/proto"
)

type SessionManager interface {
	// RegisterUdpSession is the call provided to cloudflared to handle an incoming
	// capnproto RegisterUdpSession request from the edge.
	RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeAfterIdleHint time.Duration, traceContext string) (*RegisterUdpSessionResponse, error)
	// UnregisterUdpSession is the call provided to cloudflared to handle an incoming
	// capnproto UnregisterUdpSession request from the edge.
	UnregisterUdpSession(ctx context.Context, sessionID uuid.UUID, message string) error
}

type SessionManager_PogsImpl struct {
	impl SessionManager
}

func SessionManager_ServerToClient(s SessionManager) proto.SessionManager {
	return proto.SessionManager_ServerToClient(SessionManager_PogsImpl{s})
}

func (i SessionManager_PogsImpl) RegisterUdpSession(p proto.SessionManager_registerUdpSession) error {
	return metrics.ObserveServerHandler(func() error { return i.registerUdpSession(p) }, metrics.SessionManager, metrics.OperationRegisterUdpSession)
}

func (i SessionManager_PogsImpl) registerUdpSession(p proto.SessionManager_registerUdpSession) error {
	server.Ack(p.Options)

	sessionIDRaw, err := p.Params.SessionId()
	if err != nil {
		return err
	}
	sessionID, err := uuid.FromBytes(sessionIDRaw)
	if err != nil {
		return err
	}

	dstIPRaw, err := p.Params.DstIp()
	if err != nil {
		return err
	}
	dstIP := net.IP(dstIPRaw)
	if dstIP == nil {
		return fmt.Errorf("%v is not valid IP", dstIPRaw)
	}
	dstPort := p.Params.DstPort()

	closeIdleAfterHint := time.Duration(p.Params.CloseAfterIdleHint())

	traceContext, err := p.Params.TraceContext()
	if err != nil {
		return err
	}

	resp, registrationErr := i.impl.RegisterUdpSession(p.Ctx, sessionID, dstIP, dstPort, closeIdleAfterHint, traceContext)
	if registrationErr != nil {
		// Make sure to assign a response even if one is not returned from register
		if resp == nil {
			resp = &RegisterUdpSessionResponse{}
		}
		resp.Err = registrationErr
	}

	result, err := p.Results.NewResult()
	if err != nil {
		return err
	}

	return resp.Marshal(result)
}

func (i SessionManager_PogsImpl) UnregisterUdpSession(p proto.SessionManager_unregisterUdpSession) error {
	return metrics.ObserveServerHandler(func() error { return i.unregisterUdpSession(p) }, metrics.SessionManager, metrics.OperationUnregisterUdpSession)
}

func (i SessionManager_PogsImpl) unregisterUdpSession(p proto.SessionManager_unregisterUdpSession) error {
	server.Ack(p.Options)

	sessionIDRaw, err := p.Params.SessionId()
	if err != nil {
		return err
	}
	sessionID, err := uuid.FromBytes(sessionIDRaw)
	if err != nil {
		return err
	}

	message, err := p.Params.Message()
	if err != nil {
		return err
	}

	return i.impl.UnregisterUdpSession(p.Ctx, sessionID, message)
}

type RegisterUdpSessionResponse struct {
	Err   error
	Spans []byte // Spans in protobuf format
}

func (p *RegisterUdpSessionResponse) Marshal(s proto.RegisterUdpSessionResponse) error {
	if p.Err != nil {
		return s.SetErr(p.Err.Error())
	}
	if err := s.SetSpans(p.Spans); err != nil {
		return err
	}
	return nil
}

func (p *RegisterUdpSessionResponse) Unmarshal(s proto.RegisterUdpSessionResponse) error {
	respErr, err := s.Err()
	if err != nil {
		return err
	}
	if respErr != "" {
		p.Err = fmt.Errorf("%s", respErr)
	}
	p.Spans, err = s.Spans()
	if err != nil {
		return err
	}
	return nil
}

type SessionManager_PogsClient struct {
	Client capnp.Client
	Conn   *rpc.Conn
}

func NewSessionManager_PogsClient(client capnp.Client, conn *rpc.Conn) SessionManager_PogsClient {
	return SessionManager_PogsClient{
		Client: client,
		Conn:   conn,
	}
}

func (c SessionManager_PogsClient) Close() error {
	c.Client.Close()
	return c.Conn.Close()
}

func (c SessionManager_PogsClient) RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeAfterIdleHint time.Duration, traceContext string) (*RegisterUdpSessionResponse, error) {
	client := proto.SessionManager{Client: c.Client}
	promise := client.RegisterUdpSession(ctx, func(p proto.SessionManager_registerUdpSession_Params) error {
		if err := p.SetSessionId(sessionID[:]); err != nil {
			return err
		}
		if err := p.SetDstIp(dstIP); err != nil {
			return err
		}
		p.SetDstPort(dstPort)
		p.SetCloseAfterIdleHint(int64(closeAfterIdleHint))
		_ = p.SetTraceContext(traceContext)
		return nil
	})
	result, err := promise.Result().Struct()
	if err != nil {
		return nil, wrapRPCError(err)
	}
	response := new(RegisterUdpSessionResponse)

	err = response.Unmarshal(result)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (c SessionManager_PogsClient) UnregisterUdpSession(ctx context.Context, sessionID uuid.UUID, message string) error {
	client := proto.SessionManager{Client: c.Client}
	promise := client.UnregisterUdpSession(ctx, func(p proto.SessionManager_unregisterUdpSession_Params) error {
		if err := p.SetSessionId(sessionID[:]); err != nil {
			return err
		}
		if err := p.SetMessage(message); err != nil {
			return err
		}
		return nil
	})
	_, err := promise.Struct()
	return err
}
