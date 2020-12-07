package hpke

import (
	"bytes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"
	"log"

	"github.com/cisco/go-tls-syntax"
)

const (
	debug    = true
	rfcLabel = "HPKE-06"
)

type KEMPrivateKey interface {
	PublicKey() KEMPublicKey
}

type KEMPublicKey interface{}

type KEMScheme interface {
	ID() KEMID
	DeriveKeyPair(ikm []byte) (KEMPrivateKey, KEMPublicKey, error)
	Serialize(pk KEMPublicKey) []byte
	Deserialize(enc []byte) (KEMPublicKey, error)
	Encap(rand io.Reader, pkR KEMPublicKey) ([]byte, []byte, error)
	Decap(enc []byte, skR KEMPrivateKey) ([]byte, error)
	PublicKeySize() int
	PrivateKeySize() int

	SerializePrivate(sk KEMPrivateKey) []byte
	DeserializePrivate(enc []byte) (KEMPrivateKey, error)

	setEphemeralKeyPair(sk KEMPrivateKey)
}

type AuthKEMScheme interface {
	KEMScheme
	AuthEncap(rand io.Reader, pkR KEMPublicKey, skS KEMPrivateKey) ([]byte, []byte, error)
	AuthDecap(enc []byte, skR KEMPrivateKey, pkS KEMPublicKey) ([]byte, error)
}

type KDFScheme interface {
	ID() KDFID
	Hash(message []byte) []byte
	Extract(salt, ikm []byte) []byte
	Expand(prk, info []byte, L int) []byte
	LabeledExtract(salt []byte, suiteID []byte, label string, ikm []byte) []byte
	LabeledExpand(prk []byte, suiteID []byte, label string, info []byte, L int) []byte
	OutputSize() int
}

type AEADScheme interface {
	ID() AEADID
	New(key []byte) (cipher.AEAD, error)
	KeySize() int
	NonceSize() int
}

type CipherSuite struct {
	KEM  KEMScheme
	KDF  KDFScheme
	AEAD AEADScheme
}

func (suite CipherSuite) ID() []byte {
	suiteID := make([]byte, 6)
	binary.BigEndian.PutUint16(suiteID, uint16(suite.KEM.ID()))
	binary.BigEndian.PutUint16(suiteID[2:], uint16(suite.KDF.ID()))
	binary.BigEndian.PutUint16(suiteID[4:], uint16(suite.AEAD.ID()))
	return append([]byte("HPKE"), suiteID...)
}

type Mode uint8

const (
	modeBase    Mode = 0x00
	modePSK     Mode = 0x01
	modeAuth    Mode = 0x02
	modeAuthPSK Mode = 0x03
)

func logString(val string) {
	if debug {
		log.Printf("%s", val)
	}
}

func logVal(name string, value []byte) {
	if debug {
		log.Printf("  %6s %x", name, value)
	}
}

///////
// Core

func defaultPSK(suite CipherSuite) []byte {
	return []byte{}
}

func defaultPSKID(suite CipherSuite) []byte {
	return []byte{}
}

func verifyPSKInputs(suite CipherSuite, mode Mode, psk, pskID []byte) error {
	defaultPSK := defaultPSK(suite)
	defaultPSKID := defaultPSKID(suite)
	pskMode := map[Mode]bool{modePSK: true, modeAuthPSK: true}

	gotPSK := !bytes.Equal(psk, defaultPSK)
	gotPSKID := !bytes.Equal(pskID, defaultPSKID)

	switch {
	case gotPSK != gotPSKID:
		return fmt.Errorf("Inconsistent PSK inputs [%d] [%v] [%v]", mode, gotPSK, gotPSKID)
	case gotPSK && !pskMode[mode]:
		return fmt.Errorf("PSK input provided when not needed [%d]", mode)
	case !gotPSK && pskMode[mode]:
		return fmt.Errorf("Missing required PSK input [%d]", mode)
	}

	return nil
}

type hpkeContext struct {
	mode      Mode
	pskIDHash []byte `tls:"head=none"`
	infoHash  []byte `tls:"head=none"`
}

type contextParameters struct {
	suite              CipherSuite
	keyScheduleContext []byte
	secret             []byte
}

func (cp contextParameters) aeadKey() []byte {
	return cp.suite.KDF.LabeledExpand(cp.secret, cp.suite.ID(), "key", cp.keyScheduleContext, cp.suite.AEAD.KeySize())
}

