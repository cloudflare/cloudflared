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

// RoundTrip marks requests that come through the tunnel so that the loopback
// listener can tell them apart from other local processes.
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

	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if rel == "" {
		rel = "index.html"
	}
	if !safeRequestPath(rel) {
		http.NotFound(w, r)
		return
	}

	target := filepath.Join(o.root, filepath.FromSlash(rel))
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// This is the check that actually contains the request. The filesystem may
	// have expanded it into something the policy above never saw: a Windows 8.3
	// short name such as "GIT~1" that aliases ".git", or a symlink inside the
	// root pointing at a dotfile. Applying the same policy to the resolved path
	// rejects both, and an escape from the root surfaces here as a leading "..".
	resolvedRel, err := filepath.Rel(o.root, resolved)
	if err != nil || !safeRelativePath(resolvedRel) {
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

// safeRequestPath applies the policy to a path that came straight from the
// request. It additionally rejects backslashes: on Windows they separate path
// components for the filesystem but are opaque to path.Clean, which would give
// one file two URLs ("/sub%5ca.txt" and "/sub/a.txt").
//
// That is a normalisation rule rather than the safety boundary: what stops a
// backslash from reaching a dotfile is safeRelativePath mapping it through
// filepath.ToSlash, and ultimately the resolved-path check in ServeHTTP.
func safeRequestPath(rel string) bool {
	if strings.ContainsRune(rel, '\\') {
		return false
	}
	return safeRelativePath(rel)
}

// safeRelativePath reports whether every component of rel is something we are
// willing to serve. It is applied both to the request path and to the path the
// filesystem resolved to.
func safeRelativePath(rel string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(rel), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		// No hidden files or directories.
		if strings.HasPrefix(segment, ".") {
			return false
		}
		// Windows silently strips trailing dots and spaces, so "a.txt." would
		// alias "a.txt". Rejecting on every platform keeps behaviour identical
		// rather than aliasing on one of them.
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
