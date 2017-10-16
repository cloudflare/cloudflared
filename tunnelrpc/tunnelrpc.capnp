using Go = import "go.capnp";
@0xdb8274f9144abc7e;
$Go.package("tunnelrpc");
$Go.import("github.com/cloudflare/cloudflare-warp/tunnelrpc");

struct Authentication {
    key @0 :Text;
    email @1 :Text;
    originCAKey @2 :Text;
}

struct TunnelRegistration {
    err @0 :Text;
    # A list of URLs that the tunnel is accessible from.
    urls @1 :List(Text);
    # Used to inform the client of actions taken.
    logLines @2 :List(Text);
    # In case of error, whether the client should attempt to reconnect.
    permanentFailure @3 :Bool;
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
    poolId @4 :Text;
    # Prevents the tunnel from being accessed at <subdomain>.cftunnel.com
    exposeInternalHostname @5 :Bool;
    # Client-defined tags to associate with the tunnel
    tags @6 :List(Tag);
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
    registerTunnel @0 (auth :Authentication, hostname :Text, options :RegistrationOptions) -> (result :TunnelRegistration);
    getServerInfo @1 () -> (result :ServerInfo);
}
