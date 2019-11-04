#!/bin/bash
# regen.sh - update capnpc-go and regenerate schemas
set -euo pipefail

cd "$(dirname "$0")"

echo "** mktemplates"
(cd internal/cmd/mktemplates && go build -tags=mktemplates)

echo "** capnpc-go"
# Run tests so that we don't install a broken capnpc-go.
(cd capnpc-go && go generate && go test && go install)

echo "** schemas"
(cd std/capnp; ./gen.sh compile)
capnp compile -ogo std/go.capnp && mv std/go.capnp.go ./
go generate ./...
