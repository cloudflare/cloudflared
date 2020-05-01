module github.com/cloudflare/cloudflared

go 1.12

require (
	github.com/DATA-DOG/go-sqlmock v1.3.3
	github.com/anmitsu/go-shlex v0.0.0-20161002113705-648efa622239 // indirect
	github.com/aws/aws-sdk-go v1.25.8
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/certifi/gocertifi v0.0.0-20200211180108-c7c1fbc02894 // indirect
	github.com/cloudflare/brotli-go v0.0.0-20191101163834-d34379f7ff93
	github.com/cloudflare/cfssl v0.0.0-20141119014638-2f7f44e802e2
	github.com/cloudflare/golibs v0.0.0-20170913112048-333127dbecfc
	github.com/coredns/coredns v1.2.0
	github.com/coreos/go-oidc v0.0.0-20171002155002-a93f71fdfe73
	github.com/coreos/go-systemd v0.0.0-20190620071333-e64a0ec8b42a
	github.com/denisenkom/go-mssqldb v0.0.0-20191001013358-cfbb681360f0
	github.com/equinox-io/equinox v1.2.0
	github.com/facebookgo/ensure v0.0.0-20160127193407-b4ab57deab51 // indirect
	github.com/facebookgo/freeport v0.0.0-20150612182905-d4adf43b75b9 // indirect
	github.com/facebookgo/grace v0.0.0-20180706040059-75cf19382434
	github.com/facebookgo/stack v0.0.0-20160209184415-751773369052 // indirect
	github.com/facebookgo/subset v0.0.0-20150612182917-8dac2c3c4870 // indirect
	github.com/flynn/go-shlex v0.0.0-20150515145356-3f9db97f8568 // indirect
	github.com/fsnotify/fsnotify v1.4.9
	github.com/getsentry/raven-go v0.0.0-20180517221441-ed7bcb39ff10
	github.com/gliderlabs/ssh v0.0.0-20191009160644-63518b5243e0
	github.com/go-sql-driver/mysql v1.4.1
	github.com/golang-collections/collections v0.0.0-20130729185459-604e922904d3
	github.com/google/uuid v1.1.1
	github.com/gorilla/mux v1.7.3
	github.com/gorilla/websocket v1.4.0
	github.com/grpc-ecosystem/grpc-opentracing v0.0.0-20180507213350-8e809c8a8645 // indirect
	github.com/jmoiron/sqlx v1.2.0
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/kshvakov/clickhouse v1.3.11
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/lib/pq v1.2.0
	github.com/mattn/go-colorable v0.1.4
	github.com/mattn/go-isatty v0.0.10 // indirect
	github.com/mattn/go-sqlite3 v1.11.0
	github.com/mholt/caddy v0.0.0-20180807230124-d3b731e9255b // indirect
	github.com/miekg/dns v1.1.27
	github.com/mitchellh/go-homedir v1.1.0
	github.com/opentracing/opentracing-go v1.1.0 // indirect
	github.com/philhofer/fwd v1.0.0 // indirect
	github.com/pkg/errors v0.8.1
	github.com/pquerna/cachecontrol v0.0.0-20180517163645-1555304b9b35 // indirect
	github.com/prometheus/client_golang v1.0.0
	github.com/prometheus/client_model v0.0.0-20190812154241-14fe0d1b01d4 // indirect
	github.com/prometheus/common v0.7.0 // indirect
	github.com/prometheus/procfs v0.0.5 // indirect
	github.com/rifflock/lfshook v0.0.0-20180920164130-b9218ef580f5
	github.com/sirupsen/logrus v1.4.2
	github.com/stretchr/testify v1.3.0
	github.com/tinylib/msgp v1.1.0 // indirect
	github.com/xo/dburl v0.0.0-20191005012637-293c3298d6c0
	golang.org/x/crypto v0.0.0-20191011191535-87dc89f01550
	golang.org/x/net v0.0.0-20191014212845-da9a3fd4c582
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45 // indirect
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	golang.org/x/sys v0.0.0-20191020212454-3e7259c5e7c2
	google.golang.org/appengine v1.5.0 // indirect
	google.golang.org/genproto v0.0.0-20191007204434-a023cd5227bd // indirect
	google.golang.org/grpc v1.24.0 // indirect
	gopkg.in/coreos/go-oidc.v2 v2.1.0
	gopkg.in/square/go-jose.v2 v2.4.0 // indirect
	gopkg.in/urfave/cli.v2 v2.0.0-20180128181224-d604b6ffeee8
	gopkg.in/yaml.v2 v2.2.4
	zombiezen.com/go/capnproto2 v0.0.0-20180616160808-7cfd211c19c7
)

// ../../go/pkg/mod/github.com/coredns/coredns@v1.2.0/plugin/metrics/metrics.go:40:49: too many arguments in call to prometheus.NewProcessCollector
// have (int, string)
// want (prometheus.ProcessCollectorOpts)
replace github.com/prometheus/client_golang => github.com/prometheus/client_golang v0.9.0-pre1
