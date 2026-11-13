package ingress

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileServerServesFiles(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "index.html"), "INDEX")
	mustWriteFile(t, filepath.Join(root, "a.txt"), "AAA")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "sub", "b.txt"), "BBB")

	svc, err := newFileServer(root)
	if err != nil {
		t.Fatalf("newFileServer: %v", err)
	}
	if err := svc.resolveRoot(); err != nil {
		t.Fatalf("resolveRoot: %v", err)
	}
	if !strings.HasPrefix(svc.String(), "file:") {
		t.Fatalf("unexpected String(): %s", svc.String())
	}

	cases := []struct {
		name string
		path string
		code int
		body string
	}{
		{"root serves index", "/", 200, "INDEX"},
		{"plain file", "/a.txt", 200, "AAA"},
		{"nested file", "/sub/b.txt", 200, "BBB"},
		{"missing", "/nope.txt", 404, ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		svc.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com"+c.path, nil))
		if rec.Code != c.code {
			t.Errorf("%s: code=%d want %d", c.name, rec.Code, c.code)
		}
		if c.body != "" && !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: body=%q", c.name, rec.Body.String())
		}
	}
}

func TestFileServerRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "AAA")
	mustWriteFile(t, filepath.Join(root, ".env"), "SECRET=1")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "secret.txt"), "OUTSIDE")
	linkErr := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "evil.txt"))

	svc, err := newFileServer(root)
	if err != nil {
		t.Fatalf("newFileServer: %v", err)
	}
	if err := svc.resolveRoot(); err != nil {
		t.Fatalf("resolveRoot: %v", err)
	}

	paths := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"dotdot", "GET", "/../secret.txt", 404},
		{"dotdot encoded", "GET", "/%2e%2e/secret.txt", 404},
		{"dotfile", "GET", "/.env", 404},
		{"dot directory", "GET", "/.git/config", 404},
		{"directory listing", "GET", "/sub/", 404},
		{"trailing dot", "GET", "/a.txt.", 404},
		{"trailing dot on dotfile", "GET", "/.env.", 404},
		{"post not allowed", "POST", "/a.txt", 405},
		{"put not allowed", "PUT", "/a.txt", 405},
	}
	for _, p := range paths {
		rec := httptest.NewRecorder()
		svc.ServeHTTP(rec, httptest.NewRequest(p.method, "http://example.com"+p.path, nil))
		if rec.Code != p.want {
			t.Errorf("%s: code=%d want %d (body=%q)", p.name, rec.Code, p.want, rec.Body.String())
		}
	}

	if linkErr != nil {
		t.Skipf("symlink unavailable: %v", linkErr)
	}
	rec := httptest.NewRecorder()
	svc.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/evil.txt", nil))
	if rec.Code != 404 {
		t.Errorf("symlink escape: code=%d want 404", rec.Code)
	}
}

func TestNewFileServerOnlyChecksSyntax(t *testing.T) {
	// Configuration parsing must not touch the filesystem: a config file may be
	// validated on a machine where the directory does not exist.
	if _, err := newFileServer(""); err == nil {
		t.Error("expected error for an empty path, got nil")
	}

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	svc, err := newFileServer(missing)
	if err != nil {
		t.Fatalf("newFileServer must not stat the directory, got: %v", err)
	}
	if err := svc.resolveRoot(); err == nil {
		t.Error("resolveRoot must fail for a missing directory")
	}
}

func TestResolveRootRejectsNonDirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "f.txt")
	mustWriteFile(t, file, "x")

	svc, err := newFileServer(file)
	if err != nil {
		t.Fatalf("newFileServer: %v", err)
	}
	if err := svc.resolveRoot(); err == nil {
		t.Error("resolveRoot must reject a regular file")
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
