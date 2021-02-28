package updater

import (
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strconv"
	"strings"
)

// Options are the update options supported by the
type Options struct {
	// IsBeta is for beta updates to be installed if available
	IsBeta bool

	// IsForced is to forcibly download the latest version regardless of the current version
	IsForced bool

	// RequestedVersion is the specific version to upgrade or downgrade to
	RequestedVersion string
}

// VersionResponse is the JSON response from the Workers API endpoint
type VersionResponse struct {
	URL          string `json:"url"`
	Version      string `json:"version"`
	Checksum     string `json:"checksum"`
	IsCompressed bool   `json:"compressed"`
	UserMessage  string `json:"userMessage"`
	Error        string `json:"error"`
}

// WorkersService implements Service.
// It contains everything needed to check in with the WorkersAPI and download and apply the updates
type WorkersService struct {
	currentVersion string
	url            string
	targetPath     string
	opts           Options
}

// NewWorkersService creates a new updater Service object.
func NewWorkersService(currentVersion, url, targetPath string, opts Options) Service {
	return &WorkersService{
		currentVersion: currentVersion,
		url:            url,
		targetPath:     targetPath,
		opts:           opts,
	}
}

// Check does a check in with the Workers API to get a new version update
func (s *WorkersService) Check() (CheckResult, error) {
	client := &http.Client{
		Timeout: clientTimeout,
	}

	req, err := http.NewRequest(http.MethodGet, s.url, nil)
	q := req.URL.Query()
	q.Add(OSKeyName, runtime.GOOS)
	q.Add(ArchitectureKeyName, runtime.GOARCH)
	q.Add(ClientVersionName, s.currentVersion)

	if s.opts.IsBeta {
		q.Add(BetaKeyName, "true")
	}

	if s.opts.RequestedVersion != "" {
		q.Add(VersionKeyName, s.opts.RequestedVersion)
	}

	req.URL.RawQuery = q.Encode()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var v VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, err
	}

	if v.Error != "" {
		return nil, errors.New(v.Error)
	}

	var versionToUpdate = ""
	if s.opts.IsForced || IsNewerVersion(s.currentVersion, v.Version) {
		versionToUpdate = v.Version
	}

	return NewWorkersVersion(v.URL, versionToUpdate, v.Checksum, s.targetPath, v.UserMessage, v.IsCompressed), nil
}

// IsNewerVersion checks semantic versioning for the latest version
// cloudflared tagging is more of a date than a semantic version,
// but the same comparision logic still holds for major.minor.patch
// e.g. 2020.8.2 is newer than 2020.8.1.
func IsNewerVersion(current string, check string) bool {
	if strings.Contains(strings.ToLower(current), "dev") {
		return false // dev builds shouldn't update
	}

	cMajor, cMinor, cPatch, err := SemanticParts(current)
	if err != nil {
		return false
	}

	nMajor, nMinor, nPatch, err := SemanticParts(check)
	if err != nil {
		return false
	}

	if nMajor > cMajor {
		return true
	}

	if nMajor == cMajor && nMinor > cMinor {
		return true
	}

	if nMajor == cMajor && nMinor == cMinor && nPatch > cPatch {
		return true
	}
	return false
}

// SemanticParts gets the major, minor, and patch version of a semantic version string
// e.g. 3.1.2 would return 3, 1, 2, nil
func SemanticParts(version string) (major int, minor int, patch int, err error) {
	major = 0
	minor = 0
	patch = 0
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		err = errors.New("invalid version")
		return
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return
	}

	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return
	}

	patch, err = strconv.Atoi(parts[2])
	if err != nil {
		return
	}
	return
}
