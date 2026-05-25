package prechecks

import (
	"errors"
	"math"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/mocks"
)

const (
	emptyCert = ""
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// twoRegionAddrs returns a two-group [][]*EdgeAddr with one IPv4 address per
// region. Used by tests that only need to exercise the V4 path.
func twoRegionAddrs() [][]*allregions.EdgeAddr {
	makeV4 := func(ip string) *allregions.EdgeAddr {
		parsed := net.ParseIP(ip)
		return &allregions.EdgeAddr{
			TCP:       &net.TCPAddr{IP: parsed, Port: 7844},
			UDP:       &net.UDPAddr{IP: parsed, Port: 7844},
			IPVersion: allregions.V4,
		}
	}
	return [][]*allregions.EdgeAddr{
		{makeV4("1.2.3.4")},
		{makeV4("5.6.7.8")},
	}
}

// twoRegionAddrsBothFamilies returns a two-group [][]*EdgeAddr with one IPv4
// and one IPv6 address per region, used by per-family probe tests.
func twoRegionAddrsBothFamilies() [][]*allregions.EdgeAddr {
	makeAddr := func(ip string, v allregions.EdgeIPVersion) *allregions.EdgeAddr {
		parsed := net.ParseIP(ip)
		return &allregions.EdgeAddr{
			TCP:       &net.TCPAddr{IP: parsed, Port: 7844},
			UDP:       &net.UDPAddr{IP: parsed, Port: 7844},
			IPVersion: v,
		}
	}
	return [][]*allregions.EdgeAddr{
		{makeAddr("1.2.3.4", allregions.V4), makeAddr("2001:db8::1", allregions.V6)},
		{makeAddr("5.6.7.8", allregions.V4), makeAddr("2001:db8::2", allregions.V6)},
	}
}

// nopConn is a net.Conn whose Close() is a no-op, used as the success value
// for TCP and management dial mocks.
type nopConn struct{ net.Conn }

func (nopConn) Close() error { return nil }

// fakeQUICConn satisfies quic.Connection for tests. Only CloseWithError is
// implemented; the pre-check never opens streams so the rest of the interface
// is unused via the embedded nil.
type fakeQUICConn struct {
	quic.Connection
}

func (*fakeQUICConn) CloseWithError(_ quic.ApplicationErrorCode, _ string) error { return nil }

// requireStatuses asserts the probe statuses in report.Results match
// expected (in order), failing immediately on length mismatch.
func requireStatuses(t *testing.T, report Report, expected ...Status) {
	t.Helper()
	require.Len(t, report.Results, len(expected))
	for i, want := range expected {
		got := report.Results[i].ProbeStatus
		assert.Equalf(t, want, got,
			"result[%d] (%s/%s): got %s, want %s",
			i, report.Results[i].Component, report.Results[i].Target, got, want)
	}
}

func nopLogger() *zerolog.Logger {
	l := zerolog.Nop()
	return &l
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRun_AllPass verifies that when all probes succeed the report contains
// 7 rows: 2 DNS + 2 QUIC (one per region) + 2 HTTP/2 (one per region) + 1 API.
func TestRun_AllPass(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	dns := mocks.NewMockDNSResolver(ctrl)
	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	dns.EXPECT().Resolve(gomock.Any()).Return(twoRegionAddrs(), nil)
	// twoRegionAddrs has 2 regions × 1 V4 address each = 2 dials per transport.
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil).Times(2)
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&fakeQUICConn{}, nil).Times(2)
	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil)

	report := Run(t.Context(), emptyCert, Config{Timeout: 2 * time.Second, IPVersion: allregions.Auto},
		nopLogger(), RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

	// 2 DNS + 2 QUIC + 2 HTTP2 + 1 API = 7 results.
	requireStatuses(t, report, Pass, Pass, Pass, Pass, Pass, Pass, Pass)
	assert.NotEqual(t, uuid.Nil, report.RunID, "RunID must be set")
	require.NotNil(t, report.SuggestedProtocol)
	assert.Equal(t, connection.QUIC, *report.SuggestedProtocol)
	assert.False(t, report.hasHardFail())
	assert.False(t, report.hasWarn())
}

