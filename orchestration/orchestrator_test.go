package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws/wsutil"
	gows "github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/proxy"
	"github.com/cloudflare/cloudflared/tracing"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

var (
	testLogger = zerolog.Logger{}
	testTags   = []tunnelpogs.Tag{
		{
			Name:  "package",
			Value: "orchestration",
		},
		{
			Name:  "purpose",
			Value: "test",
		},
	}
)

// TestUpdateConfiguration tests that
// - configurations can be deserialized
// - proxy can be updated
// - last applied version and error are returned
// - configurations can be deserialized
// - receiving an old version is noop
func TestUpdateConfiguration(t *testing.T) {
	initConfig := &Config{
		Ingress: &ingress.Ingress{},
	}
	orchestrator, err := NewOrchestrator(context.Background(), initConfig, testTags, &testLogger)
	require.NoError(t, err)
	initOriginProxy, err := orchestrator.GetOriginProxy()
	require.NoError(t, err)
	require.IsType(t, &proxy.Proxy{}, initOriginProxy)

	configJSONV2 := []byte(`
{
	"unknown_field": "not_deserialized",
    "originRequest": {
        "connectTimeout": 90,
		"noHappyEyeballs": true
    },
    "ingress": [
        {
            "hostname": "jira.tunnel.org",
			"path": "^\/login",
            "service": "http://192.16.19.1:443",
            "originRequest": {
                "noTLSVerify": true,
                "connectTimeout": 10
            }
        },
		{
            "hostname": "jira.tunnel.org",
            "service": "http://172.32.20.6:80",
            "originRequest": {
                "noTLSVerify": true,
                "connectTimeout": 30
            }
        },
        {
            "service": "http_status:404"
        }
    ],
    "warp-routing": {
        "enabled": true,
        "connectTimeout": 10
    }
}	
`)

	updateWithValidation(t, orchestrator, 2, configJSONV2)
	configV2 := orchestrator.config
	// Validate ingress rule 0
	require.Equal(t, "jira.tunnel.org", configV2.Ingress.Rules[0].Hostname)
	require.True(t, configV2.Ingress.Rules[0].Matches("jira.tunnel.org", "/login"))
	require.True(t, configV2.Ingress.Rules[0].Matches("jira.tunnel.org", "/login/2fa"))
	require.False(t, configV2.Ingress.Rules[0].Matches("jira.tunnel.org", "/users"))
	require.Equal(t, "http://192.16.19.1:443", configV2.Ingress.Rules[0].Service.String())
	require.Len(t, configV2.Ingress.Rules, 3)
	// originRequest of this ingress rule overrides global default
	require.Equal(t, config.CustomDuration{Duration: time.Second * 10}, configV2.Ingress.Rules[0].Config.ConnectTimeout)
	require.Equal(t, true, configV2.Ingress.Rules[0].Config.NoTLSVerify)
	// Inherited from global default
	require.Equal(t, true, configV2.Ingress.Rules[0].Config.NoHappyEyeballs)
	// Validate ingress rule 1
	require.Equal(t, "jira.tunnel.org", configV2.Ingress.Rules[1].Hostname)
	require.True(t, configV2.Ingress.Rules[1].Matches("jira.tunnel.org", "/users"))
	require.Equal(t, "http://172.32.20.6:80", configV2.Ingress.Rules[1].Service.String())
	// originRequest of this ingress rule overrides global default
	require.Equal(t, config.CustomDuration{Duration: time.Second * 30}, configV2.Ingress.Rules[1].Config.ConnectTimeout)
	require.Equal(t, true, configV2.Ingress.Rules[1].Config.NoTLSVerify)
	// Inherited from global default
	require.Equal(t, true, configV2.Ingress.Rules[1].Config.NoHappyEyeballs)
	// Validate ingress rule 2, it's the catch-all rule
	require.True(t, configV2.Ingress.Rules[2].Matches("blogs.tunnel.io", "/2022/02/10"))
	// Inherited from global default
	require.Equal(t, config.CustomDuration{Duration: time.Second * 90}, configV2.Ingress.Rules[2].Config.ConnectTimeout)
	require.Equal(t, false, configV2.Ingress.Rules[2].Config.NoTLSVerify)
	require.Equal(t, true, configV2.Ingress.Rules[2].Config.NoHappyEyeballs)
	require.True(t, configV2.WarpRouting.Enabled)
	require.Equal(t, configV2.WarpRouting.ConnectTimeout.Duration, 10*time.Second)

	originProxyV2, err := orchestrator.GetOriginProxy()
	require.NoError(t, err)
	require.IsType(t, &proxy.Proxy{}, originProxyV2)
	require.NotEqual(t, originProxyV2, initOriginProxy)

	// Should not downgrade to an older version
	resp := orchestrator.UpdateConfig(1, nil)
	require.NoError(t, resp.Err)
	require.Equal(t, int32(2), resp.LastAppliedVersion)

	invalidJSON := []byte(`
{
	"originRequest":
}
	
`)

	resp = orchestrator.UpdateConfig(3, invalidJSON)
	require.Error(t, resp.Err)
	require.Equal(t, int32(2), resp.LastAppliedVersion)
	originProxyV3, err := orchestrator.GetOriginProxy()
	require.NoError(t, err)
	require.Equal(t, originProxyV2, originProxyV3)

	configJSONV10 := []byte(`
{
    "ingress": [
        {
            "service": "hello-world"
        }
    ],
    "warp-routing": {
        "enabled": false
    }
}	
`)
	updateWithValidation(t, orchestrator, 10, configJSONV10)
	configV10 := orchestrator.config
	require.Len(t, configV10.Ingress.Rules, 1)
	require.True(t, configV10.Ingress.Rules[0].Matches("blogs.tunnel.io", "/2022/02/10"))
	require.Equal(t, ingress.HelloWorldService, configV10.Ingress.Rules[0].Service.String())
	require.False(t, configV10.WarpRouting.Enabled)

	originProxyV10, err := orchestrator.GetOriginProxy()
	require.NoError(t, err)
	require.IsType(t, &proxy.Proxy{}, originProxyV10)
	require.NotEqual(t, originProxyV10, originProxyV2)
}

