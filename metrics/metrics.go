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

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"golang.org/x/net/trace"
)

const (
	shutdownTimeout = time.Second * 15
	startupTime     = time.Millisecond * 500
)

type orchestrator interface {
	GetVersionedConfigJSON() ([]byte, error)
}

func newMetricsHandler(
	readyServer *ReadyServer,
	quickTunnelHostname string,
	orchestrator orchestrator,
	log *zerolog.Logger,
) *mux.Router {
	router := mux.NewRouter()
	router.PathPrefix("/debug/").Handler(http.DefaultServeMux)

	router.Handle("/metrics", promhttp.Handler())
	router.HandleFunc("/healthcheck", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "OK\n")
	})
	if readyServer != nil {
		router.Handle("/ready", readyServer)
	}
	router.HandleFunc("/quicktunnel", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"hostname":"%s"}`, quickTunnelHostname)
	})
	if orchestrator != nil {
		router.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
			json, err := orchestrator.GetVersionedConfigJSON()
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
	shutdownC <-chan struct{},
	readyServer *ReadyServer,
	quickTunnelHostname string,
	orchestrator orchestrator,
	log *zerolog.Logger,
) (err error) {
	var wg sync.WaitGroup
	// Metrics port is privileged, so no need for further access control
	trace.AuthRequest = func(*http.Request) (bool, bool) { return true, true }
	// TODO: parameterize ReadTimeout and WriteTimeout. The maximum time we can
	// profile CPU usage depends on WriteTimeout
	h := newMetricsHandler(readyServer, quickTunnelHostname, orchestrator, log)
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

	<-shutdownC
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
