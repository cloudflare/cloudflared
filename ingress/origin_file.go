package ingress

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

// FileFlag is the CLI flag used to serve a local directory through the tunnel.
const FileFlag = "file"

// fileServer is an OriginService that serves a local directory read-only.
//
// It starts an in-process HTTP server bound to loopback and points the embedded
// httpService at it, in the same way helloWorld does. Nothing is ever written.
type fileServer struct {
	httpService
	root   string
	server net.Listener
}

func newFileServer(dir string) (*fileServer, error) {
	if dir == "" {
		return nil, errors.New("file service requires a directory")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, errors.Wrap(err, "invalid directory")
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, errors.Wrap(err, "cannot resolve directory")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, errors.Wrap(err, "cannot stat directory")
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	return &fileServer{root: root}, nil
}

func (o *fileServer) String() string { return "file:" + o.root }

func (o fileServer) MarshalJSON() ([]byte, error) { return json.Marshal(o.String()) }

func (o *fileServer) start(log *zerolog.Logger, shutdownC <-chan struct{}, cfg OriginRequestConfig) error {
	if err := o.httpService.start(log, shutdownC, cfg); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return errors.Wrap(err, "cannot start local file server")
	}
	o.server = listener
	server := &http.Server{
		Handler:           o,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	go func() {
		<-shutdownC
		_ = listener.Close()
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
func (o *fileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if rel == "" {
		rel = "index.html"
	}
	for _, segment := range strings.Split(rel, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.HasPrefix(segment, ".") {
			http.NotFound(w, r)
			return
		}
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

	file, err := os.Open(resolved)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func isWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
