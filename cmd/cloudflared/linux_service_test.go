//go:build linux

package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"os"
	"path/filepath"
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

func TestInstallLinuxServiceWithCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		withoutToken bool
		autoUpdate   bool
		failTemplate string
		failCommand  string
	}{
		{name: "success with auto-update", autoUpdate: true},
		{name: "success without auto-update"},
		{name: "main service template failure", failTemplate: cloudflaredService},
		{name: "update service template failure", autoUpdate: true, failTemplate: cloudflaredUpdateService},
		{name: "update timer template failure", autoUpdate: true, failTemplate: cloudflaredUpdateTimer},
		{name: "enable service failure", failCommand: "systemctl enable cloudflared.service"},
		{name: "enable timer failure", autoUpdate: true, failCommand: "systemctl enable cloudflared-update.timer"},
		{name: "start timer failure", autoUpdate: true, failCommand: "systemctl start cloudflared-update.timer"},
		{name: "daemon reload failure", failCommand: "systemctl daemon-reload"},
		{name: "service startup failure", failCommand: "systemctl start cloudflared.service"},
		{name: "service startup failure with auto-update", autoUpdate: true, failCommand: "systemctl start cloudflared.service"},
		{name: "config based installation", withoutToken: true},
		{name: "config based startup failure", withoutToken: true, failCommand: "systemctl start cloudflared.service"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			configDir := filepath.Join(t.TempDir(), "config")
			templates := systemdTemplatesForTest(t)
			if tt.failTemplate != "" {
				template := templates[tt.failTemplate]
				template.Content = "{{"
				templates[tt.failTemplate] = template
			}
			token := base64.StdEncoding.EncodeToString([]byte(`{"a":"test-account","s":"dGVzdA==","t":"00000000-0000-0000-0000-000000000001"}`))
			args := []string{token}
			templateArgs := ServiceTemplateArgs{Path: "/usr/bin/cloudflared", ExtraArgs: buildArgsForTokenFile(configDir)}
			if tt.withoutToken {
				args = nil
				templateArgs.ExtraArgs = []string{"--config", filepath.Join(configDir, serviceConfigFile), "tunnel", "run"}
			}
			c := serviceInstallContext(t, args...)
			recorder := &commandRecorder{failOn: tt.failCommand}
			log := zerolog.Nop()
			install := func() error {
				if !tt.withoutToken {
					contents, err := os.ReadFile(tokenPath(configDir))
					require.NoError(t, err)
					require.Equal(t, token, string(contents), "the token must be written before installation starts")
				}
				return installSystemdWithTemplates(&templateArgs, tt.autoUpdate, templates, recorder.run, &log)
			}
			err := installLinuxServiceWithCredentials(c, configDir, templates[cloudflaredService], install, &log)
			switch {
			case tt.failTemplate != "":
				require.ErrorContains(t, err, "template")
			case tt.failCommand != "":
				require.EqualError(t, err, "command failed")
			default:
				require.NoError(t, err)
			}

			if tt.failTemplate == cloudflaredService {
				assert.NoFileExists(t, templates[cloudflaredService].Path)
				assert.NoFileExists(t, tokenPath(configDir))
			} else {
				unit, err := os.ReadFile(templates[cloudflaredService].Path)
				require.NoError(t, err)
				assert.Contains(t, string(unit), strings.Join(templateArgs.ExtraArgs, " "))
				assert.NotContains(t, string(unit), token, "credentials must not be embedded in the unit")
				if !tt.withoutToken {
					contents, err := os.ReadFile(tokenPath(configDir))
					require.NoError(t, err, "an installed service must retain its token even when installation returns an error")
					assert.Equal(t, token, string(contents))
					info, err := os.Stat(tokenPath(configDir))
					require.NoError(t, err)
					assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
				}
			}
			if tt.withoutToken {
				assert.NoDirExists(t, configDir, "config based installs must not create a token directory")
			}

			var wantCommands []string
			if tt.failTemplate == "" {
				wantCommands = []string{"systemctl enable cloudflared.service"}
				if tt.autoUpdate {
					wantCommands = append(wantCommands, "systemctl enable cloudflared-update.timer", "systemctl start cloudflared-update.timer")
				}
				wantCommands = append(wantCommands, "systemctl daemon-reload", "systemctl start cloudflared.service")
				for i, command := range wantCommands {
					if command == tt.failCommand {
						wantCommands = wantCommands[:i+1]
						break
					}
				}
			}
			assert.Equal(t, wantCommands, recorder.commands, "failures must propagate without continuing or rolling back the installed service")
		})
	}
}

