package orchestration

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/watcher"
)

const (
	// debounceInterval is the time to wait after a file change before reloading.
	// This prevents multiple rapid reloads when editors save files multiple times.
	debounceInterval = 500 * time.Millisecond

	// pollInterval is the interval for polling file changes as a fallback.
	// This handles cases where fsnotify stops working (e.g., file replaced via
	// symlink rotation, Kubernetes ConfigMap updates).
	pollInterval = 30 * time.Second

	// localConfigVersionStart is the starting version for local config updates.
	// Local config uses high positive versions (1_000_000+) to avoid conflicts with
	// remote config versions (0, 1, 2, ...). At typical change rates (<100/day),
	// collision would require decades of continuous operation.
	localConfigVersionStart int32 = 1_000_000

	// maxReloadRetries limits consecutive reloads when config keeps changing.
	// This prevents infinite loops if the file is constantly being modified.
	maxReloadRetries = 3
)

// LocalConfigWatcher watches a local configuration file for changes and updates
// the Orchestrator when changes are detected. It supports both automatic file
// watching via fsnotify and manual reload via SIGHUP signal.
//
// The watcher uses a hybrid approach: fsnotify for immediate notifications plus
// periodic polling as a fallback. This ensures config changes are detected even
// when fsnotify fails (e.g., file replaced via symlink, Kubernetes ConfigMap).
type LocalConfigWatcher struct {
	orchestrator *Orchestrator
	configPath   string
	log          *zerolog.Logger

	// mu protects version, lastModTime and serializes reload operations
	mu          sync.Mutex
	version     int32
	lastModTime time.Time

	reloadChan chan struct{}
}

// NewLocalConfigWatcher creates a new LocalConfigWatcher.
// Panics if orchestrator is nil (programming error, not recoverable).
func NewLocalConfigWatcher(
	orchestrator *Orchestrator,
	configPath string,
	log *zerolog.Logger,
) *LocalConfigWatcher {
	if orchestrator == nil {
		panic("orchestrator cannot be nil")
	}
	return &LocalConfigWatcher{
		orchestrator: orchestrator,
		configPath:   configPath,
		log:          log,
		version:      localConfigVersionStart,
		reloadChan:   make(chan struct{}, 1),
	}
}

// Run starts the config watcher. It watches for file changes and listens
// for manual reload signals on reloadC.
//
// Returns a channel that is closed when the watcher is ready to receive signals.
// Callers should wait on this channel before starting the signal handler to avoid
// race conditions where signals arrive before the watcher is listening.
func (w *LocalConfigWatcher) Run(ctx context.Context, reloadC <-chan struct{}) <-chan struct{} {
	readyC := make(chan struct{})

	fileWatcher, err := watcher.NewFile()
	if err != nil {
		w.log.Warn().Err(err).Msg("Failed to create file watcher, falling back to SIGHUP only")
		go func() {
			w.log.Info().Str("config", w.configPath).Msg("Configuration reload available via SIGHUP signal")
			close(readyC)
			w.runWithoutFileWatcher(ctx, reloadC)
		}()
		return readyC
	}

	if err := fileWatcher.Add(w.configPath); err != nil {
		w.log.Warn().Err(err).Str("config", w.configPath).Msg("Failed to watch config file, falling back to SIGHUP only")
		go func() {
			w.log.Info().Str("config", w.configPath).Msg("Configuration reload available via SIGHUP signal")
			close(readyC)
			w.runWithoutFileWatcher(ctx, reloadC)
		}()
		return readyC
	}

	w.log.Info().Str("config", w.configPath).Msg("Started watching configuration file for changes")

	go fileWatcher.Start(w)

	// Initialize lastModTime before signaling ready to avoid race with early SIGHUP
	w.initLastModTime()

	go func() {
		close(readyC)
		w.runLoop(ctx, reloadC, fileWatcher)
	}()

	return readyC
}

// runWithoutFileWatcher runs the watcher loop without file watching.
// Only manual SIGHUP reloads will work.
func (w *LocalConfigWatcher) runWithoutFileWatcher(ctx context.Context, reloadC <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-reloadC:
			w.doReload()
		}
	}
}

// runLoop is the main event loop that handles file changes and reload signals.
func (w *LocalConfigWatcher) runLoop(ctx context.Context, reloadC <-chan struct{}, fileWatcher *watcher.File) {
	// Use a stopped timer initially; we'll reset it when file changes occur
	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}
	debounceActive := false

	// Poll timer as fallback for when fsnotify misses changes
	pollTicker := time.NewTicker(pollInterval)

	defer func() {
		debounceTimer.Stop()
		pollTicker.Stop()
		fileWatcher.Shutdown()
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case <-reloadC:
			w.log.Info().Msg("Received reload signal")
			w.doReload()

		case <-w.reloadChan:
			// Stop existing timer and drain if necessary.
			// If Stop() returns false, timer already expired and channel has value.
			if !debounceTimer.Stop() && debounceActive {
				<-debounceTimer.C
			}
			debounceTimer.Reset(debounceInterval)
			debounceActive = true

		case <-debounceTimer.C:
			debounceActive = false
			w.doReload()

		case <-pollTicker.C:
			// Fallback polling for when fsnotify misses changes (e.g., symlink rotation)
			if w.checkFileChanged() {
				w.log.Debug().Msg("Poll detected config file change")
				w.doReload()
			}
		}
	}
}

