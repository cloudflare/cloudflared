// Copyright 2023 Cloudflare, Inc. All rights reserved. Use of this source code
// is governed by a BSD-style license that can be found in the LICENSE file.
//
// Glue to add Circl's (post-quantum) hybrid KEMs.
//
// To enable set CurvePreferences with the desired scheme as the first element:
//
//   import (
//      "github.com/cloudflare/circl/kem/tls"
//      "github.com/cloudflare/circl/kem/hybrid"
//
//          [...]
//
//   config.CurvePreferences = []tls.CurveID{
//      qtls.X25519Kyber512Draft00,
//      qtls.X25519,
//      qtls.P256,
//   }

package qtls

import (
	"github.com/cloudflare/circl/kem"
	"github.com/cloudflare/circl/kem/hybrid"

	"crypto/ecdh"
	"crypto/tls"
	"fmt"
	"io"
	"sync"
	"time"
)

// Either *ecdh.PrivateKey or kem.PrivateKey
type clientKeySharePrivate interface{}

var (
	X25519Kyber512Draft00 = CurveID(0xfe30)
	X25519Kyber768Draft00 = CurveID(0xfe31)
	invalidCurveID        = CurveID(0)
)

func kemSchemeKeyToCurveID(s kem.Scheme) CurveID {
	switch s.Name() {
	case "Kyber512-X25519":
		return X25519Kyber512Draft00
	case "Kyber768-X25519":
		return X25519Kyber768Draft00
	default:
		return invalidCurveID
	}
}

// Extract CurveID from clientKeySharePrivate
func clientKeySharePrivateCurveID(ks clientKeySharePrivate) CurveID {
	switch v := ks.(type) {
	case kem.PrivateKey:
		ret := kemSchemeKeyToCurveID(v.Scheme())
		if ret == invalidCurveID {
			panic("cfkem: internal error: don't know CurveID for this KEM")
		}
		return ret
	case *ecdh.PrivateKey:
		ret, ok := curveIDForCurve(v.Curve())
		if !ok {
			panic("cfkem: internal error: unknown curve")
		}
		return ret
	default:
		panic("cfkem: internal error: unknown clientKeySharePrivate")
	}
}

// Returns scheme by CurveID if supported by Circl
func curveIdToCirclScheme(id CurveID) kem.Scheme {
	switch id {
	case X25519Kyber512Draft00:
		return hybrid.Kyber512X25519()
	case X25519Kyber768Draft00:
		return hybrid.Kyber768X25519()
	}
	return nil
}

// Generate a new shared secret and encapsulates it for the packed
// public key in ppk using randomness from rnd.
func encapsulateForKem(scheme kem.Scheme, rnd io.Reader, ppk []byte) (
	ct, ss []byte, alert alert, err error) {
	pk, err := scheme.UnmarshalBinaryPublicKey(ppk)
	if err != nil {
		return nil, nil, alertIllegalParameter, fmt.Errorf("unpack pk: %w", err)
	}
	seed := make([]byte, scheme.EncapsulationSeedSize())
	if _, err := io.ReadFull(rnd, seed); err != nil {
		return nil, nil, alertInternalError, fmt.Errorf("random: %w", err)
	}
	ct, ss, err = scheme.EncapsulateDeterministically(pk, seed)
	return ct, ss, alertIllegalParameter, err
}

// Generate a new keypair using randomness from rnd.
func generateKemKeyPair(scheme kem.Scheme, rnd io.Reader) (
	kem.PublicKey, kem.PrivateKey, error) {
	seed := make([]byte, scheme.SeedSize())
	if _, err := io.ReadFull(rnd, seed); err != nil {
		return nil, nil, err
	}
	pk, sk := scheme.DeriveKeyPair(seed)
	return pk, sk, nil
}

// Events. We cannot use the same approach as used in our plain Go fork
// as we cannot change tls.Config, tls.ConnectionState, etc. Also we do
// not want to maintain a fork of quic-go itself as well. This seems
// the simplest option.

// CFEvent. There are two events: one emitted on HRR and one emitted
type CFEvent interface {
	// Common to all events
	ServerSide() bool // true if server-side; false if on client-side

	// HRR event. Emitted when an HRR happened.
	IsHRR() bool // true if this is an HRR event

	// Handshake event.
	IsHandshake() bool       // true if this is a handshake event.
	Duration() time.Duration // how long did the handshake take?
	KEX() tls.CurveID        // which kex was established?
}

type CFEventHandler func(CFEvent)

// Registers a handler to be called when a CFEvent is emitted; returns
// the previous handler.
func SetCFEventHandler(handler CFEventHandler) CFEventHandler {
	cfEventMux.Lock()
	ret := cfEventHandler
	cfEventHandler = handler
	cfEventMux.Unlock()
	return ret
}

func raiseCFEvent(ev CFEvent) {
	cfEventMux.Lock()
	handler := cfEventHandler
	cfEventMux.Unlock()
	if handler != nil {
		handler(ev)
	}
}

var (
	cfEventMux     sync.Mutex
	cfEventHandler CFEventHandler
)

type cfEventHRR struct{ serverSide bool }

func (*cfEventHRR) IsHRR() bool                { return true }
func (ev *cfEventHRR) ServerSide() bool        { return ev.serverSide }
func (*cfEventHRR) IsHandshake() bool          { return false }
func (ev *cfEventHRR) Duration() time.Duration { panic("wrong event") }
func (ev *cfEventHRR) KEX() tls.CurveID        { panic("wrong event") }

type cfEventHandshake struct {
	serverSide bool
	duration   time.Duration
	kex        tls.CurveID
}

func (*cfEventHandshake) IsHRR() bool                { return false }
func (ev *cfEventHandshake) ServerSide() bool        { return ev.serverSide }
func (*cfEventHandshake) IsHandshake() bool          { return true }
func (ev *cfEventHandshake) Duration() time.Duration { return ev.duration }
func (ev *cfEventHandshake) KEX() tls.CurveID        { return ev.kex }
