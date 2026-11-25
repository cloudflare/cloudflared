//go:build !windows

package ingress

import "syscall"

const errOriginConnectionRefused = syscall.ECONNREFUSED
