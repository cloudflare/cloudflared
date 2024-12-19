# ws

[![GoDoc][godoc-image]][godoc-url]
[![CI][ci-badge]][ci-url]

> [RFC6455][rfc-url] WebSocket implementation in Go.

# Features

- Zero-copy upgrade
- No intermediate allocations during I/O
- Low-level API which allows to build your own logic of packet handling and
  buffers reuse
- High-level wrappers and helpers around API in `wsutil` package, which allow
  to start fast without digging the protocol internals

# Documentation

[GoDoc][godoc-url].

# Why

Existing WebSocket implementations do not allow users to reuse I/O buffers
between connections in clear way. This library aims to export efficient
low-level interface for working with the protocol without forcing only one way
it could be used.

By the way, if you want get the higher-level tools, you can use `wsutil`
package.

# Status

Library is tagged as `v1*` so its API must not be broken during some
improvements or refactoring.

This implementation of RFC6455 passes [Autobahn Test
Suite](https://github.com/crossbario/autobahn-testsuite) and currently has
about 78% coverage.

# Examples

Example applications using `ws` are developed in separate repository
[ws-examples](https://github.com/gobwas/ws-examples).

# Usage

The higher-level example of WebSocket echo server:

```go
package main

import (
	"net/http"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func main() {
	http.ListenAndServe(":8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			// handle error
		}
		go func() {
			defer conn.Close()

			for {
				msg, op, err := wsutil.ReadClientData(conn)
				if err != nil {
					// handle error
				}
				err = wsutil.WriteServerMessage(conn, op, msg)
				if err != nil {
					// handle error
				}
			}
		}()
	}))
}
```

Lower-level, but still high-level example:


```go
import (
	"net/http"
	"io"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func main() {
	http.ListenAndServe(":8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			// handle error
		}
		go func() {
			defer conn.Close()

			var (
				state  = ws.StateServerSide
				reader = wsutil.NewReader(conn, state)
				writer = wsutil.NewWriter(conn, state, ws.OpText)
			)
			for {
				header, err := reader.NextFrame()
				if err != nil {
					// handle error
				}

				// Reset writer to write frame with right operation code.
				writer.Reset(conn, state, header.OpCode)

				if _, err = io.Copy(writer, reader); err != nil {
					// handle error
				}
				if err = writer.Flush(); err != nil {
					// handle error
				}
			}
		}()
	}))
}
```

We can apply the same pattern to read and write structured responses through a JSON encoder and decoder.:

```go
	...
	var (
		r = wsutil.NewReader(conn, ws.StateServerSide)
		w = wsutil.NewWriter(conn, ws.StateServerSide, ws.OpText)
		decoder = json.NewDecoder(r)
		encoder = json.NewEncoder(w)
	)
	for {
		hdr, err = r.NextFrame()
		if err != nil {
			return err
		}
		if hdr.OpCode == ws.OpClose {
			return io.EOF
		}
		var req Request
		if err := decoder.Decode(&req); err != nil {
			return err
		}
		var resp Response
		if err := encoder.Encode(&resp); err != nil {
			return err
		}
		if err = w.Flush(); err != nil {
			return err
		}
	}
	...
```

The lower-level example without `wsutil`:

```go
package main

import (
	"net"
	"io"

	"github.com/gobwas/ws"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:8080")
	if err != nil {
		log.Fatal(err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			// handle error
		}
		_, err = ws.Upgrade(conn)
		if err != nil {
			// handle error
		}

		go func() {
			defer conn.Close()

			for {
				header, err := ws.ReadHeader(conn)
				if err != nil {
					// handle error
				}

				payload := make([]byte, header.Length)
				_, err = io.ReadFull(conn, payload)
				if err != nil {
					// handle error
				}
				if header.Masked {
					ws.Cipher(payload, header.Mask, 0)
				}

				// Reset the Masked flag, server frames must not be masked as
				// RFC6455 says.
				header.Masked = false

				if err := ws.WriteHeader(conn, header); err != nil {
					// handle error
				}
				if _, err := conn.Write(payload); err != nil {
					// handle error
				}

				if header.OpCode == ws.OpClose {
					return
				}
			}
		}()
	}
}
```

# Zero-copy upgrade

Zero-copy upgrade helps to avoid unnecessary allocations and copying while
handling HTTP Upgrade request.

Processing of all non-websocket headers is made in place with use of registered
user callbacks whose arguments are only valid until callback returns.

The simple example looks like this:

```go
package main

import (
	"net"
	"log"

	"github.com/gobwas/ws"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:8080")
	if err != nil {
		log.Fatal(err)
	}
	u := ws.Upgrader{
		OnHeader: func(key, value []byte) (err error) {
			log.Printf("non-websocket header: %q=%q", key, value)
			return
		},
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			// handle error
		}

		_, err = u.Upgrade(conn)
		if err != nil {
			// handle error
		}
	}
}
```

Usage of `ws.Upgrader` here brings ability to control incoming connections on
tcp level and simply not to accept them by some logic.

Zero-copy upgrade is for high-load services which have to control many
resources such as connections buffers.

The real life example could be like this:

```go
package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"

	"github.com/gobwas/httphead"
	"github.com/gobwas/ws"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:8080")
	if err != nil {
		// handle error
	}

	// Prepare handshake header writer from http.Header mapping.
	header := ws.HandshakeHeaderHTTP(http.Header{
		"X-Go-Version": []string{runtime.Version()},
	})

	u := ws.Upgrader{
		OnHost: func(host []byte) error {
			if string(host) == "github.com" {
				return nil
			}
			return ws.RejectConnectionError(
				ws.RejectionStatus(403),
				ws.RejectionHeader(ws.HandshakeHeaderString(
					"X-Want-Host: github.com\r\n",
				)),
			)
		},
		OnHeader: func(key, value []byte) error {
			if string(key) != "Cookie" {
				return nil
			}
			ok := httphead.ScanCookie(value, func(key, value []byte) bool {
				// Check session here or do some other stuff with cookies.
				// Maybe copy some values for future use.
				return true
			})
			if ok {
				return nil
			}
			return ws.RejectConnectionError(
				ws.RejectionReason("bad cookie"),
				ws.RejectionStatus(400),
			)
		},
		OnBeforeUpgrade: func() (ws.HandshakeHeader, error) {
			return header, nil
		},
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		_, err = u.Upgrade(conn)
		if err != nil {
			log.Printf("upgrade error: %s", err)
		}
	}
}
```

# Compression

There is a `ws/wsflate` package to support [Permessage-Deflate Compression
Extension][rfc-pmce].

It provides minimalistic I/O wrappers to be used in conjunction with any
deflate implementation (for example, the standard library's
[compress/flate][compress/flate]).

It is also compatible with `wsutil`'s reader and writer by providing
`wsflate.MessageState` type, which implements `wsutil.SendExtension` and
`wsutil.RecvExtension` interfaces.

```go
package main

import (
	"bytes"
	"log"
	"net"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsflate"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:8080")
	if err != nil {
		// handle error
	}
	e := wsflate.Extension{
		// We are using default parameters here since we use
		// wsflate.{Compress,Decompress}Frame helpers below in the code.
		// This assumes that we use standard compress/flate package as flate
		// implementation.
		Parameters: wsflate.DefaultParameters,
	}
	u := ws.Upgrader{
		Negotiate: e.Negotiate,
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}

		// Reset extension after previous upgrades.
		e.Reset()

		_, err = u.Upgrade(conn)
		if err != nil {
			log.Printf("upgrade error: %s", err)
			continue
		}
		if _, ok := e.Accepted(); !ok {
			log.Printf("didn't negotiate compression for %s", conn.RemoteAddr())
			conn.Close()
			continue
		}

		go func() {
			defer conn.Close()
			for {
				frame, err := ws.ReadFrame(conn)
				if err != nil {
					// Handle error.
					return
				}

				frame = ws.UnmaskFrameInPlace(frame)

				if wsflate.IsCompressed(frame.Header) {
					// Note that even after successful negotiation of
					// compression extension, both sides are able to send
					// non-compressed messages.
					frame, err = wsflate.DecompressFrame(frame)
					if err != nil {
						// Handle error.
						return
					}
				}

				// Do something with frame...

				ack := ws.NewTextFrame([]byte("this is an acknowledgement"))

				// Compress response unconditionally.
				ack, err = wsflate.CompressFrame(ack)
				if err != nil {
					// Handle error.
					return
				}
				if err = ws.WriteFrame(conn, ack); err != nil {
					// Handle error.
					return
				}
			}
		}()
	}
}
```

You can use compression with `wsutil` package this way:

```go
	// Upgrade somehow and negotiate compression to get the conn...

	// Initialize flate reader. We are using nil as a source io.Reader because
	// we will Reset() it in the message i/o loop below.
	fr := wsflate.NewReader(nil, func(r io.Reader) wsflate.Decompressor {
		return flate.NewReader(r)
	})
	// Initialize flate writer. We are using nil as a destination io.Writer
	// because we will Reset() it in the message i/o loop below.
	fw := wsflate.NewWriter(nil, func(w io.Writer) wsflate.Compressor {
		f, _ := flate.NewWriter(w, 9)
		return f
	})

	// Declare compression message state variable.
	//
	// It has two goals:
	// - Allow users to check whether received message is compressed or not.
	// - Help wsutil.Reader and wsutil.Writer to set/unset appropriate
	//   WebSocket header bits while writing next frame to the wire (it
	//   implements wsutil.RecvExtension and wsutil.SendExtension).
	var msg wsflate.MessageState

	// Initialize WebSocket reader as previously. 
	// Please note the use of Reader.Extensions field as well as
	// of ws.StateExtended flag.
	rd := &wsutil.Reader{
		Source:     conn,
		State:      ws.StateServerSide | ws.StateExtended,
		Extensions: []wsutil.RecvExtension{
			&msg, 
		},
	}

	// Initialize WebSocket writer with ws.StateExtended flag as well.
	wr := wsutil.NewWriter(conn, ws.StateServerSide|ws.StateExtended, 0)
	// Use the message state as wsutil.SendExtension.
	wr.SetExtensions(&msg)

	for {
		h, err := rd.NextFrame()
		if err != nil {
			// handle error.
		}
		if h.OpCode.IsControl() {
			// handle control frame.
		}
		if !msg.IsCompressed() {
			// handle uncompressed frame (skipped for the sake of example
			// simplicity).
		}

		// Reset the writer to echo same op code.
		wr.Reset(h.OpCode)

		// Reset both flate reader and writer to start the new round of i/o.
		fr.Reset(rd)
		fw.Reset(wr)

		// Copy whole message from reader to writer decompressing it and
		// compressing again.
		if _, err := io.Copy(fw, fr); err != nil {
			// handle error.
		}
		// Flush any remaining buffers from flate writer to WebSocket writer.
		if err := fw.Close(); err != nil {
			// handle error.
		}
		// Flush the whole WebSocket message to the wire.
		if err := wr.Flush(); err != nil {
			// handle error.
		}
	}
```


[rfc-url]: https://tools.ietf.org/html/rfc6455
[rfc-pmce]: https://tools.ietf.org/html/rfc7692#section-7
[godoc-image]: https://godoc.org/github.com/gobwas/ws?status.svg
[godoc-url]: https://godoc.org/github.com/gobwas/ws
[compress/flate]: https://golang.org/pkg/compress/flate/
[ci-badge]:    https://github.com/gobwas/ws/workflows/CI/badge.svg
[ci-url]:      https://github.com/gobwas/ws/actions?query=workflow%3ACI
