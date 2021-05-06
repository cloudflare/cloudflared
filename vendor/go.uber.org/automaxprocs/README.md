# automaxprocs [![GoDoc][doc-img]][doc] [![Build Status][ci-img]][ci] [![Coverage Status][cov-img]][cov]

Automatically set `GOMAXPROCS` to match Linux container CPU quota.

## Installation

`go get -u go.uber.org/automaxprocs`

## Quick Start

```go
import _ "go.uber.org/automaxprocs"

func main() {
  // Your application logic here.
}
```

## Development Status: Stable

All APIs are finalized, and no breaking changes will be made in the 1.x series
of releases. Users of semver-aware dependency management systems should pin
automaxprocs to `^1`.

## Contributing

We encourage and support an active, healthy community of contributors &mdash;
including you! Details are in the [contribution guide](CONTRIBUTING.md) and
the [code of conduct](CODE_OF_CONDUCT.md). The automaxprocs maintainers keep
an eye on issues and pull requests, but you can also report any negative
conduct to oss-conduct@uber.com. That email list is a private, safe space;
even the automaxprocs maintainers don't have access, so don't hesitate to hold
us to a high standard.

<hr>

Released under the [MIT License](LICENSE).

[doc-img]: https://godoc.org/go.uber.org/automaxprocs?status.svg
[doc]: https://godoc.org/go.uber.org/automaxprocs
[ci-img]: https://travis-ci.com/uber-go/automaxprocs.svg?branch=master
[ci]: https://travis-ci.com/uber-go/automaxprocs
[cov-img]: https://codecov.io/gh/uber-go/automaxprocs/branch/master/graph/badge.svg
[cov]: https://codecov.io/gh/uber-go/automaxprocs


