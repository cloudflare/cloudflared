# HPKE 

[![Coverage Status](https://coveralls.io/repos/github/cisco/go-hpke/badge.svg?branch=ci)](https://coveralls.io/github/cisco/go-hpke?branch=ci)

This repo provides a Go implementation of the HPKE primitive proposed for discussion at CFRG.

https://tools.ietf.org/html/draft-irtf-cfrg-hpke

## Test vector generation

To generate test vectors, run:

```
$ HPKE_TEST_VECTORS_OUT=test-vectors.json go test -v -run TestVectorGenerate
```

To check test vectors, run:

```
$ HPKE_TEST_VECTORS_IN=test-vectors.json go test -v -run TestVectorVerify
```
