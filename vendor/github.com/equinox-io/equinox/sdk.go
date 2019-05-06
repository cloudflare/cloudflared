package equinox

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/equinox-io/equinox/internal/go-update"
	"github.com/equinox-io/equinox/internal/osext"
	"github.com/equinox-io/equinox/proto"
)

const protocolVersion = "1"
const defaultCheckURL = "https://update.equinox.io/check"
const userAgent = "EquinoxSDK/1.0"

var NotAvailableErr = errors.New("No update available")

type Options struct {
	// Channel specifies the name of an Equinox release channel to check for
	// a newer version of the application.
	//
	// If empty, defaults to 'stable'.
	Channel string

	// Version requests an update to a specific version of the application.
	// If specified, `Channel` is ignored.
	Version string

	// TargetPath defines the path to the file to update.
	// The emptry string means 'the executable file of the running program'.
	TargetPath string

	// Create TargetPath replacement with this file mode. If zero, defaults to 0755.
	TargetMode os.FileMode

	// Public key to use for signature verification. If nil, no signature
	// verification is done. Use `SetPublicKeyPEM` to set this field with PEM data.
	PublicKey crypto.PublicKey

	// Target operating system of the update. Uses the same standard OS names used
	// by Go build tags (windows, darwin, linux, etc).
	// If empty, it will be populated by consulting runtime.GOOS
	OS string

	// Target architecture of the update. Uses the same standard Arch names used
	// by Go build tags (amd64, 386, arm, etc).
	// If empty, it will be populated by consulting runtime.GOARCH
	Arch string

	// Target ARM architecture, if a specific one if required. Uses the same names
	// as the GOARM environment variable (5, 6, 7).
	//
	// GoARM is ignored if Arch != 'arm'.
	// GoARM is ignored if it is the empty string. Omit it if you do not need
	// to distinguish between ARM versions.
	GoARM string

	// The current application version. This is used for statistics and reporting only,
	// it is optional.
	CurrentVersion string

	// CheckURL is the URL to request an update check from. You should only set
	// this if you are running an on-prem Equinox server.
	// If empty the default Equinox update service endpoint is used.
	CheckURL string

	// HTTPClient is used to make all HTTP requests necessary for the update check protocol.
	// You may configure it to use custom timeouts, proxy servers or other behaviors.
	HTTPClient *http.Client
}

// Response is returned by Check when an update is available. It may be
// passed to Apply to perform the update.
type Response struct {
	// Version of the release that will be updated to if applied.
	ReleaseVersion string

	// Title of the the release
	ReleaseTitle string

	// Additional details about the release
	ReleaseDescription string

	// Creation date of the release
	ReleaseDate time.Time

	downloadURL string
	checksum    []byte
	signature   []byte
	patch       proto.PatchKind
	opts        Options
}

