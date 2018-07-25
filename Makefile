VERSION       := $(shell git describe --tags --always --dirty="-dev")
DATE          := $(shell date -u '+%Y-%m-%d-%H%M UTC')
VERSION_FLAGS := -ldflags='-X "main.Version=$(VERSION)" -X "main.BuildTime=$(DATE)"'

IMPORT_PATH   := github.com/cloudflare/cloudflared
PACKAGE_DIR   := $(CURDIR)/packaging
INSTALL_BINDIR := usr/local/bin

.PHONY: all
all: cloudflared test

.PHONY: cloudflared
cloudflared:
	go build -v $(VERSION_FLAGS) $(IMPORT_PATH)/cmd/cloudflared

.PHONY: test
test:
	go test -v -race $(VERSION_FLAGS) ./...

.PHONY: cloudflared-deb
cloudflared-deb: cloudflared
	mkdir -p $(PACKAGE_DIR)
	cp cloudflared $(PACKAGE_DIR)/cloudflared
	fakeroot fpm -C $(PACKAGE_DIR) -s dir -t deb --deb-compression bzip2 \
		-a $(GOARCH) -v $(VERSION) -n cloudflared
