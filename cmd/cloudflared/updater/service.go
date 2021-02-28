package updater

// CheckResult is the behaviour resulting from checking in with the Update Service
type CheckResult interface {
	Apply() error
	Version() string
	UserMessage() string
}

// Service is the functions to get check for new updates
type Service interface {
	Check() (CheckResult, error)
}

const (
	// OSKeyName is the url parameter key to send to the checkin API for the operating system of the local cloudflared (e.g. windows, darwin, linux)
	OSKeyName = "os"

	// ArchitectureKeyName is the url parameter key to send to the checkin API for the architecture of the local cloudflared (e.g. amd64, x86)
	ArchitectureKeyName = "arch"

	// BetaKeyName is the url parameter key to send to the checkin API to signal if the update should be a beta version or not
	BetaKeyName = "beta"

	// VersionKeyName is the url parameter key to send to the checkin API to specific what version to upgrade or downgrade to
	VersionKeyName = "version"

	// ClientVersionName is the url parameter key to send the version that this cloudflared is currently running with
	ClientVersionName = "clientVersion"
)
