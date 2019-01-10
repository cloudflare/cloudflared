# Copyright (c) 2013 CloudFlare, Inc.

RACE+=--race

PKGNAME=github.com/cloudflare/golibs/lrucache
SKIPCOVER=list.go|list_extension.go|priorityqueue.go

.PHONY: all test bench cover clean

all:
	@echo "Targets:"
	@echo "  test: run tests with race detector"
	@echo "  cover: print test coverage"
	@echo "  bench: run basic benchmarks"

test:
	@go test $(RACE) -bench=. -v $(PKGNAME)

COVEROUT=cover.out
cover:
	@go test -coverprofile=$(COVEROUT) -v $(PKGNAME)
	@cat $(COVEROUT) | egrep -v '$(SKIPCOVER)' > $(COVEROUT)~
	@go tool cover -func=$(COVEROUT)~|sed 's|^.*/\([^/]*/[^/]*/[^/]*\)$$|\1|g'

bench:
	@echo "[*] Scalability of cache/lrucache"
	@echo "[ ] Operations in shared cache using one core"
	@GOMAXPROCS=1 go test -run=- -bench='.*LRUCache.*' $(PKGNAME) \
		| egrep -v "^PASS|^ok"

	@echo "[*] Scalability of cache/multilru"
	@echo "[ ] Operations in four caches using four cores	"
	@GOMAXPROCS=4 go test -run=- -bench='.*MultiLRU.*' $(PKGNAME) \
		| egrep -v "^PASS|^ok"


	@(cd benchmark; go build $(PKGNAME)/benchmark)
	@./benchmark/benchmark

clean:
	rm -rf $(COVEROUT) $(COVEROUT)~ benchmark/benchmark