func (cp contextParameters) exporterSecret() []byte {
	return cp.suite.KDF.LabeledExpand(cp.secret, cp.suite.ID(), "exp", cp.keyScheduleContext, cp.suite.KDF.OutputSize())
}

func (cp contextParameters) aeadBaseNonce() []byte {
	return cp.suite.KDF.LabeledExpand(cp.secret, cp.suite.ID(), "base_nonce", cp.keyScheduleContext, cp.suite.AEAD.NonceSize())
}

type setupParameters struct {
	sharedSecret []byte
	enc          []byte
}

func keySchedule(suite CipherSuite, mode Mode, sharedSecret, info, psk, pskID []byte) (contextParameters, error) {
	err := verifyPSKInputs(suite, mode, psk, pskID)
	if err != nil {
		return contextParameters{}, err
	}

	suiteID := suite.ID()
	pskIDHash := suite.KDF.LabeledExtract(nil, suiteID, "psk_id_hash", pskID)
	infoHash := suite.KDF.LabeledExtract(nil, suiteID, "info_hash", info)

	contextStruct := hpkeContext{mode, pskIDHash, infoHash}
	keyScheduleContext, err := syntax.Marshal(contextStruct)
	if err != nil {
		return contextParameters{}, err
	}

	secret := suite.KDF.LabeledExtract(sharedSecret, suiteID, "secret", psk)

	params := contextParameters{
		suite:              suite,
		keyScheduleContext: keyScheduleContext,
		secret:             secret,
	}

	return params, nil
}

// contextRole specifies the role of a party in possession of a Context: if
// equal to `contextRoleSender`, then the party is the sender; if equal to
// `contextRoleReceiver`, then the party is the receiver.
type contextRole uint8

const (
	contextRoleSender   contextRole = 0x00
	contextRoleReceiver contextRole = 0x01
)

// context represents an HPKE context encoded on the wire.
type context struct {
	// Marshaled fields
	Role           contextRole
	KEMID          KEMID
	KDFID          KDFID
	AEADID         AEADID
	ExporterSecret []byte `tls:"head=1"`
	Key            []byte `tls:"head=1"`
	BaseNonce      []byte `tls:"head=1"`
	Seq            uint64

	// Operational structures
	aead  cipher.AEAD `tls:"omit"`
	suite CipherSuite `tls:"omit"`

	// Historical record
	nonces        [][]byte          `tls:"omit"`
	setupParams   setupParameters   `tls:"omit"`
	contextParams contextParameters `tls:"omit"`
}

func newContext(role contextRole, suite CipherSuite, setupParams setupParameters, contextParams contextParameters) (context, error) {
	key := contextParams.aeadKey()
	baseNonce := contextParams.aeadBaseNonce()
	exporterSecret := contextParams.exporterSecret()

	aead, err := suite.AEAD.New(key)
	if err != nil {
		return context{}, err
	}

	ctx := context{
		Role:           role,
		KEMID:          suite.KEM.ID(),
		KDFID:          suite.KDF.ID(),
		AEADID:         suite.AEAD.ID(),
		ExporterSecret: exporterSecret,
		Key:            key,
		BaseNonce:      baseNonce,
		Seq:            0,
		aead:           aead,
		suite:          suite,
		setupParams:    setupParams,
		contextParams:  contextParams,
	}

	return ctx, nil
}

func unmarshalContext(role contextRole, opaque []byte) (context, error) {
	var ctx context
	var err error
	if _, err = syntax.Unmarshal(opaque, &ctx); err != nil {
		return context{}, err
	}

	if ctx.Role != role {
		return context{}, fmt.Errorf("role mismatch")
	}

	ctx.suite, err = AssembleCipherSuite(ctx.KEMID, ctx.KDFID, ctx.AEADID)
	if err != nil {
		return context{}, err
	}

	// Construct AEAD and validate the key length.
	ctx.aead, err = ctx.suite.AEAD.New(ctx.Key)
	if err != nil {
		return context{}, err
	}

	// Validate the nonce length.
	if len(ctx.BaseNonce) != ctx.aead.NonceSize() {
		return context{}, fmt.Errorf("base nonce length: got %d; want %d", len(ctx.BaseNonce), ctx.aead.NonceSize())
	}

	// Validate the exporter secret length.
	if len(ctx.ExporterSecret) != ctx.suite.KDF.OutputSize() {
		return context{}, fmt.Errorf("exporter secret length: got %d; want %d", len(ctx.ExporterSecret), ctx.suite.KDF.OutputSize())
	}

	return ctx, nil
}

