package dnsserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"
)

// Config configuration for a single server.
type Config struct {
	// The zone of the site.
	Zone string

	// one or several hostnames to bind the server to.
	// defaults to a single empty string that denote the wildcard address
	ListenHosts []string

	// The port to listen on.
	Port string

	// The number of servers that will listen on one port.
	// By default, one server will be running.
	NumSockets int

	// Root points to a base directory we find user defined "things".
	// First consumer is the file plugin to looks for zone files in this place.
	Root string

	// Debug controls the panic/recover mechanism that is enabled by default.
	Debug bool

	// Stacktrace controls including stacktrace as part of log from recover mechanism, it is disabled by default.
	Stacktrace bool

	// The transport we implement, normally just "dns" over TCP/UDP, but could be
	// DNS-over-TLS or DNS-over-gRPC.
	Transport string

	// If this function is not nil it will be used to inspect and validate
	// HTTP requests. Although this isn't referenced in-tree, external plugins
	// may depend on it.
	HTTPRequestValidateFunc func(*http.Request) bool

	// FilterFuncs is used to further filter access
	// to this handler. E.g. to limit access to a reverse zone
	// on a non-octet boundary, i.e. /17
	FilterFuncs []FilterFunc

	// ViewName is the name of the Viewer PLugin defined in the Config
	ViewName string

	// TLSConfig when listening for encrypted connections (gRPC, DNS-over-TLS).
	TLSConfig *tls.Config

	// MaxQUICStreams defines the maximum number of concurrent QUIC streams for a QUIC server.
	// This is nil if not specified, allowing for a default to be used.
	MaxQUICStreams *int

	// MaxQUICWorkerPoolSize defines the size of the worker pool for processing QUIC streams.
	// This is nil if not specified, allowing for a default to be used.
	MaxQUICWorkerPoolSize *int

	// Timeouts for TCP, TLS and HTTPS servers.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration

	// TSIG secrets, [name]key.
	TsigSecret map[string]string

	// Plugin stack.
	Plugin []plugin.Plugin

	// Compiled plugin stack.
	pluginChain plugin.Handler

	// Plugin interested in announcing that they exist, so other plugin can call methods
	// on them should register themselves here. The name should be the name as return by the
	// Handler's Name method.
	registry map[string]plugin.Handler

	// firstConfigInBlock is used to reference the first config in a server block, for the
	// purpose of sharing single instance of each plugin among all zones in a server block.
	firstConfigInBlock *Config

	// metaCollector references the first MetadataCollector plugin, if one exists
	metaCollector MetadataCollector
}

// FilterFunc is a function that filters requests from the Config
type FilterFunc func(context.Context, *request.Request) bool

// keyForConfig builds a key for identifying the configs during setup time
func keyForConfig(blocIndex int, blocKeyIndex int) string {
	return fmt.Sprintf("%d:%d", blocIndex, blocKeyIndex)
}

// GetConfig gets the Config that corresponds to c.
// If none exist nil is returned.
func GetConfig(c *caddy.Controller) *Config {
	ctx := c.Context().(*dnsContext)
	key := keyForConfig(c.ServerBlockIndex, c.ServerBlockKeyIndex)
	if cfg, ok := ctx.keysToConfigs[key]; ok {
		return cfg
	}
	// we should only get here during tests because directive
	// actions typically skip the server blocks where we make
	// the configs.
	ctx.saveConfig(key, &Config{ListenHosts: []string{""}})
	return GetConfig(c)
}
