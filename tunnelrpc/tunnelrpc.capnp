using Go = import "go.capnp";
@0xdb8274f9144abc7e;
$Go.package("tunnelrpc");
$Go.import("github.com/cloudflare/cloudflared/tunnelrpc");

struct Authentication {
    key @0 :Text;
    email @1 :Text;
    originCAKey @2 :Text;
}

struct TunnelRegistration {
    err @0 :Text;
    # the url to access the tunnel
    url @1 :Text;
    # Used to inform the client of actions taken.
    logLines @2 :List(Text);
    # In case of error, whether the client should attempt to reconnect.
    permanentFailure @3 :Bool;
    # Displayed to user
    tunnelID @4 :Text;
}

struct RegistrationOptions {
    # The tunnel client's unique identifier, used to verify a reconnection.
    clientId @0 :Text;
    # Information about the running binary.
    version @1 :Text;
    os @2 :Text;
    # What to do with existing tunnels for the given hostname.
    existingTunnelPolicy @3 :ExistingTunnelPolicy;
    # If using the balancing policy, identifies the LB pool to use.
    poolName @4 :Text;
    # Client-defined tags to associate with the tunnel
    tags @5 :List(Tag);
    # A unique identifier for a high-availability connection made by a single client.
    connectionId @6 :UInt8;
    # origin LAN IP
    originLocalIp @7 :Text;
    # whether Argo Tunnel client has been autoupdated
    isAutoupdated @8 :Bool;
    # whether Argo Tunnel client is run from a terminal
    runFromTerminal @9 :Bool;
    # cross stream compression setting, 0 - off, 3 - high
    compressionQuality @10 :UInt64;
    uuid @11 :Text;
}

struct CapnpConnectParameters {
    # certificate and token to prove ownership of a zone
    originCert @0 :Data;
    # UUID assigned to this cloudflared obtained from Hello
    cloudflaredID @1 :Data;
    # number of previous attempts to send Connect
    numPreviousAttempts @2 :UInt8;
    # user defined labels for this cloudflared
    tags @3 :List(Tag);
    # release version of cloudflared
    cloudflaredVersion @4 :Text;
    # which intent this cloudflared instance should get its behaviour from
    intentLabel @5 :Text;
}

struct ConnectResult {
    err @0 :ConnectError;
    # Information about the server this connection is established with
    serverInfo @1 :ServerInfo;
    # How this cloudflared instance should be configured
    clientConfig @2 :ClientConfig;
}

struct ConnectError {
    cause @0 :Text;
    # How long should this connection wait to retry in ns
    retryAfter @1 :Int64;
    shouldRetry @2 :Bool;
}

struct ClientConfig {
    # Version of this configuration. This value is opaque, but is guaranteed
    # to monotonically increase in value. Any configuration supplied to
    # useConfiguration() with a smaller `version` should be ignored.
    version @0 :UInt64;
    # supervisorConfig  is configuration for supervisor, the component that manages connection manager,
    # autoupdater and metrics server
    supervisorConfig @1 :SupervisorConfig;
    # edgeConnectionConfig is configuration for connection manager, the componenet that manages connections with the edge
    edgeConnectionConfig @2 :EdgeConnectionConfig;
    # Configuration for cloudflared to run as a DNS-over-HTTPS proxy.
    # cloudflared CLI option: `proxy-dns`
    dohProxyConfigs @3 :List(DoHProxyConfig);
    # Configuration for cloudflared to run as an HTTP reverse proxy.
    reverseProxyConfigs @4 :List(ReverseProxyConfig);
}

