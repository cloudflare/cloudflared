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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"golang.org/x/net/trace"
)

const (
	startupTime            = time.Millisecond * 500
	defaultShutdownTimeout = time.Second * 15
)

type Config struct {
	ReadyServer         *ReadyServer
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

	return router
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
