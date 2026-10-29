//go:build linux

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
