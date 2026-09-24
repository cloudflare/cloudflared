package tunnel

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/quicktunnelauth"
)

func TestBuildQuickTunnelRequestBody_PublicMode(t *testing.T) {
	t.Parallel()

	assert.Empty(t, buildQuickTunnelRequestBody(false))
}

func TestBuildQuickTunnelRequestBody_ProtectedMode(t *testing.T) {
	t.Parallel()

	body := buildQuickTunnelRequestBody(true)

	var result map[string]string
	require.NoError(t, json.Unmarshal(body, &result))
	assert.Equal(t, "otp", result["auth_mode"])
}

func TestFormatQuickTunnelAllowedRecipientCounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		emailAddresses int
		emailDomains   int
		expected       string
	}{
		{name: "no recipients", expected: "none"},
		{name: "one address", emailAddresses: 1, expected: "1 address"},
		{name: "multiple addresses", emailAddresses: 2, expected: "2 addresses"},
		{name: "one domain", emailDomains: 1, expected: "1 domain rule"},
		{name: "multiple domains", emailDomains: 2, expected: "2 domain rules"},
		{name: "one of each", emailAddresses: 1, emailDomains: 1, expected: "1 address, 1 domain rule"},
		{name: "multiple of each", emailAddresses: 2, emailDomains: 3, expected: "2 addresses, 3 domain rules"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.expected, formatQuickTunnelAllowedRecipientCounts(test.emailAddresses, test.emailDomains))
		})
	}
}

func TestQuickTunnelStartupLinesProtected(t *testing.T) {
	t.Parallel()

	const (
		emailAddress   = "private.person@example.com"
		otherAddress   = "other@example.com"
		wildcardDomain = "private.example"
	)
	policy, err := quicktunnelauth.NewQuickTunnelAuthRecipientPolicy([]string{
		emailAddress,
		otherAddress,
		"*@" + wildcardDomain,
	})
	require.NoError(t, err)

	lines := quickTunnelStartupLines(
		true,
		"https://example.trycloudflare.com",
		"http://localhost:8080",
		policy,
	)
	assert.Equal(t, []string{
		"Your protected quick Tunnel has been created! Visit it at (it may take some time to be reachable):",
		"https://example.trycloudflare.com",
		"Authentication: One-Time PIN (using Cloudflare Access)",
		"Allowed recipients: 2 addresses, 1 domain rule",
		"Local origin: http://localhost:8080",
	}, lines)

	output := strings.Join(lines, "\n")
	assert.NotContains(t, output, emailAddress)
	assert.NotContains(t, output, otherAddress)
	assert.NotContains(t, output, wildcardDomain)
}

func TestQuickTunnelStartupLinesPublicUnchanged(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):",
		"https://example.trycloudflare.com",
	}, quickTunnelStartupLines(false, "https://example.trycloudflare.com", "", nil))
}

func TestNormalizeQuickTunnelURL(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "https://example.trycloudflare.com", normalizeQuickTunnelURL("example.trycloudflare.com"))
	assert.Equal(t, "https://example.trycloudflare.com", normalizeQuickTunnelURL("https://example.trycloudflare.com"))
}

func TestDescribeQuickTunnelLocalOrigin(t *testing.T) {
	t.Parallel()

	t.Run("url is validated and userinfo is removed", func(t *testing.T) {
		t.Parallel()

		flagSet := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
		urlFlag := &cli.StringFlag{Name: "url"}
		require.NoError(t, urlFlag.Apply(flagSet))
		require.NoError(t, flagSet.Parse([]string{"--url", "https://user:password@localhost:8080/private"}))
		ctx := cli.NewContext(cli.NewApp(), flagSet, nil)

		origin, err := describeQuickTunnelLocalOrigin(ctx, false)
		require.NoError(t, err)
		assert.Equal(t, "https://localhost:8080", origin)
		assert.NotContains(t, origin, "user")
		assert.NotContains(t, origin, "password")
	})

	t.Run("hello world wins over url", func(t *testing.T) {
		t.Parallel()

		flagSet := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
		for _, cliFlag := range []cli.Flag{
			&cli.StringFlag{Name: "url"},
			&cli.BoolFlag{Name: ingress.HelloWorldFlag},
		} {
			require.NoError(t, cliFlag.Apply(flagSet))
		}
		require.NoError(t, flagSet.Parse([]string{"--url", "http://localhost:8080", "--hello-world"}))
		ctx := cli.NewContext(cli.NewApp(), flagSet, nil)

		origin, err := describeQuickTunnelLocalOrigin(ctx, false)
		require.NoError(t, err)
		assert.Equal(t, quickTunnelHelloWorldOrigin, origin)
		assert.NotContains(t, origin, "127.0.0.1")
		assert.NotContains(t, origin, ":")
	})
}

func TestDecodeQuickTunnelProvisioningResponseDoesNotExposeBody(t *testing.T) {
	t.Parallel()

	const credential = "sensitive-tunnel-credential"
	malformedSuccess := []byte(`{"success":true,"result":{"secret":"` + credential + `"}} trailing`)
	_, err := decodeQuickTunnelProvisioningResponse(http.StatusOK, malformedSuccess)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), credential)

	_, err = decodeQuickTunnelProvisioningResponse(http.StatusBadGateway, []byte(credential))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), credential)
}

func TestReadQuickTunnelProvisioningResponseBoundsBody(t *testing.T) {
	t.Parallel()

	response, err := readQuickTunnelProvisioningResponse(bytes.NewReader(bytes.Repeat([]byte("x"), quickTunnelMaxProvisioningResponse)))
	require.NoError(t, err)
	assert.Len(t, response, quickTunnelMaxProvisioningResponse)

	_, err = readQuickTunnelProvisioningResponse(bytes.NewReader(bytes.Repeat([]byte("x"), quickTunnelMaxProvisioningResponse+1)))
	require.ErrorContains(t, err, "response exceeds maximum size")
}

func TestFormatQuickTunnelErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		errors   []QuickTunnelError
		expected string
	}{
		{
			name:     "empty",
			errors:   []QuickTunnelError{},
			expected: "",
		},
		{
			name:     "single error",
			errors:   []QuickTunnelError{{Code: 1, Message: "first error"}},
			expected: "[1] first error",
		},
		{
			name: "multiple errors",
			errors: []QuickTunnelError{
				{Code: 1, Message: "first error"},
				{Code: 2, Message: "second error"},
			},
			expected: "[1] first error; [2] second error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, formatQuickTunnelErrors(tt.errors))
		})
	}
}
