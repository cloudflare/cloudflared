package quic

import (
	"errors"
	"time"

	"github.com/lucas-clemente/quic-go/internal/utils"

	"github.com/lucas-clemente/quic-go/internal/protocol"
)

// Clone clones a Config
func (c *Config) Clone() *Config {
	copy := *c
	return &copy
}

func (c *Config) handshakeTimeout() time.Duration {
	return utils.MaxDuration(protocol.DefaultHandshakeTimeout, 2*c.HandshakeIdleTimeout)
}

func validateConfig(config *Config) error {
	if config == nil {
		return nil
	}
	if config.MaxIncomingStreams > 1<<60 {
		return errors.New("invalid value for Config.MaxIncomingStreams")
	}
	if config.MaxIncomingUniStreams > 1<<60 {
		return errors.New("invalid value for Config.MaxIncomingUniStreams")
	}
	return nil
}

// populateServerConfig populates fields in the quic.Config with their default values, if none are set
// it may be called with nil
func populateServerConfig(config *Config) *Config {
	config = populateConfig(config, protocol.DefaultConnectionIDLength)
	if config.AcceptToken == nil {
		config.AcceptToken = defaultAcceptToken
	}
	return config
}

// populateClientConfig populates fields in the quic.Config with their default values, if none are set
// it may be called with nil
func populateClientConfig(config *Config, createdPacketConn bool) *Config {
	var defaultConnIdLen = protocol.DefaultConnectionIDLength
	if createdPacketConn {
		defaultConnIdLen = 0
	}

	config = populateConfig(config, defaultConnIdLen)
	return config
}

func populateConfig(config *Config, defaultConnIDLen int) *Config {
	if config == nil {
		config = &Config{}
	}
	versions := config.Versions
	if len(versions) == 0 {
		versions = protocol.SupportedVersions
	}
	if config.ConnectionIDLength == 0 {
		config.ConnectionIDLength = defaultConnIDLen
	}
	handshakeIdleTimeout := protocol.DefaultHandshakeIdleTimeout
	if config.HandshakeIdleTimeout != 0 {
		handshakeIdleTimeout = config.HandshakeIdleTimeout
	}
	idleTimeout := protocol.DefaultIdleTimeout
	if config.MaxIdleTimeout != 0 {
		idleTimeout = config.MaxIdleTimeout
	}
	initialStreamReceiveWindow := config.InitialStreamReceiveWindow
	if initialStreamReceiveWindow == 0 {
		initialStreamReceiveWindow = protocol.DefaultInitialMaxStreamData
	}
	maxStreamReceiveWindow := config.MaxStreamReceiveWindow
	if maxStreamReceiveWindow == 0 {
		maxStreamReceiveWindow = protocol.DefaultMaxReceiveStreamFlowControlWindow
	}
	initialConnectionReceiveWindow := config.InitialConnectionReceiveWindow
	if initialConnectionReceiveWindow == 0 {
		initialConnectionReceiveWindow = protocol.DefaultInitialMaxData
	}
	maxConnectionReceiveWindow := config.MaxConnectionReceiveWindow
	if maxConnectionReceiveWindow == 0 {
		maxConnectionReceiveWindow = protocol.DefaultMaxReceiveConnectionFlowControlWindow
	}
	maxIncomingStreams := config.MaxIncomingStreams
	if maxIncomingStreams == 0 {
		maxIncomingStreams = protocol.DefaultMaxIncomingStreams
	} else if maxIncomingStreams < 0 {
		maxIncomingStreams = 0
	}
	maxIncomingUniStreams := config.MaxIncomingUniStreams
	if maxIncomingUniStreams == 0 {
		maxIncomingUniStreams = protocol.DefaultMaxIncomingUniStreams
	} else if maxIncomingUniStreams < 0 {
		maxIncomingUniStreams = 0
	}
	maxDatagrameFrameSize := config.MaxDatagramFrameSize
	if maxDatagrameFrameSize == 0 {
		maxDatagrameFrameSize = int64(protocol.DefaultMaxDatagramFrameSize)
	}
	connIDGenerator := config.ConnectionIDGenerator
	if connIDGenerator == nil {
		connIDGenerator = &protocol.DefaultConnectionIDGenerator{ConnLen: config.ConnectionIDLength}
	}

	return &Config{
		Versions:                         versions,
		HandshakeIdleTimeout:             handshakeIdleTimeout,
		MaxIdleTimeout:                   idleTimeout,
		AcceptToken:                      config.AcceptToken,
		KeepAlivePeriod:                  config.KeepAlivePeriod,
		InitialStreamReceiveWindow:       initialStreamReceiveWindow,
		MaxStreamReceiveWindow:           maxStreamReceiveWindow,
		InitialConnectionReceiveWindow:   initialConnectionReceiveWindow,
		MaxConnectionReceiveWindow:       maxConnectionReceiveWindow,
		AllowConnectionWindowIncrease:    config.AllowConnectionWindowIncrease,
		MaxIncomingStreams:               maxIncomingStreams,
		MaxIncomingUniStreams:            maxIncomingUniStreams,
		ConnectionIDLength:               config.ConnectionIDLength,
		ConnectionIDGenerator:            connIDGenerator,
		StatelessResetKey:                config.StatelessResetKey,
		TokenStore:                       config.TokenStore,
		EnableDatagrams:                  config.EnableDatagrams,
		MaxDatagramFrameSize:             maxDatagrameFrameSize,
		DisablePathMTUDiscovery:          config.DisablePathMTUDiscovery,
		DisableVersionNegotiationPackets: config.DisableVersionNegotiationPackets,
		Tracer:                           config.Tracer,
	}
}