// TestConcurrentUpdateAndRead makes sure orchestrator can receive updates and return origin proxy concurrently
func TestConcurrentUpdateAndRead(t *testing.T) {
	const (
		concurrentRequests = 200
		hostname           = "public.tunnels.org"
		expectedHost       = "internal.tunnels.svc.cluster.local"
		tcpBody            = "testProxyTCP"
	)

	httpOrigin := httptest.NewServer(&validateHostHandler{
		expectedHost: expectedHost,
		body:         t.Name(),
	})
	defer httpOrigin.Close()

	tcpOrigin, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer tcpOrigin.Close()

	var (
		configJSONV1 = []byte(fmt.Sprintf(`
{
    "originRequest": {
        "connectTimeout": 90,
		"noHappyEyeballs": true
    },
    "ingress": [
        {
            "hostname": "%s",
            "service": "%s",
            "originRequest": {
				"httpHostHeader": "%s",
                "connectTimeout": 10
            }
        },
        {
            "service": "http_status:404"
        }
    ],
    "warp-routing": {
        "enabled": true
    }
}
`, hostname, httpOrigin.URL, expectedHost))
		configJSONV2 = []byte(`
{
    "ingress": [
        {
            "service": "http_status:204"
        }
    ],
    "warp-routing": {
        "enabled": false
    }
}
`)

		configJSONV3 = []byte(`
{
    "ingress": [
        {
            "service": "http_status:418"
        }
    ],
    "warp-routing": {
        "enabled": true
    }
}
`)

		// appliedV2 makes sure v3 is applied after v2
		appliedV2 = make(chan struct{})

		initConfig = &Config{
			Ingress: &ingress.Ingress{},
		}
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orchestrator, err := NewOrchestrator(ctx, initConfig, testTags, &testLogger)
	require.NoError(t, err)

	updateWithValidation(t, orchestrator, 1, configJSONV1)

	var wg sync.WaitGroup
	// tcpOrigin will be closed when the test exits. Only the handler routines are included in the wait group
	go func() {
		serveTCPOrigin(t, tcpOrigin, &wg)
	}()
	for i := 0; i < concurrentRequests; i++ {
		originProxy, err := orchestrator.GetOriginProxy()
		require.NoError(t, err)
		wg.Add(1)
		go func(i int, originProxy connection.OriginProxy) {
			defer wg.Done()
			resp, err := proxyHTTP(originProxy, hostname)
			require.NoError(t, err, "proxyHTTP %d failed %v", i, err)
			defer resp.Body.Close()

			var warpRoutingDisabled bool
			// The response can be from initOrigin, http_status:204 or http_status:418
			switch resp.StatusCode {
			// v1 proxy, warp enabled
			case 200:
				body, err := ioutil.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Equal(t, t.Name(), string(body))
				warpRoutingDisabled = false
			// v2 proxy, warp disabled
			case 204:
				require.Greater(t, i, concurrentRequests/4)
				warpRoutingDisabled = true
			// v3 proxy, warp enabled
			case 418:
				require.Greater(t, i, concurrentRequests/2)
				warpRoutingDisabled = false
			}

			// Once we have originProxy, it won't be changed by configuration updates.
			// We can infer the version by the ProxyHTTP response code
			pr, pw := io.Pipe()

			w := newRespReadWriteFlusher()

			// Write TCP message and make sure it's echo back. This has to be done in a go routune since ProxyTCP doesn't
			// return until the stream is closed.
			if !warpRoutingDisabled {
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer pw.Close()
					tcpEyeball(t, pw, tcpBody, w)
				}()
			}

			err = proxyTCP(ctx, originProxy, tcpOrigin.Addr().String(), w, pr)
			if warpRoutingDisabled {
				require.Error(t, err, "expect proxyTCP %d to return error", i)
			} else {
				require.NoError(t, err, "proxyTCP %d failed %v", i, err)
			}

		}(i, originProxy)

		if i == concurrentRequests/4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				updateWithValidation(t, orchestrator, 2, configJSONV2)
				close(appliedV2)
			}()
		}

		if i == concurrentRequests/2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Makes sure v2 is applied before v3
				<-appliedV2
				updateWithValidation(t, orchestrator, 3, configJSONV3)
			}()
		}
	}

	wg.Wait()
}

