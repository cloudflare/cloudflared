rm -rf /tmp/go
export GOCACHE=/tmp/gocache
rm -rf $GOCACHE

brew install go@1.24

go version
which go
go env

