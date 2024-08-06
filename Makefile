# The targets cannot be run in parallel
.NOTPARALLEL:

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
CF_GO_PATH     := /tmp/go
PATH           := $(CF_GO_PATH)/bin:$(PATH)

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
else ifeq ($(LOCAL_OS),openbsd)
    TARGET_OS ?= openbsd
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

#for FIPS compliance, FPM defaults to MD5.
RPM_DIGEST := --rpm-digest sha256

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
	GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) $(ARM_COMMAND) go build -mod=vendor $(GO_BUILD_TAGS) $(LDFLAGS) $(IMPORT_PATH)/cmd/cloudflared
ifeq ($(FIPS), true)
	rm -f cmd/cloudflared/fips.go
	./check-fips.sh cloudflared
endif

.PHONY: container
container:
	docker build --build-arg=TARGET_ARCH=$(TARGET_ARCH) --build-arg=TARGET_OS=$(TARGET_OS) -t cloudflare/cloudflared-$(TARGET_OS)-$(TARGET_ARCH):"$(VERSION)" .

.PHONY: generate-docker-version
generate-docker-version:
	echo latest $(VERSION) > versions


.PHONY: test
test: vet
ifndef CI
	go test -v -mod=vendor -race $(LDFLAGS) ./...
else
	@mkdir -p .cover
	go test -v -mod=vendor -race $(LDFLAGS) -coverprofile=".cover/c.out" ./...
endif

.PHONY: cover
cover:
	@echo ""
	@echo "=====> Total test coverage: <====="
	@echo ""
	# Print the overall coverage here for quick access.
	$Q go tool cover -func ".cover/c.out" | grep "total:" | awk '{print $$3}'
	# Generate the HTML report that can be viewed from the browser in CI.
	$Q go tool cover -html ".cover/c.out" -o .cover/all.html

.PHONY: test-ssh-server
test-ssh-server:
	docker-compose -f ssh_server_tests/docker-compose.yml up

.PHONY: install-go
install-go:
	rm -rf ${CF_GO_PATH}
	./.teamcity/install-cloudflare-go.sh

.PHONY: cleanup-go
cleanup-go:
	rm -rf ${CF_GO_PATH}

cloudflared.1: cloudflared_man_template
	sed -e 's/\$${VERSION}/$(VERSION)/; s/\$${DATE}/$(DATE)/' cloudflared_man_template > cloudflared.1

install: install-go cloudflared cloudflared.1 cleanup-go
	mkdir -p $(DESTDIR)$(INSTALL_BINDIR) $(DESTDIR)$(INSTALL_MANDIR)
	install -m755 cloudflared $(DESTDIR)$(INSTALL_BINDIR)/cloudflared
	install -m644 cloudflared.1 $(DESTDIR)$(INSTALL_MANDIR)/cloudflared.1

# When we build packages, the package name will be FIPS-aware.
# But we keep the binary installed by it to be named "cloudflared" regardless.
define build_package
	mkdir -p $(PACKAGE_DIR)
	cp cloudflared $(PACKAGE_DIR)/cloudflared
	cp cloudflared.1 $(PACKAGE_DIR)/cloudflared.1
	fpm -C $(PACKAGE_DIR) -s dir -t $(1) \
		--description 'Cloudflare Tunnel daemon' \
		--vendor 'Cloudflare' \
		--license 'Apache License Version 2.0' \
		--url 'https://github.com/cloudflare/cloudflared' \
		-m 'Cloudflare <support@cloudflare.com>' \
	    -a $(PACKAGE_ARCH) -v $(VERSION) -n $(DEB_PACKAGE_NAME) $(RPM_DIGEST) $(NIGHTLY_FLAGS) --after-install postinst.sh --after-remove postrm.sh \
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
cloudflared-msi:
	wixl --define Version=$(VERSION) --define Path=$(EXECUTABLE_PATH) --output cloudflared-$(VERSION)-$(TARGET_ARCH).msi cloudflared.wxs

.PHONY: github-release-dryrun
github-release-dryrun:
	python3 github_release.py --path $(PWD)/built_artifacts --release-version $(VERSION) --dry-run

.PHONY: github-release
github-release:
	python3 github_release.py --path $(PWD)/built_artifacts --release-version $(VERSION)
	python3 github_message.py --release-version $(VERSION)

.PHONY: r2-linux-release
r2-linux-release:
	python3 ./release_pkgs.py

.PHONY: capnp
capnp:
	which capnp  # https://capnproto.org/install.html
	which capnpc-go  # go install zombiezen.com/go/capnproto2/capnpc-go@latest
	capnp compile -ogo tunnelrpc/proto/tunnelrpc.capnp tunnelrpc/proto/quic_metadata_protocol.capnp

.PHONY: vet
vet:
	go vet -mod=vendor github.com/cloudflare/cloudflared/...

.PHONY: fmt
fmt:
	goimports -l -w -local github.com/cloudflare/cloudflared $$(go list -mod=vendor -f '{{.Dir}}' -a ./... | fgrep -v tunnelrpc/proto)