func proxyHTTP(originProxy connection.OriginProxy, hostname string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", hostname), nil)
	if err != nil {
		return nil, err
	}

	w := httptest.NewRecorder()
	log := zerolog.Nop()
	respWriter, err := connection.NewHTTP2RespWriter(req, w, connection.TypeHTTP, &log)
	if err != nil {
		return nil, err
	}

	err = originProxy.ProxyHTTP(respWriter, tracing.NewTracedRequest(req), false)
	if err != nil {
		return nil, err
	}

	return w.Result(), nil
}

func tcpEyeball(t *testing.T, reqWriter io.WriteCloser, body string, respReadWriter *respReadWriteFlusher) {
	writeN, err := reqWriter.Write([]byte(body))
	require.NoError(t, err)

	readBuffer := make([]byte, writeN)
	n, err := respReadWriter.Read(readBuffer)
	require.NoError(t, err)
	require.Equal(t, body, string(readBuffer[:n]))
	require.Equal(t, writeN, n)
}

func proxyTCP(ctx context.Context, originProxy connection.OriginProxy, originAddr string, w http.ResponseWriter, reqBody io.ReadCloser) error {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", originAddr), reqBody)
	if err != nil {
		return err
	}

	log := zerolog.Nop()
	respWriter, err := connection.NewHTTP2RespWriter(req, w, connection.TypeTCP, &log)
	if err != nil {
		return err
	}

	tcpReq := &connection.TCPRequest{
		Dest:    originAddr,
		CFRay:   "123",
		LBProbe: false,
	}
	rws := connection.NewHTTPResponseReadWriterAcker(respWriter, req)

	return originProxy.ProxyTCP(ctx, rws, tcpReq)
}

func serveTCPOrigin(t *testing.T, tcpOrigin net.Listener, wg *sync.WaitGroup) {
	for {
		conn, err := tcpOrigin.Accept()
		if err != nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer conn.Close()

			echoTCP(t, conn)
		}()
	}
}

func echoTCP(t *testing.T, conn net.Conn) {
	readBuf := make([]byte, 1000)
	readN, err := conn.Read(readBuf)
	require.NoError(t, err)

	writeN, err := conn.Write(readBuf[:readN])
	require.NoError(t, err)
	require.Equal(t, readN, writeN)
}

