package pogs

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/google/uuid"
	capnp "zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/rpc"
	"zombiezen.com/go/capnproto2/server"
)

type SessionManager interface {
	RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeAfterIdleHint time.Duration) error
	UnregisterUdpSession(ctx context.Context, sessionID uuid.UUID, message string) error
}

type SessionManager_PogsImpl struct {
	impl SessionManager
}

func SessionManager_ServerToClient(s SessionManager) tunnelrpc.SessionManager {
	return tunnelrpc.SessionManager_ServerToClient(SessionManager_PogsImpl{s})
}

func (i SessionManager_PogsImpl) RegisterUdpSession(p tunnelrpc.SessionManager_registerUdpSession) error {
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

	resp := RegisterUdpSessionResponse{}
	registrationErr := i.impl.RegisterUdpSession(p.Ctx, sessionID, dstIP, dstPort, closeIdleAfterHint)
	if registrationErr != nil {
		resp.Err = registrationErr
	}

	result, err := p.Results.NewResult()
	if err != nil {
		return err
	}

	return resp.Marshal(result)
}

func (i SessionManager_PogsImpl) UnregisterUdpSession(p tunnelrpc.SessionManager_unregisterUdpSession) error {
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
	Err error
}

func (p *RegisterUdpSessionResponse) Marshal(s tunnelrpc.RegisterUdpSessionResponse) error {
	if p.Err != nil {
		return s.SetErr(p.Err.Error())
	}
	return nil
}

func (p *RegisterUdpSessionResponse) Unmarshal(s tunnelrpc.RegisterUdpSessionResponse) error {
	respErr, err := s.Err()
	if err != nil {
		return err
	}
	if respErr != "" {
		p.Err = fmt.Errorf(respErr)
	}
	return nil
}

type SessionManager_PogsClient struct {
	Client capnp.Client
	Conn   *rpc.Conn
}

func (c SessionManager_PogsClient) Close() error {
	c.Client.Close()
	return c.Conn.Close()
}

func (c SessionManager_PogsClient) RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeAfterIdleHint time.Duration) (*RegisterUdpSessionResponse, error) {
	client := tunnelrpc.SessionManager{Client: c.Client}
	promise := client.RegisterUdpSession(ctx, func(p tunnelrpc.SessionManager_registerUdpSession_Params) error {
		if err := p.SetSessionId(sessionID[:]); err != nil {
			return err
		}
		if err := p.SetDstIp(dstIP); err != nil {
			return err
		}
		p.SetDstPort(dstPort)
		p.SetCloseAfterIdleHint(int64(closeAfterIdleHint))
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
	client := tunnelrpc.SessionManager{Client: c.Client}
	promise := client.UnregisterUdpSession(ctx, func(p tunnelrpc.SessionManager_unregisterUdpSession_Params) error {
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
