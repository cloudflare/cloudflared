package equinox

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/equinox-io/equinox/proto"
)

const fakeAppID = "fake_app_id"

var (
	fakeBinary    = []byte{0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1}
	newFakeBinary = []byte{0x2, 0x2, 0x2, 0x2, 0x2, 0x2, 0x2, 0x2}
	ts            *httptest.Server
	key           *ecdsa.PrivateKey
	sha           string
	newSHA        string
	signature     string
)

func init() {
	shaBytes := sha256.Sum256(fakeBinary)
	sha = hex.EncodeToString(shaBytes[:])
	newSHABytes := sha256.Sum256(newFakeBinary)
	newSHA = hex.EncodeToString(newSHABytes[:])

	var err error
	key, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("Failed to generate ecdsa key: %v", err))
	}
	sig, err := key.Sign(rand.Reader, newSHABytes[:], nil)
	if err != nil {
		panic(fmt.Sprintf("Failed to sign new binary: %v", err))
	}
	signature = hex.EncodeToString(sig)
}

func TestNotAvailable(t *testing.T) {
	opts := setup(t, "TestNotAvailable", proto.Response{
		Available: false,
	})
	defer cleanup(opts)

	_, err := Check(fakeAppID, opts)
	if err != NotAvailableErr {
		t.Fatalf("Expected not available error, got: %v", err)
	}
}

func TestEndToEnd(t *testing.T) {
	opts := setup(t, "TestEndtoEnd", proto.Response{
		Available: true,
		Release: proto.Release{
			Version:     "0.1.2.3",
			Title:       "Release Title",
			Description: "Release Description",
			CreateDate:  time.Now(),
		},
		Checksum:  newSHA,
		Signature: signature,
	})
	defer cleanup(opts)

	resp, err := Check(fakeAppID, opts)
	if err != nil {
		t.Fatalf("Failed check: %v", err)
	}
	err = resp.Apply()
	if err != nil {
		t.Fatalf("Failed apply: %v", err)
	}

	buf, err := ioutil.ReadFile(opts.TargetPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if !bytes.Equal(buf, newFakeBinary) {
		t.Fatalf("Binary did not update to new expected value. Got %v, expected %v", buf, newFakeBinary)
	}
}

func TestInvalidPatch(t *testing.T) {
	opts := setup(t, "TestInavlidPatch", proto.Response{
		Available: true,
		Release: proto.Release{
			Version:     "0.1.2.3",
			Title:       "Release Title",
			Description: "Release Description",
			CreateDate:  time.Now(),
		},
		DownloadURL: "bad-request",
		Checksum:    newSHA,
		Signature:   signature,
		Patch:       proto.PatchBSDiff,
	})
	defer cleanup(opts)

	resp, err := Check(fakeAppID, opts)
	if err != nil {
		t.Fatalf("Failed check: %v", err)
	}
	err = resp.Apply()
	if err == nil {
		t.Fatalf("Apply succeeded")
	}
	if err.Error() != "error downloading patch: bad-request" {
		t.Fatalf("Expected a different error message: %s", err)
	}
}

func setup(t *testing.T, name string, resp proto.Response) Options {
	checkUserAgent := func(req *http.Request) {
		if req.Header.Get("User-Agent") != userAgent {
			t.Errorf("Expected user agent to be %s, not %s", userAgent, req.Header.Get("User-Agent"))
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/check", func(w http.ResponseWriter, r *http.Request) {
		checkUserAgent(r)
		var req proto.Request
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			t.Fatalf("Failed to decode proto request: %v", err)
		}
		if resp.Available {
			if req.AppID != fakeAppID {
				t.Fatalf("Unexpected app ID. Got %v, expected %v", err)
			}
			if req.CurrentSHA256 != sha {
				t.Fatalf("Unexpected request SHA: %v", sha)
			}
		}
		json.NewEncoder(w).Encode(resp)
	})

	// Keying off the download URL may not be the best idea...
	if resp.DownloadURL == "bad-request" {
		mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
			checkUserAgent(r)
			http.Error(w, "bad-request", http.StatusBadRequest)
		})
	} else {
		mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
			checkUserAgent(r)
			w.Write(newFakeBinary)
		})
	}

	ts = httptest.NewServer(mux)
	resp.DownloadURL = ts.URL + "/bin"

	var opts Options
	opts.CheckURL = ts.URL + "/check"
	opts.PublicKey = key.Public()

	if name != "" {
		opts.TargetPath = name
		ioutil.WriteFile(name, fakeBinary, 0644)
	}
	return opts
}

func cleanup(opts Options) {
	if opts.TargetPath != "" {
		os.Remove(opts.TargetPath)
	}
	ts.Close()
}