func TestInstallLinuxServicePreservesExistingServiceToken(t *testing.T) {
	t.Parallel()

	for _, symlink := range []bool{false, true} {
		name := "existing service"
		if symlink {
			name = "dangling service symlink"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			configDir := t.TempDir()
			const originalToken = "existing-token"
			require.NoError(t, os.WriteFile(tokenPath(configDir), []byte(originalToken), 0o600))
			serviceTemplate := ServiceTemplate{Path: filepath.Join(t.TempDir(), cloudflaredService)}
			if symlink {
				require.NoError(t, os.Symlink(serviceTemplate.Path+".missing", serviceTemplate.Path))
			} else {
				require.NoError(t, os.WriteFile(serviceTemplate.Path, []byte("existing unit"), 0o600))
			}
			for _, token := range []string{"invalid-token", base64.StdEncoding.EncodeToString([]byte(`{"a":"replacement"}`))} {
				log := zerolog.Nop()
				install := func() error {
					t.Error("an existing service must be rejected before installation")
					return errors.New("already installed")
				}
				err := installLinuxServiceWithCredentials(serviceInstallContext(t, token), configDir, serviceTemplate, install, &log)
				require.ErrorContains(t, err, "already installed")
				contents, err := os.ReadFile(tokenPath(configDir))
				require.NoError(t, err)
				assert.Equal(t, originalToken, string(contents))
			}
		})
	}
}

func TestInstallLinuxServiceTokenWriteFailure(t *testing.T) {
	t.Parallel()

	for _, existingToken := range []bool{false, true} {
		name := "new token"
		if existingToken {
			name = "existing token"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			configDir := t.TempDir()
			if existingToken {
				require.NoError(t, os.WriteFile(tokenPath(configDir), []byte("existing-token"), 0o600))
			}
			serviceTemplate := ServiceTemplate{Path: filepath.Join(t.TempDir(), cloudflaredService)}
			log := zerolog.Nop()
			install := func() error {
				t.Error("invalid credentials must prevent installation")
				return nil
			}
			err := installLinuxServiceWithCredentials(serviceInstallContext(t, "invalid-token"), configDir, serviceTemplate, install, &log)
			require.ErrorContains(t, err, "Provided tunnel token is not valid")
			assert.NoFileExists(t, serviceTemplate.Path)
			if existingToken {
				contents, err := os.ReadFile(tokenPath(configDir))
				require.NoError(t, err)
				assert.Equal(t, "existing-token", string(contents))
			} else {
				assert.NoFileExists(t, tokenPath(configDir))
			}
		})
	}
}

func TestInstallLinuxServiceRejectsExistingToken(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"different token", "identical token", "symlink", "dangling symlink"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			configDir := t.TempDir()
			serviceTemplate := ServiceTemplate{Path: filepath.Join(t.TempDir(), cloudflaredService)}
			tokenA := base64.StdEncoding.EncodeToString([]byte(`{"a":"original-account","s":"dGVzdA==","t":"00000000-0000-0000-0000-000000000001"}`))
			tokenB := base64.StdEncoding.EncodeToString([]byte(`{"a":"replacement-account","s":"dGVzdA==","t":"00000000-0000-0000-0000-000000000002"}`))
			if name == "identical token" {
				tokenB = tokenA
			}
			path := tokenPath(configDir)
			symlink := name == "symlink" || name == "dangling symlink"
			if symlink {
				path = filepath.Join(t.TempDir(), "original-token")
				require.NoError(t, os.Symlink(path, tokenPath(configDir)))
			}
			if name != "dangling symlink" {
				require.NoError(t, os.WriteFile(path, []byte(tokenA), 0o600))
				// A rejected installation must not even change existing permissions.
				require.NoError(t, os.Chmod(path, 0o400))
			}
			install := func() error {
				t.Error("existing credentials must be rejected before installation")
				return errors.New("installation failed")
			}
			log := zerolog.Nop()
			err := installLinuxServiceWithCredentials(serviceInstallContext(t, tokenB), configDir, serviceTemplate, install, &log)
			require.ErrorIs(t, err, os.ErrExist)
			assert.NoFileExists(t, serviceTemplate.Path)
			if name == "dangling symlink" {
				assert.NoFileExists(t, path)
			} else {
				contents, err := os.ReadFile(path) //nolint:gosec // path refers to a test-owned file.
				require.NoError(t, err)
				assert.Equal(t, tokenA, string(contents))
				info, err := os.Stat(path)
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o400), info.Mode().Perm())
			}
			if symlink {
				target, err := os.Readlink(tokenPath(configDir))
				require.NoError(t, err)
				assert.Equal(t, path, target)
			}
		})
	}
}

