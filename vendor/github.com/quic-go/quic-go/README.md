# A QUIC implementation in pure Go

<img src="docs/quic.png" width=303 height=124>

[![PkgGoDev](https://pkg.go.dev/badge/github.com/quic-go/quic-go)](https://pkg.go.dev/github.com/quic-go/quic-go)
[![Code Coverage](https://img.shields.io/codecov/c/github/quic-go/quic-go/master.svg?style=flat-square)](https://codecov.io/gh/quic-go/quic-go/)

quic-go is an implementation of the QUIC protocol ([RFC 9000](https://datatracker.ietf.org/doc/html/rfc9000), [RFC 9001](https://datatracker.ietf.org/doc/html/rfc9001), [RFC 9002](https://datatracker.ietf.org/doc/html/rfc9002)) in Go, including the Unreliable Datagram Extension ([RFC 9221](https://datatracker.ietf.org/doc/html/rfc9221)) and Datagram Packetization Layer Path MTU
   Discovery (DPLPMTUD, [RFC 8899](https://datatracker.ietf.org/doc/html/rfc8899)). It has support for HTTP/3 ([RFC 9114](https://datatracker.ietf.org/doc/html/rfc9114)), including QPACK ([RFC 9204](https://datatracker.ietf.org/doc/html/rfc9204)).

In addition to the RFCs listed above, it currently implements the [IETF QUIC draft-29](https://tools.ietf.org/html/draft-ietf-quic-transport-29). Support for draft-29 will eventually be dropped, as it is phased out of the ecosystem.

## Guides

*We currently support Go 1.19.x and Go 1.20.x*

Running tests:

    go test ./...

### QUIC without HTTP/3

Take a look at [this echo example](example/echo/echo.go).

## Usage

### As a server

See the [example server](example/main.go). Starting a QUIC server is very similar to the standard lib http in go:

```go
http.Handle("/", http.FileServer(http.Dir(wwwDir)))
http3.ListenAndServeQUIC("localhost:4242", "/path/to/cert/chain.pem", "/path/to/privkey.pem", nil)
```

### As a client

See the [example client](example/client/main.go). Use a `http3.RoundTripper` as a `Transport` in a `http.Client`.

```go
http.Client{
  Transport: &http3.RoundTripper{},
}
```

## Projects using quic-go

| Project                                                   | Description                                                                                                                                                       | Stars                                                                                               |
|-----------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------|
| [AdGuardHome](https://github.com/AdguardTeam/AdGuardHome) | Free and open source, powerful network-wide ads & trackers blocking DNS server.                                                                                   | ![GitHub Repo stars](https://img.shields.io/github/stars/AdguardTeam/AdGuardHome?style=flat-square) |
| [algernon](https://github.com/xyproto/algernon)           | Small self-contained pure-Go web server with Lua, Markdown, HTTP/2, QUIC, Redis and PostgreSQL support                                                            | ![GitHub Repo stars](https://img.shields.io/github/stars/xyproto/algernon?style=flat-square)        |
| [caddy](https://github.com/caddyserver/caddy/)            | Fast, multi-platform web server with automatic HTTPS                                                                                                              | ![GitHub Repo stars](https://img.shields.io/github/stars/caddyserver/caddy?style=flat-square)       |
| [cloudflared](https://github.com/cloudflare/cloudflared)  | A tunneling daemon that proxies traffic from the Cloudflare network to your origins                                                                               | ![GitHub Repo stars](https://img.shields.io/github/stars/cloudflare/cloudflared?style=flat-square)  |
| [go-libp2p](https://github.com/libp2p/go-libp2p)          | libp2p implementation in Go, powering [Kubo](https://github.com/ipfs/kubo) (IPFS) and [Lotus](https://github.com/filecoin-project/lotus) (Filecoin), among others | ![GitHub Repo stars](https://img.shields.io/github/stars/libp2p/go-libp2p?style=flat-square)        |
| [Mercure](https://github.com/dunglas/mercure)             | An open, easy, fast, reliable and battery-efficient solution for real-time communications                                                                         | ![GitHub Repo stars](https://img.shields.io/github/stars/dunglas/mercure?style=flat-square)         |
| [OONI Probe](https://github.com/ooni/probe-cli)           | Next generation OONI Probe. Library and CLI tool.                                                                                                                 | ![GitHub Repo stars](https://img.shields.io/github/stars/ooni/probe-cli?style=flat-square)          |
| [syncthing](https://github.com/syncthing/syncthing/)      | Open Source Continuous File Synchronization                                                                                                                       | ![GitHub Repo stars](https://img.shields.io/github/stars/syncthing/syncthing?style=flat-square)     |
| [traefik](https://github.com/traefik/traefik)             | The Cloud Native Application Proxy                                                                                                                                | ![GitHub Repo stars](https://img.shields.io/github/stars/traefik/traefik?style=flat-square)         |
| [v2ray-core](https://github.com/v2fly/v2ray-core)         | A platform for building proxies to bypass network restrictions                                                                                                    | ![GitHub Repo stars](https://img.shields.io/github/stars/v2fly/v2ray-core?style=flat-square)        |
| [YoMo](https://github.com/yomorun/yomo)                   | Streaming Serverless Framework for Geo-distributed System                                                                                                         | ![GitHub Repo stars](https://img.shields.io/github/stars/yomorun/yomo?style=flat-square)            |

## Contributing

We are always happy to welcome new contributors! We have a number of self-contained issues that are suitable for first-time contributors, they are tagged with [help wanted](https://github.com/quic-go/quic-go/issues?q=is%3Aissue+is%3Aopen+label%3A%22help+wanted%22). If you have any questions, please feel free to reach out by opening an issue or leaving a comment.
