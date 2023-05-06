module github.com/cloudflare/cloudflared

go 1.19

require (
	github.com/cloudflare/brotli-go v0.0.0-20191101163834-d34379f7ff93
	github.com/cloudflare/golibs v0.0.0-20170913112048-333127dbecfc
	github.com/coredns/coredns v1.10.0
	github.com/coreos/go-oidc/v3 v3.4.0
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf
	github.com/facebookgo/grace v0.0.0-20180706040059-75cf19382434
	github.com/fsnotify/fsnotify v1.4.9
	github.com/getsentry/raven-go v0.2.0
	github.com/getsentry/sentry-go v0.16.0
	github.com/go-chi/chi/v5 v5.0.8
	github.com/go-jose/go-jose/v3 v3.0.0
	github.com/gobwas/ws v1.0.4
	github.com/golang-collections/collections v0.0.0-20130729185459-604e922904d3
	github.com/google/gopacket v1.1.19
	github.com/google/uuid v1.3.0
	github.com/gorilla/websocket v1.4.2
	github.com/json-iterator/go v1.1.12
	github.com/mattn/go-colorable v0.1.13
	github.com/miekg/dns v1.1.50
	github.com/mitchellh/go-homedir v1.1.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.13.0
	github.com/prometheus/client_model v0.2.0
	github.com/quic-go/quic-go v0.0.0-00010101000000-000000000000
	github.com/rs/zerolog v1.20.0
	github.com/stretchr/testify v1.8.1
	github.com/urfave/cli/v2 v2.3.0
	go.opentelemetry.io/contrib/propagators v0.22.0
	go.opentelemetry.io/otel v1.6.3
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.6.3
	go.opentelemetry.io/otel/sdk v1.6.3
	go.opentelemetry.io/otel/trace v1.6.3
	go.opentelemetry.io/proto/otlp v0.15.0
	go.uber.org/automaxprocs v1.4.0
	golang.org/x/crypto v0.8.0
	golang.org/x/net v0.9.0
	golang.org/x/sync v0.1.0
	golang.org/x/sys v0.7.0
	golang.org/x/term v0.7.0
	google.golang.org/protobuf v1.28.1
	gopkg.in/coreos/go-oidc.v2 v2.2.1
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	gopkg.in/square/go-jose.v2 v2.6.0
	gopkg.in/yaml.v3 v3.0.1
	nhooyr.io/websocket v1.8.7
	zombiezen.com/go/capnproto2 v2.18.0+incompatible
)

require (
	github.com/BurntSushi/toml v1.2.0 // indirect
	github.com/apparentlymart/go-cidr v1.1.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/certifi/gocertifi v0.0.0-20210507211836-431795d63e8d // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/cloudflare/circl v1.2.1-0.20220809205628-0a9554f37a47 // indirect
	github.com/coredns/caddy v1.1.1 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/facebookgo/ensure v0.0.0-20160127193407-b4ab57deab51 // indirect
	github.com/facebookgo/freeport v0.0.0-20150612182905-d4adf43b75b9 // indirect
	github.com/facebookgo/stack v0.0.0-20160209184415-751773369052 // indirect
	github.com/facebookgo/subset v0.0.0-20150612182917-8dac2c3c4870 // indirect
	github.com/flynn/go-shlex v0.0.0-20150515145356-3f9db97f8568 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-task/slim-sprig v0.0.0-20210107165309-348f09dbbbc0 // indirect
	github.com/gobwas/httphead v0.0.0-20200921212729-da3d93bc3c58 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/pprof v0.0.0-20210720184732-4bb14d4b1be1 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.7.0 // indirect
	github.com/grpc-ecosystem/grpc-opentracing v0.0.0-20180507213350-8e809c8a8645 // indirect
	github.com/klauspost/compress v1.15.11 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mattn/go-isatty v0.0.16 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/onsi/ginkgo/v2 v2.4.0 // indirect
	github.com/onsi/gomega v1.23.0 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/pquerna/cachecontrol v0.0.0-20180517163645-1555304b9b35 // indirect
	github.com/prometheus/common v0.37.0 // indirect
	github.com/prometheus/procfs v0.8.0 // indirect
	github.com/quic-go/qtls-go1-19 v0.3.2 // indirect
	github.com/quic-go/qtls-go1-20 v0.2.2 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	golang.org/x/exp v0.0.0-20221205204356-47842c84f3db // indirect
	golang.org/x/mod v0.8.0 // indirect
	golang.org/x/oauth2 v0.4.0 // indirect
	golang.org/x/text v0.9.0 // indirect
	golang.org/x/tools v0.6.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20221202195650-67e5cbc046fd // indirect
	google.golang.org/grpc v1.51.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)

replace github.com/urfave/cli/v2 => github.com/ipostelnik/cli/v2 v2.3.1-0.20210324024421-b6ea8234fe3d

// Avoid 'CVE-2022-21698'
replace github.com/prometheus/golang_client => github.com/prometheus/golang_client v1.12.1

replace gopkg.in/yaml.v3 => gopkg.in/yaml.v3 v3.0.1

replace github.com/quic-go/quic-go => github.com/devincarr/quic-go v0.0.0-20230502200822-d1f4edacbee7

// Post-quantum tunnel RTG-1339
replace (
	// Branches go1.19 go1.20 on github.com/cloudflare/qtls-pq
	github.com/quic-go/qtls-go1-19 => github.com/cloudflare/qtls-pq v0.0.0-20230320123031-3faac1a945b2
	github.com/quic-go/qtls-go1-20 => github.com/cloudflare/qtls-pq v0.0.0-20230320122459-4ed280d0d633
)
