package origin

import (
	"runtime"
)

type BuildInfo struct {
	GoOS               string `json:"go_os"`
	GoVersion          string `json:"go_version"`
	GoArch             string `json:"go_arch"`
}

func GetBuildInfo() *BuildInfo {
	return &BuildInfo{
		GoOS:      runtime.GOOS,
		GoVersion: runtime.Version(),
		GoArch:    runtime.GOARCH,
	}
}
