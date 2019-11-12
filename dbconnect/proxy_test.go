package dbconnect

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/stretchr/testify/assert"
)

func TestNewInsecureProxy(t *testing.T) {
	origins := []string{
		"",
		":/",
		"http://localhost",
		"tcp://localhost:9000?debug=true",
		"mongodb://127.0.0.1",
	}

	for _, origin := range origins {
		proxy, err := NewInsecureProxy(context.Background(), origin)

		assert.Error(t, err)
		assert.Empty(t, proxy)
	}
}

func TestProxyIsAllowed(t *testing.T) {
	proxy := helperNewProxy(t)
	req := httptest.NewRequest("GET", "https://1.1.1.1/ping", nil)
	assert.True(t, proxy.IsAllowed(req))

	proxy = helperNewProxy(t, true)
	req.Header.Set("Cf-access-jwt-assertion", "xxx")
	assert.False(t, proxy.IsAllowed(req))
}

func TestProxyStart(t *testing.T) {
	proxy := helperNewProxy(t)
	ctx := context.Background()
	listenerC := make(chan net.Listener)

	err := proxy.Start(ctx, "1.1.1.1:", listenerC)
	assert.Error(t, err)

	err = proxy.Start(ctx, "127.0.0.1:-1", listenerC)
	assert.Error(t, err)

	ctx, cancel := context.WithTimeout(ctx, 0)
	defer cancel()

	err = proxy.Start(ctx, "127.0.0.1:", listenerC)
	assert.IsType(t, http.ErrServerClosed, err)
}

func TestProxyHTTPRouter(t *testing.T) {
	proxy := helperNewProxy(t)
	router := proxy.httpRouter()

	tests := []struct {
		path   string
		method string
		valid  bool
	}{
		{"", "GET", false},
		{"/", "GET", false},
		{"/ping", "GET", true},
		{"/ping", "HEAD", true},
		{"/ping", "POST", false},
		{"/submit", "POST", true},
		{"/submit", "GET", false},
		{"/submit/extra", "POST", false},
	}

	for _, test := range tests {
		match := &mux.RouteMatch{}
		ok := router.Match(httptest.NewRequest(test.method, "https://1.1.1.1"+test.path, nil), match)

		assert.True(t, ok == test.valid, test.path)
	}
}

func TestProxyHTTPPing(t *testing.T) {
	proxy := helperNewProxy(t)

	server := httptest.NewServer(proxy.httpPing())
	defer server.Close()
	client := server.Client()

	res, err := client.Get(server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, int64(2), res.ContentLength)

	res, err = client.Head(server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, int64(-1), res.ContentLength)
}

func TestProxyHTTPSubmit(t *testing.T) {
	proxy := helperNewProxy(t)

	server := httptest.NewServer(proxy.httpSubmit())
	defer server.Close()
	client := server.Client()

	tests := []struct {
		input  string
		status int
		output string
	}{
		{"", http.StatusBadRequest, "request body cannot be empty"},
		{"{}", http.StatusBadRequest, "cannot provide an empty statement"},
		{"{\"statement\":\"Ok\"}", http.StatusUnprocessableEntity, "cannot provide invalid sql mode: ''"},
		{"{\"statement\":\"Ok\",\"mode\":\"query\"}", http.StatusUnprocessableEntity, "near \"Ok\": syntax error"},
		{"{\"statement\":\"CREATE TABLE t (a INT);\",\"mode\":\"exec\"}", http.StatusOK, "{\"last_insert_id\":0,\"rows_affected\":0}\n"},
	}

	for _, test := range tests {
		res, err := client.Post(server.URL, "application/json", strings.NewReader(test.input))

		assert.NoError(t, err)
		assert.Equal(t, test.status, res.StatusCode)
		if res.StatusCode > http.StatusOK {
			assert.Equal(t, "text/plain; charset=utf-8", res.Header.Get("Content-type"))
		} else {
			assert.Equal(t, "application/json", res.Header.Get("Content-type"))
		}

		data, err := ioutil.ReadAll(res.Body)
		defer res.Body.Close()
		str := string(data)

		assert.NoError(t, err)
		assert.Equal(t, test.output, str)
	}
}

func TestProxyHTTPSubmitForbidden(t *testing.T) {
	proxy := helperNewProxy(t, true)

	server := httptest.NewServer(proxy.httpSubmit())
	defer server.Close()
	client := server.Client()

	res, err := client.Get(server.URL)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, res.StatusCode)
	assert.Zero(t, res.ContentLength)
}

func TestProxyHTTPRespond(t *testing.T) {
	proxy := helperNewProxy(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy.httpRespond(w, r, http.StatusAccepted, "Hello")
	}))
	defer server.Close()
	client := server.Client()

	res, err := client.Get(server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, res.StatusCode)
	assert.Equal(t, int64(5), res.ContentLength)

	data, err := ioutil.ReadAll(res.Body)
	defer res.Body.Close()
	assert.Equal(t, []byte("Hello"), data)
}

func TestProxyHTTPRespondForbidden(t *testing.T) {
	proxy := helperNewProxy(t, true)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy.httpRespond(w, r, http.StatusAccepted, "Hello")
	}))
	defer server.Close()
	client := server.Client()

	res, err := client.Get(server.URL)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, res.StatusCode)
	assert.Equal(t, int64(0), res.ContentLength)
}

func TestHTTPError(t *testing.T) {
	_, errTimeout := net.DialTimeout("tcp", "127.0.0.1", 0)
	assert.Error(t, errTimeout)

	tests := []struct {
		input  error
		status int
		output error
	}{
		{nil, http.StatusNotImplemented, fmt.Errorf("error expected but found none")},
		{io.EOF, http.StatusBadRequest, fmt.Errorf("request body cannot be empty")},
		{context.DeadlineExceeded, http.StatusRequestTimeout, nil},
		{context.Canceled, 444, nil},
		{errTimeout, http.StatusRequestTimeout, nil},
		{fmt.Errorf(""), http.StatusInternalServerError, nil},
	}

	for _, test := range tests {
		status, err := httpError(http.StatusInternalServerError, test.input)

		assert.Error(t, err)
		assert.Equal(t, test.status, status)
		if test.output == nil {
			test.output = test.input
		}
		assert.Equal(t, test.output, err)
	}
}

func helperNewProxy(t *testing.T, secure ...bool) *Proxy {
	t.Helper()

	proxy, err := NewSecureProxy(context.Background(), "file::memory:?cache=shared", "test.cloudflareaccess.com", "")
	assert.NoError(t, err)
	assert.NotNil(t, proxy)

	if len(secure) == 0 {
		proxy.accessValidator = nil // Mark as insecure
	}

	return proxy
}