func (ctx *context) computeNonce() []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, ctx.Seq)

	Nn := len(ctx.BaseNonce)
	nonce := make([]byte, Nn)
	copy(nonce, ctx.BaseNonce)
	for i := range buf {
		nonce[Nn-8+i] ^= buf[i]
	}

	ctx.nonces = append(ctx.nonces, nonce)
	return nonce
}

func (ctx *context) incrementSeq() {
	ctx.Seq += 1
	if ctx.Seq == 0 {
		panic("sequence number wrapped")
	}
}

func (ctx *context) Export(context []byte, L int) []byte {
	return ctx.suite.KDF.LabeledExpand(ctx.ExporterSecret, ctx.suite.ID(), "sec", context, L)
}

func (ctx *context) Marshal() ([]byte, error) {
	return syntax.Marshal(ctx)
}

type EncryptContext struct {
	context
}

func newEncryptContext(suite CipherSuite, setupParams setupParameters, contextParams contextParameters) (*EncryptContext, error) {
	ctx, err := newContext(contextRoleSender, suite, setupParams, contextParams)
	if err != nil {
		return nil, err
	}

	return &EncryptContext{ctx}, nil
}

func (ctx *EncryptContext) Seal(aad, pt []byte) []byte {
	ct := ctx.aead.Seal(nil, ctx.computeNonce(), pt, aad)
	ctx.incrementSeq()
	return ct
}

func UnmarshalEncryptContext(opaque []byte) (*EncryptContext, error) {
	ctx, err := unmarshalContext(contextRoleSender, opaque)
	if err != nil {
		return nil, err
	}

	return &EncryptContext{ctx}, nil
}

type DecryptContext struct {
	context
}

func newDecryptContext(suite CipherSuite, setupParams setupParameters, contextParams contextParameters) (*DecryptContext, error) {
	ctx, err := newContext(contextRoleReceiver, suite, setupParams, contextParams)
	if err != nil {
		return nil, err
	}

	return &DecryptContext{ctx}, nil
}

func (ctx *DecryptContext) Open(aad, ct []byte) ([]byte, error) {
	pt, err := ctx.aead.Open(nil, ctx.computeNonce(), ct, aad)
	if err != nil {
		return nil, err
	}

	ctx.incrementSeq()
	return pt, nil
}

func UnmarshalDecryptContext(opaque []byte) (*DecryptContext, error) {
	ctx, err := unmarshalContext(contextRoleReceiver, opaque)
	if err != nil {
		return nil, err
	}

	return &DecryptContext{ctx}, nil
}

///////
// Base

func SetupBaseS(suite CipherSuite, rand io.Reader, pkR KEMPublicKey, info []byte) ([]byte, *EncryptContext, error) {
	// sharedSecret, enc = Encap(pkR)
	sharedSecret, enc, err := suite.KEM.Encap(rand, pkR)
	if err != nil {
		return nil, nil, err
	}

	setupParams := setupParameters{
		sharedSecret: sharedSecret,
		enc:          enc,
	}

	params, err := keySchedule(suite, modeBase, sharedSecret, info, defaultPSK(suite), defaultPSKID(suite))
	if err != nil {
		return nil, nil, err
	}

	ctx, err := newEncryptContext(suite, setupParams, params)
	return enc, ctx, err
}

func SetupBaseR(suite CipherSuite, skR KEMPrivateKey, enc, info []byte) (*DecryptContext, error) {
	// sharedSecret = Decap(enc, skR)
	sharedSecret, err := suite.KEM.Decap(enc, skR)
	if err != nil {
		return nil, err
	}

	setupParams := setupParameters{
		sharedSecret: sharedSecret,
		enc:          enc,
	}

	params, err := keySchedule(suite, modeBase, sharedSecret, info, defaultPSK(suite), defaultPSKID(suite))
	if err != nil {
		return nil, err
	}

	return newDecryptContext(suite, setupParams, params)
}

//////
// PSK

