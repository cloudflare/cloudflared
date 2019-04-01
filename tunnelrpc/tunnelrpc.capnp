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
}

struct ConnectResult {
    err @0 :ConnectError;
    # Information about the server this connection is established with
    serverInfo @1 :ServerInfo;
}

struct ConnectError {
    cause @0 :Text;
    # How long should this connection wait to retry in ns
    retryAfter @1 :Int64;
    shouldRetry @2 :Bool;
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

interface TunnelServer {
    registerTunnel @0 (originCert :Data, hostname :Text, options :RegistrationOptions) -> (result :TunnelRegistration);
    getServerInfo @1 () -> (result :ServerInfo);
    unregisterTunnel @2 (gracePeriodNanoSec :Int64) -> ();
    connect @3 (parameters :CapnpConnectParameters) -> (result :ConnectResult);
}
