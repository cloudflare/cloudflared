VERSION       := $(shell git describe --tags --always --dirty="-dev" --match "[0-9][0-9][0-9][0-9].*.*")
MSI_VERSION   := $(shell git tag -l --sort=v:refname | grep "w" | tail -1 | cut -c2-)
#MSI_VERSION expects the format of the tag to be: (wX.X.X). Starts with the w character to not break cfsetup.
#e.g. w3.0.1 or w4.2.10. It trims off the w character when creating the MSI.

ifeq ($(FIPS), true)
	GO_BUILD_TAGS := $(GO_BUILD_TAGS) fips
endif

ifneq ($(GO_BUILD_TAGS),)
	GO_BUILD_TAGS := -tags $(GO_BUILD_TAGS)
endif

ifeq ($(NIGHTLY), true)
	DEB_PACKAGE_NAME := cloudflared-nightly
	NIGHTLY_FLAGS := --conflicts cloudflared --replaces cloudflared
else
	DEB_PACKAGE_NAME := cloudflared
endif

DATE          := $(shell date -u '+%Y-%m-%d-%H%M UTC')
VERSION_FLAGS := -ldflags='-X "main.Version=$(VERSION)" -X "main.BuildTime=$(DATE)"'

IMPORT_PATH   := github.com/cloudflare/cloudflared
PACKAGE_DIR   := $(CURDIR)/packaging
INSTALL_BINDIR := /usr/bin/
MAN_DIR := /usr/share/man/man1/

EQUINOX_FLAGS = --version="$(VERSION)" \
	--platforms="$(EQUINOX_BUILD_PLATFORMS)" \
	--app="$(EQUINOX_APP_ID)" \
	--token="$(EQUINOX_TOKEN)" \
	--channel="$(EQUINOX_CHANNEL)"

ifeq ($(EQUINOX_IS_DRAFT), true)
	EQUINOX_FLAGS := --draft $(EQUINOX_FLAGS)
endif

LOCAL_ARCH ?= $(shell uname -m)
ifneq ($(GOARCH),)
    TARGET_ARCH ?= $(GOARCH)
else ifeq ($(LOCAL_ARCH),x86_64)
    TARGET_ARCH ?= amd64
else ifeq ($(LOCAL_ARCH),amd64)
    TARGET_ARCH ?= amd64
else ifeq ($(LOCAL_ARCH),i686)
    TARGET_ARCH ?= amd64
else ifeq ($(shell echo $(LOCAL_ARCH) | head -c 5),armv8)
    TARGET_ARCH ?= arm64
else ifeq ($(LOCAL_ARCH),aarch64)
    TARGET_ARCH ?= arm64
else ifeq ($(shell echo $(LOCAL_ARCH) | head -c 4),armv)
    TARGET_ARCH ?= arm
else
    $(error This system's architecture $(LOCAL_ARCH) isn't supported)
endif

LOCAL_OS ?= $(shell go env GOOS)
ifeq ($(LOCAL_OS),linux)
    TARGET_OS ?= linux
else ifeq ($(LOCAL_OS),darwin)
    TARGET_OS ?= darwin
else ifeq ($(LOCAL_OS),windows)
    TARGET_OS ?= windows
else ifeq ($(LOCAL_OS),freebsd)
    TARGET_OS ?= freebsd
else
    $(error This system's OS $(LOCAL_OS) isn't supported)
endif

ifeq ($(TARGET_OS), windows)
	EXECUTABLE_PATH=./cloudflared.exe
else
	EXECUTABLE_PATH=./cloudflared
endif

ifeq ($(FLAVOR), centos-7)
	TARGET_PUBLIC_REPO ?= el7
else
	TARGET_PUBLIC_REPO ?= $(FLAVOR)
endif

.PHONY: all
all: cloudflared test

.PHONY: clean
clean:
	go clean

.PHONY: cloudflared
cloudflared: tunnel-deps
ifeq ($(FIPS), true)
	$(info Building cloudflared with go-fips)
	-test -f fips/fips.go && mv fips/fips.go fips/fips.go.linux-amd64
	mv fips/fips.go.linux-amd64 fips/fips.go
