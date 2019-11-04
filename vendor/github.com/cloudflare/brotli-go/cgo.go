// Copyright 2017 Google Inc. All Rights Reserved.
//
// Distributed under MIT license.
// See file LICENSE for detail or copy at https://opensource.org/licenses/MIT

package brotli

// Inform golang build system that it should link brotli libraries.

// #cgo CFLAGS: -O3
// #cgo LDFLAGS: -lm
import "C"

import (
	_ "github.com/cloudflare/brotli-go/brotli"
	_ "github.com/cloudflare/brotli-go/common"
	_ "github.com/cloudflare/brotli-go/dec"
	_ "github.com/cloudflare/brotli-go/enc"
)
