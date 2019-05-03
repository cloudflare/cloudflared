package connection

import (
	"context"
	"net"
	"time"

	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	// Waiting time before retrying a failed tunnel connection
	reconnectDuration = time.Second * 10
	// SRV record resolution TTL
	resolveTTL = time.Hour
	// Interval between establishing new connection
	connectionInterval = time.Second
)

type CloudflaredConfig struct {
	ConnectionConfig   *ConnectionConfig
	OriginCert         []byte
	Tags               []tunnelpogs.Tag
	EdgeAddrs          []string
	HAConnections      uint
	Logger             *logrus.Logger
	CloudflaredVersion string
}

// Supervisor is a stateful object that manages connections with the edge
type Supervisor struct {
	config     *CloudflaredConfig
	state      *supervisorState
	connErrors chan error
}

type supervisorState struct {
	// IPs to connect to cloudflare's edge network
	edgeIPs []*net.TCPAddr
	// index of the next element to use in edgeIPs
	nextEdgeIPIndex int
	// last time edgeIPs were refreshed
	lastResolveTime time.Time
	// ID of this cloudflared instance
	cloudflaredID uuid.UUID
	// connectionPool is a pool of connectionHandlers that can be used to make RPCs
	connectionPool *connectionPool
}

func (s *supervisorState) getNextEdgeIP() *net.TCPAddr {
	ip := s.edgeIPs[s.nextEdgeIPIndex%len(s.edgeIPs)]
	s.nextEdgeIPIndex++
	return ip
}

func NewSupervisor(config *CloudflaredConfig) *Supervisor {
	return &Supervisor{
		config: config,
		state: &supervisorState{
			connectionPool: &connectionPool{},
		},
		connErrors: make(chan error),
	}
}

func (s *Supervisor) Run(ctx context.Context) error {
	logger := s.config.Logger
	if err := s.initialize(); err != nil {
		logger.WithError(err).Error("Failed to get edge IPs")
		return err
	}
	defer s.state.connectionPool.close()

	var currentConnectionCount uint
	expectedConnectionCount := s.config.HAConnections
	if uint(len(s.state.edgeIPs)) < s.config.HAConnections {
		logger.Warnf("You requested %d HA connections but I can give you at most %d.", s.config.HAConnections, len(s.state.edgeIPs))
		expectedConnectionCount = uint(len(s.state.edgeIPs))
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case connErr := <-s.connErrors:
			logger.WithError(connErr).Warnf("Connection dropped unexpectedly")
			currentConnectionCount--
		default:
			time.Sleep(5 * time.Second)
		}
		if currentConnectionCount < expectedConnectionCount {
			h, err := newH2MuxHandler(ctx, s.config.ConnectionConfig, s.state.getNextEdgeIP())
			if err != nil {
				logger.WithError(err).Error("Failed to create new connection handler")
				continue
			}
			go func() {
				s.connErrors <- h.serve(ctx)
			}()
			connResult, err := s.connect(ctx, s.config, s.state.cloudflaredID, h)
			if err != nil {
				logger.WithError(err).Errorf("Failed to connect to cloudflared's edge network")
				h.shutdown()
				continue
			}
			if connErr := connResult.Err; connErr != nil && !connErr.ShouldRetry {
				logger.WithError(connErr).Errorf("Server respond with don't retry to connect")
				h.shutdown()
				return err
			}
			logger.Infof("Connected to %s", connResult.ServerInfo.LocationName)
			s.state.connectionPool.put(h)
			currentConnectionCount++
		}
	}
}

func (s *Supervisor) initialize() error {
	edgeIPs, err := ResolveEdgeIPs(s.config.Logger, s.config.EdgeAddrs)
	if err != nil {
		return errors.Wrapf(err, "Failed to resolve cloudflare edge network address")
	}
	s.state.edgeIPs = edgeIPs
	s.state.lastResolveTime = time.Now()
	cloudflaredID, err := uuid.NewRandom()
	if err != nil {
		return errors.Wrap(err, "Failed to generate cloudflared ID")
	}
	s.state.cloudflaredID = cloudflaredID
	return nil
}

func (s *Supervisor) connect(ctx context.Context,
	config *CloudflaredConfig,
	cloudflaredID uuid.UUID,
	h connectionHandler,
) (*tunnelpogs.ConnectResult, error) {
	connectParameters := &tunnelpogs.ConnectParameters{
		OriginCert:          config.OriginCert,
		CloudflaredID:       cloudflaredID,
		NumPreviousAttempts: 0,
		CloudflaredVersion:  config.CloudflaredVersion,
	}
	return h.connect(ctx, connectParameters)
}
