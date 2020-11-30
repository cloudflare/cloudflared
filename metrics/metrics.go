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

	"golang.org/x/net/trace"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	shutdownTimeout = time.Second * 15
	startupTime     = time.Millisecond * 500
)

func newMetricsHandler(connectionEvents <-chan connection.Event, log logger.Service) *http.ServeMux {
	readyServer := NewReadyServer(connectionEvents, log)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthcheck", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "OK\n")
	})
	mux.Handle("/ready", readyServer)
	return mux
}

func ServeMetrics(
	l net.Listener,
	shutdownC <-chan struct{},
	connectionEvents <-chan connection.Event,
	logger logger.Service,
) (err error) {
	var wg sync.WaitGroup
	// Metrics port is privileged, so no need for further access control
	trace.AuthRequest = func(*http.Request) (bool, bool) { return true, true }
	// TODO: parameterize ReadTimeout and WriteTimeout. The maximum time we can
	// profile CPU usage depends on WriteTimeout
	h := newMetricsHandler(connectionEvents, logger)
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
	logger.Infof("Starting metrics server on %s", fmt.Sprintf("%v/metrics", l.Addr()))
	// server.Serve will hang if server.Shutdown is called before the server is
	// fully started up. So add artificial delay.
	time.Sleep(startupTime)

	<-shutdownC
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	server.Shutdown(ctx)
	cancel()

	wg.Wait()
	if err == http.ErrServerClosed {
		logger.Info("Metrics server stopped")
		return nil
	}
	logger.Errorf("Metrics server quit with error: %s", err)
	return err
}

func RegisterBuildInfo(buildTime string, version string) {
	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			// Don't namespace build_info, since we want it to be consistent across all Cloudflare services
			Name: "build_info",
			Help: "Build and version information",
		},
		[]string{"goversion", "revision", "version"},
	)
	prometheus.MustRegister(buildInfo)
	buildInfo.WithLabelValues(runtime.Version(), buildTime, version).Set(1)
}
