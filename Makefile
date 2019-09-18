VERSION       := $(shell git describe --tags --always --dirty="-dev")
DATE          := $(shell date -u '+%Y-%m-%d-%H%M UTC')
VERSION_FLAGS := -ldflags='-X "main.Version=$(VERSION)" -X "main.BuildTime=$(DATE)"'

IMPORT_PATH   := github.com/cloudflare/cloudflared
PACKAGE_DIR   := $(CURDIR)/packaging
INSTALL_BINDIR := usr/local/bin

EQUINOX_FLAGS = --version="$(VERSION)" \
				 --platforms="$(EQUINOX_BUILD_PLATFORMS)" \
				 --app="$(EQUINOX_APP_ID)" \
				 --token="$(EQUINOX_TOKEN)" \
				 --channel="$(EQUINOX_CHANNEL)"

ifeq ($(EQUINOX_IS_DRAFT), true)
	EQUINOX_FLAGS := --draft $(EQUINOX_FLAGS)
endif

ifeq ($(GOARCH),)
	GOARCH := amd64
endif

.PHONY: all
all: cloudflared test

.PHONY: clean
clean:
	go clean

.PHONY: cloudflared
cloudflared: tunnel-deps
	go build -v $(VERSION_FLAGS) $(IMPORT_PATH)/cmd/cloudflared

.PHONY: container
container:
	docker build -t cloudflare/cloudflared:"$(VERSION)" .

.PHONY: test
test: vet
	go test -v -race $(VERSION_FLAGS) ./...

.PHONY: test-ssh-server
test-ssh-server:
	docker-compose -f ssh_server_tests/docker-compose.yml up

.PHONY: cloudflared-deb
cloudflared-deb: cloudflared
	mkdir -p $(PACKAGE_DIR)
	cp cloudflared $(PACKAGE_DIR)/cloudflared
	fakeroot fpm -C $(PACKAGE_DIR) -s dir -t deb --deb-compression bzip2 \
		-a $(GOARCH) -v $(VERSION) -n cloudflared cloudflared=/usr/local/bin/

.PHONY: cloudflared-darwin-amd64.tgz
cloudflared-darwin-amd64.tgz: cloudflared
	tar czf cloudflared-darwin-amd64.tgz cloudflared
	rm cloudflared

.PHONY: homebrew-upload
homebrew-upload: cloudflared-darwin-amd64.tgz
	aws s3 --endpoint-url $(S3_ENDPOINT) cp --acl public-read $$^ $(S3_URI)/cloudflared-$$(VERSION)-$1.tgz
	aws s3 --endpoint-url $(S3_ENDPOINT) cp --acl public-read $(S3_URI)/cloudflared-$$(VERSION)-$1.tgz  $(S3_URI)/cloudflared-stable-$1.tgz

.PHONY: homebrew-release
homebrew-release: homebrew-upload
	./publish-homebrew-formula.sh cloudflared-darwin-amd64.tgz $(VERSION) homebrew-cloudflare

.PHONY: release
release: bin/equinox
	bin/equinox release $(EQUINOX_FLAGS) -- $(VERSION_FLAGS) $(IMPORT_PATH)/cmd/cloudflared

bin/equinox:
	mkdir -p bin
	curl -s https://bin.equinox.io/c/75JtLRTsJ3n/release-tool-beta-$(EQUINOX_PLATFORM).tgz | tar xz -C bin/

.PHONY: tunnel-deps
tunnel-deps: tunnelrpc/tunnelrpc.capnp.go

tunnelrpc/tunnelrpc.capnp.go: tunnelrpc/tunnelrpc.capnp
	which capnp  # https://capnproto.org/install.html
	which capnpc-go  # go get zombiezen.com/go/capnproto2/capnpc-go
	capnp compile -ogo tunnelrpc/tunnelrpc.capnp

.PHONY: vet
vet:
	go vet ./...
	which go-sumtype  # go get github.com/BurntSushi/go-sumtype
	go-sumtype $$(go list ./...)
