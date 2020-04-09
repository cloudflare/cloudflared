package streamhandler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/tunnelhostnamemapper"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"zombiezen.com/go/capnproto2/rpc"
)

const (
	statusPseudoHeader = ":status"
)

type httpErrorStatus struct {
	status string
	text   []byte
}

var (
	statusBadRequest = newHTTPErrorStatus(http.StatusBadRequest)
	statusNotFound   = newHTTPErrorStatus(http.StatusNotFound)
	statusBadGateway = newHTTPErrorStatus(http.StatusBadGateway)
)

func newHTTPErrorStatus(status int) *httpErrorStatus {
	return &httpErrorStatus{
		status: strconv.Itoa(status),
		text:   []byte(http.StatusText(status)),
	}
}

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

// UseConfiguration implements ClientService
func (s *StreamHandler) UseConfiguration(ctx context.Context, config *pogs.ClientConfig) (*pogs.UseConfigurationResult, error) {
	select {
	case <-ctx.Done():
		err := fmt.Errorf("Timeout while sending new config to Supervisor")
		s.logger.Error(err)
		return nil, err
	case s.newConfigChan <- config:
	}
	select {
	case <-ctx.Done():
		err := fmt.Errorf("Timeout applying new configuration")
		s.logger.Error(err)
		return nil, err
	case result := <-s.useConfigResultChan:
		return result, nil
	}
}

// UpdateConfig replaces current originmapper mapping with mappings from newConfig
func (s *StreamHandler) UpdateConfig(newConfig []*pogs.ReverseProxyConfig) (failedConfigs []*pogs.FailedConfig) {

	// Delete old configs that aren't in the `newConfig`
	toRemove := s.tunnelHostnameMapper.ToRemove(newConfig)
	for _, hostnameToRemove := range toRemove {
		s.tunnelHostnameMapper.Delete(hostnameToRemove)
	}

	// Add new configs that weren't in the old mapper
	toAdd := s.tunnelHostnameMapper.ToAdd(newConfig)
	for _, tunnelConfig := range toAdd {
		tunnelHostname := tunnelConfig.TunnelHostname
		originSerice, err := tunnelConfig.OriginConfig.Service()
		if err != nil {
			s.logger.WithField("tunnelHostname", tunnelHostname).WithError(err).Error("Invalid origin service config")
			failedConfigs = append(failedConfigs, &pogs.FailedConfig{
				Config: tunnelConfig,
				Reason: tunnelConfig.FailReason(err),
			})
			continue
		}
		s.tunnelHostnameMapper.Add(tunnelConfig.TunnelHostname, originSerice)
		s.logger.WithField("tunnelHostname", tunnelHostname).Infof("New origin service config: %v", originSerice.Summary())
	}
	return
}

// ServeStream implements MuxedStreamHandler interface
func (s *StreamHandler) ServeStream(stream *h2mux.MuxedStream) error {
	if stream.IsRPCStream() {
		return s.serveRPC(stream)
	}
	if err := s.serveRequest(stream); err != nil {
		s.logger.Error(err)
		return err
	}
	return nil
}

func (s *StreamHandler) serveRPC(stream *h2mux.MuxedStream) error {
	stream.WriteHeaders([]h2mux.Header{{Name: ":status", Value: "200"}})
	main := pogs.ClientService_ServerToClient(s)
	rpcLogger := s.logger.WithField("subsystem", "clientserver-rpc")
	rpcConn := rpc.NewConn(
		tunnelrpc.NewTransportLogger(rpcLogger, rpc.StreamTransport(stream)),
		rpc.MainInterface(main.Client),
		tunnelrpc.ConnLog(s.logger.WithField("subsystem", "clientserver-rpc-transport")),
	)
	return rpcConn.Wait()
}

func (s *StreamHandler) serveRequest(stream *h2mux.MuxedStream) error {
	tunnelHostname := stream.TunnelHostname()
	if !tunnelHostname.IsSet() {
		s.writeErrorStatus(stream, statusBadRequest)
		return fmt.Errorf("stream doesn't have tunnelHostname")
	}

	originService, ok := s.tunnelHostnameMapper.Get(tunnelHostname)
	if !ok {
		s.writeErrorStatus(stream, statusNotFound)
		return fmt.Errorf("cannot map tunnel hostname %s to origin", tunnelHostname)
	}

	req, err := createRequest(stream, originService.URL())
	if err != nil {
		s.writeErrorStatus(stream, statusBadRequest)
		return errors.Wrap(err, "cannot create request")
	}

	logger := s.requestLogger(req, tunnelHostname)
	logger.Debugf("Request Headers %+v", req.Header)

	resp, err := originService.Proxy(stream, req)
	if err != nil {
		s.writeErrorStatus(stream, statusBadGateway)
		return errors.Wrap(err, "cannot proxy request")
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

func (s *StreamHandler) writeErrorStatus(stream *h2mux.MuxedStream, status *httpErrorStatus) {
	_ = stream.WriteHeaders([]h2mux.Header{
		{
			Name:  statusPseudoHeader,
			Value: status.status,
		},
		h2mux.CreateResponseMetaHeader(h2mux.ResponseSourceCloudflared),
	})
	_, _ = stream.Write(status.text)
}
