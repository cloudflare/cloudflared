package prechecks

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"testing"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/mocks"
)

// Test constants for repeated string values.
const (
	testRegion1Global = region1Global
	testRegion2Global = region2Global
	testRegion1US     = region1US
	testRegion2US     = region2US
	testRegion1Fed    = region1Fed
	testRegion2Fed    = region2Fed

	testEdgePort = 7844
)

// testTLSConfig is a minimal *tls.Config for tests. Mock dialers never
// perform a real TLS handshake, so an empty config is sufficient.
var testTLSConfig = &tls.Config{} //nolint:gosec

// mockQuicConnection is a minimal test double for quic.Connection.
type mockQuicConnection struct {
	closeErr error
}

func (m *mockQuicConnection) AcceptStream(_ context.Context) (quic.Stream, error) {
	return nil, nil
}

func (m *mockQuicConnection) AcceptUniStream(_ context.Context) (quic.ReceiveStream, error) {
	return nil, nil
}

func (m *mockQuicConnection) OpenStream() (quic.Stream, error) {
	return nil, nil
}

func (m *mockQuicConnection) OpenStreamSync(_ context.Context) (quic.Stream, error) {
	return nil, nil
}

func (m *mockQuicConnection) OpenUniStream() (quic.SendStream, error) {
	return nil, nil
}

func (m *mockQuicConnection) OpenUniStreamSync(_ context.Context) (quic.SendStream, error) {
	return nil, nil
}

func (m *mockQuicConnection) LocalAddr() net.Addr {
	return nil
}

func (m *mockQuicConnection) RemoteAddr() net.Addr {
	return nil
}

func (m *mockQuicConnection) CloseWithError(_ quic.ApplicationErrorCode, _ string) error {
	return m.closeErr
}

func (m *mockQuicConnection) Context() context.Context {
	return context.Background()
}

func (m *mockQuicConnection) ConnectionState() quic.ConnectionState {
	return quic.ConnectionState{}
}

func (m *mockQuicConnection) SendDatagram(_ []byte) error {
	return nil
}

func (m *mockQuicConnection) ReceiveDatagram(_ context.Context) ([]byte, error) {
	return nil, nil
}

func (m *mockQuicConnection) AddPath(*quic.Transport) (*quic.Path, error) {
	return nil, nil
}

// Helper to create test edge addresses.
func createTestEdgeAddr(ip string, port int, version allregions.EdgeIPVersion) *allregions.EdgeAddr {
	parsedIP := net.ParseIP(ip)
	return &allregions.EdgeAddr{
		TCP:       &net.TCPAddr{IP: parsedIP, Port: port},
		UDP:       &net.UDPAddr{IP: parsedIP, Port: port},
		IPVersion: version,
	}
}

// probeDNS tests.

func TestProbeDNS_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	v4Addr := createTestEdgeAddr("192.0.2.1", testEdgePort, allregions.V4)
	v6Addr := createTestEdgeAddr("2001:db8::1", testEdgePort, allregions.V6)

	resolver := mocks.NewMockDNSResolver(ctrl)
	resolver.EXPECT().Resolve("").Return([][]*allregions.EdgeAddr{{v4Addr, v6Addr}}, nil)

	addrs, results := probeDNS(resolver, "")

	require.NotNil(t, addrs)
	require.Len(t, results, 1)
	assert.Len(t, addrs, 1)
	assert.Equal(t, ProbeTypeDNS, results[0].Type)
	assert.Equal(t, testRegion1Global, results[0].Target)
	assert.Equal(t, Pass, results[0].ProbeStatus)
	assert.Equal(t, detailsResolvedSuccessfully, results[0].Details)
}

