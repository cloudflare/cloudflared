package buildinfo

import (
	"fmt"
	"runtime"

	"github.com/rs/zerolog"
)

type BuildInfo struct {
	GoOS               string `json:"go_os"`
	GoVersion          string `json:"go_version"`
	GoArch             string `json:"go_arch"`
	CloudflaredVersion string `json:"cloudflared_version"`
}

func GetBuildInfo(cloudflaredVersion string) *BuildInfo {
	return &BuildInfo{
		GoOS:               runtime.GOOS,
		GoVersion:          runtime.Version(),
		GoArch:             runtime.GOARCH,
		CloudflaredVersion: cloudflaredVersion,
	}
}

func (bi *BuildInfo) Log(log *zerolog.Logger) {
	log.Info().Msgf("Version %s", bi.CloudflaredVersion)
	log.Info().Msgf("GOOS: %s, GOVersion: %s, GoArch: %s", bi.GoOS, bi.GoVersion, bi.GoArch)
}

func (bi *BuildInfo) OSArch() string {
	return fmt.Sprintf("%s_%s", bi.GoOS, bi.GoArch)
}
