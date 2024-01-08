cd /tmp/
rm -rf go
rm -rf gocache
export GOCACHE=/tmp/gocache

../install-cloudflare-go.sh

export PATH="/tmp/go/bin:$PATH"
go version
which go
go env