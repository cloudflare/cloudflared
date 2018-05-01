# Cap'n Proto bindings for Go

[![GoDoc](https://godoc.org/zombiezen.com/go/capnproto2?status.svg)][godoc]
[![Build Status](https://travis-ci.org/capnproto/go-capnproto2.svg?branch=master)][travis]

go-capnproto consists of:
- a Go code generator for [Cap'n Proto](https://capnproto.org/)
- a Go package that provides runtime support
- a Go package that implements Level 1 of the RPC protocol

[godoc]: https://godoc.org/zombiezen.com/go/capnproto2
[travis]: https://travis-ci.org/capnproto/go-capnproto2

## Getting started

You will need the `capnp` tool to compile schemas into Go.
This package has been tested with Cap'n Proto 0.5.0.

```
$ go get -u -t zombiezen.com/go/capnproto2/...
$ go test -v zombiezen.com/go/capnproto2/...
```

This library uses [SemVer tags][] to indicate stable releases.
While the goal is that master should always be passing all known tests, tagged releases are vetted more.
When possible, use the [latest release tag](https://github.com/capnproto/go-capnproto2/releases).

```
$ cd $GOPATH/src/zombiezen.com/go/capnproto2
$ git fetch
$ git checkout v2.16.0  # check the releases page for the latest
```

Then read the [Getting Started guide][].

[SemVer tags]: http://semver.org/
[Getting Started guide]: https://github.com/capnproto/go-capnproto2/wiki/Getting-Started

## API Compatibility

Consider this package's API as beta software, since the Cap'n Proto spec is not final.
In the spirit of the [Go 1 compatibility guarantee][gocompat], I will make every effort to avoid making breaking API changes.
The major cases where I reserve the right to make breaking changes are:

- Security.
- Changes in the Cap'n Proto specification.
- Bugs.

The `pogs` package is relatively new and may change over time.
However, its functionality has been well-tested and will probably only relax restrictions.

[gocompat]: https://golang.org/doc/go1compat

## Documentation

See the docs on [godoc.org][godoc].

## What is Cap'n Proto?

The best cerealization...

https://capnproto.org/

## License

MIT - see [LICENSE][] file

[LICENSE]: https://github.com/capnproto/go-capnproto2/blob/master/LICENSE
