# Test interfaces for RPC tests.

using Go = import "/go.capnp";

@0xef12a34b9807e19c;
$Go.package("testcapnp");
$Go.import("zombiezen.com/go/capnproto2/rpc/internal/testcapnp");

interface Handle {}

interface HandleFactory {
  newHandle @0 () -> (handle :Handle);
}

interface Hanger {
  hang @0 () -> ();
  # Block until context is cancelled
}

interface CallOrder {
  getCallSequence @0 (expected: UInt32) -> (n: UInt32);
  # First call returns 0, next returns 1, ...
  #
  # The input `expected` is ignored but useful for disambiguating debug logs.
}

interface Echoer extends(CallOrder) {
  echo @0 (cap :CallOrder) -> (cap :CallOrder);
  # Just returns the input cap.
}

interface PingPong {
  echoNum @0 (n :Int32) -> (n :Int32);
}

# Example interfaces

interface Adder {
  add @0 (a :Int32, b :Int32) -> (result :Int32);
}
