package updater

// Version is the functions needed to perform an update
type Version interface {
	Apply() error
	String() string
}

// Service is the functions to get check for new updates
type Service interface {
	Check() (Version, error)
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
)
