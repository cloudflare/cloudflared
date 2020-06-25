package pogs

import (
	"fmt"
	"time"
)

type RetryableError struct {
	err   error
	Delay time.Duration
}

func (re *RetryableError) Error() string {
	return re.err.Error()
}

// RetryErrorAfter wraps err to indicate that client should retry after delay
func RetryErrorAfter(err error, delay time.Duration) *RetryableError {
	return &RetryableError{
		err:   err,
		Delay: delay,
	}
}

func (re *RetryableError) Unwrap() error {
	return re.err
}

// RPCError is used to indicate errors returned by the RPC subsystem rather
// than failure of a remote operation
type RPCError struct {
	err error
}

func (re *RPCError) Error() string {
	return re.err.Error()
}

func wrapRPCError(err error) *RPCError {
	if err != nil {
		return &RPCError{
			err: err,
		}
	}
	return nil
}

func newRPCError(format string, args ...interface{}) *RPCError {
	return &RPCError{
		fmt.Errorf(format, args...),
	}
}

func (re *RPCError) Unwrap() error {
	return re.err
}
