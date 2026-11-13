package ingress

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

// FileFlag is the CLI flag used to serve a local directory through the tunnel.
const FileFlag = "file"

// localFileAuthHeader marks requests that came through the tunnel proxy rather
// than from another process on the machine listening on the loopback port.
const localFileAuthHeader = "X-Cloudflared-File-Server"

// fileServer is an OriginService that serves a local directory read-only.
//
// It starts an in-process HTTP server bound to loopback and points the embedded
// httpService at it, in the same way helloWorld does. Nothing is ever written.
type fileServer struct {
	httpService
	root  string
	token string
}

// newFileServer records the directory to serve.
//
// The path is only made absolute here. Resolving and validating it happens in
// start(), so that validating a configuration file does not require the
// directory to exist on the machine doing the validation.
func newFileServer(dir string) (*fileServer, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("file service requires a directory")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, errors.Wrap(err, "invalid directory")
	}
	token, err := newLocalToken()
	if err != nil {
		return nil, err
	}
	return &fileServer{root: abs, token: token}, nil
}

func newLocalToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", errors.Wrap(err, "cannot generate loopback token")
	}
	return hex.EncodeToString(buf), nil
}

// resolveRoot resolves symlinks and checks that root is an existing directory.
func (o *fileServer) resolveRoot() error {
	resolved, err := filepath.EvalSymlinks(o.root)
	if err != nil {
		return errors.Wrapf(err, "cannot resolve directory %s", o.root)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return errors.Wrapf(err, "cannot stat directory %s", resolved)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", resolved)
	}
	o.root = resolved
	return nil
}

func (o *fileServer) String() string { return "file:" + o.root }

func (o fileServer) MarshalJSON() ([]byte, error) { return json.Marshal(o.String()) }

// RoundTrip marks requests that come from the tunnel so the loopback listener
// can tell them apart from other local processes.
func (o *fileServer) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set(localFileAuthHeader, o.token)
	return o.httpService.RoundTrip(clone)
}

func (o *fileServer) start(log *zerolog.Logger, shutdownC <-chan struct{}, cfg OriginRequestConfig) error {
	if err := o.resolveRoot(); err != nil {
		return err
	}
	if err := o.httpService.start(log, shutdownC, cfg); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return errors.Wrap(err, "cannot start local file server")
	}
	server := &http.Server{
		Handler:           o,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go func() { _ = server.Serve(listener) }()
	go func() {
		<-shutdownC
		// Close also tears down keep-alive connections the shared transport
		// may still be holding open.
		_ = server.Close()
	}()
	o.httpService.url = &url.URL{Scheme: "http", Host: listener.Addr().String()}
	log.Info().
		Str("root", o.root).
		Str("addr", listener.Addr().String()).
		Msg("Serving local directory")
	return nil
}

// ServeHTTP serves files under root. It is deliberately stricter than
// http.FileServer: no directory listings, no dotfiles, no symlink escapes and
// only GET/HEAD.
//
// Only the root path maps to index.html. Requests for a directory such as
// "/sub/" are 404 by design, so a site whose links end in a slash needs the
// page to be requested explicitly ("/sub/index.html").
func (o *fileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Only the tunnel may talk to this listener.
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(localFileAuthHeader)), []byte(o.token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// A backslash is never valid in a URL path. On Windows it is a separator
	// for the filesystem but not for path.Clean, so allowing it would let
	// "/%5c.env" slip past the segment checks below and resolve to ".env".
	if strings.ContainsRune(r.URL.Path, '\\') {
		http.NotFound(w, r)
		return
	}

	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if rel == "" {
		rel = "index.html"
	}
	if !safeRelativePath(rel) {
		http.NotFound(w, r)
		return
	}

	target := filepath.Join(o.root, filepath.FromSlash(rel))
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !isWithin(o.root, resolved) {
		http.NotFound(w, r)
		return
	}

	// Re-check the resolved path: the filesystem may have expanded the request
	// into something the checks above never saw, for example a Windows 8.3
	// short name such as "GIT~1" that aliases ".git".
	if resolvedRel, err := filepath.Rel(o.root, resolved); err != nil || !safeRelativePath(resolvedRel) {
		http.NotFound(w, r)
		return
	}

	file, err := os.Open(resolved)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

// safeRelativePath reports whether every component of rel is something we are
// willing to serve. It is applied both to the request path and to the resolved
// filesystem path.
func safeRelativePath(rel string) bool {
	// A backslash is a separator on Windows but not on Unix. Reject it
	// everywhere so the policy does not depend on the platform.
	if strings.ContainsRune(rel, 0x5C) {
		return false
	}
	for _, segment := range strings.Split(filepath.ToSlash(rel), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		// No hidden files or directories.
		if strings.HasPrefix(segment, ".") {
			return false
		}
		// Windows silently strips trailing dots and spaces, so "a.txt." would
		// alias "a.txt". Rejecting everywhere keeps behaviour identical across
		// platforms rather than aliasing on one of them.
		if strings.TrimRight(segment, ". ") != segment {
			return false
		}
		if isReservedName(segment) {
			return false
		}
	}
	return true
}

// isReservedName reports Windows device names, which are not regular files and
// can block on open.
func isReservedName(segment string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	name := strings.ToUpper(segment)
	if idx := strings.IndexByte(name, '.'); idx >= 0 {
		name = name[:idx]
	}
	switch name {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(name) == 4 {
		prefix, digit := name[:3], name[3]
		if digit >= '1' && digit <= '9' && (prefix == "COM" || prefix == "LPT") {
			return true
		}
	}
	return false
}

func isWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