func TestProbeDNS_MultipleRegions(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	v4Addr1 := createTestEdgeAddr("192.0.2.1", testEdgePort, allregions.V4)
	v4Addr2 := createTestEdgeAddr("192.0.2.2", testEdgePort, allregions.V4)

	resolver := mocks.NewMockDNSResolver(ctrl)
	resolver.EXPECT().Resolve("").Return([][]*allregions.EdgeAddr{{v4Addr1}, {v4Addr2}}, nil)

	addrs, results := probeDNS(resolver, "")

	require.NotNil(t, addrs)
	require.Len(t, results, 2)
	assert.Len(t, addrs, 2)

	assert.Equal(t, testRegion1Global, results[0].Target)
	assert.Equal(t, Pass, results[0].ProbeStatus)

	assert.Equal(t, testRegion2Global, results[1].Target)
	assert.Equal(t, Pass, results[1].ProbeStatus)
}

func TestProbeDNS_ResolverError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	resolver := mocks.NewMockDNSResolver(ctrl)
	resolver.EXPECT().Resolve("").Return(nil, errors.New("DNS lookup failed"))

	addrs, results := probeDNS(resolver, "")

	assert.Nil(t, addrs)
	require.Len(t, results, 2)

	assert.Equal(t, Fail, results[0].ProbeStatus)
	assert.Equal(t, "DNS lookup failed", results[0].Details)
	assert.Contains(t, results[0].Action, testRegion1Global)
	assert.Contains(t, results[1].Action, testRegion2Global)

	assert.Equal(t, Fail, results[1].ProbeStatus)
}

func TestProbeDNS_EmptyResults(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	resolver := mocks.NewMockDNSResolver(ctrl)
	resolver.EXPECT().Resolve("").Return([][]*allregions.EdgeAddr{}, nil)

	addrs, results := probeDNS(resolver, "")

	assert.Nil(t, addrs)
	require.Len(t, results, 2)
	assert.Equal(t, Fail, results[0].ProbeStatus)
	assert.Equal(t, "No addresses returned", results[0].Details)
}

func TestProbeDNS_EmptyGroup(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	resolver := mocks.NewMockDNSResolver(ctrl)
	resolver.EXPECT().Resolve("").Return([][]*allregions.EdgeAddr{{}}, nil)

	addrs, results := probeDNS(resolver, "")

	require.NotNil(t, addrs)
	require.Len(t, results, 1)
	assert.Equal(t, Fail, results[0].ProbeStatus)
	assert.Equal(t, "No addresses returned", results[0].Details)
}

func TestProbeDNS_RegionFlag(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	v4Addr := createTestEdgeAddr("192.0.2.1", testEdgePort, allregions.V4)
	resolver := mocks.NewMockDNSResolver(ctrl)
	resolver.EXPECT().Resolve("us").Return([][]*allregions.EdgeAddr{{v4Addr}}, nil)

	_, results := probeDNS(resolver, "us")

	require.Len(t, results, 1)
	assert.Equal(t, testRegion1US, results[0].Target)
}

// probeQUIC tests.

func TestProbeQUIC_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConn := &mockQuicConnection{}
	dialer := mocks.NewMockQUICDialer(ctrl)
	dialer.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(mockConn, nil)

	addr := createTestEdgeAddr("192.0.2.1", testEdgePort, allregions.V4)
	logger := zerolog.New(nil)

	result := probeQUIC(context.Background(), testTLSConfig, dialer, addr, &logger)

	assert.Equal(t, ProbeTypeQUIC, result.Type)
	assert.Equal(t, Pass, result.ProbeStatus)
	assert.Equal(t, detailsHandshakeSuccessful, result.Details)
}

func TestProbeQUIC_DialError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dialer := mocks.NewMockQUICDialer(ctrl)
	dialer.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("connection refused"))

	addr := createTestEdgeAddr("192.0.2.1", testEdgePort, allregions.V4)
	logger := zerolog.New(nil)

	result := probeQUIC(context.Background(), testTLSConfig, dialer, addr, &logger)

	assert.Equal(t, ProbeTypeQUIC, result.Type)
	assert.Equal(t, Fail, result.ProbeStatus)
	assert.Equal(t, detailsHandshakeFailed, result.Details)
	assert.Equal(t, actionQUICBlocked, result.Action)
}

