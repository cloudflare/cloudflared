//go:build windows

package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc/eventlog"
)

func TestInstallEventLogSourceReplacesStaleSource(t *testing.T) {
	// Not parallel: the test replaces package-level eventlog seams.
	origInstall, origRemove := installEventSource, removeEventSource
	t.Cleanup(func() {
		installEventSource, removeEventSource = origInstall, origRemove
	})

	// A source left behind by an earlier install whose uninstall did not finish.
	registered := true
	installEventSource = func(src string, _ uint32) error {
		if registered {
			return errors.New(src + " registry key already exists")
		}
		registered = true
		return nil
	}
	removeEventSource = func(string) error {
		registered = false
		return nil
	}

	require.NoError(t, installEventLogSource(windowsServiceName))
	require.True(t, registered)
}

func TestInstallEventLogSourceIgnoresMissingSource(t *testing.T) {
	// Not parallel: the test replaces package-level eventlog seams.
	origInstall, origRemove := installEventSource, removeEventSource
	t.Cleanup(func() {
		installEventSource, removeEventSource = origInstall, origRemove
	})

	var gotEvents uint32
	installEventSource = func(_ string, events uint32) error {
		gotEvents = events
		return nil
	}
	removeEventSource = func(string) error {
		return registry.ErrNotExist
	}

	require.NoError(t, installEventLogSource(windowsServiceName))
	require.Equal(t, uint32(eventlog.Error|eventlog.Warning|eventlog.Info), gotEvents)
}