type validateHostHandler struct {
	expectedHost string
	body         string
}

func (vhh *validateHostHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Host != vhh.expectedHost {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(vhh.body))
}

func updateWithValidation(t *testing.T, orchestrator *Orchestrator, version int32, config []byte) {
	resp := orchestrator.UpdateConfig(version, config)
	require.NoError(t, resp.Err)
	require.Equal(t, version, resp.LastAppliedVersion)
}

// TestClosePreviousProxies makes sure proxies started in the pervious configuration version are shutdown
func TestClosePreviousProxies(t *testing.T) {
	var (
		hostname             = "hello.tunnel1.org"
		configWithHelloWorld = []byte(fmt.Sprintf(`
{
    "ingress": [
        {
			"hostname": "%s",
            "service": "hello-world"
        },
		{
			"service": "http_status:404"
		}
    ],
    "warp-routing": {
        "enabled": true
    }
}
`, hostname))

		configTeapot = []byte(`
{
    "ingress": [
		{
			"service": "http_status:418"
		}
    ],
    "warp-routing": {
        "enabled": true
    }
}
`)
		initConfig = &Config{
			Ingress: &ingress.Ingress{},
		}
	)

	ctx, cancel := context.WithCancel(context.Background())
	orchestrator, err := NewOrchestrator(ctx, initConfig, testTags, &testLogger)
	require.NoError(t, err)

	updateWithValidation(t, orchestrator, 1, configWithHelloWorld)

	originProxyV1, err := orchestrator.GetOriginProxy()
	require.NoError(t, err)
	resp, err := proxyHTTP(originProxyV1, hostname)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	updateWithValidation(t, orchestrator, 2, configTeapot)

	originProxyV2, err := orchestrator.GetOriginProxy()
	require.NoError(t, err)
	resp, err = proxyHTTP(originProxyV2, hostname)
	require.NoError(t, err)
	require.Equal(t, http.StatusTeapot, resp.StatusCode)

	// The hello-world server in config v1 should have been stopped
	resp, err = proxyHTTP(originProxyV1, hostname)
	require.Error(t, err)
	require.Nil(t, resp)

	// Apply the config with hello world server again, orchestrator should spin up another hello world server
	updateWithValidation(t, orchestrator, 3, configWithHelloWorld)

	originProxyV3, err := orchestrator.GetOriginProxy()
	require.NoError(t, err)
	require.NotEqual(t, originProxyV1, originProxyV3)

	resp, err = proxyHTTP(originProxyV3, hostname)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// cancel the context should terminate the last proxy
	cancel()
	// Wait for proxies to shutdown
	time.Sleep(time.Millisecond * 10)

	resp, err = proxyHTTP(originProxyV3, hostname)
	require.Error(t, err)
	require.Nil(t, resp)
}

