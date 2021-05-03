// +build !windows

package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var testFilePath = filepath.Join(os.TempDir(), "test")

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

const mostRecentVersion = "2021.2.5"
const mostRecentBetaVersion = "2021.3.0"
const knownBuggyVersion = "2020.12.0"
const expectedUserMsg = "This message is expected when running a known buggy version"

func updateHandler(w http.ResponseWriter, r *http.Request) {
	version := mostRecentVersion
	host := fmt.Sprintf("http://%s", r.Host)
	url := host + "/download"

	query := r.URL.Query()

	if query.Get(BetaKeyName) == "true" {
		version = mostRecentBetaVersion
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

	var userMessage = ""
	if query.Get(ClientVersionName) == knownBuggyVersion {
		userMessage = expectedUserMsg
	}
	shouldUpdate := requestedVersion != "" || IsNewerVersion(query.Get(ClientVersionName), version)

	v := VersionResponse{
		URL: url, Version: version, Checksum: checksum, UserMessage: userMessage, ShouldUpdate: shouldUpdate,
	}
	respondWithJSON(w, v, http.StatusOK)
}

func gzipUpdateHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("got a request!")
	version := "2020.09.02"
	h := sha256.New()
	fmt.Fprint(h, version)
	checksum := fmt.Sprintf("%x", h.Sum(nil))

	url := fmt.Sprintf("http://%s/gzip-download.tgz", r.Host)
	v := VersionResponse{URL: url, Version: version, Checksum: checksum, ShouldUpdate: true}
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
	version := mostRecentVersion
	requestedVersion := r.URL.Query().Get(VersionKeyName)
	if requestedVersion != "" {
		version = requestedVersion
	}
	respondWithData(w, []byte(version), http.StatusOK)
}

func betaHandler(w http.ResponseWriter, r *http.Request) {
	respondWithData(w, []byte(mostRecentBetaVersion), http.StatusOK)
}

func failureHandler(w http.ResponseWriter, r *http.Request) {
	respondWithJSON(w, VersionResponse{Error: "unsupported os and architecture"}, http.StatusBadRequest)
}

func IsNewerVersion(current string, check string) bool {
	if current == "" || check == "" {
		return false
	}
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
	f, err := os.Create(path)
	require.NoError(t, err)
	fmt.Fprint(f, "2020.08.04")
	f.Close()
}

func TestUpdateService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)
	log.Println("server url: ", ts.URL)

	s := NewWorkersService("2020.8.2", fmt.Sprintf("%s/updater", ts.URL), testFilePath, Options{})
	v, err := s.Check()
	require.NoError(t, err)
	require.Equal(t, v.Version(), mostRecentVersion)

	require.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	require.NoError(t, err)

	require.Equal(t, string(dat), mostRecentVersion)
}

func TestBetaUpdateService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.2", fmt.Sprintf("%s/updater", ts.URL), testFilePath, Options{IsBeta: true})
	v, err := s.Check()
	require.NoError(t, err)
	require.Equal(t, v.Version(), mostRecentBetaVersion)

	require.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	require.NoError(t, err)

	require.Equal(t, string(dat), mostRecentBetaVersion)
}

func TestFailUpdateService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

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

	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService(mostRecentVersion, fmt.Sprintf("%s/updater", ts.URL), testFilePath, Options{})
	v, err := s.Check()
	require.NoError(t, err)
	require.NotNil(t, v)
	require.Empty(t, v.Version())
}

func TestForcedUpdateService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.5", fmt.Sprintf("%s/updater", ts.URL), testFilePath, Options{IsForced: true})
	v, err := s.Check()
	require.NoError(t, err)
	require.Equal(t, v.Version(), mostRecentVersion)

	require.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	require.NoError(t, err)

	require.Equal(t, string(dat), mostRecentVersion)
}

func TestUpdateSpecificVersionService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)
	reqVersion := "2020.9.1"

	s := NewWorkersService("2020.8.2", fmt.Sprintf("%s/updater", ts.URL), testFilePath, Options{RequestedVersion: reqVersion})
	v, err := s.Check()
	require.NoError(t, err)
	require.Equal(t, reqVersion, v.Version())

	require.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	require.NoError(t, err)

	require.Equal(t, reqVersion, string(dat))
}

func TestCompressedUpdateService(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService("2020.8.2", fmt.Sprintf("%s/compressed", ts.URL), testFilePath, Options{})
	v, err := s.Check()
	require.NoError(t, err)
	require.Equal(t, "2020.09.02", v.Version())

	require.NoError(t, v.Apply())
	dat, err := ioutil.ReadFile(testFilePath)
	require.NoError(t, err)

	require.Equal(t, "2020.09.02", string(dat))
}

func TestUpdateWhenRunningKnownBuggyVersion(t *testing.T) {
	ts := createServer()
	defer ts.Close()

	createTestFile(t, testFilePath)
	defer os.Remove(testFilePath)

	s := NewWorkersService(knownBuggyVersion, fmt.Sprintf("%s/updater", ts.URL), testFilePath, Options{})
	v, err := s.Check()
	require.NoError(t, err)
	require.Equal(t, v.Version(), mostRecentVersion)
	require.Equal(t, v.UserMessage(), expectedUserMsg)
}
