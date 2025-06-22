package cliutil

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/rs/zerolog"
)

type BuildInfo struct {
	GoOS               string `json:"go_os"`
	GoVersion          string `json:"go_version"`
	GoArch             string `json:"go_arch"`
	BuildType          string `json:"build_type"`
	CloudflaredVersion string `json:"cloudflared_version"`
	Checksum           string `json:"checksum"`
}

func GetBuildInfo(buildType, version string) *BuildInfo {
	return &BuildInfo{
		GoOS:               runtime.GOOS,
		GoVersion:          runtime.Version(),
		GoArch:             runtime.GOARCH,
		BuildType:          buildType,
		CloudflaredVersion: version,
		Checksum:           currentBinaryChecksum(),
	}
}

func (bi *BuildInfo) Log(log *zerolog.Logger) {
	log.Info().Msgf("Version %s (Checksum %s)", bi.CloudflaredVersion, bi.Checksum)
	if bi.BuildType != "" {
		log.Info().Msgf("Built%s", bi.GetBuildTypeMsg())
	}
	log.Info().Msgf("GOOS: %s, GOVersion: %s, GoArch: %s", bi.GoOS, bi.GoVersion, bi.GoArch)
}

func (bi *BuildInfo) OSArch() string {
	return fmt.Sprintf("%s_%s", bi.GoOS, bi.GoArch)
}

func (bi *BuildInfo) Version() string {
	return bi.CloudflaredVersion
}

func (bi *BuildInfo) GetBuildTypeMsg() string {
	if bi.BuildType == "" {
		return ""
	}
	return fmt.Sprintf(" with %s", bi.BuildType)
}

func (bi *BuildInfo) UserAgent() string {
	return fmt.Sprintf("cloudflared/%s", bi.CloudflaredVersion)
}

// FileChecksum opens a file and returns the SHA256 checksum.
func FileChecksum(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func currentBinaryChecksum() string {
	currentPath, err := os.Executable()
	if err != nil {
		return ""
	}
	sum, _ := FileChecksum(currentPath)
	return sum
}
