package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
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
	ShouldUpdate bool   `json:"shouldUpdate"`
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
	if err != nil {
		return nil, err
	}
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

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unable to check for update: %d", resp.StatusCode)
	}

	var v VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, err
	}

	if v.Error != "" {
		return nil, errors.New(v.Error)
	}

	versionToUpdate := ""
	if v.ShouldUpdate {
		versionToUpdate = v.Version
	}

	return NewWorkersVersion(v.URL, versionToUpdate, v.Checksum, s.targetPath, v.UserMessage, v.IsCompressed), nil
}
