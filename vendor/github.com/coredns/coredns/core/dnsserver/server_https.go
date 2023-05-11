package dnsserver

import (
	"context"
	"crypto/tls"
	"fmt"
	stdlog "log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/plugin/metrics/vars"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	"github.com/coredns/coredns/plugin/pkg/doh"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/plugin/pkg/response"
	"github.com/coredns/coredns/plugin/pkg/reuseport"
	"github.com/coredns/coredns/plugin/pkg/transport"
)

// ServerHTTPS represents an instance of a DNS-over-HTTPS server.
type ServerHTTPS struct {
	*Server
	httpsServer  *http.Server
	listenAddr   net.Addr
	tlsConfig    *tls.Config
	validRequest func(*http.Request) bool
}

// loggerAdapter is a simple adapter around CoreDNS logger made to implement io.Writer in order to log errors from HTTP server
type loggerAdapter struct {
}

func (l *loggerAdapter) Write(p []byte) (n int, err error) {
	clog.Debug(string(p))
	return len(p), nil
}

// HTTPRequestKey is the context key for the current processed HTTP request (if current processed request was done over DOH)
type HTTPRequestKey struct{}

// NewServerHTTPS returns a new CoreDNS HTTPS server and compiles all plugins in to it.
func NewServerHTTPS(addr string, group []*Config) (*ServerHTTPS, error) {
	s, err := NewServer(addr, group)
	if err != nil {
		return nil, err
	}
	// The *tls* plugin must make sure that multiple conflicting
	// TLS configuration returns an error: it can only be specified once.
	var tlsConfig *tls.Config
	for _, z := range s.zones {
		for _, conf := range z {
			// Should we error if some configs *don't* have TLS?
			tlsConfig = conf.TLSConfig
		}
	}

	// http/2 is recommended when using DoH. We need to specify it in next protos
	// or the upgrade won't happen.
	if tlsConfig != nil {
		tlsConfig.NextProtos = []string{"h2", "http/1.1"}
	}

	// Use a custom request validation func or use the standard DoH path check.
	var validator func(*http.Request) bool
	for _, z := range s.zones {
		for _, conf := range z {
			validator = conf.HTTPRequestValidateFunc
		}
	}
	if validator == nil {
		validator = func(r *http.Request) bool { return r.URL.Path == doh.Path }
	}

	srv := &http.Server{
		ReadTimeout:  s.readTimeout,
		WriteTimeout: s.writeTimeout,
		IdleTimeout:  s.idleTimeout,
		ErrorLog:     stdlog.New(&loggerAdapter{}, "", 0),
	}
	sh := &ServerHTTPS{
		Server: s, tlsConfig: tlsConfig, httpsServer: srv, validRequest: validator,
	}
	sh.httpsServer.Handler = sh

	return sh, nil
}

// Compile-time check to ensure Server implements the caddy.GracefulServer interface
var _ caddy.GracefulServer = &Server{}

// Serve implements caddy.TCPServer interface.
func (s *ServerHTTPS) Serve(l net.Listener) error {
	s.m.Lock()
	s.listenAddr = l.Addr()
	s.m.Unlock()

	if s.tlsConfig != nil {
		l = tls.NewListener(l, s.tlsConfig)
	}
	return s.httpsServer.Serve(l)
}

// ServePacket implements caddy.UDPServer interface.
func (s *ServerHTTPS) ServePacket(p net.PacketConn) error { return nil }

// Listen implements caddy.TCPServer interface.
func (s *ServerHTTPS) Listen() (net.Listener, error) {
	l, err := reuseport.Listen("tcp", s.Addr[len(transport.HTTPS+"://"):])
	if err != nil {
		return nil, err
	}
	return l, nil
}

// ListenPacket implements caddy.UDPServer interface.
func (s *ServerHTTPS) ListenPacket() (net.PacketConn, error) { return nil, nil }

// OnStartupComplete lists the sites served by this server
// and any relevant information, assuming Quiet is false.
func (s *ServerHTTPS) OnStartupComplete() {
	if Quiet {
		return
	}

	out := startUpZones(transport.HTTPS+"://", s.Addr, s.zones)
	if out != "" {
		fmt.Print(out)
	}
}

// Stop stops the server. It blocks until the server is totally stopped.
func (s *ServerHTTPS) Stop() error {
	s.m.Lock()
	defer s.m.Unlock()
	if s.httpsServer != nil {
		s.httpsServer.Shutdown(context.Background())
	}
	return nil
}

// ServeHTTP is the handler that gets the HTTP request and converts to the dns format, calls the plugin
// chain, converts it back and write it to the client.
func (s *ServerHTTPS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.validRequest(r) {
		http.Error(w, "", http.StatusNotFound)
		s.countResponse(http.StatusNotFound)
		return
	}

	msg, err := doh.RequestToMsg(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		s.countResponse(http.StatusBadRequest)
		return
	}

	// Create a DoHWriter with the correct addresses in it.
	h, p, _ := net.SplitHostPort(r.RemoteAddr)
	port, _ := strconv.Atoi(p)
	dw := &DoHWriter{
		laddr:   s.listenAddr,
		raddr:   &net.TCPAddr{IP: net.ParseIP(h), Port: port},
		request: r,
	}

	// We just call the normal chain handler - all error handling is done there.
	// We should expect a packet to be returned that we can send to the client.
	ctx := context.WithValue(context.Background(), Key{}, s.Server)
	ctx = context.WithValue(ctx, LoopKey{}, 0)
	ctx = context.WithValue(ctx, HTTPRequestKey{}, r)
	s.ServeDNS(ctx, dw, msg)

	// See section 4.2.1 of RFC 8484.
	// We are using code 500 to indicate an unexpected situation when the chain
	// handler has not provided any response message.
	if dw.Msg == nil {
		http.Error(w, "No response", http.StatusInternalServerError)
		s.countResponse(http.StatusInternalServerError)
		return
	}

	buf, _ := dw.Msg.Pack()

	mt, _ := response.Typify(dw.Msg, time.Now().UTC())
	age := dnsutil.MinimalTTL(dw.Msg, mt)

	w.Header().Set("Content-Type", doh.MimeType)
	w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%f", age.Seconds()))
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	w.WriteHeader(http.StatusOK)
	s.countResponse(http.StatusOK)

	w.Write(buf)
}

func (s *ServerHTTPS) countResponse(status int) {
	vars.HTTPSResponsesCount.WithLabelValues(s.Addr, strconv.Itoa(status)).Inc()
}

// Shutdown stops the server (non gracefully).
func (s *ServerHTTPS) Shutdown() error {
	if s.httpsServer != nil {
		s.httpsServer.Shutdown(context.Background())
	}
	return nil
}
