package sidh

import (
	"errors"
	"io"

	"github.com/cloudflare/circl/dh/sidh/internal/common"
	"github.com/cloudflare/circl/dh/sidh/internal/p503"
	"github.com/cloudflare/circl/dh/sidh/internal/p751"
)

// I keep it bool in order to be able to apply logical NOT
type KeyVariant uint

// Base type for public and private key. Used mainly to carry domain
// parameters.
type key struct {
	// Domain parameters of the algorithm to be used with a key
	params *common.SidhParams
	// Flag indicates wether corresponds to 2-, 3-torsion group or SIKE
	keyVariant KeyVariant
}

// Defines operations on public key
type PublicKey struct {
	key
	// x-coordinates of P,Q,P-Q in this exact order
	affine3Pt [3]common.Fp2
}

// Defines operations on private key
type PrivateKey struct {
	key
	// Secret key
	Scalar []byte
	// Used only by KEM
	S []byte
}

// Id's correspond to bitlength of the prime field characteristic
// Currently Fp751 is the only one supported by this implementation
const (
	Fp503 = common.Fp503
	Fp751 = common.Fp751
)

const (
	// First 2 bits identify SIDH variant third bit indicates
	// wether key is a SIKE variant (set) or SIDH (not set)

	// 001 - SIDH: corresponds to 2-torsion group
	KeyVariantSidhA KeyVariant = 1 << 0
	// 010 - SIDH: corresponds to 3-torsion group
	KeyVariantSidhB = 1 << 1
	// 110 - SIKE
	KeyVariantSike = 1<<2 | KeyVariantSidhB
)

// Accessor to key variant
func (key *key) Variant() KeyVariant {
	return key.keyVariant
}

// NewPublicKey initializes public key.
// Usage of this function guarantees that the object is correctly initialized.
func NewPublicKey(id uint8, v KeyVariant) *PublicKey {
	return &PublicKey{key: key{params: common.Params(id), keyVariant: v}}
}

// Import clears content of the public key currently stored in the structure
// and imports key stored in the byte string. Returns error in case byte string
// size is wrong. Doesn't perform any validation.
func (pub *PublicKey) Import(input []byte) error {
	if len(input) != pub.Size() {
		return errors.New("sidh: input to short")
	}
	ssSz := pub.params.SharedSecretSize
	common.BytesToFp2(&pub.affine3Pt[0], input[0:ssSz], pub.params.Bytelen)
	common.BytesToFp2(&pub.affine3Pt[1], input[ssSz:2*ssSz], pub.params.Bytelen)
	common.BytesToFp2(&pub.affine3Pt[2], input[2*ssSz:3*ssSz], pub.params.Bytelen)
	switch pub.params.ID {
	case Fp503:
		p503.ToMontgomery(&pub.affine3Pt[0], &pub.affine3Pt[0])
		p503.ToMontgomery(&pub.affine3Pt[1], &pub.affine3Pt[1])
		p503.ToMontgomery(&pub.affine3Pt[2], &pub.affine3Pt[2])
	case Fp751:
		p751.ToMontgomery(&pub.affine3Pt[0], &pub.affine3Pt[0])
		p751.ToMontgomery(&pub.affine3Pt[1], &pub.affine3Pt[1])
		p751.ToMontgomery(&pub.affine3Pt[2], &pub.affine3Pt[2])
	default:
		panic("Unsupported key")
	}
	return nil
}

// Exports currently stored key. In case structure hasn't been filled with key data
// returned byte string is filled with zeros.
func (pub *PublicKey) Export(out []byte) {
	var feTmp [3]common.Fp2
	ssSz := pub.params.SharedSecretSize
	switch pub.params.ID {
	case Fp503:
		p503.FromMontgomery(&feTmp[0], &pub.affine3Pt[0])
		p503.FromMontgomery(&feTmp[1], &pub.affine3Pt[1])
		p503.FromMontgomery(&feTmp[2], &pub.affine3Pt[2])
	case Fp751:
		p751.FromMontgomery(&feTmp[0], &pub.affine3Pt[0])
		p751.FromMontgomery(&feTmp[1], &pub.affine3Pt[1])
		p751.FromMontgomery(&feTmp[2], &pub.affine3Pt[2])
	default:
		panic("Unsupported key")
	}
	common.Fp2ToBytes(out[0:ssSz], &feTmp[0], pub.params.Bytelen)
	common.Fp2ToBytes(out[ssSz:2*ssSz], &feTmp[1], pub.params.Bytelen)
	common.Fp2ToBytes(out[2*ssSz:3*ssSz], &feTmp[2], pub.params.Bytelen)
}

// Size returns size of the public key in bytes
func (pub *PublicKey) Size() int {
	return pub.params.PublicKeySize
}

// NewPrivateKey initializes private key.
// Usage of this function guarantees that the object is correctly initialized.
func NewPrivateKey(id uint8, v KeyVariant) *PrivateKey {
	prv := &PrivateKey{key: key{params: common.Params(id), keyVariant: v}}
	if (v & KeyVariantSidhA) == KeyVariantSidhA {
		prv.Scalar = make([]byte, prv.params.A.SecretByteLen)
	} else {
		prv.Scalar = make([]byte, prv.params.B.SecretByteLen)
	}
	if v == KeyVariantSike {
		prv.S = make([]byte, prv.params.MsgLen)
	}
	return prv
}

