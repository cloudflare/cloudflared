rm -rf /tmp/go
export GOCACHE=/tmp/gocache
rm -rf $GOCACHE

./.teamcity/install-cloudflare-go.sh

export PATH="/tmp/go/bin:$PATH"
go version
which go
go env