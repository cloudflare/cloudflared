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
    # How long should this connection wait to retry in seconds, if the error wasn't permanent
    retryAfterSeconds @5 :UInt16;
    # A unique ID used to reconnect this tunnel.
    eventDigest @6 :Data;
    # A unique ID used to prove this tunnel was previously connected to a given metal.
    connDigest @7 :Data;
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
    # number of previous attempts to send RegisterTunnel/ReconnectTunnel
    numPreviousAttempts @12 :UInt8;
    # Set of features this cloudflared knows it supports
    features @13 :List(Text);
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

struct AuthenticateResponse {
    permanentErr @0 :Text;
    retryableErr @1 :Text;
    jwt @2 :Data;
    hoursUntilRefresh @3 :UInt8;
}

interface TunnelServer {
    registerTunnel @0 (originCert :Data, hostname :Text, options :RegistrationOptions) -> (result :TunnelRegistration);
    getServerInfo @1 () -> (result :ServerInfo);
    unregisterTunnel @2 (gracePeriodNanoSec :Int64) -> ();
    # obsoleteDeclarativeTunnelConnect RPC deprecated in TUN-3019
    obsoleteDeclarativeTunnelConnect @3 () -> ();
    authenticate @4 (originCert :Data, hostname :Text, options :RegistrationOptions) -> (result :AuthenticateResponse);
    reconnectTunnel @5 (jwt :Data, eventDigest :Data, connDigest :Data, hostname :Text, options :RegistrationOptions) -> (result :TunnelRegistration);
}
