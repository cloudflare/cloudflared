package k8s

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// ServiceChangeHandler is called whenever the set of discovered services changes.
type ServiceChangeHandler func(services []ServiceInfo)

// Watcher periodically polls the Kubernetes API for service changes and
// notifies registered handlers.
type Watcher struct {
	cfg     *Config
	log     *zerolog.Logger
	handler ServiceChangeHandler

	mu       sync.Mutex
	services []ServiceInfo

	stopOnce sync.Once
	stopC    chan struct{}
}

// NewWatcher creates a Watcher that will poll the Kubernetes API at the
// configured resync interval.
func NewWatcher(cfg *Config, log *zerolog.Logger, handler ServiceChangeHandler) *Watcher {
	if cfg.ResyncPeriod == 0 {
		cfg.ResyncPeriod = DefaultResyncPeriod
	}
	return &Watcher{
		cfg:     cfg,
		log:     log,
		handler: handler,
		stopC:   make(chan struct{}),
	}
}

// Run starts the watch loop. It blocks until ctx is cancelled or Stop is called.
func (w *Watcher) Run(ctx context.Context) {
	w.log.Info().
		Str("namespace", w.cfg.Namespace).
		Str("baseDomain", w.cfg.BaseDomain).
		Dur("resyncPeriod", w.cfg.ResyncPeriod).
		Msg("Starting Kubernetes service watcher")

	// Initial sync
	w.sync(ctx)

	ticker := time.NewTicker(w.cfg.ResyncPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info().Msg("Kubernetes service watcher stopped (context cancelled)")
			return
		case <-w.stopC:
			w.log.Info().Msg("Kubernetes service watcher stopped")
			return
		case <-ticker.C:
			w.sync(ctx)
		}
	}
}

// Stop signals the watcher to stop.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopC)
	})
}

// Services returns a snapshot of the currently discovered services.
func (w *Watcher) Services() []ServiceInfo {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]ServiceInfo, len(w.services))
	copy(out, w.services)
	return out
}

// sync performs one discovery cycle.
func (w *Watcher) sync(ctx context.Context) {
	services, err := DiscoverServices(ctx, w.cfg, w.log)
	if err != nil {
		w.log.Err(err).Msg("Failed to discover Kubernetes services")
		return
	}

	w.mu.Lock()
	changed := !servicesEqual(w.services, services)
	w.services = services
	w.mu.Unlock()

	w.log.Info().Int("count", len(services)).Bool("changed", changed).Msg("Kubernetes service sync complete")

	if changed && w.handler != nil {
		w.handler(services)
	}
}

// servicesEqual performs a simple equality check on two ServiceInfo slices.
func servicesEqual(a, b []ServiceInfo) bool {
	if len(a) != len(b) {
		return false
	}
	// Build a set from a, check against b.
	set := make(map[string]struct{}, len(a))
	for _, s := range a {
		set[s.key()] = struct{}{}
	}
	for _, s := range b {
		if _, ok := set[s.key()]; !ok {
			return false
		}
	}
	return true
}

// key returns a stable string representation for comparison.
func (s *ServiceInfo) key() string {
	return s.Namespace + "/" + s.Name + ":" + s.OriginURL() + "@" + s.Hostname + "#" + s.Path
}
