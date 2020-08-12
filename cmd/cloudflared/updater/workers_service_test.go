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
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
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
	host := "http://localhost:8090"
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

	v := VersionResponse{URL: "http://localhost:8090/gzip-download.tgz", Version: version, Checksum: checksum}
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

func startServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/updater", updateHandler)
	mux.HandleFunc("/download", downloadHandler)
	mux.HandleFunc("/beta", betaHandler)
	mux.HandleFunc("/fail", failureHandler)
	mux.HandleFunc("/compressed", gzipUpdateHandler)
	mux.HandleFunc("/gzip-download.tgz", compressedDownloadHandler)
	http.ListenAndServe(":8090", mux)
}

func createTestFile(t *testing.T, path string) {
	f, err := os.Create("tmpfile")
	assert.NoError(t, err)
	fmt.Fprint(f, "2020.08.04")
	f.Close()
}

func TestUpdateService(t *testing.T) {
	go startServer()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.2", "http://localhost:8090/updater", testFilePath, Options{})
	v, err := s.Check()
	assert.NoError(t, err)
	assert.Equal(t, v.String(), "2020.08.05")

	assert.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	assert.NoError(t, err)

	assert.Equal(t, string(dat), "2020.08.05")
}

func TestBetaUpdateService(t *testing.T) {
	go startServer()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.2", "http://localhost:8090/updater", testFilePath, Options{IsBeta: true})
	v, err := s.Check()
	assert.NoError(t, err)
	assert.Equal(t, v.String(), "2020.08.06")

	assert.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	assert.NoError(t, err)

	assert.Equal(t, string(dat), "2020.08.06")
}

func TestFailUpdateService(t *testing.T) {
	go startServer()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.2", "http://localhost:8090/fail", testFilePath, Options{})
	v, err := s.Check()
	assert.Error(t, err)
	assert.Nil(t, v)
}

func TestNoUpdateService(t *testing.T) {
	go startServer()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.5", "http://localhost:8090/updater", testFilePath, Options{})
	v, err := s.Check()
	assert.NoError(t, err)
	assert.Nil(t, v)
}

func TestForcedUpdateService(t *testing.T) {
	go startServer()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.5", "http://localhost:8090/updater", testFilePath, Options{IsForced: true})
	v, err := s.Check()
	assert.NoError(t, err)
	assert.Equal(t, v.String(), "2020.08.05")

	assert.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	assert.NoError(t, err)

	assert.Equal(t, string(dat), "2020.08.05")
}

func TestUpdateSpecificVersionService(t *testing.T) {
	go startServer()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)
	reqVersion := "2020.9.1"

	s := NewWorkersService("2020.8.2", "http://localhost:8090/updater", testFilePath, Options{RequestedVersion: reqVersion})
	v, err := s.Check()
	assert.NoError(t, err)
	assert.Equal(t, reqVersion, v.String())

	assert.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	assert.NoError(t, err)

	assert.Equal(t, reqVersion, string(dat))
}

func TestCompressedUpdateService(t *testing.T) {
	go startServer()

	testFilePath := "tmpfile"
	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.2", "http://localhost:8090/compressed", testFilePath, Options{})
	v, err := s.Check()
	assert.NoError(t, err)
	assert.Equal(t, "2020.09.02", v.String())

	assert.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	assert.NoError(t, err)

	assert.Equal(t, "2020.09.02", string(dat))
}

func TestVersionParsing(t *testing.T) {
	assert.False(t, IsNewerVersion("2020.8.2", "2020.8.2"))
	assert.True(t, IsNewerVersion("2020.8.2", "2020.8.3"))
	assert.True(t, IsNewerVersion("2020.8.2", "2021.1.2"))
	assert.True(t, IsNewerVersion("2020.8.2", "2020.9.1"))
	assert.True(t, IsNewerVersion("2020.8.2", "2020.12.45"))
	assert.False(t, IsNewerVersion("2020.8.2", "2020.6.3"))
	assert.False(t, IsNewerVersion("DEV", "2020.8.5"))
	assert.False(t, IsNewerVersion("2020.8.2", "asdlkfjasdf"))
	assert.True(t, IsNewerVersion("3.0.1", "4.2.1"))
}