// TestRun_QUICBlocked verifies that when QUIC is blocked on all regions,
// the report is degraded (warn) and HTTP/2 is the suggested protocol.
func TestRun_QUICBlocked(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	dns := mocks.NewMockDNSResolver(ctrl)
	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	dns.EXPECT().Resolve(gomock.Any()).Return(twoRegionAddrs(), nil)
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil).AnyTimes()
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, errors.New("connection refused")).AnyTimes()
	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil)

	report := Run(t.Context(), emptyCert, Config{Timeout: 2 * time.Second, IPVersion: allregions.Auto},
		nopLogger(), RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

	// 2 DNS Pass + 2 QUIC Fail + 2 HTTP2 Pass + 1 API Pass.
	requireStatuses(t, report, Pass, Pass, Fail, Fail, Pass, Pass, Pass)
	require.NotNil(t, report.SuggestedProtocol)
	assert.Equal(t, connection.HTTP2, *report.SuggestedProtocol)
	assert.False(t, report.hasHardFail())
	assert.True(t, report.hasWarn())
}

// TestRun_HTTP2Blocked verifies that when HTTP/2 is blocked on all regions,
// the report is degraded (warn) and QUIC is the suggested protocol.
func TestRun_HTTP2Blocked(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	dns := mocks.NewMockDNSResolver(ctrl)
	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	dns.EXPECT().Resolve(gomock.Any()).Return(twoRegionAddrs(), nil)
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, errors.New("connection refused")).AnyTimes()
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&fakeQUICConn{}, nil).AnyTimes()
	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil)

	report := Run(t.Context(), emptyCert, Config{Timeout: 2 * time.Second, IPVersion: allregions.Auto},
		nopLogger(), RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

	// 2 DNS Pass + 2 QUIC Pass + 2 HTTP2 Fail + 1 API Pass.
	requireStatuses(t, report, Pass, Pass, Pass, Pass, Fail, Fail, Pass)
	require.NotNil(t, report.SuggestedProtocol)
	assert.Equal(t, connection.QUIC, *report.SuggestedProtocol)
	assert.False(t, report.hasHardFail())
	assert.True(t, report.hasWarn())
}

// TestRun_BothTransportsBlocked verifies that when both QUIC and HTTP/2 are
// blocked on all regions it is a hard fail with no suggested protocol.
func TestRun_BothTransportsBlocked(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	dns := mocks.NewMockDNSResolver(ctrl)
	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	dns.EXPECT().Resolve(gomock.Any()).Return(twoRegionAddrs(), nil)
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, errors.New("blocked")).AnyTimes()
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, errors.New("blocked")).AnyTimes()
	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil)

	report := Run(t.Context(), emptyCert, Config{Timeout: 2 * time.Second, IPVersion: allregions.Auto},
		nopLogger(), RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

	// 2 DNS Pass + 2 QUIC Fail + 2 HTTP2 Fail + 1 API Pass.
	requireStatuses(t, report, Pass, Pass, Fail, Fail, Fail, Fail, Pass)
	assert.Nil(t, report.SuggestedProtocol)
	assert.True(t, report.hasHardFail())
}

// TestRun_PartialRegionQUICFail verifies "worst wins" semantics: when QUIC
// passes for region1 but fails for region2, QUIC is treated as failed and
// HTTP/2 becomes the suggested protocol.
func TestRun_PartialRegionQUICFail(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	dns := mocks.NewMockDNSResolver(ctrl)
	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	// Two regions: 1.2.3.4 (region1) and 5.6.7.8 (region2).
	dns.EXPECT().Resolve(gomock.Any()).Return(twoRegionAddrs(), nil)

	// TCP/HTTP2: both regions pass.
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil).AnyTimes()

	// QUIC: region1 (1.2.3.4) passes, region2 (5.6.7.8) fails.
	region1Addr := &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 7844}
	region2Addr := &net.UDPAddr{IP: net.ParseIP("5.6.7.8"), Port: 7844}
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), region1Addr.AddrPort(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&fakeQUICConn{}, nil).AnyTimes()
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), region2Addr.AddrPort(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, errors.New("connection refused")).AnyTimes()

	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil)

	report := Run(t.Context(), emptyCert, Config{Timeout: 2 * time.Second, IPVersion: allregions.Auto},
		nopLogger(), RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

	// 2 DNS Pass + QUIC-region1 Pass + QUIC-region2 Fail + 2 HTTP2 Pass + 1 API Pass.
	requireStatuses(t, report, Pass, Pass, Pass, Fail, Pass, Pass, Pass)

	// Worst wins: region2 QUIC failed, so QUIC is treated as failed overall.
	// HTTP/2 passes on all regions → HTTP/2 is the suggested protocol.
	require.NotNil(t, report.SuggestedProtocol)
	assert.Equal(t, connection.HTTP2, *report.SuggestedProtocol)
	assert.False(t, report.hasHardFail())
	assert.True(t, report.hasWarn())
}

