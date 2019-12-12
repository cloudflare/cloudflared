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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

const (
	shutdownTimeout = time.Second * 15
	startupTime     = time.Millisecond * 500
)

func ServeMetrics(l net.Listener, shutdownC <-chan struct{}, logger *logrus.Logger) (err error) {
	var wg sync.WaitGroup
	// Metrics port is privileged, so no need for further access control
	trace.AuthRequest = func(*http.Request) (bool, bool) { return true, true }
	// TODO: parameterize ReadTimeout and WriteTimeout. The maximum time we can
	// profile CPU usage depends on WriteTimeout
	server := &http.Server{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/healthcheck", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "OK\n")
	}))

	wg.Add(1)
	go func() {
		defer wg.Done()
		err = server.Serve(l)
	}()
	logger.WithField("addr", l.Addr()).Info("Starting metrics server")
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
	logger.WithError(err).Error("Metrics server quit with error")
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
