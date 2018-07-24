VERSION       := $(shell git describe --tags --always --dirty="-dev")
DATE          := $(shell date -u '+%Y-%m-%d-%H%M UTC')
VERSION_FLAGS := -ldflags='-X "main.Version=$(VERSION)" -X "main.BuildTime=$(DATE)"'

.PHONY: all
all: cloudflared test

.PHONY: cloudflared
cloudflared:
	go build -v $(VERSION_FLAGS) ./...

.PHONY: test
test:
	go test -v -race $(VERSION_FLAGS) ./...
