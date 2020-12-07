### x448 - curve448 ECDH
#### Yawning Angel (yawning at schwanenlied dot me)

A straight forward port of Michael Hamburg's x448 code to Go lang.

See: https://www.rfc-editor.org/rfc/rfc7748.txt

If you're familiar with how to use golang.org/x/crypto/curve25519, you will be
right at home with using x448, since the functions are the same.  Generate a
random secret key, ScalarBaseMult() to get the public key, etc etc etc.

Both routines return 0 on success, -1 on failure which MUST be checked, and
the handshake aborted on failure.
