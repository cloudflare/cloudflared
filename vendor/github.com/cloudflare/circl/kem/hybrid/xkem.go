package hybrid

import (
	"bytes"
	cryptoRand "crypto/rand"
	"crypto/subtle"

	"github.com/cloudflare/circl/dh/x25519"
	"github.com/cloudflare/circl/dh/x448"
	"github.com/cloudflare/circl/internal/sha3"
	"github.com/cloudflare/circl/kem"
)

type xPublicKey struct {
	scheme *xScheme
	key    []byte
}
type xPrivateKey struct {
	scheme *xScheme
	key    []byte
}
type xScheme struct {
	size int
}

var (
	x25519Kem = &xScheme{x25519.Size}
	x448Kem   = &xScheme{x448.Size}
)

func (sch *xScheme) Name() string {
	switch sch.size {
	case x25519.Size:
		return "X25519"
	case x448.Size:
		return "X448"
	}
	panic(kem.ErrTypeMismatch)
}

func (sch *xScheme) PublicKeySize() int         { return sch.size }
func (sch *xScheme) PrivateKeySize() int        { return sch.size }
func (sch *xScheme) SeedSize() int              { return sch.size }
func (sch *xScheme) SharedKeySize() int         { return sch.size }
func (sch *xScheme) CiphertextSize() int        { return sch.size }
func (sch *xScheme) EncapsulationSeedSize() int { return sch.size }

func (sk *xPrivateKey) Scheme() kem.Scheme { return sk.scheme }
func (pk *xPublicKey) Scheme() kem.Scheme  { return pk.scheme }

func (sk *xPrivateKey) MarshalBinary() ([]byte, error) {
	ret := make([]byte, len(sk.key))
	copy(ret, sk.key)
	return ret, nil
}

func (sk *xPrivateKey) Equal(other kem.PrivateKey) bool {
	oth, ok := other.(*xPrivateKey)
	if !ok {
		return false
	}
	if oth.scheme != sk.scheme {
		return false
	}
	return subtle.ConstantTimeCompare(oth.key, sk.key) == 1
}

func (sk *xPrivateKey) Public() kem.PublicKey {
	pk := xPublicKey{sk.scheme, make([]byte, sk.scheme.size)}
	switch sk.scheme.size {
	case x25519.Size:
		var sk2, pk2 x25519.Key
		copy(sk2[:], sk.key)
		x25519.KeyGen(&pk2, &sk2)
		copy(pk.key, pk2[:])
	case x448.Size:
		var sk2, pk2 x448.Key
		copy(sk2[:], sk.key)
		x448.KeyGen(&pk2, &sk2)
		copy(pk.key, pk2[:])
	}
	return &pk
}

func (pk *xPublicKey) Equal(other kem.PublicKey) bool {
	oth, ok := other.(*xPublicKey)
	if !ok {
		return false
	}
	if oth.scheme != pk.scheme {
		return false
	}
	return bytes.Equal(oth.key, pk.key)
}

func (pk *xPublicKey) MarshalBinary() ([]byte, error) {
	ret := make([]byte, pk.scheme.size)
	copy(ret, pk.key)
	return ret, nil
}

func (sch *xScheme) GenerateKeyPair() (kem.PublicKey, kem.PrivateKey, error) {
	seed := make([]byte, sch.SeedSize())
	_, err := cryptoRand.Read(seed)
	if err != nil {
		return nil, nil, err
	}
	pk, sk := sch.DeriveKeyPair(seed)
	return pk, sk, nil
}

func (sch *xScheme) DeriveKeyPair(seed []byte) (kem.PublicKey, kem.PrivateKey) {
	if len(seed) != sch.SeedSize() {
		panic(kem.ErrSeedSize)
	}
	sk := xPrivateKey{scheme: sch, key: make([]byte, sch.size)}

	h := sha3.NewShake256()
	_, _ = h.Write(seed)
	_, _ = h.Read(sk.key)

	return sk.Public(), &sk
}

func (sch *xScheme) Encapsulate(pk kem.PublicKey) (ct, ss []byte, err error) {
	seed := make([]byte, sch.EncapsulationSeedSize())
	_, err = cryptoRand.Read(seed)
	if err != nil {
		return
	}
	return sch.EncapsulateDeterministically(pk, seed)
}

func (pk *xPublicKey) X(sk *xPrivateKey) []byte {
	if pk.scheme != sk.scheme {
		panic(kem.ErrTypeMismatch)
	}

	switch pk.scheme.size {
	case x25519.Size:
		var ss2, pk2, sk2 x25519.Key
		copy(pk2[:], pk.key)
		copy(sk2[:], sk.key)
		x25519.Shared(&ss2, &sk2, &pk2)
		return ss2[:]
	case x448.Size:
		var ss2, pk2, sk2 x448.Key
		copy(pk2[:], pk.key)
		copy(sk2[:], sk.key)
		x448.Shared(&ss2, &sk2, &pk2)
		return ss2[:]
	}
	panic(kem.ErrTypeMismatch)
}

func (sch *xScheme) EncapsulateDeterministically(
	pk kem.PublicKey, seed []byte,
) (ct, ss []byte, err error) {
	if len(seed) != sch.EncapsulationSeedSize() {
		return nil, nil, kem.ErrSeedSize
	}
	pub, ok := pk.(*xPublicKey)
	if !ok || pub.scheme != sch {
		return nil, nil, kem.ErrTypeMismatch
	}

	pk2, sk2 := sch.DeriveKeyPair(seed)
	ss = pub.X(sk2.(*xPrivateKey))
	ct, _ = pk2.MarshalBinary()
	return
}

func (sch *xScheme) Decapsulate(sk kem.PrivateKey, ct []byte) ([]byte, error) {
	if len(ct) != sch.CiphertextSize() {
		return nil, kem.ErrCiphertextSize
	}

	priv, ok := sk.(*xPrivateKey)
	if !ok || priv.scheme != sch {
		return nil, kem.ErrTypeMismatch
	}

	pk, err := sch.UnmarshalBinaryPublicKey(ct)
	if err != nil {
		return nil, err
	}

	ss := pk.(*xPublicKey).X(priv)
	return ss, nil
}

func (sch *xScheme) UnmarshalBinaryPublicKey(buf []byte) (kem.PublicKey, error) {
	if len(buf) != sch.PublicKeySize() {
		return nil, kem.ErrPubKeySize
	}
	ret := xPublicKey{sch, make([]byte, sch.size)}
	copy(ret.key, buf)
	return &ret, nil
}

func (sch *xScheme) UnmarshalBinaryPrivateKey(buf []byte) (kem.PrivateKey, error) {
	if len(buf) != sch.PrivateKeySize() {
		return nil, kem.ErrPrivKeySize
	}
	ret := xPrivateKey{sch, make([]byte, sch.size)}
	copy(ret.key, buf)
	return &ret, nil
}