// TestRun_DNSFail_SkipsTransports verifies that when DNS fails, transport rows
// are emitted as Skip (one per DNS region) and no transport dials are made.
func TestRun_DNSFail_SkipsTransports(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	dns := mocks.NewMockDNSResolver(ctrl)
	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	dns.EXPECT().Resolve(gomock.Any()).
		Return(nil, errors.New("no such host")).AnyTimes()
	// Transport dialers must NOT be called when DNS fails.
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil)

	report := Run(t.Context(), emptyCert, Config{Timeout: 2 * time.Second, IPVersion: allregions.Auto},
		nopLogger(), RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

	// DNS failure emits 2 Fail rows (one per default region).
	// Transport rows: one skip per DNS region for QUIC and HTTP/2 = 2 QUIC skips + 2 HTTP2 skips.
	// 2 DNS Fail + 2 QUIC Skip + 2 HTTP2 Skip + 1 API Pass = 7 results.
	require.Len(t, report.Results, 7)
	assert.Equal(t, Fail, report.Results[0].ProbeStatus, "DNS region1")
	assert.Equal(t, Fail, report.Results[1].ProbeStatus, "DNS region2")
	assert.Equal(t, Skip, report.Results[2].ProbeStatus, "QUIC region1 must be skipped")
	assert.Equal(t, Skip, report.Results[3].ProbeStatus, "QUIC region2 must be skipped")
	assert.Equal(t, Skip, report.Results[4].ProbeStatus, "HTTP/2 region1 must be skipped")
	assert.Equal(t, Skip, report.Results[5].ProbeStatus, "HTTP/2 region2 must be skipped")
	assert.Equal(t, Pass, report.Results[6].ProbeStatus, "API still runs")
	assert.True(t, report.hasHardFail())
}

// TestRun_ManagementAPIFail verifies that a Management API failure results
// in a warning (not a hard fail) and QUIC remains the suggested protocol.
func TestRun_ManagementAPIFail(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	dns := mocks.NewMockDNSResolver(ctrl)
	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	dns.EXPECT().Resolve(gomock.Any()).Return(twoRegionAddrs(), nil)
	// twoRegionAddrs has 2 regions × 1 V4 address each; each succeeds on first try.
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil).Times(2)
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&fakeQUICConn{}, nil).Times(2)
	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, errors.New("connection refused")).AnyTimes()

	report := Run(t.Context(), emptyCert, Config{Timeout: 2 * time.Second, IPVersion: allregions.Auto},
		nopLogger(), RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

	// 2 DNS Pass + 2 QUIC Pass + 2 HTTP2 Pass + 1 API Fail.
	requireStatuses(t, report, Pass, Pass, Pass, Pass, Pass, Pass, Fail)
	require.NotNil(t, report.SuggestedProtocol)
	assert.Equal(t, connection.QUIC, *report.SuggestedProtocol)
	assert.False(t, report.hasHardFail())
	assert.True(t, report.hasWarn())
}

// TestRun_RegionFlagForwardedToDNS verifies that the --region flag is passed
// verbatim to the DNS resolver and that regional hostnames appear in the results.
func TestRun_RegionFlagForwardedToDNS(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	dns := mocks.NewMockDNSResolver(ctrl)
	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	// The region string must be forwarded verbatim to the DNS resolver.
	dns.EXPECT().Resolve("us").Return(twoRegionAddrs(), nil)
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil).Times(2)
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&fakeQUICConn{}, nil).Times(2)
	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil)

	report := Run(t.Context(), emptyCert, Config{Region: "us", Timeout: 2 * time.Second, IPVersion: allregions.Auto},
		nopLogger(), RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

	// DNS rows carry regional hostnames (indices 0 and 1).
	assert.Equal(t, "us-region1.v2.argotunnel.com", report.Results[0].Target, "DNS region1")
	assert.Equal(t, "us-region2.v2.argotunnel.com", report.Results[1].Target, "DNS region2")

	// Transport rows reuse the same regional hostnames (QUIC: 2,3 / HTTP2: 4,5).
	assert.Equal(t, "us-region1.v2.argotunnel.com", report.Results[2].Target, "QUIC region1")
	assert.Equal(t, "us-region2.v2.argotunnel.com", report.Results[3].Target, "QUIC region2")
	assert.Equal(t, "us-region1.v2.argotunnel.com", report.Results[4].Target, "HTTP2 region1")
	assert.Equal(t, "us-region2.v2.argotunnel.com", report.Results[5].Target, "HTTP2 region2")
}

