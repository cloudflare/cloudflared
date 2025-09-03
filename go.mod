module github.com/cloudflare/cloudflared

go 1.24

require (
	github.com/coredns/coredns v1.12.2
	github.com/coreos/go-oidc/v3 v3.10.0
	github.com/coreos/go-systemd/v22 v22.5.0
	github.com/facebookgo/grace v0.0.0-20180706040059-75cf19382434
	github.com/fortytw2/leaktest v1.3.0
	github.com/fsnotify/fsnotify v1.4.9
	github.com/getsentry/sentry-go v0.16.0
	github.com/go-chi/chi/v5 v5.2.2
	github.com/go-chi/cors v1.2.1
	github.com/go-jose/go-jose/v4 v4.1.0
	github.com/gobwas/ws v1.2.1
	github.com/google/gopacket v1.1.19
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.0
	github.com/json-iterator/go v1.1.12
	github.com/mattn/go-colorable v0.1.13
	github.com/miekg/dns v1.1.66
	github.com/mitchellh/go-homedir v1.1.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.22.0
	github.com/prometheus/client_model v0.6.2
	github.com/quic-go/quic-go v0.52.0
	github.com/rs/zerolog v1.20.0
	github.com/stretchr/testify v1.10.0
	github.com/urfave/cli/v2 v2.3.0
	go.opentelemetry.io/contrib/propagators v0.22.0
	go.opentelemetry.io/otel v1.35.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.26.0
	go.opentelemetry.io/otel/sdk v1.35.0
	go.opentelemetry.io/otel/trace v1.35.0
	go.opentelemetry.io/proto/otlp v1.2.0
	go.uber.org/automaxprocs v1.6.0
	go.uber.org/mock v0.5.1
	golang.org/x/crypto v0.38.0
	golang.org/x/net v0.40.0
	golang.org/x/sync v0.14.0
	golang.org/x/sys v0.33.0
	golang.org/x/term v0.32.0
	google.golang.org/protobuf v1.36.6
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	gopkg.in/yaml.v3 v3.0.1
	nhooyr.io/websocket v1.8.7
	zombiezen.com/go/capnproto2 v2.18.0+incompatible
)

require (
	github.com/BurntSushi/toml v1.2.0 // indirect
	github.com/apparentlymart/go-cidr v1.1.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bytedance/sonic v1.12.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coredns/caddy v1.1.2-0.20241029205200-8de985351a98 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/facebookgo/ensure v0.0.0-20160127193407-b4ab57deab51 // indirect
	github.com/facebookgo/freeport v0.0.0-20150612182905-d4adf43b75b9 // indirect
	github.com/facebookgo/stack v0.0.0-20160209184415-751773369052 // indirect
	github.com/facebookgo/subset v0.0.0-20150612182917-8dac2c3c4870 // indirect
	github.com/flynn/go-shlex v0.0.0-20150515145356-3f9db97f8568 // indirect
	github.com/gin-gonic/gin v1.9.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/validator/v10 v10.15.1 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/pprof v0.0.0-20250418163039-24c5476c6587 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.3 // indirect
	github.com/grpc-ecosystem/grpc-opentracing v0.0.0-20180507213350-8e809c8a8645 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.5 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/onsi/ginkgo/v2 v2.23.4 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/pelletier/go-toml/v2 v2.0.9 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/common v0.64.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel/metric v1.35.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.35.0 // indirect
	golang.org/x/arch v0.4.0 // indirect
	golang.org/x/mod v0.24.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	golang.org/x/tools v0.32.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250505200425-f936aa4a68b2 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250512202823-5a2f75b736a9 // indirect
	google.golang.org/grpc v1.72.2 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)

replace github.com/urfave/cli/v2 => github.com/ipostelnik/cli/v2 v2.3.1-0.20210324024421-b6ea8234fe3d

// Avoid 'CVE-2022-21698'
replace github.com/prometheus/golang_client => github.com/prometheus/golang_client v1.12.1

replace gopkg.in/yaml.v3 => gopkg.in/yaml.v3 v3.0.1

// This fork is based on quic-go v0.45
replace github.com/quic-go/quic-go => github.com/chungthuang/quic-go v0.45.1-0.20250428085412-43229ad201fd
