# Generate scopes.capnp.out with:
# capnp compile -o- scopes.capnp > scopes.capnp.out
# Must run inside this directory to preserve paths.

using Go = import "go.capnp";

@0xc260cb50ae622e10;

$Go.package("const");
$Go.import("zombiezen.com/go/capnproto2/capnpc-go/testdata/const");

const answer @0xda96e2255811b258 :Int64 = 42;
const blob @0xe0a385c7be1fea4d :Data = "\x01\x02\x03";