func SetupPSKS(suite CipherSuite, rand io.Reader, pkR KEMPublicKey, psk, pskID, info []byte) ([]byte, *EncryptContext, error) {
	// sharedSecret, enc = Encap(pkR)
	sharedSecret, enc, err := suite.KEM.Encap(rand, pkR)
	if err != nil {
		return nil, nil, err
	}

	setupParams := setupParameters{
		sharedSecret: sharedSecret,
		enc:          enc,
	}

	params, err := keySchedule(suite, modePSK, sharedSecret, info, psk, pskID)
	if err != nil {
		return nil, nil, err
	}

	ctx, err := newEncryptContext(suite, setupParams, params)
	return enc, ctx, err
}

func SetupPSKR(suite CipherSuite, skR KEMPrivateKey, enc, psk, pskID, info []byte) (*DecryptContext, error) {
	// sharedSecret = Decap(enc, skR)
	sharedSecret, err := suite.KEM.Decap(enc, skR)
	if err != nil {
		return nil, err
	}

	setupParams := setupParameters{
		sharedSecret: sharedSecret,
		enc:          enc,
	}

	params, err := keySchedule(suite, modePSK, sharedSecret, info, psk, pskID)
	if err != nil {
		return nil, err
	}

	return newDecryptContext(suite, setupParams, params)
}

///////
// Auth

func SetupAuthS(suite CipherSuite, rand io.Reader, pkR KEMPublicKey, skS KEMPrivateKey, info []byte) ([]byte, *EncryptContext, error) {
	// sharedSecret, enc = AuthEncap(pkR, skS)
	auth := suite.KEM.(AuthKEMScheme)
	sharedSecret, enc, err := auth.AuthEncap(rand, pkR, skS)
	if err != nil {
		return nil, nil, err
	}

	setupParams := setupParameters{
		sharedSecret: sharedSecret,
		enc:          enc,
	}

	params, err := keySchedule(suite, modeAuth, sharedSecret, info, defaultPSK(suite), defaultPSKID(suite))
	if err != nil {
		return nil, nil, err
	}

	ctx, err := newEncryptContext(suite, setupParams, params)
	return enc, ctx, err
}

func SetupAuthR(suite CipherSuite, skR KEMPrivateKey, pkS KEMPublicKey, enc, info []byte) (*DecryptContext, error) {
	// sharedSecret = AuthDecap(enc, skR, pkS)
	auth := suite.KEM.(AuthKEMScheme)
	sharedSecret, err := auth.AuthDecap(enc, skR, pkS)
	if err != nil {
		return nil, err
	}

	setupParams := setupParameters{
		sharedSecret: sharedSecret,
		enc:          enc,
	}

	params, err := keySchedule(suite, modeAuth, sharedSecret, info, defaultPSK(suite), defaultPSKID(suite))
	if err != nil {
		return nil, err
	}

	return newDecryptContext(suite, setupParams, params)
}

/////////////
// PSK + Auth

func SetupAuthPSKS(suite CipherSuite, rand io.Reader, pkR KEMPublicKey, skS KEMPrivateKey, psk, pskID, info []byte) ([]byte, *EncryptContext, error) {
	// sharedSecret, enc = AuthEncap(pkR, skS)
	auth := suite.KEM.(AuthKEMScheme)
	sharedSecret, enc, err := auth.AuthEncap(rand, pkR, skS)
	if err != nil {
		return nil, nil, err
	}

	setupParams := setupParameters{
		sharedSecret: sharedSecret,
		enc:          enc,
	}

	params, err := keySchedule(suite, modeAuthPSK, sharedSecret, info, psk, pskID)
	if err != nil {
		return nil, nil, err
	}

	ctx, err := newEncryptContext(suite, setupParams, params)
	return enc, ctx, err
}

func SetupAuthPSKR(suite CipherSuite, skR KEMPrivateKey, pkS KEMPublicKey, enc, psk, pskID, info []byte) (*DecryptContext, error) {
	// sharedSecret = AuthDecap(enc, skR, pkS)
	auth := suite.KEM.(AuthKEMScheme)
	sharedSecret, err := auth.AuthDecap(enc, skR, pkS)
	if err != nil {
		return nil, err
	}

	setupParams := setupParameters{
		sharedSecret: sharedSecret,
		enc:          enc,
	}

	params, err := keySchedule(suite, modeAuthPSK, sharedSecret, info, psk, pskID)
	if err != nil {
		return nil, err
	}

	return newDecryptContext(suite, setupParams, params)
}