struct SupervisorConfig {
    # Frequency (in ns) to check Equinox for updates.
    # Zero means auto-update is disabled.
    # cloudflared CLI option: `autoupdate-freq`
    autoUpdateFrequency @0 :Int64;
    # Frequency (in ns) to update connection-based metrics.
    # cloudflared CLI option: `metrics-update-freq`
    metricsUpdateFrequency @1 :Int64;
    # Time (in ns) to continue serving requests after cloudflared receives its
    # first SIGINT/SIGTERM. A second SIGINT/SIGTERM will force cloudflared to
    # shutdown immediately. For example, this field can be used to gracefully
    # transition traffic to another cloudflared instance.
    # cloudflared CLI option: `grace-period`
    gracePeriod @2 :Int64;
}

struct EdgeConnectionConfig {
    # cloudflared CLI option: `ha-connections`
    numHAConnections @0 :UInt8;
    # Interval (in ns) between heartbeats with the Cloudflare edge
    # cloudflared CLI option: `heartbeat-interval`
    heartbeatInterval @1 :Int64;
    # Maximum wait time to connect with the edge.
    timeout  @2 :Int64;
    # Number of unacked heartbeats for cloudflared to send before
    # closing the connection to the edge.
    # cloudflared CLI option: `heartbeat-count`
    maxFailedHeartbeats @3 :UInt64;
    # Absolute path of the file containing certificate and token to connect with the edge
    userCredentialPath @4 :Text;
}

struct ReverseProxyConfig {
    tunnelHostname @0 :Text;
    originConfig :union {
        http @1 :HTTPOriginConfig;
        websocket @2 :WebSocketOriginConfig;
        helloWorld @3 :HelloWorldOriginConfig;
    }
    # Maximum number of retries for connection/protocol errors.
    # cloudflared CLI option: `retries`
    retries @4 :UInt64;
    # maximum time (in ns) for cloudflared to wait to establish a connection
    # to the origin. Zero means no timeout.
    # cloudflared CLI option: `proxy-connect-timeout`
    connectionTimeout @5 :Int64;
    # (beta) Use cross-stream compression instead of HTTP compression.
    # 0=off, 1=low, 2=medium, 3=high.
    # For more context see the mapping here: https://github.com/cloudflare/cloudflared/blob/2019.3.2/h2mux/h2_dictionaries.go#L62
    # cloudflared CLI option: `compression-quality`
    compressionQuality @6 :UInt64;
}

struct WebSocketOriginConfig {
    # URI of the origin service.
    # cloudflared will start a websocket server that forwards data to this URI
    # cloudflared CLI option: `url`
    # cloudflared logic: https://github.com/cloudflare/cloudflared/blob/2019.3.2/cmd/cloudflared/tunnel/cmd.go#L304
    urlString @0 :Text;
    # Whether cloudflared should verify TLS connections to the origin.
    # negation of cloudflared CLI option: `no-tls-verify`
    tlsVerify @1 :Bool;
    # originCAPool specifies the root CA that cloudflared should use when
    # verifying TLS connections to the origin.
    #   - if tlsVerify is false, originCAPool will be ignored.
    #   - if tlsVerify is true and originCAPool is empty, the system CA pool
    #     will be loaded if possible.
    #   - if tlsVerify is true and originCAPool is non-empty, cloudflared will
    #     treat it as the filepath to the root CA.
    # cloudflared CLI option: `origin-ca-pool`
    originCAPool @2 :Text;
    # Hostname to use when verifying TLS connections to the origin.
    # cloudflared CLI option: `origin-server-name`
    originServerName @3 :Text;
}

