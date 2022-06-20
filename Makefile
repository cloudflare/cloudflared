VERSION       := $(shell git describe --tags --always --match "[0-9][0-9][0-9][0-9].*.*")
MSI_VERSION   := $(shell git tag -l --sort=v:refname | grep "w" | tail -1 | cut -c2-)
#MSI_VERSION expects the format of the tag to be: (wX.X.X). Starts with the w character to not break cfsetup.
#e.g. w3.0.1 or w4.2.10. It trims off the w character when creating the MSI.

ifeq ($(ORIGINAL_NAME), true)
	# Used for builds that want FIPS compilation but want the artifacts generated to still have the original name.
	BINARY_NAME := cloudflared
else ifeq ($(FIPS), true)
	# Used for FIPS compliant builds that do not match the case above.
	BINARY_NAME := cloudflared-fips
else
	# Used for all other (non-FIPS) builds.
	BINARY_NAME := cloudflared
endif

ifeq ($(NIGHTLY), true)
	DEB_PACKAGE_NAME := $(BINARY_NAME)-nightly
	NIGHTLY_FLAGS := --conflicts cloudflared --replaces cloudflared
else
	DEB_PACKAGE_NAME := $(BINARY_NAME)
endif

DATE          := $(shell date -u '+%Y-%m-%d-%H%M UTC')
VERSION_FLAGS := -X "main.Version=$(VERSION)" -X "main.BuildTime=$(DATE)"
ifdef PACKAGE_MANAGER
	VERSION_FLAGS := $(VERSION_FLAGS) -X "github.com/cloudflare/cloudflared/cmd/cloudflared/updater.BuiltForPackageManager=$(PACKAGE_MANAGER)"
endif

LINK_FLAGS :=
ifeq ($(FIPS), true)
	LINK_FLAGS := -linkmode=external -extldflags=-static $(LINK_FLAGS)
	# Prevent linking with libc regardless of CGO enabled or not.
	GO_BUILD_TAGS := $(GO_BUILD_TAGS) osusergo netgo fips
	VERSION_FLAGS := $(VERSION_FLAGS) -X "main.BuildType=FIPS"
endif

LDFLAGS := -ldflags='$(VERSION_FLAGS) $(LINK_FLAGS)'
ifneq ($(GO_BUILD_TAGS),)
	GO_BUILD_TAGS := -tags "$(GO_BUILD_TAGS)"
endif

ifeq ($(debug), 1)
	GO_BUILD_TAGS += -gcflags="all=-N -l"
endif

IMPORT_PATH    := github.com/cloudflare/cloudflared
PACKAGE_DIR    := $(CURDIR)/packaging
PREFIX         := /usr
INSTALL_BINDIR := $(PREFIX)/bin/
INSTALL_MANDIR := $(PREFIX)/share/man/man1/

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
else ifeq ($(LOCAL_ARCH),arm64)
    TARGET_ARCH ?= arm64
else ifeq ($(shell echo $(LOCAL_ARCH) | head -c 4),armv)
    TARGET_ARCH ?= arm
else ifeq ($(LOCAL_ARCH),s390x)
    TARGET_ARCH ?= s390x
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
	EXECUTABLE_PATH=./$(BINARY_NAME).exe
else
	EXECUTABLE_PATH=./$(BINARY_NAME)
endif

ifeq ($(FLAVOR), centos-7)
	TARGET_PUBLIC_REPO ?= el7
else
	TARGET_PUBLIC_REPO ?= $(FLAVOR)
endif

ifneq ($(TARGET_ARM), )
	ARM_COMMAND := GOARM=$(TARGET_ARM)
endif

ifeq ($(TARGET_ARM), 7) 
	PACKAGE_ARCH := armhf
else
	PACKAGE_ARCH := $(TARGET_ARCH)
endif

.PHONY: all
all: cloudflared test

.PHONY: clean
clean:
	go clean

.PHONY: cloudflared
cloudflared:
ifeq ($(FIPS), true)
	$(info Building cloudflared with go-fips)
	cp -f fips/fips.go.linux-amd64 cmd/cloudflared/fips.go
endif
	GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) $(ARM_COMMAND) go build -v -mod=vendor $(GO_BUILD_TAGS) $(LDFLAGS) $(IMPORT_PATH)/cmd/cloudflared
ifeq ($(FIPS), true)
	rm -f cmd/cloudflared/fips.go
	./check-fips.sh cloudflared
endif

.PHONY: container
container:
	docker build --build-arg=TARGET_ARCH=$(TARGET_ARCH) --build-arg=TARGET_OS=$(TARGET_OS) -t cloudflare/cloudflared-$(TARGET_OS)-$(TARGET_ARCH):"$(VERSION)" .

