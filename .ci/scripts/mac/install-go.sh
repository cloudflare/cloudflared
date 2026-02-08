rm -rf /tmp/go
export GOCACHE=/tmp/gocache
rm -rf $GOCACHE

if [ -z "$1" ]
  then
    echo "No go version supplied"
fi

brew install "$1"

go version
which go
go env