func TestInstallLinuxServiceFailedInstallCleanup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		partialUnit   bool
		unreadableDir bool
		withoutToken  bool
		wantToken     bool
		template      ServiceTemplate
	}{
		{name: "new token removed before service creation"},
		{name: "partial service file retained", partialUnit: true, wantToken: true},
		{name: "unknown service state retains token", unreadableDir: true, wantToken: true},
		{name: "config based failure leaves token alone", withoutToken: true, wantToken: true},
		{name: "failure after SysV service creation", template: sysvTemplate, wantToken: true},
		{name: "failure after OpenRC service creation", template: openrcTemplate, wantToken: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			configDir := t.TempDir()
			unitDir := filepath.Join(t.TempDir(), "units")
			require.NoError(t, os.Mkdir(unitDir, 0o700))
			serviceTemplate := tt.template
			serviceTemplate.Path = filepath.Join(unitDir, "cloudflared")
			token := base64.StdEncoding.EncodeToString([]byte(`{"a":"test-account"}`))
			if tt.withoutToken {
				require.NoError(t, os.WriteFile(tokenPath(configDir), []byte(token), 0o600))
			}
			args := []string{token}
			if tt.withoutToken {
				args = nil
			}
			installErr := errors.New("installation failed")
			install := func() error {
				if serviceTemplate.Content != "" {
					require.NoError(t, serviceTemplate.Generate(&ServiceTemplateArgs{
						Path: "/usr/bin/cloudflared", ExtraArgs: buildArgsForTokenFile(configDir),
					}))
				}
				if tt.partialUnit {
					require.NoError(t, os.WriteFile(serviceTemplate.Path, []byte("partial unit"), 0o600))
				}
				if tt.unreadableDir {
					// A symlink loop makes Lstat fail even when tests run as root.
					require.NoError(t, os.Remove(unitDir))
					require.NoError(t, os.Symlink(unitDir, unitDir))
				}
				return installErr
			}
			log := zerolog.Nop()
			err := installLinuxServiceWithCredentials(serviceInstallContext(t, args...), configDir, serviceTemplate, install, &log)
			require.ErrorIs(t, err, installErr)
			if tt.unreadableDir {
				require.ErrorContains(t, err, "before token cleanup")
			}
			if tt.wantToken {
				contents, err := os.ReadFile(tokenPath(configDir))
				require.NoError(t, err)
				assert.Equal(t, token, string(contents))
			} else {
				assert.NoFileExists(t, tokenPath(configDir))
			}
		})
	}
}

func TestInstallLinuxServicePathInspectionFailure(t *testing.T) {
	t.Parallel()

	for _, inspectToken := range []bool{false, true} {
		name := "service path"
		if inspectToken {
			name = "token path"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			loop := filepath.Join(root, "loop")
			require.NoError(t, os.Symlink(loop, loop))
			configDir := filepath.Join(root, "config")
			servicePath := filepath.Join(loop, cloudflaredService)
			if inspectToken {
				configDir = loop
				servicePath = filepath.Join(root, cloudflaredService)
			}
			install := func() error {
				t.Error("an uninspectable path must prevent installation")
				return nil
			}
			log := zerolog.Nop()
			token := base64.StdEncoding.EncodeToString([]byte(`{"a":"test-account"}`))
			err := installLinuxServiceWithCredentials(serviceInstallContext(t, token), configDir, ServiceTemplate{Path: servicePath}, install, &log)
			if inspectToken {
				require.ErrorContains(t, err, "create service token file")
			} else {
				require.ErrorContains(t, err, "check existing service")
			}
			assert.NoDirExists(t, filepath.Join(root, "config"))
		})
	}
}

func serviceInstallContext(t *testing.T, args ...string) *cli.Context {
	t.Helper()
	flags := flag.NewFlagSet("service install", flag.ContinueOnError)
	require.NoError(t, flags.Parse(args))
	return cli.NewContext(cli.NewApp(), flags, nil)
}

func systemdTemplatesForTest(t *testing.T) map[string]ServiceTemplate {
	t.Helper()
	templates := make(map[string]ServiceTemplate, len(systemdAllTemplates))
	unitDir := t.TempDir()
	for name, template := range systemdAllTemplates {
		template.Path = filepath.Join(unitDir, name)
		templates[name] = template
	}
	return templates
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
