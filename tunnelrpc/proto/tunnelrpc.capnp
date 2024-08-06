using Go = import "go.capnp";
@0xdb8274f9144abc7e;
$Go.package("proto");
$Go.import("github.com/cloudflare/cloudflared/tunnelrpc");

# === DEPRECATED Legacy Tunnel Authentication and Registration methods/servers ===
# 
# These structs and interfaces are no longer used but it is important to keep
# them around to make sure backwards compatibility within the rpc protocol is
# maintained.

struct Authentication @0xc082ef6e0d42ed1d {
    # DEPRECATED: Legacy tunnel authentication mechanism
    key @0 :Text;
    email @1 :Text;
    originCAKey @2 :Text;
}

struct TunnelRegistration @0xf41a0f001ad49e46 {
    # DEPRECATED: Legacy tunnel authentication mechanism
    err @0 :Text;
    # the url to access the tunnel
    url @1 :Text;
    # Used to inform the client of actions taken.
    logLines @2 :List(Text);
    # In case of error, whether the client should attempt to reconnect.
    permanentFailure @3 :Bool;
    # Displayed to user
    tunnelID @4 :Text;
    # How long should this connection wait to retry in seconds, if the error wasn't permanent
    retryAfterSeconds @5 :UInt16;
    # A unique ID used to reconnect this tunnel.
    eventDigest @6 :Data;
    # A unique ID used to prove this tunnel was previously connected to a given metal.
    connDigest @7 :Data;
}

struct RegistrationOptions @0xc793e50592935b4a {
    # DEPRECATED: Legacy tunnel authentication mechanism
    
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
    # number of previous attempts to send RegisterTunnel/ReconnectTunnel
    numPreviousAttempts @12 :UInt8;
    # Set of features this cloudflared knows it supports
    features @13 :List(Text);
}

enum ExistingTunnelPolicy @0x84cb9536a2cf6d3c {
    # DEPRECATED: Legacy tunnel registration mechanism

    ignore @0;
    disconnect @1;
    balance @2;
}

struct ServerInfo @0xf2c68e2547ec3866 {
    # DEPRECATED: Legacy tunnel registration mechanism

    locationName @0 :Text;
}

struct AuthenticateResponse @0x82c325a07ad22a65 {
    # DEPRECATED: Legacy tunnel registration mechanism

    permanentErr @0 :Text;
    retryableErr @1 :Text;
    jwt @2 :Data;
    hoursUntilRefresh @3 :UInt8;
}

interface TunnelServer @0xea58385c65416035 extends (RegistrationServer) {
    # DEPRECATED: Legacy tunnel authentication server

    registerTunnel @0 (originCert :Data, hostname :Text, options :RegistrationOptions) -> (result :TunnelRegistration);
    getServerInfo @1 () -> (result :ServerInfo);
    unregisterTunnel @2 (gracePeriodNanoSec :Int64) -> ();
    # obsoleteDeclarativeTunnelConnect RPC deprecated in TUN-3019
    obsoleteDeclarativeTunnelConnect @3 () -> ();
    authenticate @4 (originCert :Data, hostname :Text, options :RegistrationOptions) -> (result :AuthenticateResponse);
    reconnectTunnel @5 (jwt :Data, eventDigest :Data, connDigest :Data, hostname :Text, options :RegistrationOptions) -> (result :TunnelRegistration);
}

struct Tag @0xcbd96442ae3bb01a {
    # DEPRECATED: Legacy tunnel additional HTTP header mechanism 

    name @0 :Text;
    value @1 :Text;
}

# === End DEPRECATED Objects ===

struct ClientInfo @0x83ced0145b2f114b {
    # The tunnel client's unique identifier, used to verify a reconnection.
    clientId @0 :Data;
    # Set of features this cloudflared knows it supports
    features @1 :List(Text);
    # Information about the running binary.
    version @2 :Text;
    # Client OS and CPU info
    arch @3 :Text;
}

struct ConnectionOptions @0xb4bf9861fe035d04 {
    # client details
    client @0 :ClientInfo;
    # origin LAN IP
    originLocalIp @1 :Data;
    # What to do if connection already exists
    replaceExisting @2 :Bool;
    # cross stream compression setting, 0 - off, 3 - high
    compressionQuality @3 :UInt8;
    # number of previous attempts to send RegisterConnection
    numPreviousAttempts @4 :UInt8;
}

struct ConnectionResponse @0xdbaa9d03d52b62dc {
    result :union {
        error @0 :ConnectionError;
        connectionDetails @1 :ConnectionDetails;
    }
}

struct ConnectionError @0xf5f383d2785edb86 {
    cause @0 :Text;
    # How long should this connection wait to retry in ns
    retryAfter @1 :Int64;
    shouldRetry @2 :Bool;
}

struct ConnectionDetails @0xb5f39f082b9ac18a {
    # identifier of this connection
    uuid @0 :Data;
    # airport code of the colo where this connection landed
    locationName @1 :Text;
    # tells if the tunnel is remotely managed
    tunnelIsRemotelyManaged @2: Bool;
}

struct TunnelAuth @0x9496331ab9cd463f {
  accountTag @0 :Text;
  tunnelSecret @1 :Data;
}

interface RegistrationServer @0xf71695ec7fe85497 {
    registerConnection @0 (auth :TunnelAuth, tunnelId :Data, connIndex :UInt8, options :ConnectionOptions) -> (result :ConnectionResponse);
    unregisterConnection @1 () -> ();
    updateLocalConfiguration @2 (config :Data) -> ();
}

struct RegisterUdpSessionResponse @0xab6d5210c1f26687 {
    err @0 :Text;
    spans @1 :Data;
}

interface SessionManager @0x839445a59fb01686 {
    # Let the edge decide closeAfterIdle to make sure cloudflared doesn't close session before the edge closes its side
    registerUdpSession @0 (sessionId :Data, dstIp :Data, dstPort :UInt16, closeAfterIdleHint :Int64, traceContext :Text = "") -> (result :RegisterUdpSessionResponse);
    unregisterUdpSession @1 (sessionId :Data, message :Text) -> ();
}

struct UpdateConfigurationResponse @0xdb58ff694ba05cf9 {
    # Latest configuration that was applied successfully. The err field might be populated at the same time to indicate
    # that cloudflared is using an older configuration because the latest cannot be applied
    latestAppliedVersion @0 :Int32;
    # Any error encountered when trying to apply the last configuration
    err @1 :Text;
}

# ConfigurationManager defines RPC to manage cloudflared configuration remotely
interface ConfigurationManager @0xb48edfbdaa25db04 {
    updateConfiguration @0 (version :Int32, config :Data) -> (result: UpdateConfigurationResponse);
}

interface CloudflaredServer @0xf548cef9dea2a4a1 extends(SessionManager, ConfigurationManager) {}