// TestRun_QUICUsesProbeConnIndex verifies that the QUIC probe always uses the
// reserved sentinel connIndex (math.MaxUint8 = 255) to bypass port-reuse checks.
func TestRun_QUICUsesProbeConnIndex(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	dns := mocks.NewMockDNSResolver(ctrl)
	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	dns.EXPECT().Resolve(gomock.Any()).Return(twoRegionAddrs(), nil)
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil).Times(2)
	// connIndex must be the reserved sentinel (math.MaxUint8 = 255), never 0.
	// twoRegionAddrs has 2 regions × 1 V4 address each → 2 calls.
	quicD.EXPECT().DialQuic(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Eq(uint8(math.MaxUint8)),
		gomock.Any(), gomock.Any(),
	).Return(&fakeQUICConn{}, nil).Times(2)
	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil)

	Run(t.Context(), emptyCert, Config{Timeout: 2 * time.Second, IPVersion: allregions.Auto},
		nopLogger(), RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})
}

// TestRun_BothFamiliesProbed verifies that when both V4 and V6 addresses are
// present in the DNS response, both are probed (2 regions × 2 families = 4 dials).
func TestRun_BothFamiliesProbed(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	dns := mocks.NewMockDNSResolver(ctrl)
	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	dns.EXPECT().Resolve(gomock.Any()).Return(twoRegionAddrsBothFamilies(), nil)
	// 2 regions × 2 families = 4 dial calls each for QUIC and HTTP/2.
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil).Times(4)
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&fakeQUICConn{}, nil).Times(4)
	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil)

	report := Run(t.Context(), emptyCert, Config{Timeout: 2 * time.Second, IPVersion: allregions.Auto},
		nopLogger(), RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

	// 2 DNS + 2 QUIC + 2 HTTP2 + 1 API = 7 results, all passing.
	requireStatuses(t, report, Pass, Pass, Pass, Pass, Pass, Pass, Pass)
	require.NotNil(t, report.SuggestedProtocol)
	assert.Equal(t, connection.QUIC, *report.SuggestedProtocol)
}

// TestRun_IPVersionRestriction verifies that when a single IP family is
// configured, only that family is probed (2 regions × 1 addr = 2 dials per
// transport) and the excluded family is never dialled.
func TestRun_IPVersionRestriction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ipVersion allregions.ConfigIPVersion
	}{
		{"IPv4Only skips V6", allregions.IPv4Only},
		{"IPv6Only skips V4", allregions.IPv6Only},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)

			dns := mocks.NewMockDNSResolver(ctrl)
			tcp := mocks.NewMockTCPDialer(ctrl)
			quicD := mocks.NewMockQUICDialer(ctrl)
			mgmt := mocks.NewMockManagementDialer(ctrl)

			dns.EXPECT().Resolve(gomock.Any()).Return(twoRegionAddrsBothFamilies(), nil)
			// 2 regions × 1 addr per restricted family = 2 dials each.
			tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nopConn{}, nil).Times(2)
			quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&fakeQUICConn{}, nil).Times(2)
			mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nopConn{}, nil)

			report := Run(t.Context(), emptyCert, Config{Timeout: 2 * time.Second, IPVersion: tt.ipVersion},
				nopLogger(), RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

			requireStatuses(t, report, Pass, Pass, Pass, Pass, Pass, Pass, Pass)
		})
	}
}

// TestRun_EdgeAddrs_SingleAddr verifies that a single --edge addr bypasses DNS
// probing. The report contains one DNS Skip row, transport rows labeled with
// the raw addr string, and the Management API row.
func TestRun_EdgeAddrs_SingleAddr(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	// DNS resolver must NOT be called when EdgeAddrs is set.
	dns := mocks.NewMockDNSResolver(ctrl)
	dns.EXPECT().Resolve(gomock.Any()).Times(0)

	// One addr resolves to one group → one dial per transport.
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil).Times(1)
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&fakeQUICConn{}, nil).Times(1)
	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil)

	cfg := Config{
		EdgeAddrs: []string{"127.0.0.1:7844"},
		Timeout:   2 * time.Second,
		IPVersion: allregions.Auto,
	}
	report := Run(t.Context(), emptyCert, cfg, nopLogger(),
		RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

	// 1 DNS Skip + 1 QUIC + 1 HTTP2 + 1 API = 4 results.
	requireStatuses(t, report, Pass, Pass, Pass, Pass)
	assert.Equal(t, ProbeTypeDNS, report.Results[0].Type, "first row must be DNS skip")
	assert.Equal(t, "127.0.0.1:7844", report.Results[1].Target, "QUIC target must be the raw --edge addr")
	assert.Equal(t, "127.0.0.1:7844", report.Results[2].Target, "HTTP2 target must be the raw --edge addr")
	require.NotNil(t, report.SuggestedProtocol)
	assert.Equal(t, connection.QUIC, *report.SuggestedProtocol)
}

