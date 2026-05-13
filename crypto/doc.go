// Package crypto centralizes the cryptographic primitives and TLS
// configuration used by cloudflared when establishing connections to the
// Cloudflare edge.
//
// The primary responsibility of the package is to expose a single, canonical
// source of TLS curve preferences so that every edge-facing transport (QUIC
// and HTTP/2) negotiates the same key-exchange algorithms regardless of the
// code path that sets up the connection.
//
// # Post-Quantum key exchange
//
// cloudflared supports the X25519MLKEM768 hybrid post-quantum key exchange.
// Two operating modes are exposed via the features.PostQuantumMode flag:
//
//   - PostQuantumPrefer: advertise X25519MLKEM768 and the deprecated
//     P256Kyber768Draft00 first, then fall back to the classical CurveP256
//     if the peer does not support either PQ curve. This is the default
//     used for every outbound edge connection.
//   - PostQuantumStrict: advertise only the PQ curves (X25519MLKEM768 and
//     P256Kyber768Draft00). Activated by the user via the --post-quantum
//     CLI flag. No classical fallback is offered, so a peer that does not
//     support any PQ curve will fail the handshake.
//
// The resulting curve lists are identical under FIPS and non-FIPS builds,
// which is why GetCurvePreferences does not take a FIPS toggle. If that
// property ever changes (for example, if a curve stops being FIPS-approved),
// the divergence should be expressed inside this package so callers remain
// unchanged.
//
// # Thread-safety
//
// GetCurvePreferences returns a fresh slice on every call. Callers are free
// to mutate the returned slice without affecting the package-level defaults
// or other callers.
package crypto