// SetPublicKeyPEM is a convenience method to set the PublicKey property
// used for checking a completed update's signature by parsing a
// Public Key formatted as PEM data.
func (o *Options) SetPublicKeyPEM(pembytes []byte) error {
	block, _ := pem.Decode(pembytes)
	if block == nil {
		return errors.New("couldn't parse PEM data")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	o.PublicKey = pub
	return nil
}

// Check communicates with an Equinox update service to determine if
// an update for the given application matching the specified options is
// available. The returned error is nil only if an update is available.
//
// The appID is issued to you when creating an application at https://equinox.io
//
// You can compare the returned error to NotAvailableErr to differentiate between
// a successful check that found no update from other errors like a failed
// network connection.
func Check(appID string, opts Options) (Response, error) {
	var req, err = checkRequest(appID, &opts)

	if err != nil {
		return Response{}, err
	}

	return doCheckRequest(opts, req)
}

func checkRequest(appID string, opts *Options) (*http.Request, error) {
	if opts.Channel == "" {
		opts.Channel = "stable"
	}
	if opts.TargetPath == "" {
		var err error
		opts.TargetPath, err = osext.Executable()
		if err != nil {
			return nil, err
		}
	}
	if opts.OS == "" {
		opts.OS = runtime.GOOS
	}
	if opts.Arch == "" {
		opts.Arch = runtime.GOARCH
	}
	if opts.CheckURL == "" {
		opts.CheckURL = defaultCheckURL
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = new(http.Client)
	}
	opts.HTTPClient.Transport = newUserAgentTransport(userAgent, opts.HTTPClient.Transport)

	checksum := computeChecksum(opts.TargetPath)

	payload, err := json.Marshal(proto.Request{
		AppID:          appID,
		Channel:        opts.Channel,
		OS:             opts.OS,
		Arch:           opts.Arch,
		GoARM:          opts.GoARM,
		TargetVersion:  opts.Version,
		CurrentVersion: opts.CurrentVersion,
		CurrentSHA256:  checksum,
	})

	req, err := http.NewRequest("POST", opts.CheckURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", fmt.Sprintf("application/json; q=1; version=%s; charset=utf-8", protocolVersion))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Close = true

	return req, err
}

func doCheckRequest(opts Options, req *http.Request) (r Response, err error) {
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return r, fmt.Errorf("Server responded with %s: %s", resp.Status, body)
	}

	var protoResp proto.Response
	err = json.NewDecoder(resp.Body).Decode(&protoResp)
	if err != nil {
		return r, err
	}

	if !protoResp.Available {
		return r, NotAvailableErr
	}

	r.ReleaseVersion = protoResp.Release.Version
	r.ReleaseTitle = protoResp.Release.Title
	r.ReleaseDescription = protoResp.Release.Description
	r.ReleaseDate = protoResp.Release.CreateDate
	r.downloadURL = protoResp.DownloadURL
	r.patch = protoResp.Patch
	r.opts = opts
	r.checksum, err = hex.DecodeString(protoResp.Checksum)
	if err != nil {
		return r, err
	}
	r.signature, err = hex.DecodeString(protoResp.Signature)
	if err != nil {
		return r, err
	}

	return r, nil
}

func computeChecksum(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Apply performs an update of the current executable (or TargetFile, if it was
// set on the Options) with the update specified by Response.
//
// Error is nil if and only if the entire update completes successfully.
func (r Response) Apply() error {
	var req, opts, err = r.applyRequest()

	if err != nil {
		return err
	}

	return r.applyUpdate(req, opts)
}

func (r Response) applyRequest() (*http.Request, update.Options, error) {
	opts := update.Options{
		TargetPath: r.opts.TargetPath,
		TargetMode: r.opts.TargetMode,
		Checksum:   r.checksum,
		Signature:  r.signature,
		Verifier:   update.NewECDSAVerifier(),
		PublicKey:  r.opts.PublicKey,
	}
	switch r.patch {
	case proto.PatchBSDiff:
		opts.Patcher = update.NewBSDiffPatcher()
	}

	if err := opts.CheckPermissions(); err != nil {
		return nil, opts, err
	}

	req, err := http.NewRequest("GET", r.downloadURL, nil)
	return req, opts, err
}

func (r Response) applyUpdate(req *http.Request, opts update.Options) error {
	// fetch the update
	req.Close = true
	resp, err := r.opts.HTTPClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	// check that we got a patch
	if resp.StatusCode >= 400 {
		msg := "error downloading patch"

		id := resp.Header.Get("Request-Id")
		if id != "" {
			msg += ", request " + id
		}

		blob, err := ioutil.ReadAll(resp.Body)
		if err == nil {
			msg += ": " + string(bytes.TrimSpace(blob))
		}
		return fmt.Errorf(msg)
	}

	return update.Apply(resp.Body, opts)
}

type userAgentTransport struct {
	userAgent string
	http.RoundTripper
}

func newUserAgentTransport(userAgent string, rt http.RoundTripper) *userAgentTransport {
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &userAgentTransport{userAgent, rt}
}

func (t *userAgentTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", t.userAgent)
	}
	return t.RoundTripper.RoundTrip(r)
}
