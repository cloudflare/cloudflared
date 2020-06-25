package buildinfo

import (
	"runtime"

	"github.com/cloudflare/cloudflared/logger"
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

func (bi *BuildInfo) Log(logger logger.Service) {
	logger.Infof("Version %s", bi.CloudflaredVersion)
	logger.Infof("GOOS: %s, GOVersion: %s, GoArch: %s", bi.GoOS, bi.GoVersion, bi.GoArch)
}
