package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func respondWithJSON(w http.ResponseWriter, v interface{}, status int) {
	data, _ := json.Marshal(v)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(data)
}

func respondWithData(w http.ResponseWriter, b []byte, status int) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(status)
	w.Write(b)
}

func updateHandler(w http.ResponseWriter, r *http.Request) {
	version := "2020.08.05"
	host := fmt.Sprintf("http://%s", r.Host)
	url := host + "/download"

	query := r.URL.Query()

	if query.Get(BetaKeyName) == "true" {
		version = "2020.08.06"
		url = host + "/beta"
	}

	requestedVersion := query.Get(VersionKeyName)
	if requestedVersion != "" {
		version = requestedVersion
		url = fmt.Sprintf("%s?version=%s", url, requestedVersion)
	}

	if query.Get(ArchitectureKeyName) != runtime.GOARCH || query.Get(OSKeyName) != runtime.GOOS {
		respondWithJSON(w, VersionResponse{Error: "unsupported os and architecture"}, http.StatusBadRequest)
		return
	}

	h := sha256.New()
	fmt.Fprint(h, version)
	checksum := fmt.Sprintf("%x", h.Sum(nil))

	v := VersionResponse{URL: url, Version: version, Checksum: checksum}
	respondWithJSON(w, v, http.StatusOK)
}

func gzipUpdateHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("got a request!")
	version := "2020.09.02"
	h := sha256.New()
	fmt.Fprint(h, version)
	checksum := fmt.Sprintf("%x", h.Sum(nil))

	url := fmt.Sprintf("http://%s/gzip-download.tgz", r.Host)
	v := VersionResponse{URL: url, Version: version, Checksum: checksum}
	respondWithJSON(w, v, http.StatusOK)
}

func compressedDownloadHandler(w http.ResponseWriter, r *http.Request) {
	version := "2020.09.02"
	buf := new(bytes.Buffer)

	gw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gw)

	header := &tar.Header{
		Size: int64(len(version)),
		Name: "download",
	}
	tw.WriteHeader(header)
	tw.Write([]byte(version))

	tw.Close()
	gw.Close()

	respondWithData(w, buf.Bytes(), http.StatusOK)
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	version := "2020.08.05"
	requestedVersion := r.URL.Query().Get(VersionKeyName)
	if requestedVersion != "" {
		version = requestedVersion
	}
	respondWithData(w, []byte(version), http.StatusOK)
}

func betaHandler(w http.ResponseWriter, r *http.Request) {
	respondWithData(w, []byte("2020.08.06"), http.StatusOK)
}

func failureHandler(w http.ResponseWriter, r *http.Request) {
	respondWithJSON(w, VersionResponse{Error: "unsupported os and architecture"}, http.StatusBadRequest)
}

func createServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/updater", updateHandler)
	mux.HandleFunc("/download", downloadHandler)
	mux.HandleFunc("/beta", betaHandler)
	mux.HandleFunc("/fail", failureHandler)
	mux.HandleFunc("/compressed", gzipUpdateHandler)
	mux.HandleFunc("/gzip-download.tgz", compressedDownloadHandler)
	return httptest.NewServer(mux)
}

func createTestFile(t *testing.T, path string) {
	f, err := os.Create("tmpfile")
	require.NoError(t, err)
	fmt.Fprint(f, "2020.08.04")
	f.Close()
}

func TestUpdateService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)
	log.Println("server url: ", ts.URL)

	s := NewWorkersService("2020.8.2", fmt.Sprintf("%s/updater", ts.URL), testFilePath, Options{})
	v, err := s.Check()
	require.NoError(t, err)
	require.Equal(t, v.String(), "2020.08.05")

	require.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	require.NoError(t, err)

	require.Equal(t, string(dat), "2020.08.05")
}

func TestBetaUpdateService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.2", fmt.Sprintf("%s/updater", ts.URL), testFilePath, Options{IsBeta: true})
	v, err := s.Check()
	require.NoError(t, err)
	require.Equal(t, v.String(), "2020.08.06")

	require.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	require.NoError(t, err)

	require.Equal(t, string(dat), "2020.08.06")
}

func TestFailUpdateService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.2", fmt.Sprintf("%s/fail", ts.URL), testFilePath, Options{})
	v, err := s.Check()
	require.Error(t, err)
	require.Nil(t, v)
}

func TestNoUpdateService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.5", fmt.Sprintf("%s/updater", ts.URL), testFilePath, Options{})
	v, err := s.Check()
	require.NoError(t, err)
	require.Nil(t, v)
}

func TestForcedUpdateService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.5", fmt.Sprintf("%s/updater", ts.URL), testFilePath, Options{IsForced: true})
	v, err := s.Check()
	require.NoError(t, err)
	require.Equal(t, v.String(), "2020.08.05")

	require.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	require.NoError(t, err)

	require.Equal(t, string(dat), "2020.08.05")
}

func TestUpdateSpecificVersionService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)
	reqVersion := "2020.9.1"

	s := NewWorkersService("2020.8.2", fmt.Sprintf("%s/updater", ts.URL), testFilePath, Options{RequestedVersion: reqVersion})
	v, err := s.Check()
	require.NoError(t, err)
	require.Equal(t, reqVersion, v.String())

	require.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	require.NoError(t, err)

	require.Equal(t, reqVersion, string(dat))
}

func TestCompressedUpdateService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.2", fmt.Sprintf("%s/compressed", ts.URL), testFilePath, Options{})
	v, err := s.Check()
	require.NoError(t, err)
	require.Equal(t, "2020.09.02", v.String())

	require.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	require.NoError(t, err)

	require.Equal(t, "2020.09.02", string(dat))
}

func TestVersionParsing(t *testing.T) {
	require.False(t, IsNewerVersion("2020.8.2", "2020.8.2"))
	require.True(t, IsNewerVersion("2020.8.2", "2020.8.3"))
	require.True(t, IsNewerVersion("2020.8.2", "2021.1.2"))
	require.True(t, IsNewerVersion("2020.8.2", "2020.9.1"))
	require.True(t, IsNewerVersion("2020.8.2", "2020.12.45"))
	require.False(t, IsNewerVersion("2020.8.2", "2020.6.3"))
	require.False(t, IsNewerVersion("DEV", "2020.8.5"))
	require.False(t, IsNewerVersion("2020.8.2", "asdlkfjasdf"))
	require.True(t, IsNewerVersion("3.0.1", "4.2.1"))
}