// Exports currently stored key. In case structure hasn't been filled with key data
// returned byte string is filled with zeros.
func (prv *PrivateKey) Export(out []byte) {
	copy(out, prv.S)
	copy(out[len(prv.S):], prv.Scalar)
}

// Size returns size of the private key in bytes
func (prv *PrivateKey) Size() int {
	tmp := len(prv.Scalar)
	if prv.Variant() == KeyVariantSike {
		tmp += prv.params.MsgLen
	}
	return tmp
}

// Size returns size of the shared secret
func (prv *PrivateKey) SharedSecretSize() int {
	return prv.params.SharedSecretSize
}

// Import clears content of the private key currently stored in the structure
// and imports key from octet string. In case of SIKE, the random value 'S'
// must be prepended to the value of actual private key (see SIKE spec for details).
// Function doesn't import public key value to PrivateKey object.
func (prv *PrivateKey) Import(input []byte) error {
	if len(input) != prv.Size() {
		return errors.New("sidh: input to short")
	}
	copy(prv.S, input[:len(prv.S)])
	copy(prv.Scalar, input[len(prv.S):])
	return nil
}

// Generates random private key for SIDH or SIKE. Generated value is
// formed as little-endian integer from key-space <2^(e2-1)..2^e2 - 1>
// for KeyVariant_A or <2^(s-1)..2^s - 1>, where s = floor(log_2(3^e3)),
// for KeyVariant_B.
//
// Returns error in case user provided RNG fails.
func (prv *PrivateKey) Generate(rand io.Reader) error {
	var dp *common.DomainParams

	if (prv.keyVariant & KeyVariantSidhA) == KeyVariantSidhA {
		dp = &prv.params.A
	} else {
		dp = &prv.params.B
	}

	if prv.keyVariant == KeyVariantSike {
		if _, err := io.ReadFull(rand, prv.S); err != nil {
			return err
		}
	}

	// Private key generation takes advantage of the fact that keyspace for secret
	// key is (0, 2^x - 1), for some possitivite value of 'x' (see SIKE, 1.3.8).
	// It means that all bytes in the secret key, but the last one, can take any
	// value between <0x00,0xFF>. Similarly for the last byte, but generation
	// needs to chop off some bits, to make sure generated value is an element of
	// a key-space.
	if _, err := io.ReadFull(rand, prv.Scalar); err != nil {
		return err
	}

	prv.Scalar[len(prv.Scalar)-1] &= (1 << (dp.SecretBitLen % 8)) - 1
	// Make sure scalar is SecretBitLen long. SIKE spec says that key
	// space starts from 0, but I'm not comfortable with having low
	// value scalars used for private keys. It is still secrure as per
	// table 5.1 in [SIKE].
	prv.Scalar[len(prv.Scalar)-1] |= 1 << ((dp.SecretBitLen % 8) - 1)

	return nil
}

// Generates public key.
func (prv *PrivateKey) GeneratePublicKey(pub *PublicKey) {
	var isA = (prv.keyVariant & KeyVariantSidhA) == KeyVariantSidhA

	if (pub.keyVariant != prv.keyVariant) || (pub.params.ID != prv.params.ID) {
		panic("sidh: incompatbile public key")
	}

	switch prv.params.ID {
	case Fp503:
		if isA {
			p503.PublicKeyGenA(&pub.affine3Pt, prv.Scalar)
		} else {
			p503.PublicKeyGenB(&pub.affine3Pt, prv.Scalar)
		}
	case Fp751:
		if isA {
			p751.PublicKeyGenA(&pub.affine3Pt, prv.Scalar)
		} else {
			p751.PublicKeyGenB(&pub.affine3Pt, prv.Scalar)
		}
	default:
		panic("Field not supported")
	}
}

// Computes a SIDH shared secret. Function requires that pub has different
// KeyVariant than prv. Length of returned output is 2*ceil(log_2 P)/8),
// where P is a prime defining finite field.
//
// Caller must make sure key SIDH key pair is not used more than once.
func (prv *PrivateKey) DeriveSecret(ss []byte, pub *PublicKey) {
	var isA = (prv.keyVariant & KeyVariantSidhA) == KeyVariantSidhA

	if (pub.keyVariant == prv.keyVariant) || (pub.params.ID != prv.params.ID) {
		panic("sidh: public and private are incompatbile")
	}

	switch prv.params.ID {
	case Fp503:
		if isA {
			p503.DeriveSecretA(ss, prv.Scalar, &pub.affine3Pt)
		} else {
			p503.DeriveSecretB(ss, prv.Scalar, &pub.affine3Pt)
		}
	case Fp751:
		if isA {
			p751.DeriveSecretA(ss, prv.Scalar, &pub.affine3Pt)
		} else {
			p751.DeriveSecretB(ss, prv.Scalar, &pub.affine3Pt)
		}
	default:
		panic("Field not supported")
	}
}