// TestRun_EdgeAddrs_MultipleAddrs verifies that multiple --edge addrs produce
// one transport row per addr, each labeled with its original addr string.
func TestRun_EdgeAddrs_MultipleAddrs(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	dns := mocks.NewMockDNSResolver(ctrl)
	dns.EXPECT().Resolve(gomock.Any()).Times(0)

	// Two addrs → two groups → two dials per transport.
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil).Times(2)
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&fakeQUICConn{}, nil).Times(2)
	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil)

	cfg := Config{
		EdgeAddrs: []string{"127.0.0.1:7844", "127.0.0.2:7844"},
		Timeout:   2 * time.Second,
		IPVersion: allregions.Auto,
	}
	report := Run(t.Context(), emptyCert, cfg, nopLogger(),
		RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

	// 2 DNS Pass (one per addr) + 2 QUIC + 2 HTTP2 + 1 API = 7 results.
	requireStatuses(t, report, Pass, Pass, Pass, Pass, Pass, Pass, Pass)
	assert.Equal(t, ProbeTypeDNS, report.Results[0].Type, "first row must be DNS skip addr1")
	assert.Equal(t, "127.0.0.1:7844", report.Results[0].Target, "DNS skip addr1 label")
	assert.Equal(t, ProbeTypeDNS, report.Results[1].Type, "second row must be DNS skip addr2")
	assert.Equal(t, "127.0.0.2:7844", report.Results[1].Target, "DNS skip addr2 label")
	assert.Equal(t, "127.0.0.1:7844", report.Results[2].Target, "QUIC addr1")
	assert.Equal(t, "127.0.0.2:7844", report.Results[3].Target, "QUIC addr2")
	assert.Equal(t, "127.0.0.1:7844", report.Results[4].Target, "HTTP2 addr1")
	assert.Equal(t, "127.0.0.2:7844", report.Results[5].Target, "HTTP2 addr2")
}

// TestRun_EdgeAddrs_UnresolvableAddr verifies that when all --edge addrs fail
// to resolve, the DNS resolver is not called and transport rows are skipped,
// mirroring the DNS skip row.
func TestRun_EdgeAddrs_UnresolvableAddr(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	tcp := mocks.NewMockTCPDialer(ctrl)
	quicD := mocks.NewMockQUICDialer(ctrl)
	mgmt := mocks.NewMockManagementDialer(ctrl)

	dns := mocks.NewMockDNSResolver(ctrl)
	dns.EXPECT().Resolve(gomock.Any()).Times(0)

	// Unresolvable addr → no groups → no transport dials.
	tcp.EXPECT().DialEdge(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	quicD.EXPECT().DialQuic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	mgmt.EXPECT().DialContext(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nopConn{}, nil)

	cfg := Config{
		EdgeAddrs: []string{"not-a-valid-addr"},
		Timeout:   2 * time.Second,
		IPVersion: allregions.Auto,
	}
	report := Run(t.Context(), emptyCert, cfg, nopLogger(),
		RunDialers{DNSResolver: dns, TCPDialer: tcp, QUICDialer: quicD, ManagementDialer: mgmt})

	// 1 DNS Fail + 1 QUIC Skip + 1 HTTP2 Skip + 1 API = 4 results.
	requireStatuses(t, report, Fail, Skip, Skip, Pass)
	assert.Equal(t, ProbeTypeDNS, report.Results[0].Type)
	assert.Equal(t, "not-a-valid-addr", report.Results[0].Target)
	assert.Equal(t, ProbeTypeQUIC, report.Results[1].Type)
	assert.Equal(t, ProbeTypeHTTP2, report.Results[2].Type)
	assert.Nil(t, report.SuggestedProtocol)
	assert.True(t, report.hasHardFail())
}
