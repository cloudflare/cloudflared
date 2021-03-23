package pogs

import (
	"errors"
	"time"
)

// AuthenticateResponse is the serialized response from the Authenticate RPC.
// It's a 1:1 representation of the capnp message, so it's not very useful for programmers.
// Instead, you should call the `Outcome()` method to get a programmer-friendly sum type, with one
// case for each possible outcome.
type AuthenticateResponse struct {
	PermanentErr      string
	RetryableErr      string
	Jwt               []byte
	HoursUntilRefresh uint8
}

// Outcome turns the deserialized response of Authenticate into a programmer-friendly sum type.
func (ar AuthenticateResponse) Outcome() AuthOutcome {
	// If the user's authentication was unsuccessful, the server will return an error explaining why.
	// cloudflared should fatal with this error.
	if ar.PermanentErr != "" {
		return NewAuthFail(errors.New(ar.PermanentErr))
	}

	// If there was a network error, then cloudflared should retry later,
	// because origintunneld couldn't prove whether auth was correct or not.
	if ar.RetryableErr != "" {
		return NewAuthUnknown(errors.New(ar.RetryableErr), ar.HoursUntilRefresh)
	}

	// If auth succeeded, return the token and refresh it when instructed.
	if len(ar.Jwt) > 0 {
		return NewAuthSuccess(ar.Jwt, ar.HoursUntilRefresh)
	}

	// Otherwise the state got messed up.
	return nil
}

// AuthOutcome is a programmer-friendly sum type denoting the possible outcomes of Authenticate.
//go-sumtype:decl AuthOutcome
type AuthOutcome interface {
	isAuthOutcome()
	// Serialize into an AuthenticateResponse which can be sent via Capnp
	Serialize() AuthenticateResponse
}

// AuthSuccess means the backend successfully authenticated this cloudflared.
type AuthSuccess struct {
	jwt               []byte
	hoursUntilRefresh uint8
}

func NewAuthSuccess(jwt []byte, hoursUntilRefresh uint8) AuthSuccess {
	return AuthSuccess{jwt: jwt, hoursUntilRefresh: hoursUntilRefresh}
}

func (ao AuthSuccess) JWT() []byte {
	return ao.jwt
}

// RefreshAfter is how long cloudflared should wait before rerunning Authenticate.
func (ao AuthSuccess) RefreshAfter() time.Duration {
	return hoursToTime(ao.hoursUntilRefresh)
}

// Serialize into an AuthenticateResponse which can be sent via Capnp
func (ao AuthSuccess) Serialize() AuthenticateResponse {
	return AuthenticateResponse{
		Jwt:               ao.jwt,
		HoursUntilRefresh: ao.hoursUntilRefresh,
	}
}

func (ao AuthSuccess) isAuthOutcome() {}

// AuthFail means this cloudflared has the wrong auth and should exit.
type AuthFail struct {
	err error
}

func NewAuthFail(err error) AuthFail {
	return AuthFail{err: err}
}

func (ao AuthFail) Error() string {
	return ao.err.Error()
}

// Serialize into an AuthenticateResponse which can be sent via Capnp
func (ao AuthFail) Serialize() AuthenticateResponse {
	return AuthenticateResponse{
		PermanentErr: ao.err.Error(),
	}
}

func (ao AuthFail) isAuthOutcome() {}

// AuthUnknown means the backend couldn't finish checking authentication. Try again later.
type AuthUnknown struct {
	err               error
	hoursUntilRefresh uint8
}

func NewAuthUnknown(err error, hoursUntilRefresh uint8) AuthUnknown {
	return AuthUnknown{err: err, hoursUntilRefresh: hoursUntilRefresh}
}

func (ao AuthUnknown) Error() string {
	return ao.err.Error()
}

// RefreshAfter is how long cloudflared should wait before rerunning Authenticate.
func (ao AuthUnknown) RefreshAfter() time.Duration {
	return hoursToTime(ao.hoursUntilRefresh)
}

// Serialize into an AuthenticateResponse which can be sent via Capnp
func (ao AuthUnknown) Serialize() AuthenticateResponse {
	return AuthenticateResponse{
		RetryableErr:      ao.err.Error(),
		HoursUntilRefresh: ao.hoursUntilRefresh,
	}
}

func (ao AuthUnknown) isAuthOutcome() {}

func hoursToTime(hours uint8) time.Duration {
	return time.Duration(hours) * time.Hour
}