// TestPersistentConnection makes sure updating the ingress doesn't intefere with existing connections
func TestPersistentConnection(t *testing.T) {
	const (
		hostname = "http://ws.tunnel.org"
	)
	msg := t.Name()
	initConfig := &Config{
		Ingress: &ingress.Ingress{},
	}
	orchestrator, err := NewOrchestrator(context.Background(), initConfig, testTags, &testLogger)
	require.NoError(t, err)

	wsOrigin := httptest.NewServer(http.HandlerFunc(wsEcho))
	defer wsOrigin.Close()

	tcpOrigin, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer tcpOrigin.Close()

	configWithWSAndWarp := []byte(fmt.Sprintf(`
{
    "ingress": [
        {
            "service": "%s"
        }
    ],
    "warp-routing": {
        "enabled": true
    }
}
`, wsOrigin.URL))

	updateWithValidation(t, orchestrator, 1, configWithWSAndWarp)

	originProxy, err := orchestrator.GetOriginProxy()
	require.NoError(t, err)

	wsReqReader, wsReqWriter := io.Pipe()
	wsRespReadWriter := newRespReadWriteFlusher()

	tcpReqReader, tcpReqWriter := io.Pipe()
	tcpRespReadWriter := newRespReadWriteFlusher()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(3)
	// Start TCP origin
	go func() {
		defer wg.Done()
		conn, err := tcpOrigin.Accept()
		require.NoError(t, err)
		defer conn.Close()

		// Expect 3 TCP messages
		for i := 0; i < 3; i++ {
			echoTCP(t, conn)
		}
	}()
	// Simulate cloudflared recieving a TCP connection
	go func() {
		defer wg.Done()
		require.NoError(t, proxyTCP(ctx, originProxy, tcpOrigin.Addr().String(), tcpRespReadWriter, tcpReqReader))
	}()
	// Simulate cloudflared recieving a WS connection
	go func() {
		defer wg.Done()

		req, err := http.NewRequest(http.MethodGet, hostname, wsReqReader)
		require.NoError(t, err)
		// ProxyHTTP will add Connection, Upgrade and Sec-Websocket-Version headers
		req.Header.Add("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

		log := zerolog.Nop()
		respWriter, err := connection.NewHTTP2RespWriter(req, wsRespReadWriter, connection.TypeWebsocket, &log)
		require.NoError(t, err)

		err = originProxy.ProxyHTTP(respWriter, tracing.NewTracedRequest(req), true)
		require.NoError(t, err)
	}()

	// Simulate eyeball WS and TCP connections
	validateWsEcho(t, msg, wsReqWriter, wsRespReadWriter)
	tcpEyeball(t, tcpReqWriter, msg, tcpRespReadWriter)

	configNoWSAndWarp := []byte(`
{
    "ingress": [
        {
            "service": "http_status:404"
        }
    ],
    "warp-routing": {
        "enabled": false
    }
}
`)

	updateWithValidation(t, orchestrator, 2, configNoWSAndWarp)
	// Make sure connection is still up
	validateWsEcho(t, msg, wsReqWriter, wsRespReadWriter)
	tcpEyeball(t, tcpReqWriter, msg, tcpRespReadWriter)

	updateWithValidation(t, orchestrator, 3, configWithWSAndWarp)
	// Make sure connection is still up
	validateWsEcho(t, msg, wsReqWriter, wsRespReadWriter)
	tcpEyeball(t, tcpReqWriter, msg, tcpRespReadWriter)

	wsReqWriter.Close()
	tcpReqWriter.Close()
	wg.Wait()
}

func TestSerializeLocalConfig(t *testing.T) {
	c := &newLocalConfig{
		RemoteConfig: ingress.RemoteConfig{
			Ingress: ingress.Ingress{},
		},
		ConfigurationFlags: map[string]string{"a": "b"},
	}

	result, _ := json.Marshal(c)
	fmt.Println(string(result))
}

func wsEcho(w http.ResponseWriter, r *http.Request) {
	upgrader := gows.Upgrader{}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	for {
		mt, message, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("read message err", err)
			break
		}
		err = conn.WriteMessage(mt, message)
		if err != nil {
			fmt.Println("write message err", err)
			break
		}
	}
}

func validateWsEcho(t *testing.T, msg string, reqWriter io.Writer, respReadWriter io.ReadWriter) {
	err := wsutil.WriteClientText(reqWriter, []byte(msg))
	require.NoError(t, err)

	receivedMsg, err := wsutil.ReadServerText(respReadWriter)
	require.NoError(t, err)
	require.Equal(t, msg, string(receivedMsg))
}

type respReadWriteFlusher struct {
	io.Reader
	w             io.Writer
	headers       http.Header
	statusCode    int
	setStatusOnce sync.Once
	hasStatus     chan struct{}
}

func newRespReadWriteFlusher() *respReadWriteFlusher {
	pr, pw := io.Pipe()
	return &respReadWriteFlusher{
		Reader:    pr,
		w:         pw,
		headers:   make(http.Header),
		hasStatus: make(chan struct{}),
	}
}

func (rrw *respReadWriteFlusher) Write(buf []byte) (int, error) {
	rrw.WriteHeader(http.StatusOK)
	return rrw.w.Write(buf)
}

func (rrw *respReadWriteFlusher) Flush() {}

func (rrw *respReadWriteFlusher) Header() http.Header {
	return rrw.headers
}

func (rrw *respReadWriteFlusher) WriteHeader(statusCode int) {
	rrw.setStatusOnce.Do(func() {
		rrw.statusCode = statusCode
		close(rrw.hasStatus)
	})
}