.PHONY: test
test: vet
ifndef CI
	go test -v -mod=vendor -race $(LDFLAGS) ./...
else
	@mkdir -p .cover
	go test -v -mod=vendor -race $(LDFLAGS) -coverprofile=".cover/c.out" ./...
	go tool cover -html ".cover/c.out" -o .cover/all.html
endif

.PHONY: test-ssh-server
test-ssh-server:
	docker-compose -f ssh_server_tests/docker-compose.yml up

define publish_package
	chmod 664 $(BINARY_NAME)*.$(1); \
	for HOST in $(CF_PKG_HOSTS); do \
		ssh-keyscan -t ecdsa $$HOST >> ~/.ssh/known_hosts; \
		scp -p -4 $(BINARY_NAME)*.$(1) cfsync@$$HOST:/state/cf-pkg/staging/$(2)/$(TARGET_PUBLIC_REPO)/$(BINARY_NAME)/; \
	done
endef

.PHONY: publish-deb
publish-deb: cloudflared-deb
	$(call publish_package,deb,apt)

.PHONY: publish-rpm
publish-rpm: cloudflared-rpm
	$(call publish_package,rpm,yum)

cloudflared.1: cloudflared_man_template
	cat cloudflared_man_template | sed -e 's/\$${VERSION}/$(VERSION)/; s/\$${DATE}/$(DATE)/' > cloudflared.1

install: cloudflared cloudflared.1
	mkdir -p $(DESTDIR)$(INSTALL_BINDIR) $(DESTDIR)$(INSTALL_MANDIR)
	install -m755 cloudflared $(DESTDIR)$(INSTALL_BINDIR)/cloudflared
	install -m644 cloudflared.1 $(DESTDIR)$(INSTALL_MANDIR)/cloudflared.1

# When we build packages, the package name will be FIPS-aware.
# But we keep the binary installed by it to be named "cloudflared" regardless.
define build_package
	mkdir -p $(PACKAGE_DIR)
	cp cloudflared $(PACKAGE_DIR)/cloudflared
	cp cloudflared.1 $(PACKAGE_DIR)/cloudflared.1
	fakeroot fpm -C $(PACKAGE_DIR) -s dir -t $(1) \
		--description 'Cloudflare Tunnel daemon' \
		--vendor 'Cloudflare' \
		--license 'Apache License Version 2.0' \
		--url 'https://github.com/cloudflare/cloudflared' \
		-m 'Cloudflare <support@cloudflare.com>' \
	    -a $(PACKAGE_ARCH) -v $(VERSION) -n $(DEB_PACKAGE_NAME) $(NIGHTLY_FLAGS) --after-install postinst.sh --after-remove postrm.sh \
		cloudflared=$(INSTALL_BINDIR) cloudflared.1=$(INSTALL_MANDIR)
endef

.PHONY: cloudflared-deb
cloudflared-deb: cloudflared cloudflared.1
	$(call build_package,deb)

.PHONY: cloudflared-rpm
cloudflared-rpm: cloudflared cloudflared.1
	$(call build_package,rpm)

.PHONY: cloudflared-pkg
cloudflared-pkg: cloudflared cloudflared.1
	$(call build_package,osxpkg)

.PHONY: cloudflared-msi
cloudflared-msi: cloudflared
	wixl --define Version=$(VERSION) --define Path=$(EXECUTABLE_PATH) --output cloudflared-$(VERSION)-$(TARGET_ARCH).msi cloudflared.wxs

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

.PHONY: github-release
github-release: cloudflared
	python3 github_release.py --path $(EXECUTABLE_PATH) --release-version $(VERSION)

.PHONY: github-release-built-pkgs
github-release-built-pkgs:
	python3 github_release.py --path $(PWD)/built_artifacts --release-version $(VERSION)

.PHONY: release-pkgs-linux
release-pkgs-linux:
	python3 ./release_pkgs.py

.PHONY: github-message
github-message:
	python3 github_message.py --release-version $(VERSION)

.PHONY: github-mac-upload
github-mac-upload:
	python3 github_release.py --path artifacts/cloudflared-darwin-amd64.tgz --release-version $(VERSION) --name cloudflared-darwin-amd64.tgz
	python3 github_release.py --path artifacts/cloudflared-amd64.pkg --release-version $(VERSION) --name cloudflared-amd64.pkg

.PHONY: tunnelrpc-deps
tunnelrpc-deps:
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

.PHONY: goimports
goimports:
	for d in $$(go list -mod=readonly -f '{{.Dir}}' -a ./... | fgrep -v tunnelrpc) ; do goimports -format-only -local github.com/cloudflare/cloudflared -w $$d ; done