func TestProbeQUIC_CloseErrorDoesNotAffectResult(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConn := &mockQuicConnection{closeErr: errors.New("close failed")}
	dialer := mocks.NewMockQUICDialer(ctrl)
	dialer.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(mockConn, nil)

	addr := createTestEdgeAddr("192.0.2.1", testEdgePort, allregions.V4)
	logger := zerolog.New(nil)

	result := probeQUIC(context.Background(), testTLSConfig, dialer, addr, &logger)

	assert.Equal(t, ProbeTypeQUIC, result.Type)
	assert.Equal(t, Pass, result.ProbeStatus)
	assert.Equal(t, detailsHandshakeSuccessful, result.Details)
}

func TestProbeQUIC_ContextTimeout(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dialer := mocks.NewMockQUICDialer(ctrl)
	dialer.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, context.DeadlineExceeded)

	addr := createTestEdgeAddr("192.0.2.1", testEdgePort, allregions.V4)
	logger := zerolog.New(nil)

	result := probeQUIC(context.Background(), testTLSConfig, dialer, addr, &logger)

	assert.Equal(t, Fail, result.ProbeStatus)
	assert.Equal(t, detailsHandshakeFailed, result.Details)
}

// probeHTTP2 tests.

func TestProbeHTTP2_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dialer := mocks.NewMockTCPDialer(ctrl)
	dialer.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&net.TCPConn{}, nil)

	addr := createTestEdgeAddr("192.0.2.1", testEdgePort, allregions.V4)

	result := probeHTTP2(context.Background(), testTLSConfig, dialer, addr)

	assert.Equal(t, ProbeTypeHTTP2, result.Type)
	assert.Equal(t, Pass, result.ProbeStatus)
	assert.Equal(t, detailsTLSHandshakeSuccessful, result.Details)
}

func TestProbeHTTP2_DialError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dialer := mocks.NewMockTCPDialer(ctrl)
	dialer.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("connection refused"))

	addr := createTestEdgeAddr("192.0.2.1", testEdgePort, allregions.V4)

	result := probeHTTP2(context.Background(), testTLSConfig, dialer, addr)

	assert.Equal(t, ProbeTypeHTTP2, result.Type)
	assert.Equal(t, Fail, result.ProbeStatus)
	assert.Equal(t, detailsBlockedOrUnreachable, result.Details)
	assert.Equal(t, actionHTTP2Blocked, result.Action)
}

// probeManagementAPI tests.

func TestProbeManagementAPI_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dialer := mocks.NewMockManagementDialer(ctrl)
	dialer.EXPECT().DialContext(gomock.Any(), "tcp", "api.cloudflare.com:443").Return(&net.TCPConn{}, nil)

	result := probeManagementAPI(context.Background(), dialer)

	assert.Equal(t, ProbeTypeManagementAPI, result.Type)
	assert.Equal(t, "Cloudflare API", result.Component)
	assert.Equal(t, "api.cloudflare.com:443", result.Target)
	assert.Equal(t, Pass, result.ProbeStatus)
	assert.Equal(t, detailsTCPPortReachable, result.Details)
}

func TestProbeManagementAPI_DialError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dialer := mocks.NewMockManagementDialer(ctrl)
	dialer.EXPECT().DialContext(gomock.Any(), "tcp", "api.cloudflare.com:443").Return(nil, errors.New("connection refused"))

	result := probeManagementAPI(context.Background(), dialer)

	assert.Equal(t, ProbeTypeManagementAPI, result.Type)
	assert.Equal(t, Fail, result.ProbeStatus)
	assert.Equal(t, detailsConnectionFailed, result.Details)
	assert.Equal(t, actionAPIUnreachable, result.Action)
}

// skipResult tests.

func TestSkipResult(t *testing.T) {
	t.Parallel()

	result := skipResult(ProbeTypeQUIC, "UDP Connectivity", "Port 7844 (QUIC)")

	assert.Equal(t, ProbeTypeQUIC, result.Type)
	assert.Equal(t, "UDP Connectivity", result.Component)
	assert.Equal(t, "Port 7844 (QUIC)", result.Target)
	assert.Equal(t, Skip, result.ProbeStatus)
	assert.Equal(t, detailsDNSPrerequisiteFailed, result.Details)
}

// regionTargets tests.

func TestRegionTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		region      string
		wantRegion1 string
		wantRegion2 string
		description string
	}{
		{
			name:        "empty region returns global hostnames",
			region:      "",
			wantRegion1: testRegion1Global,
			wantRegion2: testRegion2Global,
		},
		{
			name:        "us region returns US hostnames",
			region:      "us",
			wantRegion1: testRegion1US,
			wantRegion2: testRegion2US,
		},
		{
			name:        "fed region returns fed hostnames",
			region:      "fed",
			wantRegion1: testRegion1Fed,
			wantRegion2: testRegion2Fed,
		},
		{
			name:        "unknown region defaults to global hostnames",
			region:      "eu",
			wantRegion1: testRegion1Global,
			wantRegion2: testRegion2Global,
			description: "Unknown regions should default to global hostnames",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotR1, gotR2 := regionTargets(tt.region)
			assert.Equal(t, tt.wantRegion1, gotR1)
			assert.Equal(t, tt.wantRegion2, gotR2)
		})
	}
}

// addrsByFamily tests.

func TestAddrsByFamily(t *testing.T) {
	t.Parallel()

	v4Addr := createTestEdgeAddr("192.0.2.1", testEdgePort, allregions.V4)
	v6Addr := createTestEdgeAddr("2001:db8::1", testEdgePort, allregions.V6)

	tests := []struct {
		name      string
		group     []*allregions.EdgeAddr
		ipVersion allregions.ConfigIPVersion
		wantV4    bool
		wantV6    bool
	}{
		{
			name:      "auto returns both v4 and v6",
			group:     []*allregions.EdgeAddr{v4Addr, v6Addr},
			ipVersion: allregions.Auto,
			wantV4:    true,
			wantV6:    true,
		},
		{
			name:      "ipv4 only returns v4 and nil v6",
			group:     []*allregions.EdgeAddr{v4Addr, v6Addr},
			ipVersion: allregions.IPv4Only,
			wantV4:    true,
			wantV6:    false,
		},
		{
			name:      "ipv6 only returns nil v4 and v6",
			group:     []*allregions.EdgeAddr{v4Addr, v6Addr},
			ipVersion: allregions.IPv6Only,
			wantV4:    false,
			wantV6:    true,
		},
		{
			name:      "empty group returns nil for both",
			group:     []*allregions.EdgeAddr{},
			ipVersion: allregions.Auto,
			wantV4:    false,
			wantV6:    false,
		},
		{
			name:      "only v4 available returns v4 and nil v6",
			group:     []*allregions.EdgeAddr{v4Addr},
			ipVersion: allregions.Auto,
			wantV4:    true,
			wantV6:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotV4, gotV6 := addrsByFamily(tt.group, tt.ipVersion)
			if tt.wantV4 {
				assert.NotNil(t, gotV4)
			} else {
				assert.Nil(t, gotV4)
			}
			if tt.wantV6 {
				assert.NotNil(t, gotV6)
			} else {
				assert.Nil(t, gotV6)
			}
		})
	}
}

// IPv6 address tests for probeQUIC.

func TestProbeQUIC_IPv6Address(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConn := &mockQuicConnection{}
	dialer := mocks.NewMockQUICDialer(ctrl)
	dialer.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(mockConn, nil)

	addr := createTestEdgeAddr("2001:db8::1", testEdgePort, allregions.V6)
	logger := zerolog.New(nil)

	result := probeQUIC(context.Background(), testTLSConfig, dialer, addr, &logger)

	assert.Equal(t, Pass, result.ProbeStatus)
	assert.Equal(t, detailsHandshakeSuccessful, result.Details)
}

// IPv6 address tests for probeHTTP2.

func TestProbeHTTP2_IPv6Address(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dialer := mocks.NewMockTCPDialer(ctrl)
	dialer.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&net.TCPConn{}, nil)

	addr := createTestEdgeAddr("2001:db8::1", testEdgePort, allregions.V6)

	result := probeHTTP2(context.Background(), testTLSConfig, dialer, addr)

	assert.Equal(t, Pass, result.ProbeStatus)
}
