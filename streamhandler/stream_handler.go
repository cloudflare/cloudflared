package streamhandler

import (
	"fmt"
	"net/http"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/tunnelhostnamemapper"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/sirupsen/logrus"
)

// StreamHandler handles new stream opened by the edge. The streams can be used to proxy requests or make RPC.
type StreamHandler struct {
	// newConfigChan is a send-only channel to notify Supervisor of a new ClientConfig
	newConfigChan chan<- *pogs.ClientConfig
	// useConfigResultChan is a receive-only channel for Supervisor to communicate the result of applying a new ClientConfig
	useConfigResultChan <-chan *pogs.UseConfigurationResult
	// originMapper maps tunnel hostname to origin service
	tunnelHostnameMapper *tunnelhostnamemapper.TunnelHostnameMapper
	logger               *logrus.Entry
}

// NewStreamHandler creates a new StreamHandler
func NewStreamHandler(newConfigChan chan<- *pogs.ClientConfig,
	useConfigResultChan <-chan *pogs.UseConfigurationResult,
	logger *logrus.Logger,
) *StreamHandler {
	return &StreamHandler{
		newConfigChan:        newConfigChan,
		useConfigResultChan:  useConfigResultChan,
		tunnelHostnameMapper: tunnelhostnamemapper.NewTunnelHostnameMapper(),
		logger:               logger.WithField("subsystem", "streamHandler"),
	}
}

// ServeStream implements MuxedStreamHandler interface
func (s *StreamHandler) ServeStream(stream *h2mux.MuxedStream) error {
	if stream.IsRPCStream() {
		return fmt.Errorf("serveRPC not implemented")
	}
	return s.serveRequest(stream)
}

func (s *StreamHandler) serveRequest(stream *h2mux.MuxedStream) error {
	tunnelHostname := stream.TunnelHostname()
	if !tunnelHostname.IsSet() {
		err := fmt.Errorf("stream doesn't have tunnelHostname")
		s.logger.Error(err)
		return err
	}

	originService, ok := s.tunnelHostnameMapper.Get(tunnelHostname)
	if !ok {
		err := fmt.Errorf("cannot map tunnel hostname %s to origin", tunnelHostname)
		s.logger.Error(err)
		return err
	}

	req, err := CreateRequest(stream, originService.OriginAddr())
	if err != nil {
		return err
	}

	logger := s.requestLogger(req, tunnelHostname)
	logger.Debugf("Request Headers %+v", req.Header)

	resp, err := originService.Proxy(stream, req)
	if err != nil {
		logger.WithError(err).Error("Request error")
		return err
	}

	logger.WithField("status", resp.Status).Debugf("Response Headers %+v", resp.Header)
	return nil
}

func (s *StreamHandler) requestLogger(req *http.Request, tunnelHostname h2mux.TunnelHostname) *logrus.Entry {
	cfRay := FindCfRayHeader(req)
	lbProbe := IsLBProbeRequest(req)
	logger := s.logger.WithField("tunnelHostname", tunnelHostname)
	if cfRay != "" {
		logger = logger.WithField("CF-RAY", cfRay)
		logger.Debugf("%s %s %s", req.Method, req.URL, req.Proto)
	} else if lbProbe {
		logger.Debugf("Load Balancer health check %s %s %s", req.Method, req.URL, req.Proto)
	} else {
		logger.Warnf("Requests %v does not have CF-RAY header. Please open a support ticket with Cloudflare.", req)
	}
	return logger
}
