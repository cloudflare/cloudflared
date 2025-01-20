using Go = import "go.capnp";
@0xb29021ef7421cc32;

$Go.package("proto");
$Go.import("github.com/cloudflare/cloudflared/tunnelrpc");


struct ConnectRequest @0xc47116a1045e4061 {
	dest @0 :Text;
	type @1 :ConnectionType;
	metadata @2 :List(Metadata);
}

enum ConnectionType @0xc52e1bac26d379c8 {
	http @0;
	websocket @1;
	tcp @2;
}

struct Metadata @0xe1446b97bfd1cd37 {
    key @0 :Text;
	val @1 :Text;
}

struct ConnectResponse @0xb1032ec91cef8727 {
	error @0 :Text;
	metadata @1 :List(Metadata);
}
