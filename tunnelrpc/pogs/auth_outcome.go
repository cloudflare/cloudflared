package pogs

import (
	"fmt"
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
	// If there was a network error, then cloudflared should retry later,
	// because origintunneld couldn't prove whether auth was correct or not.
	if ar.RetryableErr != "" {
		return &AuthUnknown{Err: fmt.Errorf(ar.RetryableErr), HoursUntilRefresh: ar.HoursUntilRefresh}
	}

	// If the user's authentication was unsuccessful, the server will return an error explaining why.
	// cloudflared should fatal with this error.
	if ar.PermanentErr != "" {
		return &AuthFail{Err: fmt.Errorf(ar.PermanentErr)}
	}

	// If auth succeeded, return the token and refresh it when instructed.
	if ar.PermanentErr == "" && len(ar.Jwt) > 0 {
		return &AuthSuccess{Jwt: ar.Jwt, HoursUntilRefresh: ar.HoursUntilRefresh}
	}

	// Otherwise the state got messed up.
	return nil
}

// AuthOutcome is a programmer-friendly sum type denoting the possible outcomes of Authenticate.
//go-sumtype:decl AuthOutcome
type AuthOutcome interface {
	isAuthOutcome()
}

// AuthSuccess means the backend successfully authenticated this cloudflared.
type AuthSuccess struct {
	Jwt               []byte
	HoursUntilRefresh uint8
}

// RefreshAfter is how long cloudflared should wait before rerunning Authenticate.
func (ao *AuthSuccess) RefreshAfter() time.Duration {
	return hoursToTime(ao.HoursUntilRefresh)
}

func (ao *AuthSuccess) isAuthOutcome() {}

// AuthFail means this cloudflared has the wrong auth and should exit.
type AuthFail struct {
	Err error
}

func (ao *AuthFail) isAuthOutcome() {}

// AuthUnknown means the backend couldn't finish checking authentication. Try again later.
type AuthUnknown struct {
	Err               error
	HoursUntilRefresh uint8
}

// RefreshAfter is how long cloudflared should wait before rerunning Authenticate.
func (ao *AuthUnknown) RefreshAfter() time.Duration {
	return hoursToTime(ao.HoursUntilRefresh)
}

func (ao *AuthUnknown) isAuthOutcome() {}

func hoursToTime(hours uint8) time.Duration {
	return time.Duration(hours) * time.Hour
}