endif

	GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) go build -v -mod=vendor $(GO_BUILD_TAGS) $(VERSION_FLAGS) $(IMPORT_PATH)/cmd/cloudflared

ifeq ($(FIPS), true)
	mv fips/fips.go fips/fips.go.linux-amd64
endif

.PHONY: container
container:
	docker build --build-arg=TARGET_ARCH=$(TARGET_ARCH) --build-arg=TARGET_OS=$(TARGET_OS) -t cloudflare/cloudflared-$(TARGET_OS)-$(TARGET_ARCH):"$(VERSION)" .

.PHONY: test
test: vet
ifndef CI
	go test -v -mod=vendor -race $(VERSION_FLAGS) ./...
else
	@mkdir -p .cover
	go test -v -mod=vendor -race $(VERSION_FLAGS) -coverprofile=".cover/c.out" ./...
	go tool cover -html ".cover/c.out" -o .cover/all.html
endif

.PHONY: test-ssh-server
test-ssh-server:
	docker-compose -f ssh_server_tests/docker-compose.yml up

define publish_package
	chmod 664 cloudflared*.$(1); \
	for HOST in $(CF_PKG_HOSTS); do \
		ssh-keyscan -t rsa $$HOST >> ~/.ssh/known_hosts; \
		scp -p -4 cloudflared*.$(1) cfsync@$$HOST:/state/cf-pkg/staging/$(2)/$(TARGET_PUBLIC_REPO)/cloudflared/; \
	done
endef

.PHONY: publish-deb
publish-deb: cloudflared-deb
	$(call publish_package,deb,apt)

.PHONY: publish-rpm
publish-rpm: cloudflared-rpm
	$(call publish_package,rpm,yum)

define build_package
	mkdir -p $(PACKAGE_DIR)
	cp cloudflared $(PACKAGE_DIR)/cloudflared
	cat cloudflared_man_template | sed -e 's/\$${VERSION}/$(VERSION)/; s/\$${DATE}/$(DATE)/' > $(PACKAGE_DIR)/cloudflared.1
	fakeroot fpm -C $(PACKAGE_DIR) -s dir -t $(1) \
		--description 'Cloudflare Argo tunnel daemon' \
		--vendor 'Cloudflare' \
		--license 'Cloudflare Service Agreement' \
		--url 'https://github.com/cloudflare/cloudflared' \
		-m 'Cloudflare <support@cloudflare.com>' \
		-a $(TARGET_ARCH) -v $(VERSION) -n $(DEB_PACKAGE_NAME) $(NIGHTLY_FLAGS) --after-install postinst.sh --after-remove postrm.sh \
		cloudflared=$(INSTALL_BINDIR) cloudflared.1=$(MAN_DIR)
endef

.PHONY: cloudflared-deb
cloudflared-deb: cloudflared
	$(call build_package,deb)

.PHONY: cloudflared-rpm
cloudflared-rpm: cloudflared
	$(call build_package,rpm)

.PHONY: cloudflared-darwin-amd64.tgz
cloudflared-darwin-amd64.tgz: cloudflared
	tar czf cloudflared-darwin-amd64.tgz cloudflared
	rm cloudflared

.PHONY: cloudflared-junos
cloudflared-junos: cloudflared jetez-certificate.pem jetez-key.pem
	jetez --source . \
		  -j jet.yaml \
		  --key jetez-key.pem \
		  --cert jetez-certificate.pem \
		  --version $(VERSION)
	rm jetez-*.pem

jetez-certificate.pem:
ifndef JETEZ_CERT
	$(error JETEZ_CERT not defined)
endif
	@echo "Writing JetEZ certificate"
	@echo "$$JETEZ_CERT" > jetez-certificate.pem

jetez-key.pem:
ifndef JETEZ_KEY
	$(error JETEZ_KEY not defined)
endif
	@echo "Writing JetEZ key"
	@echo "$$JETEZ_KEY" > jetez-key.pem

