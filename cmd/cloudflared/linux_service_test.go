//go:build linux

package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

// commandRecorder records the commands it is asked to run and fails the one
// equal to failOn, if set.
type commandRecorder struct {
	commands []string
	failOn   string
}

func (r *commandRecorder) run(command string, args ...string) error {
	cmd := strings.Join(append([]string{command}, args...), " ")
	r.commands = append(r.commands, cmd)
	if cmd == r.failOn {
		return errors.New("command failed")
	}
	return nil
}

func TestUpdateTimerHasRandomizedDelay(t *testing.T) {
	t.Parallel()

	_, timerSection, found := strings.Cut(systemdAllTemplates[cloudflaredUpdateTimer].Content, "[Timer]\n")
	require.True(t, found, "update timer has no [Timer] section")
	timerSection, _, _ = strings.Cut(timerSection, "\n[")
	assert.Regexp(t, `(?m)^RandomizedDelaySec=\S+$`, timerSection)
}

func TestEnableSystemdUnits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		autoUpdate bool
		failOn     string
		wantErr    bool
		want       []string
	}{
		{
			name:       "auto-update enables and starts the timer",
			autoUpdate: true,
			want: []string{
				"systemctl enable cloudflared.service",
				"systemctl enable cloudflared-update.timer",
				"systemctl start cloudflared-update.timer",
				"systemctl daemon-reload",
				"systemctl start cloudflared.service",
			},
		},
		{
			name:       "no auto-update leaves the timer alone",
			autoUpdate: false,
			want: []string{
				"systemctl enable cloudflared.service",
				"systemctl daemon-reload",
				"systemctl start cloudflared.service",
			},
		},
		{
			name:       "failing to enable the timer stops the install",
			autoUpdate: true,
			failOn:     "systemctl enable cloudflared-update.timer",
			wantErr:    true,
			want: []string{
				"systemctl enable cloudflared.service",
				"systemctl enable cloudflared-update.timer",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			recorder := &commandRecorder{failOn: tt.failOn}
			log := zerolog.Nop()
			err := enableSystemdUnits(recorder.run, tt.autoUpdate, &log)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, recorder.commands)
		})
	}
}

// TestInstallLinuxServiceWithToken checks that a token install leaves the
// token file in place whatever the install outcome. The service definition
// points at the token file, so deleting it on failure leaves an enabled
// service restarting against a missing file.
func TestInstallLinuxServiceWithToken(t *testing.T) {
	t.Parallel()

	// A token that satisfies tunnel.ParseToken: base64-encoded JSON with an
	// account tag, a secret, and a tunnel ID.
	token := base64.StdEncoding.EncodeToString([]byte(
		`{"a":"test-account","s":"dGVzdC1zZWNyZXQ=","t":"11111111-1111-1111-1111-111111111111"}`))

	tests := []struct {
		name       string
		installErr error
	}{
		{
			name:       "generating the service definition fails",
			installErr: errors.New("cloudflared service is already installed"),
		},
		{
			name:       "starting the service fails",
			installErr: errors.New("systemctl start cloudflared.service failed"),
		},
		{
			name: "install succeeds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			configDir := t.TempDir()
			log := zerolog.Nop()

			flagSet := flag.NewFlagSet("service install", flag.ContinueOnError)
			flagSet.Bool(noUpdateServiceFlag.Name, false, "")
			require.NoError(t, flagSet.Parse([]string{token}))
			ctx := cli.NewContext(cli.NewApp(), flagSet, nil)

			var gotArgs []string
			install := func(templateArgs *ServiceTemplateArgs, autoUpdate bool, log *zerolog.Logger) error {
				gotArgs = templateArgs.ExtraArgs
				return tt.installErr
			}

			err := installLinuxServiceWith(ctx, &log, configDir, install)
			if tt.installErr != nil {
				require.ErrorIs(t, err, tt.installErr)
			} else {
				require.NoError(t, err)
			}

			content, readErr := os.ReadFile(tokenPath(configDir))
			require.NoError(t, readErr, "token file must survive the install outcome")
			assert.Equal(t, token, string(content))
			assert.Equal(t, buildArgsForTokenFile(configDir), gotArgs)
		})
	}
}

// TestRemoveTokenFile checks the uninstall path still removes the token
// file, and that removing an already-missing file is not an error.
func TestRemoveTokenFile(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	log := zerolog.Nop()
	require.NoError(t, os.WriteFile(tokenPath(configDir), []byte("token"), 0o600))

	removeTokenFile(configDir, &log)
	_, err := os.Stat(tokenPath(configDir))
	require.ErrorIs(t, err, os.ErrNotExist)

	removeTokenFile(configDir, &log)
}

func TestDisableSystemdUnits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		installed []string
		want      []string
	}{
		{
			name:      "service and timer",
			installed: []string{cloudflaredService, cloudflaredUpdateService, cloudflaredUpdateTimer},
			want: []string{
				"systemctl disable cloudflared.service",
				"systemctl stop cloudflared.service",
				"systemctl disable cloudflared-update.timer",
				"systemctl stop cloudflared-update.timer",
			},
		},
		{
			name:      "service installed without the update timer",
			installed: []string{cloudflaredService},
			want: []string{
				"systemctl disable cloudflared.service",
				"systemctl stop cloudflared.service",
			},
		},
		{
			name:      "nothing installed",
			installed: nil,
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			installed := make(map[string]ServiceTemplate)
			for _, name := range tt.installed {
				installed[name] = systemdAllTemplates[name]
			}
			recorder := &commandRecorder{}
			log := zerolog.Nop()
			require.NoError(t, disableSystemdUnits(recorder.run, installed, &log))
			assert.Equal(t, tt.want, recorder.commands)
		})
	}
}
