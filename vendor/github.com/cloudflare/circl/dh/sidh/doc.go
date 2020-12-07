// Package sidh provides implementation of experimental post-quantum
// Supersingular Isogeny Diffie-Hellman (SIDH) as well as Supersingular
// Isogeny Key Encapsulation (SIKE).
//
// It comes with implementations of 2 different field arithmetic
// implementations sidh.Fp503 and sidh.Fp751.
//
//	| Algoirthm | Public Key Size | Shared Secret Size | Ciphertext Size |
//	|-----------|-----------------|--------------------|-----------------|
//	| SIDH/p503 |          376    |        126         | N/A             |
//	| SIDH/p751 |          564    |        188         | N/A             |
//	| SIKE/p503 |          376    |         16         | 402             |
//	| SIKE/p751 |          564    |         24         | 596             |
//
// In order to instantiate SIKE/p751 KEM one needs to create a KEM object
// and allocate internal structures. This can be done with NewSike751 helper.
// After that kem can be used multiple times.
//
//	var kem = sike.NewSike751(rand.Reader)
//	kem.Encapsulate(ciphertext, sharedSecret, publicBob)
//	kem.Decapsulate(sharedSecret, privateBob, PublicBob, ciphertext)
//
// Code is optimized for AMD64 and aarch64. Generic implementation
// is provided for other architectures.
//
// References:
// - [SIDH] https://eprint.iacr.org/2011/506
// - [SIKE] http://www.sike.org/files/SIDH-spec.pdf
//
package sidh