.PHONY: publish-cloudflared-junos
publish-cloudflared-junos: cloudflared-junos cloudflared-x86-64.latest.s3
ifndef S3_ENDPOINT
	$(error S3_HOST not defined)
endif
ifndef S3_URI
	$(error S3_URI not defined)
endif
ifndef S3_ACCESS_KEY
	$(error S3_ACCESS_KEY not defined)
endif
ifndef S3_SECRET_KEY
	$(error S3_SECRET_KEY not defined)
endif
	sha256sum cloudflared-x86-64-$(VERSION).tgz | awk '{printf $$1}' > cloudflared-x86-64-$(VERSION).tgz.shasum
	s4cmd --endpoint-url $(S3_ENDPOINT) --force --API-GrantRead=uri=http://acs.amazonaws.com/groups/global/AllUsers \
		put cloudflared-x86-64-$(VERSION).tgz $(S3_URI)/cloudflared-x86-64-$(VERSION).tgz
	s4cmd --endpoint-url $(S3_ENDPOINT) --force --API-GrantRead=uri=http://acs.amazonaws.com/groups/global/AllUsers \
		put cloudflared-x86-64-$(VERSION).tgz.shasum $(S3_URI)/cloudflared-x86-64-$(VERSION).tgz.shasum
	dpkg --compare-versions "$(VERSION)" gt "$(shell cat cloudflared-x86-64.latest.s3)" && \
		echo -n "$(VERSION)" > cloudflared-x86-64.latest && \
		s4cmd --endpoint-url $(S3_ENDPOINT) --force --API-GrantRead=uri=http://acs.amazonaws.com/groups/global/AllUsers \
			put cloudflared-x86-64.latest $(S3_URI)/cloudflared-x86-64.latest || \
		echo "Latest version not updated"

cloudflared-x86-64.latest.s3:
	s4cmd --endpoint-url $(S3_ENDPOINT) --force \
		get $(S3_URI)/cloudflared-x86-64.latest cloudflared-x86-64.latest.s3

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

.PHONY: github-release
github-release: cloudflared
	python3 github_release.py --path $(EXECUTABLE_PATH) --release-version $(VERSION)

.PHONY: github-message
github-message:
	python3 github_message.py --release-version $(VERSION)

.PHONY: github-mac-upload
github-mac-upload:
	python3 github_release.py --path artifacts/cloudflared-darwin-amd64.tgz --release-version $(VERSION) --name cloudflared-darwin-amd64.tgz
	python3 github_release.py --path artifacts/cloudflared-amd64.pkg --release-version $(VERSION) --name cloudflared-amd64.pkg

bin/equinox:
	mkdir -p bin
	curl -s https://bin.equinox.io/c/75JtLRTsJ3n/release-tool-beta-$(EQUINOX_PLATFORM).tgz | tar xz -C bin/

.PHONY: tunnel-deps
tunnel-deps: tunnelrpc/tunnelrpc.capnp.go

tunnelrpc/tunnelrpc.capnp.go: tunnelrpc/tunnelrpc.capnp
	which capnp  # https://capnproto.org/install.html
	which capnpc-go  # go get zombiezen.com/go/capnproto2/capnpc-go
	capnp compile -ogo tunnelrpc/tunnelrpc.capnp

.PHONY: quic-deps
quic-deps: 
	which capnp 
	which capnpc-go
	capnp compile -ogo quic/schema/quic_metadata_protocol.capnp

.PHONY: vet
vet:
	go vet -mod=vendor ./...
	which go-sumtype  # go get github.com/BurntSushi/go-sumtype (don't do this in build directory or this will cause vendor issues)
	go-sumtype $$(go list -mod=vendor ./...)

.PHONY: msi
msi: cloudflared
	go-msi make --msi cloudflared.msi --version $(MSI_VERSION)

.PHONY: goimports
goimports:
	for d in $$(go list -mod=readonly -f '{{.Dir}}' -a ./... | fgrep -v tunnelrpc) ; do goimports -format-only -local github.com/cloudflare/cloudflared -w $$d ; done
