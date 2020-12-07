[![Coverage Status](https://coveralls.io/repos/github/cisco/go-tls-syntax/badge.svg)](https://coveralls.io/github/cisco/go-tls-syntax)

TLS Syntax
==========

TLS defines [its own syntax](https://tlswg.github.io/tls13-spec/#rfc.section.3)
for describing structures used in that protocol.  To facilitate the reuse of
this serialization format in other context, this module maps that syntax to
the Go structure syntax, taking advantage of Go's type annotations to encode
non-type information carried in the TLS presentation format.

For example, in the TLS specification, a ClientHello message has the following
structure:

~~~~~
uint16 ProtocolVersion;
opaque Random[32];
uint8 CipherSuite[2];
enum { server_name(0), ... (65535)} ExtensionType;

struct {
    ExtensionType extension_type;
    opaque extension_data<0..2^16-1>;
} Extension;

struct {
    ProtocolVersion legacy_version = 0x0303;    /* TLS v1.2 */
    Random random;
    opaque legacy_session_id<0..32>;
    CipherSuite cipher_suites<2..2^16-2>;
    opaque legacy_compression_methods<1..2^8-1>;
    Extension extensions<0..2^16-1>;
} ClientHello;
~~~~~

This maps to the following Go type definitions:

~~~~~
type protocolVersion uint16
type random [32]byte
type cipherSuite uint16 // or [2]byte

type ExtensionType uint16

const (
  ExtensionTypeServerName ExtensionType = 0
  // ...
)

type Extension struct {
  ExtensionType ExtensionType
  ExtensionData []byte `tls:"head=2"`
}

type ClientHello struct {
	LegacyVersion            ProtocolVersion
	Random                   Random
	LegacySessionID          []byte        `tls:"head=1,max=32"`
	CipherSuites             []CipherSuite `tls:"head=2,min=2"`
	LegacyCompressionMethods []byte        `tls:"head=1,min=1"`
	Extensions               []Extension   `tls:"head=2"`
}
~~~~~

Then you can just declare, marshal, and unmarshal structs just like you would
with, say JSON.

The available annotations are as follows (with supported types noted):

* `omit`: Do not encode/decode this field (for: any)
* `head=n`: Encode the length header as an `n`-byte integer (for: slice)
* `head=varint`: Encode the length header as a [QUIC-style
  varint](https://tools.ietf.org/html/draft-ietf-quic-transport-27#section-16)
  (for: slice)
* `head=none`: Omit the length header on encode; consume the remainder of the
  buffer on decode (for: slice)
* `min`: The minimum length of the vector, in bytes (for: slice)
* `max`: The maximum length of the vector, in bytes (for: slice)
* `varint`: Encode the value as a QUIC-style varint (for:
  uint8, uint16, uint32, uint64)
* `optional`: Encode a pointer value as an [MLS-style
  optional](https://github.com/mlswg/mls-protocol/blob/master/draft-ietf-mls-protocol.md#tree-hashes)
  (for: pointer)

The `Marshaler` and `Unmarshaler` interfaces play the same role as in
`encoding/json`, i.e., they let the type define its own encoding directly.  The
`Validator` interface allows a type to define validation rules to be applied
when marshaling or unmarshaling.  The latter is especially helpful for `enum`
values.

## Not supported

* The `select()` syntax for creating alternate version of the same struct (see,
  e.g., the KeyShare extension)

* The backreference syntax for array lengths or select parameters, as in `opaque
  fragment[TLSPlaintext.length]`.  Note, however, that in cases where the length
  immediately preceds the array, these can be reframed as vectors with
  appropriate sizes.

## History

This code was originally part of the [mint](https://github.com/bifurcation/mint)
TLS 1.3 stack, and has been moved to this repository with the agreement of the
contributors.  Please see that repo for history before the move.
