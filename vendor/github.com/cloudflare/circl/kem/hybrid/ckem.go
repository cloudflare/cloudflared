package hybrid

// TODO move over to crypto/ecdh once we can assume Go 1.20.

import (
	"crypto/elliptic"
	cryptoRand "crypto/rand"
	"crypto/subtle"
	"math/big"

	"github.com/cloudflare/circl/kem"
	"github.com/cloudflare/circl/xof"
)

type cPublicKey struct {
	scheme *cScheme
	x, y   *big.Int
}
type cPrivateKey struct {
	scheme *cScheme
	key    []byte
}
type cScheme struct {
	curve elliptic.Curve
}

var p256Kem = &cScheme{elliptic.P256()}

func (sch *cScheme) scSize() int {
	return (sch.curve.Params().N.BitLen() + 7) / 8
}

func (sch *cScheme) ptSize() int {
	return (sch.curve.Params().BitSize + 7) / 8
}

func (sch *cScheme) Name() string {
	return sch.curve.Params().Name
}

func (sch *cScheme) PublicKeySize() int {
	return 2*sch.ptSize() + 1
}

func (sch *cScheme) PrivateKeySize() int {
	return sch.scSize()
}

func (sch *cScheme) SeedSize() int {
	return sch.PrivateKeySize()
}

func (sch *cScheme) SharedKeySize() int {
	return sch.ptSize()
}

func (sch *cScheme) CiphertextSize() int {
	return sch.PublicKeySize()
}

func (sch *cScheme) EncapsulationSeedSize() int {
	return sch.SeedSize()
}

func (sk *cPrivateKey) Scheme() kem.Scheme { return sk.scheme }
func (pk *cPublicKey) Scheme() kem.Scheme  { return pk.scheme }

func (sk *cPrivateKey) MarshalBinary() ([]byte, error) {
	ret := make([]byte, len(sk.key))
	copy(ret, sk.key)
	return ret, nil
}

func (sk *cPrivateKey) Equal(other kem.PrivateKey) bool {
	oth, ok := other.(*cPrivateKey)
	if !ok {
		return false
	}
	if oth.scheme != sk.scheme {
		return false
	}
	return subtle.ConstantTimeCompare(oth.key, sk.key) == 1
}

func (sk *cPrivateKey) Public() kem.PublicKey {
	x, y := sk.scheme.curve.ScalarBaseMult(sk.key)
	return &cPublicKey{
		sk.scheme,
		x,
		y,
	}
}

func (pk *cPublicKey) Equal(other kem.PublicKey) bool {
	oth, ok := other.(*cPublicKey)
	if !ok {
		return false
	}
	if oth.scheme != pk.scheme {
		return false
	}
	return oth.x.Cmp(pk.x) == 0 && oth.y.Cmp(pk.y) == 0
}

func (pk *cPublicKey) MarshalBinary() ([]byte, error) {
	return elliptic.Marshal(pk.scheme.curve, pk.x, pk.y), nil
}

func (sch *cScheme) GenerateKeyPair() (kem.PublicKey, kem.PrivateKey, error) {
	seed := make([]byte, sch.SeedSize())
	_, err := cryptoRand.Read(seed)
	if err != nil {
		return nil, nil, err
	}
	pk, sk := sch.DeriveKeyPair(seed)
	return pk, sk, nil
}

func (sch *cScheme) DeriveKeyPair(seed []byte) (kem.PublicKey, kem.PrivateKey) {
	if len(seed) != sch.SeedSize() {
		panic(kem.ErrSeedSize)
	}
	h := xof.SHAKE256.New()
	_, _ = h.Write(seed)
	key, x, y, err := elliptic.GenerateKey(sch.curve, h)
	if err != nil {
		panic(err)
	}

	sk := cPrivateKey{scheme: sch, key: key}
	pk := cPublicKey{scheme: sch, x: x, y: y}

	return &pk, &sk
}

func (sch *cScheme) Encapsulate(pk kem.PublicKey) (ct, ss []byte, err error) {
	seed := make([]byte, sch.EncapsulationSeedSize())
	_, err = cryptoRand.Read(seed)
	if err != nil {
		return
	}
	return sch.EncapsulateDeterministically(pk, seed)
}

func (pk *cPublicKey) X(sk *cPrivateKey) []byte {
	if pk.scheme != sk.scheme {
		panic(kem.ErrTypeMismatch)
	}

	sharedKey := make([]byte, pk.scheme.SharedKeySize())
	xShared, _ := pk.scheme.curve.ScalarMult(pk.x, pk.y, sk.key)
	xShared.FillBytes(sharedKey)
	return sharedKey
}

func (sch *cScheme) EncapsulateDeterministically(
	pk kem.PublicKey, seed []byte,
) (ct, ss []byte, err error) {
	if len(seed) != sch.EncapsulationSeedSize() {
		return nil, nil, kem.ErrSeedSize
	}
	pub, ok := pk.(*cPublicKey)
	if !ok || pub.scheme != sch {
		return nil, nil, kem.ErrTypeMismatch
	}

	pk2, sk2 := sch.DeriveKeyPair(seed)
	ss = pub.X(sk2.(*cPrivateKey))
	ct, _ = pk2.MarshalBinary()
	return
}

func (sch *cScheme) Decapsulate(sk kem.PrivateKey, ct []byte) ([]byte, error) {
	if len(ct) != sch.CiphertextSize() {
		return nil, kem.ErrCiphertextSize
	}

	priv, ok := sk.(*cPrivateKey)
	if !ok || priv.scheme != sch {
		return nil, kem.ErrTypeMismatch
	}

	pk, err := sch.UnmarshalBinaryPublicKey(ct)
	if err != nil {
		return nil, err
	}

	ss := pk.(*cPublicKey).X(priv)
	return ss, nil
}

func (sch *cScheme) UnmarshalBinaryPublicKey(buf []byte) (kem.PublicKey, error) {
	if len(buf) != sch.PublicKeySize() {
		return nil, kem.ErrPubKeySize
	}
	x, y := elliptic.Unmarshal(sch.curve, buf)
	return &cPublicKey{sch, x, y}, nil
}

func (sch *cScheme) UnmarshalBinaryPrivateKey(buf []byte) (kem.PrivateKey, error) {
	if len(buf) != sch.PrivateKeySize() {
		return nil, kem.ErrPrivKeySize
	}
	ret := cPrivateKey{sch, make([]byte, sch.PrivateKeySize())}
	copy(ret.key, buf)
	return &ret, nil
}
