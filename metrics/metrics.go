package metrics

import (
	"context"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"sync"
	"time"

	"github.com/facebookgo/grace/gracenet"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"golang.org/x/net/trace"

	"github.com/cloudflare/cloudflared/diagnostic"
)

const (
	startupTime            = time.Millisecond * 500
	defaultShutdownTimeout = time.Second * 15
)

// This variable is set at compile time to allow the default local address to change.
var Runtime = "host"

func GetMetricsDefaultAddress(runtimeType string) string {
	// When issuing the diagnostic command we may have to reach a server that is
	// running in a virtual environment and in that case we must bind to 0.0.0.0
	// otherwise the server won't be reachable.
	switch runtimeType {
	case "virtual":
		return "0.0.0.0:0"
	default:
		return "localhost:0"
	}
}

// GetMetricsKnownAddresses returns the addresses used by the metrics server to bind at
// startup time to allow a semi-deterministic approach to know where the server is listening at.
// The ports were selected because at the time we are in 2024 and they do not collide with any
// know/registered port according https://en.wikipedia.org/wiki/List_of_TCP_and_UDP_port_numbers.
func GetMetricsKnownAddresses(runtimeType string) []string {
	switch runtimeType {
	case "virtual":
		return []string{"0.0.0.0:20241", "0.0.0.0:20242", "0.0.0.0:20243", "0.0.0.0:20244", "0.0.0.0:20245"}
	default:
		return []string{"localhost:20241", "localhost:20242", "localhost:20243", "localhost:20244", "localhost:20245"}
	}
}

type Config struct {
	ReadyServer         *ReadyServer
	DiagnosticHandler   *diagnostic.Handler
	QuickTunnelHostname string
	Orchestrator        orchestrator

	ShutdownTimeout time.Duration
}

type orchestrator interface {
	GetVersionedConfigJSON() ([]byte, error)
}

func newMetricsHandler(
	config Config,
	log *zerolog.Logger,
) *http.ServeMux {
	router := http.NewServeMux()
	router.Handle("/debug/", http.DefaultServeMux)
	router.Handle("/metrics", promhttp.Handler())
	router.HandleFunc("/healthcheck", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "OK\n")
	})
	if config.ReadyServer != nil {
		router.Handle("/ready", config.ReadyServer)
	}
	router.HandleFunc("/quicktunnel", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"hostname":"%s"}`, config.QuickTunnelHostname)
	})
	if config.Orchestrator != nil {
		router.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
			json, err := config.Orchestrator.GetVersionedConfigJSON()
			if err != nil {
				w.WriteHeader(500)
				_, _ = fmt.Fprintf(w, "ERR: %v", err)
				log.Err(err).Msg("Failed to serve config")
				return
			}
			_, _ = w.Write(json)
		})
	}

	config.DiagnosticHandler.InstallEndpoints(router)

	return router
}

// CreateMetricsListener will create a new [net.Listener] by using an
// known set of ports when the default address is passed with the fallback
// of choosing a random port when none is available.
//
// In case the provided address is not the default one then it will be used
// as is.
func CreateMetricsListener(listeners *gracenet.Net, laddr string) (net.Listener, error) {
	if laddr == GetMetricsDefaultAddress(Runtime) {
		// On the presence of the default address select
		// a port from the known set of addresses iteratively.
		addresses := GetMetricsKnownAddresses(Runtime)
		for _, address := range addresses {
			listener, err := listeners.Listen("tcp", address)
			if err == nil {
				return listener, nil
			}
		}

		// When no port is available then bind to a random one
		listener, err := listeners.Listen("tcp", laddr)
		if err != nil {
			return nil, fmt.Errorf("failed to listen to default metrics address: %w", err)
		}

		return listener, nil
	}

	// Explicitly got a local address then bind to it
	listener, err := listeners.Listen("tcp", laddr)
	if err != nil {
		return nil, fmt.Errorf("failed to bind to address (%s): %w", laddr, err)
	}

	return listener, nil
}

func ServeMetrics(
	l net.Listener,
	ctx context.Context,
	config Config,
	log *zerolog.Logger,
) (err error) {
	var wg sync.WaitGroup
	// Metrics port is privileged, so no need for further access control
	trace.AuthRequest = func(*http.Request) (bool, bool) { return true, true }
	// TODO: parameterize ReadTimeout and WriteTimeout. The maximum time we can
	// profile CPU usage depends on WriteTimeout
	h := newMetricsHandler(config, log)
	server := &http.Server{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      h,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		err = server.Serve(l)
	}()
	log.Info().Msgf("Starting metrics server on %s", fmt.Sprintf("%v/metrics", l.Addr()))
	// server.Serve will hang if server.Shutdown is called before the server is
	// fully started up. So add artificial delay.
	time.Sleep(startupTime)

	<-ctx.Done()
	shutdownTimeout := config.ShutdownTimeout
	if shutdownTimeout == 0 {
		shutdownTimeout = defaultShutdownTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	_ = server.Shutdown(ctx)
	cancel()

	wg.Wait()
	if err == http.ErrServerClosed {
		log.Info().Msg("Metrics server stopped")
		return nil
	}
	log.Err(err).Msg("Metrics server failed")
	return err
}

func RegisterBuildInfo(buildType, buildTime, version string) {
	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			// Don't namespace build_info, since we want it to be consistent across all Cloudflare services
			Name: "build_info",
			Help: "Build and version information",
		},
		[]string{"goversion", "type", "revision", "version"},
	)
	prometheus.MustRegister(buildInfo)
	buildInfo.WithLabelValues(runtime.Version(), buildType, buildTime, version).Set(1)
}
