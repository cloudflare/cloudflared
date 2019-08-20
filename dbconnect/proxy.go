package dbconnect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/validation"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	timing "github.com/mitchellh/go-server-timing"
)

// Proxy is an HTTP server that proxies requests to a Client.
type Proxy struct {
	client          Client
	accessValidator *validation.Access
	logger          *logrus.Logger
}

// NewInsecureProxy creates a Proxy that talks to a Client at an origin.
//
// In insecure mode, the Proxy will allow all Command requests.
func NewInsecureProxy(ctx context.Context, origin string) (*Proxy, error) {
	originURL, err := url.Parse(origin)
	if err != nil {
		return nil, errors.Wrap(err, "must provide a valid database url")
	}

	client, err := NewClient(ctx, originURL)
	if err != nil {
		return nil, err
	}

	err = client.Ping(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "could not connect to the database")
	}

	return &Proxy{client, nil, logrus.New()}, nil
}

// NewSecureProxy creates a Proxy that talks to a Client at an origin.
//
// In secure mode, the Proxy will reject any Command requests that are
// not authenticated by Cloudflare Access with a valid JWT.
func NewSecureProxy(ctx context.Context, origin, authDomain, applicationAUD string) (*Proxy, error) {
	proxy, err := NewInsecureProxy(ctx, origin)
	if err != nil {
		return nil, err
	}

	validator, err := validation.NewAccessValidator(ctx, authDomain, authDomain, applicationAUD)
	if err != nil {
		return nil, err
	}

	proxy.accessValidator = validator
	return proxy, err
}

// IsInsecure gets whether the Proxy will accept a Command from any source.
func (proxy *Proxy) IsInsecure() bool {
	return proxy.accessValidator == nil
}

// IsAllowed checks whether a http.Request is allowed to receive data.
//
// By default, requests must pass through Cloudflare Access for authentication.
// If the proxy is explcitly set to insecure mode, all requests will be allowed.
func (proxy *Proxy) IsAllowed(r *http.Request, verbose ...bool) bool {
	if proxy.IsInsecure() {
		return true
	}

	// Access and Tunnel should prevent bad JWTs from even reaching the origin,
	// but validate tokens anyway as an abundance of caution.
	err := proxy.accessValidator.ValidateRequest(r.Context(), r)
	if err == nil {
		return true
	}

	// Warn administrators that invalid JWTs are being rejected. This is indicative
	// of either a misconfiguration of the CLI or a massive failure of upstream systems.
	if len(verbose) > 0 {
		proxy.httpLog(r, err).Error("Failed JWT authentication")
	}

	return false
}

// Start the Proxy at a given address and notify the listener channel when the server is online.
func (proxy *Proxy) Start(ctx context.Context, addr string, listenerC chan<- net.Listener) error {
	// STOR-611: use a seperate listener and consider web socket support.
	httpListener, err := hello.CreateTLSListener(addr)
	if err != nil {
		return errors.Wrapf(err, "could not create listener at %s", addr)
	}

	errC := make(chan error)
	defer close(errC)

	// Starts the HTTP server and begins to serve requests.
	go func() {
		errC <- proxy.httpListen(ctx, httpListener)
	}()

	// Continually ping the server until it comes online or 10 attempts fail.
	go func() {
		var err error
		for i := 0; i < 10; i++ {
			_, err = http.Get("http://" + httpListener.Addr().String())

			// Once no error was detected, notify the listener channel and return.
			if err == nil {
				listenerC <- httpListener
				return
			}

			// Backoff between requests to ping the server.
			<-time.After(1 * time.Second)
		}
		errC <- errors.Wrap(err, "took too long for the http server to start")
	}()

	return <-errC
}

// httpListen starts the httpServer and blocks until the context closes.
func (proxy *Proxy) httpListen(ctx context.Context, listener net.Listener) error {
	httpServer := &http.Server{
		Addr:         listener.Addr().String(),
		Handler:      timing.Middleware(proxy.httpRouter(), nil),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		httpServer.Close()
		listener.Close()
	}()

	return httpServer.Serve(listener)
}

