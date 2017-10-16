package metrics

import (
	"net"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"time"

	"golang.org/x/net/context"

	log "github.com/Sirupsen/logrus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func ServeMetrics(l net.Listener, shutdownC <-chan struct{}) error {
	server := &http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	go func() {
		<-shutdownC
		server.Shutdown(context.Background())
	}()
	http.Handle("/metrics", promhttp.Handler())
	log.WithField("addr", l.Addr()).Info("Starting metrics server")
	err := server.Serve(l)
	if err == http.ErrServerClosed {
		log.Info("Metrics server stopped")
		return nil
	}
	log.WithError(err).Error("Metrics server quit with error")
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