// initLastModTime initializes the lastModTime field from the current file state.
func (w *LocalConfigWatcher) initLastModTime() {
	info, err := os.Stat(w.configPath)
	if err != nil {
		return
	}
	w.mu.Lock()
	w.lastModTime = info.ModTime()
	w.mu.Unlock()
}

// checkFileChanged checks if the config file has been modified since last check.
// Returns true if the file changed, false otherwise.
func (w *LocalConfigWatcher) checkFileChanged() bool {
	info, err := os.Stat(w.configPath)
	if err != nil {
		return false
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	modTime := info.ModTime()
	if modTime.After(w.lastModTime) {
		w.lastModTime = modTime
		return true
	}
	return false
}

// getModTime returns the modification time of the config file.
// Returns zero time if file cannot be stat'd.
// Note: No lock needed - this reads from disk, not from struct fields.
// The lastModTime field is protected by mu where it's accessed.
func (w *LocalConfigWatcher) getModTime() time.Time {
	info, err := os.Stat(w.configPath)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// WatcherItemDidChange implements watcher.Notification interface.
// Called when the config file is modified.
func (w *LocalConfigWatcher) WatcherItemDidChange(filepath string) {
	w.log.Debug().Str("file", filepath).Msg("Config file changed, scheduling reload")
	select {
	case w.reloadChan <- struct{}{}:
	default:
	}
}

// WatcherDidError implements watcher.Notification interface.
// Called when the file watcher encounters an error.
//
// Note: If the config file is deleted and recreated (e.g., during deployment via symlink
// rotation), the file watcher may stop working. In this case, SIGHUP can still be used
// for manual reloads, or cloudflared can be restarted.
func (w *LocalConfigWatcher) WatcherDidError(err error) {
	if os.IsNotExist(err) {
		w.log.Warn().Str("config", w.configPath).
			Msg("Config file was deleted or moved, keeping current configuration")
	} else {
		w.log.Error().Err(err).Str("config", w.configPath).
			Msg("Config file watcher error, keeping current configuration")
	}
}

// doReload performs the actual configuration reload.
// Uses TryLock to skip if another reload is already in progress.
// If the config file changes during reload, it will retry up to maxReloadRetries times.
func (w *LocalConfigWatcher) doReload() {
	if !w.mu.TryLock() {
		w.log.Info().Msg("Reload already in progress, skipping")
		return
	}
	defer w.mu.Unlock()

	for i := range maxReloadRetries {
		startModTime := w.getModTime()

		cfg, err := ReadLocalConfig(w.configPath)
		if err != nil {
			w.log.Error().Err(err).Str("config", w.configPath).
				Msg("Failed to read config file, keeping current configuration")
			return
		}

		configJSON, err := ConvertAndValidateLocalConfig(cfg)
		if err != nil {
			w.log.Error().Err(err).Msg("Invalid configuration, keeping current configuration")
			return
		}

		nextVersion := w.version + 1
		resp := w.orchestrator.UpdateConfig(nextVersion, configJSON)

		if resp.Err != nil {
			w.log.Error().Err(resp.Err).Int32("version", nextVersion).
				Msg("Orchestrator rejected configuration update")
			return
		}

		w.version = resp.LastAppliedVersion

		// Get mtime once to avoid TOCTOU race
		currentModTime := w.getModTime()
		w.lastModTime = currentModTime

		w.log.Info().Int32("version", resp.LastAppliedVersion).
			Msg("Configuration reloaded successfully")

		// Check if file changed during reload (using same mtime value)
		if !currentModTime.After(startModTime) {
			return // No changes during reload, done
		}

		if i < maxReloadRetries-1 {
			w.log.Debug().Msg("Config file changed during reload, reloading again")
		}
	}

	w.log.Warn().Int("retries", maxReloadRetries).
		Msg("Config file keeps changing, giving up after max retries")
}

// ReloadConfig triggers a manual configuration reload.
// This is useful for programmatic reloads without SIGHUP.
func (w *LocalConfigWatcher) ReloadConfig() {
	w.doReload()
}

// Version returns the current config version (thread-safe).
func (w *LocalConfigWatcher) Version() int32 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.version
}
