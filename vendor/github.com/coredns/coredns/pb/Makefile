# Generate the Go files from the dns.proto protobuf, you need the utilities
# from: https://github.com/golang/protobuf to make this work.
# The generate dns.pb.go is checked into git, so for normal builds we don't need
# to run this generation step.
# Note: The following has been used when regenerate pb:
#  curl -OL https://github.com/protocolbuffers/protobuf/releases/download/v3.19.4/protoc-3.19.4-linux-x86_64.zip
#  go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.27.1
#  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2.0
#  export PATH="$PATH:$(go env GOPATH)/bin"
#  rm pb/dns.pb.go pb/dns_grpc.pb.go
#  make pb

all: dns.pb.go

dns.pb.go: dns.proto
	protoc --go_out=. --go-grpc_out=. dns.proto

.PHONY: clean
clean:
	rm dns.pb.go