struct HTTPOriginConfig {
    # HTTP(S) URL of the origin service.
    # cloudflared CLI option: `url`
    urlString @0 :Text;
    # the TCP keep-alive period (in ns) for an active network connection.
    # Zero means keep-alives are not enabled.
    # cloudflared CLI option: `proxy-tcp-keepalive`
    tcpKeepAlive @1 :Int64;
    # whether cloudflared should use a "happy eyeballs"-compliant procedure
    # to connect to origins that resolve to both IPv4 and IPv6 addresses
    # negation of cloudflared CLI option: `proxy-no-happy-eyeballs`
    dialDualStack @2 :Bool;
    # maximum time (in ns) for cloudflared to wait for a TLS handshake
    # with the origin. Zero means no timeout.
    # cloudflared CLI option: `proxy-tls-timeout`
    tlsHandshakeTimeout @3 :Int64;
    # Whether cloudflared should verify TLS connections to the origin.
    # negation of cloudflared CLI option: `no-tls-verify`
    tlsVerify @4 :Bool;
    # originCAPool specifies the root CA that cloudflared should use when
    # verifying TLS connections to the origin.
    #   - if tlsVerify is false, originCAPool will be ignored.
    #   - if tlsVerify is true and originCAPool is empty, the system CA pool
    #     will be loaded if possible.
    #   - if tlsVerify is true and originCAPool is non-empty, cloudflared will
    #     treat it as the filepath to the root CA.
    # cloudflared CLI option: `origin-ca-pool`
    originCAPool @5 :Text;
    # Hostname to use when verifying TLS connections to the origin.
    # cloudflared CLI option: `origin-server-name`
    originServerName @6 :Text;
    # maximum number of idle (keep-alive) connections for cloudflared to
    # keep open with the origin. Zero means no limit.
    # cloudflared CLI option: `proxy-keepalive-connections`
    maxIdleConnections @7 :UInt64;
    # maximum time (in ns) for an idle (keep-alive) connection to remain
    # idle before closing itself. Zero means no timeout.
    # cloudflared CLI option: `proxy-keepalive-timeout`
    idleConnectionTimeout @8 :Int64;
    # maximum amount of time a dial will wait for a connect to complete.
    proxyConnectionTimeout @9 :Int64;
    # The amount of time to wait for origin's first response headers after fully
    # writing the request headers if the request has an "Expect: 100-continue" header.
    # Zero means no timeout and causes the body to be sent immediately, without
    # waiting for the server to approve.
    expectContinueTimeout @10 :Int64;
    # Whether cloudflared should allow chunked transfer encoding to the
    # origin. (This should be disabled for WSGI origins, for example.)
    # negation of cloudflared CLI option: `no-chunked-encoding`
	chunkedEncoding @11 :Bool;
}

# configuration for cloudflared to provide a DNS over HTTPS proxy server
struct DoHProxyConfig {
    # The hostname for the DoH proxy server to listen on.
    # cloudflared CLI option: `proxy-dns-address`
    listenHost @0 :Text;
    # The port for the DoH proxy server to listen on.
    # cloudflared CLI option: `proxy-dns-port`
    listenPort @1 :UInt16;
    # Upstream endpoint URLs for the DoH proxy server.
    # cloudflared CLI option: `proxy-dns-upstream`
    upstreams @2 :List(Text);
}

struct HelloWorldOriginConfig {
    # nothing to configure
}

struct Tag {
    name @0 :Text;
    value @1 :Text;
}

enum ExistingTunnelPolicy {
    ignore @0;
    disconnect @1;
    balance @2;
}

struct ServerInfo {
    locationName @0 :Text;
}

struct UseConfigurationResult {
    success @0 :Bool;
    failedConfigs @1 :List(FailedConfig);
}

struct FailedConfig {
    config :union {
        supervisor @0 :SupervisorConfig;
        edgeConnection @1 :EdgeConnectionConfig;
        doh @2 :DoHProxyConfig;
        reverseProxy @3 :ReverseProxyConfig;
    }
	reason @4 :Text;
}

interface TunnelServer {
    registerTunnel @0 (originCert :Data, hostname :Text, options :RegistrationOptions) -> (result :TunnelRegistration);
    getServerInfo @1 () -> (result :ServerInfo);
    unregisterTunnel @2 (gracePeriodNanoSec :Int64) -> ();
    connect @3 (parameters :CapnpConnectParameters) -> (result :ConnectResult);
}

interface ClientService {
    useConfiguration @0 (clientServiceConfig :ClientConfig) -> (result :UseConfigurationResult);
}
