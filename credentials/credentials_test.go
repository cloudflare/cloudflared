package credentials

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialsRead(t *testing.T) {
	file, err := os.ReadFile("test-cloudflare-tunnel-cert-json.pem")
	require.NoError(t, err)
	dir := t.TempDir()
	certPath := filepath.Join(dir, originCertFile)
	_ = os.WriteFile(certPath, file, fs.ModePerm)
	user, err := Read(certPath, &nopLog)
	require.NoError(t, err)
	require.Equal(t, certPath, user.CertPath())
	require.Equal(t, "test-service-key", user.APIToken())
	require.Equal(t, "7b0a4d77dfb881c1a3b7d61ea9443e19", user.ZoneID())
	require.Equal(t, "abcdabcdabcdabcd1234567890abcdef", user.AccountID())
}

func TestCredentialsClient(t *testing.T) {
	user := User{
		certPath: "/tmp/cert.pem",
		cert: &OriginCert{
			ZoneID:    "7b0a4d77dfb881c1a3b7d61ea9443e19",
			AccountID: "abcdabcdabcdabcd1234567890abcdef",
			APIToken:  "test-service-key",
		},
	}
	client, err := user.Client("example.com", "cloudflared/test", &nopLog)
	require.NoError(t, err)
	require.NotNil(t, client)
}
