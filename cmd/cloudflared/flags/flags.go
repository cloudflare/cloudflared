package flags

const (
	// HaConnections specifies how many connections to make to the edge
	HaConnections = "ha-connections"

	// SshPort is the port on localhost the cloudflared ssh server will run on
	SshPort = "local-ssh-port"

	// SshIdleTimeout defines the duration a SSH session can remain idle before being closed
	SshIdleTimeout = "ssh-idle-timeout"

	// SshMaxTimeout defines the max duration a SSH session can remain open for
	SshMaxTimeout = "ssh-max-timeout"

	// SshLogUploaderBucketName is the bucket name to use for the SSH log uploader
	SshLogUploaderBucketName = "bucket-name"

	// SshLogUploaderRegionName is the AWS region name to use for the SSH log uploader
	SshLogUploaderRegionName = "region-name"

	// SshLogUploaderSecretID is the Secret id of SSH log uploader
	SshLogUploaderSecretID = "secret-id"

	// SshLogUploaderAccessKeyID is the Access key id of SSH log uploader
	SshLogUploaderAccessKeyID = "access-key-id"

	// SshLogUploaderSessionTokenID is the Session token of SSH log uploader
	SshLogUploaderSessionTokenID = "session-token"

	// SshLogUploaderS3URL is the S3 URL of SSH log uploader (e.g. don't use AWS s3 and use google storage bucket instead)
	SshLogUploaderS3URL = "s3-url-host"

	// HostKeyPath is the path of the dir to save SSH host keys too
	HostKeyPath = "host-key-path"

	// RpcTimeout is how long to wait for a Capnp RPC request to the edge
	RpcTimeout = "rpc-timeout"

	// WriteStreamTimeout sets if we should have a timeout when writing data to a stream towards the destination (edge/origin).
	WriteStreamTimeout = "write-stream-timeout"

	// QuicDisablePathMTUDiscovery sets if QUIC should not perform PTMU discovery and use a smaller (safe) packet size.
	// Packets will then be at most 1252 (IPv4) / 1232 (IPv6) bytes in size.
	// Note that this may result in packet drops for UDP proxying, since we expect being able to send at least 1280 bytes of inner packets.
	QuicDisablePathMTUDiscovery = "quic-disable-pmtu-discovery"

	// QuicConnLevelFlowControlLimit controls the max flow control limit allocated for a QUIC connection. This controls how much data is the
	// receiver willing to buffer. Once the limit is reached, the sender will send a DATA_BLOCKED frame to indicate it has more data to write,
	// but it's blocked by flow control
	QuicConnLevelFlowControlLimit = "quic-connection-level-flow-control-limit"

	// QuicStreamLevelFlowControlLimit is similar to quicConnLevelFlowControlLimit but for each QUIC stream. When the sender is blocked,
	// it will send a STREAM_DATA_BLOCKED frame
	QuicStreamLevelFlowControlLimit = "quic-stream-level-flow-control-limit"

	// Ui is to enable launching cloudflared in interactive UI mode
	Ui = "ui"

	// ConnectorLabel is the command line flag to give a meaningful label to a specific connector
	ConnectorLabel = "label"

	// MaxActiveFlows is the command line flag to set the maximum number of flows that cloudflared can be processing at the same time
	MaxActiveFlows = "max-active-flows"

	// Tag is the command line flag to set custom tags used to identify this tunnel via added HTTP request headers to the origin
	Tag = "tag"

	// Protocol is the command line flag to set the protocol to use to connect to the Cloudflare Edge
	Protocol = "protocol"

	// PostQuantum is the command line flag to force the connection to Cloudflare Edge to use Post Quantum cryptography
	PostQuantum = "post-quantum"

	// Features is the command line flag to opt into various features that are still being developed or tested
	Features = "features"

	// EdgeIpVersion is the command line flag to set the Cloudflare Edge IP address version to connect with
	EdgeIpVersion = "edge-ip-version"

	// EdgeBindAddress is the command line flag to bind to IP address for outgoing connections to Cloudflare Edge
	EdgeBindAddress = "edge-bind-address"

	// Force is the command line flag to specify if you wish to force an action
	Force = "force"

	// Edge is the command line flag to set the address of the Cloudflare tunnel server. Only works in Cloudflare's internal testing environment
	Edge = "edge"

	// Region is the command line flag to set the Cloudflare Edge region to connect to
	Region = "region"

	// IsAutoUpdated is the command line flag to signal the new process that cloudflared has been autoupdated
	IsAutoUpdated = "is-autoupdated"

	// LBPool is the command line flag to set the name of the load balancing pool to add this origin to
	LBPool = "lb-pool"

	// Retries is the command line flag to set the maximum number of retries for connection/protocol errors
	Retries = "retries"

	// MaxEdgeAddrRetries is the command line flag to set the maximum number of times to retry on edge addrs before falling back to a lower protocol
	MaxEdgeAddrRetries = "max-edge-addr-retries"

	// GracePeriod is the command line flag to set the maximum amount of time that cloudflared waits to shut down if it is still serving requests
	GracePeriod = "grace-period"

	// ICMPV4Src is the command line flag to set the source address and the interface name to send/receive ICMPv4 messages
	ICMPV4Src = "icmpv4-src"

	// ICMPV6Src is the command line flag to set the source address and the interface name to send/receive ICMPv6 messages
	ICMPV6Src = "icmpv6-src"

	// ProxyDns is the command line flag to run DNS server over HTTPS
	ProxyDns = "proxy-dns"

	// Name is the command line to set the name of the tunnel
	Name = "name"

	// AutoUpdateFreq is the command line for setting the frequency that cloudflared checks for updates
	AutoUpdateFreq = "autoupdate-freq"

	// NoAutoUpdate is the command line flag to disable cloudflared from checking for updates
	NoAutoUpdate = "no-autoupdate"

	// LogLevel is the command line flag for the cloudflared logging level
	LogLevel = "loglevel"

	// LogLevelSSH is the command line flag for the cloudflared ssh logging level
	LogLevelSSH = "log-level"

	// TransportLogLevel is the command line flag for the transport logging level
	TransportLogLevel = "transport-loglevel"

	// LogFile is the command line flag to define the file where application logs will be stored
	LogFile = "logfile"

	// LogDirectory is the command line flag to define the directory where application logs will be stored.
	LogDirectory = "log-directory"

	// LogFormatOutput allows the command line logs to be output as JSON.
	LogFormatOutput             = "output"
	LogFormatOutputValueDefault = "default"
	LogFormatOutputValueJSON    = "json"

	// TraceOutput is the command line flag to set the name of trace output file
	TraceOutput = "trace-output"

	// OriginCert is the command line flag to define the path for the origin certificate used by cloudflared
	OriginCert = "origincert"

	// Metrics is the command line flag to define the address of the metrics server
	Metrics = "metrics"

	// MetricsUpdateFreq is the command line flag to define how frequently tunnel metrics are updated
	MetricsUpdateFreq = "metrics-update-freq"

	// ApiURL is the command line flag used to define the base URL of the API
	ApiURL = "api-url"

	// Virtual DNS resolver service resolver addresses to use instead of dynamically fetching them from the OS.
	VirtualDNSServiceResolverAddresses = "dns-resolver-addrs"

	// Management hostname to signify incoming management requests
	ManagementHostname = "management-hostname"

	// Automatically close the login interstitial browser window after the user makes a decision.
	AutoCloseInterstitial = "auto-close"
)
