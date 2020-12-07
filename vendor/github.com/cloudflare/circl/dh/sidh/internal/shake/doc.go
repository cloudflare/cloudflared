// Package shake provides implementation of SHA-3 and cSHAKE
// This code has been copied from golang.org/x/crypto/sha3
// and havily modified. This version doesn't use heap when
// computing cSHAKE. It makes it possible to allocate
// heap once when object is created and then reuse heap
// allocated structures in subsequent calls.
package shake