// httpRouter creates a mux.Router for the Proxy.
func (proxy *Proxy) httpRouter() *mux.Router {
	router := mux.NewRouter()

	router.HandleFunc("/ping", proxy.httpPing()).Methods("GET", "HEAD")
	router.HandleFunc("/submit", proxy.httpSubmit()).Methods("POST")

	return router
}

// httpPing tests the connection to the database.
//
// By default, this endpoint is unauthenticated to allow for health checks.
// To enable authentication, Cloudflare Access must be enabled on this route.
func (proxy *Proxy) httpPing() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		metric := timing.FromContext(ctx).NewMetric("db").Start()
		err := proxy.client.Ping(ctx)
		metric.Stop()

		if err == nil {
			proxy.httpRespond(w, r, http.StatusOK, "")
		} else {
			proxy.httpRespondErr(w, r, http.StatusInternalServerError, err)
		}
	}
}

// httpSubmit sends a command to the database and returns its response.
//
// By default, this endpoint will reject requests that do not pass through Cloudflare Access.
// To disable authentication, the --insecure flag must be specified in the command line.
func (proxy *Proxy) httpSubmit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !proxy.IsAllowed(r, true) {
			proxy.httpRespondErr(w, r, http.StatusForbidden, fmt.Errorf(""))
			return
		}

		var cmd Command
		err := json.NewDecoder(r.Body).Decode(&cmd)
		if err != nil {
			proxy.httpRespondErr(w, r, http.StatusBadRequest, err)
			return
		}

		ctx := r.Context()
		metric := timing.FromContext(ctx).NewMetric("db").Start()
		data, err := proxy.client.Submit(ctx, &cmd)
		metric.Stop()

		if err != nil {
			proxy.httpRespondErr(w, r, http.StatusUnprocessableEntity, err)
			return
		}

		w.Header().Set("Content-type", "application/json")
		err = json.NewEncoder(w).Encode(data)
		if err != nil {
			proxy.httpRespondErr(w, r, http.StatusInternalServerError, err)
		}
	}
}

// httpRespond writes a status code and string response to the response writer.
func (proxy *Proxy) httpRespond(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.WriteHeader(status)

	// Only expose the message detail of the reponse if the request is not HEAD
	// and the user is authenticated. For example, this prevents an unauthenticated
	// failed health check from accidentally leaking sensitive information about the Client.
	if r.Method != http.MethodHead && proxy.IsAllowed(r) {
		if message == "" {
			message = http.StatusText(status)
		}
		fmt.Fprint(w, message)
	}
}

// httpRespondErr is similar to httpRespond, except it formats errors to be more friendly.
func (proxy *Proxy) httpRespondErr(w http.ResponseWriter, r *http.Request, defaultStatus int, err error) {
	status, err := httpError(defaultStatus, err)

	proxy.httpRespond(w, r, status, err.Error())
	proxy.httpLog(r, err).Warn("Database connect error")
}

// httpLog returns a logrus.Entry that is formatted to output a request Cf-ray.
func (proxy *Proxy) httpLog(r *http.Request, err error) *logrus.Entry {
	return proxy.logger.WithContext(r.Context()).WithField("CF-RAY", r.Header.Get("Cf-ray")).WithError(err)
}

// httpError extracts common errors and returns an status code and friendly error.
func httpError(defaultStatus int, err error) (int, error) {
	if err == nil {
		return http.StatusNotImplemented, fmt.Errorf("error expected but found none")
	}

	if err == io.EOF {
		return http.StatusBadRequest, fmt.Errorf("request body cannot be empty")
	}

	if err == context.DeadlineExceeded {
		return http.StatusRequestTimeout, err
	}

	_, ok := err.(net.Error)
	if ok {
		return http.StatusRequestTimeout, err
	}

	if err == context.Canceled {
		// Does not exist in Golang, but would be: http.StatusClientClosedWithoutResponse
		return 444, err
	}

	return defaultStatus, err
}
