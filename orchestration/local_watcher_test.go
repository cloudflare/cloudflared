package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/ingress"
)

func TestNewLocalConfigWatcher(t *testing.T) {
	log := zerolog.Nop()
	orchestrator := createTestOrchestrator(t)

	watcher := NewLocalConfigWatcher(orchestrator, "/tmp/config.yaml", &log)
	require.NotNil(t, watcher)
	require.Equal(t, "/tmp/config.yaml", watcher.configPath)
	require.Equal(t, int32(localConfigVersionStart), watcher.Version())
}

func TestLocalConfigWatcher_ReloadConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configContent := `
tunnel: test-tunnel-id
ingress:
  - hostname: example.com
    service: http://localhost:8080
  - service: http_status:404
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	log := zerolog.Nop()
	orchestrator := createTestOrchestrator(t)

	watcher := NewLocalConfigWatcher(orchestrator, configPath, &log)

	watcher.ReloadConfig()

	require.Equal(t, int32(localConfigVersionStart+1), watcher.Version())
}

func TestLocalConfigWatcher_ReloadConfig_InvalidYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	err := os.WriteFile(configPath, []byte("invalid: yaml: ["), 0o600)
	require.NoError(t, err)

	log := zerolog.Nop()
	orchestrator := createTestOrchestrator(t)

	watcher := NewLocalConfigWatcher(orchestrator, configPath, &log)

	watcher.ReloadConfig()

	require.Equal(t, int32(localConfigVersionStart), watcher.Version())
}

func TestLocalConfigWatcher_ReloadConfig_InvalidIngress(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Missing catch-all rule (no empty hostname at end)
	configContent := `
tunnel: test-tunnel-id
ingress:
  - hostname: example.com
    service: http://localhost:8080
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	log := zerolog.Nop()
	orchestrator := createTestOrchestrator(t)

	watcher := NewLocalConfigWatcher(orchestrator, configPath, &log)

	watcher.ReloadConfig()

	require.Equal(t, int32(localConfigVersionStart), watcher.Version())
}

func TestLocalConfigWatcher_WatcherItemDidChange(t *testing.T) {
	log := zerolog.Nop()
	orchestrator := createTestOrchestrator(t)

	watcher := NewLocalConfigWatcher(orchestrator, "/tmp/config.yaml", &log)

	watcher.WatcherItemDidChange("/tmp/config.yaml")

	select {
	case <-watcher.reloadChan:
	default:
		t.Fatal("Expected reload channel to receive signal")
	}
}

func TestLocalConfigWatcher_WatcherItemDidChange_NonBlocking(t *testing.T) {
	log := zerolog.Nop()
	orchestrator := createTestOrchestrator(t)

	watcher := NewLocalConfigWatcher(orchestrator, "/tmp/config.yaml", &log)

	watcher.reloadChan <- struct{}{}

	watcher.WatcherItemDidChange("/tmp/config.yaml")

	select {
	case <-watcher.reloadChan:
	default:
		t.Fatal("Expected reload channel to have signal")
	}
}

func TestLocalConfigWatcher_Run_ManualReload(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configContent := `
tunnel: test-tunnel-id
ingress:
  - hostname: example.com
    service: http://localhost:8080
  - service: http_status:404
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	log := zerolog.Nop()
	orchestrator := createTestOrchestrator(t)

	watcher := NewLocalConfigWatcher(orchestrator, configPath, &log)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reloadC := make(chan struct{}, 1)

	readyC := watcher.Run(ctx, reloadC)
	<-readyC // Wait until watcher is ready

	// Send reload signal
	reloadC <- struct{}{}

	// Wait for version to increment
	require.Eventually(t, func() bool {
		return watcher.Version() >= localConfigVersionStart+1
	}, 2*time.Second, 10*time.Millisecond, "version should be incremented after reload")
}

func TestLocalConfigWatcher_Run_FileChange(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configContent := `
tunnel: test-tunnel-id
ingress:
  - hostname: example.com
    service: http://localhost:8080
  - service: http_status:404
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	log := zerolog.Nop()
	orchestrator := createTestOrchestrator(t)

	watcher := NewLocalConfigWatcher(orchestrator, configPath, &log)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reloadC := make(chan struct{}, 1)

	readyC := watcher.Run(ctx, reloadC)
	<-readyC // Wait until watcher is ready

	newConfigContent := `
tunnel: test-tunnel-id
ingress:
  - hostname: new-example.com
    service: http://localhost:9090
  - service: http_status:404
`
	// Write the config file. We may need to write multiple times because fsnotify
	// may not have started watching yet. We write with increasing delays to allow
	// the debounce timer (500ms) to fire between writes.
	written := false
	for range 5 {
		err = os.WriteFile(configPath, []byte(newConfigContent), 0o600)
		require.NoError(t, err)
		written = true
		// Wait longer than debounce interval to allow reload to happen
		time.Sleep(600 * time.Millisecond)
		if watcher.Version() >= localConfigVersionStart+1 {
			break
		}
	}
	require.True(t, written, "should have written config file")
	require.GreaterOrEqual(t, watcher.Version(), int32(localConfigVersionStart+1), "version should be incremented after file change")
}

func TestLocalConfigWatcher_ConcurrentReloads(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configContent := `
tunnel: test-tunnel-id
ingress:
  - hostname: example.com
    service: http://localhost:8080
  - service: http_status:404
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	log := zerolog.Nop()
	orchestrator := createTestOrchestrator(t)

	watcher := NewLocalConfigWatcher(orchestrator, configPath, &log)

	// Run multiple concurrent reloads
	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			watcher.ReloadConfig()
		}()
	}

	wg.Wait()

	// With TryLock, concurrent reloads are skipped if one is already in progress.
	// At least one reload should succeed (version >= start+1).
	// Due to TryLock skipping, version likely won't reach start+numGoroutines.
	finalVersion := watcher.Version()
	require.GreaterOrEqual(t, finalVersion, int32(localConfigVersionStart+1),
		"At least one reload should have succeeded")
	require.LessOrEqual(t, finalVersion, int32(localConfigVersionStart+numGoroutines),
		"Version should not exceed expected reloads")
}

func TestLocalConfigWatcher_Run_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configContent := `
tunnel: test-tunnel-id
ingress:
  - service: http_status:404
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	log := zerolog.Nop()
	orchestrator := createTestOrchestrator(t)
	watcher := NewLocalConfigWatcher(orchestrator, configPath, &log)

	ctx, cancel := context.WithCancel(context.Background())
	reloadC := make(chan struct{}, 1)

	readyC := watcher.Run(ctx, reloadC)
	<-readyC

	// Cancel context and verify watcher stops without panic or hang
	cancel()
	time.Sleep(50 * time.Millisecond)
}

func createTestOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()

	log := zerolog.Nop()
	originDialer := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer: ingress.NewDialer(ingress.WarpRoutingConfig{
			ConnectTimeout: config.CustomDuration{Duration: 1 * time.Second},
			TCPKeepAlive:   config.CustomDuration{Duration: 15 * time.Second},
			MaxActiveFlows: 0,
		}),
		TCPWriteTimeout: 1 * time.Second,
	}, &log)

	initConfig := &Config{
		Ingress:             &ingress.Ingress{},
		OriginDialerService: originDialer,
	}

	orchestrator, err := NewOrchestrator(t.Context(), initConfig, nil, []ingress.Rule{}, &log)
	require.NoError(t, err)

	return orchestrator
}
