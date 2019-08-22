// Taken from https://github.com/golang/go/blob/ad644d2e86bab85787879d41c2d2aebbd7c57db8/src/os/user/user.go
// and modified to return login shell in User struct. cloudflared requires cgo for compilation because of this addition.

// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build aix darwin dragonfly freebsd !android,linux netbsd openbsd solaris
// +build cgo,!osusergo

package sshserver

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

/*
#cgo solaris CFLAGS: -D_POSIX_PTHREAD_SEMANTICS
#include <unistd.h>
#include <sys/types.h>
#include <pwd.h>
#include <grp.h>
#include <stdlib.h>

static int mygetpwuid_r(int uid, struct passwd *pwd,
	char *buf, size_t buflen, struct passwd **result) {
	return getpwuid_r(uid, pwd, buf, buflen, result);
}

static int mygetpwnam_r(const char *name, struct passwd *pwd,
	char *buf, size_t buflen, struct passwd **result) {
	return getpwnam_r(name, pwd, buf, buflen, result);
}

static int mygetgrgid_r(int gid, struct group *grp,
	char *buf, size_t buflen, struct group **result) {
 return getgrgid_r(gid, grp, buf, buflen, result);
}

static int mygetgrnam_r(const char *name, struct group *grp,
	char *buf, size_t buflen, struct group **result) {
 return getgrnam_r(name, grp, buf, buflen, result);
}
*/
import "C"

type UnknownUserIdError int

func (e UnknownUserIdError) Error() string {
	return "user: unknown userid " + strconv.Itoa(int(e))
}

// UnknownUserError is returned by Lookup when
// a user cannot be found.
type UnknownUserError string

func (e UnknownUserError) Error() string {
	return "user: unknown user " + string(e)
}

// UnknownGroupIdError is returned by LookupGroupId when
// a group cannot be found.
type UnknownGroupIdError string

func (e UnknownGroupIdError) Error() string {
	return "group: unknown groupid " + string(e)
}

// UnknownGroupError is returned by LookupGroup when
// a group cannot be found.
type UnknownGroupError string

func (e UnknownGroupError) Error() string {
	return "group: unknown group " + string(e)
}

type User struct {
	// Uid is the user ID.
	// On POSIX systems, this is a decimal number representing the uid.
	// On Windows, this is a security identifier (SID) in a string format.
	// On Plan 9, this is the contents of /dev/user.
	Uid string
	// Gid is the primary group ID.
	// On POSIX systems, this is a decimal number representing the gid.
	// On Windows, this is a SID in a string format.
	// On Plan 9, this is the contents of /dev/user.
	Gid string
	// Username is the login name.
	Username string
	// Name is the user's real or display name.
	// It might be blank.
	// On POSIX systems, this is the first (or only) entry in the GECOS field
	// list.
	// On Windows, this is the user's display name.
	// On Plan 9, this is the contents of /dev/user.
	Name string
	// HomeDir is the path to the user's home directory (if they have one).
	HomeDir string

	/****************** Begin added code ******************/
	// Login shell
	Shell string
	/****************** End added code ******************/
}

func lookupUser(username string) (*User, error) {
	var pwd C.struct_passwd
	var result *C.struct_passwd
	nameC := make([]byte, len(username)+1)
	copy(nameC, username)

	buf := alloc(userBuffer)
	defer buf.free()

	err := retryWithBuffer(buf, func() syscall.Errno {
		// mygetpwnam_r is a wrapper around getpwnam_r to avoid
		// passing a size_t to getpwnam_r, because for unknown
		// reasons passing a size_t to getpwnam_r doesn't work on
		// Solaris.
		return syscall.Errno(C.mygetpwnam_r((*C.char)(unsafe.Pointer(&nameC[0])),
			&pwd,
			(*C.char)(buf.ptr),
			C.size_t(buf.size),
			&result))
	})
	if err != nil {
		return nil, fmt.Errorf("user: lookup username %s: %v", username, err)
	}
	if result == nil {
		return nil, UnknownUserError(username)
	}
	return buildUser(&pwd), err
}

func buildUser(pwd *C.struct_passwd) *User {
	u := &User{
		Uid:      strconv.FormatUint(uint64(pwd.pw_uid), 10),
		Gid:      strconv.FormatUint(uint64(pwd.pw_gid), 10),
		Username: C.GoString(pwd.pw_name),
		Name:     C.GoString(pwd.pw_gecos),
		HomeDir:  C.GoString(pwd.pw_dir),
		/****************** Begin added code ******************/
		Shell: C.GoString(pwd.pw_shell),
		/****************** End added code ******************/
	}
	// The pw_gecos field isn't quite standardized. Some docs
	// say: "It is expected to be a comma separated list of
	// personal data where the first item is the full name of the
	// user."
	if i := strings.Index(u.Name, ","); i >= 0 {
		u.Name = u.Name[:i]
	}
	return u
}

type bufferKind C.int

const (
	userBuffer = bufferKind(C._SC_GETPW_R_SIZE_MAX)
)

func (k bufferKind) initialSize() C.size_t {
	sz := C.sysconf(C.int(k))
	if sz == -1 {
		// DragonFly and FreeBSD do not have _SC_GETPW_R_SIZE_MAX.
		// Additionally, not all Linux systems have it, either. For
		// example, the musl libc returns -1.
		return 1024
	}
	if !isSizeReasonable(int64(sz)) {
		// Truncate.  If this truly isn't enough, retryWithBuffer will error on the first run.
		return maxBufferSize
	}
	return C.size_t(sz)
}

type memBuffer struct {
	ptr  unsafe.Pointer
	size C.size_t
}

func alloc(kind bufferKind) *memBuffer {
	sz := kind.initialSize()
	return &memBuffer{
		ptr:  C.malloc(sz),
		size: sz,
	}
}

func (mb *memBuffer) resize(newSize C.size_t) {
	mb.ptr = C.realloc(mb.ptr, newSize)
	mb.size = newSize
}

func (mb *memBuffer) free() {
	C.free(mb.ptr)
}

// retryWithBuffer repeatedly calls f(), increasing the size of the
// buffer each time, until f succeeds, fails with a non-ERANGE error,
// or the buffer exceeds a reasonable limit.
func retryWithBuffer(buf *memBuffer, f func() syscall.Errno) error {
	for {
		errno := f()
		if errno == 0 {
			return nil
		} else if errno != syscall.ERANGE {
			return errno
		}
		newSize := buf.size * 2
		if !isSizeReasonable(int64(newSize)) {
			return fmt.Errorf("internal buffer exceeds %d bytes", maxBufferSize)
		}
		buf.resize(newSize)
	}
}

const maxBufferSize = 1 << 20

func isSizeReasonable(sz int64) bool {
	return sz > 0 && sz <= maxBufferSize
}
