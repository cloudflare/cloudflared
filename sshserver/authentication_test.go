//+build !windows

package sshserver

import (
	"context"
	"io/ioutil"
	"net"
	"os"
	"os/user"
	"path"
	"sync"
	"testing"

	"github.com/cloudflare/cloudflared/log"
	"github.com/gliderlabs/ssh"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

const (
	testDir              = "testdata"
	testUserKeyFilename  = "id_rsa.pub"
	testCAFilename       = "ca.pub"
	testOtherCAFilename  = "other_ca.pub"
	testUserCertFilename = "id_rsa-cert.pub"
)

var (
	logger, hook = test.NewNullLogger()
	mockUser     = &User{Username: "testUser", HomeDir: testDir}
)

func TestMain(m *testing.M) {
	authorizedKeysDir = testUserKeyFilename
	logger.SetLevel(logrus.DebugLevel)
	code := m.Run()
	os.Exit(code)
}

func TestPublicKeyAuth_Success(t *testing.T) {
	context, cancel := newMockContext(mockUser)
	defer cancel()

	sshServer := SSHServer{logger: logger}

	pubKey := getKey(t, testUserKeyFilename)
	assert.True(t, sshServer.authorizedKeyHandler(context, pubKey))
}

func TestPublicKeyAuth_MissingKey(t *testing.T) {
	context, cancel := newMockContext(mockUser)
	defer cancel()

	sshServer := SSHServer{logger: logger}

	pubKey := getKey(t, testOtherCAFilename)
	assert.False(t, sshServer.authorizedKeyHandler(context, pubKey))
	assert.Contains(t, hook.LastEntry().Message, "Matching public key not found in")
}

func TestPublicKeyAuth_InvalidUser(t *testing.T) {
	context, cancel := newMockContext(&User{Username: "notAUser"})
	defer cancel()

	sshServer := SSHServer{logger: logger}

	pubKey := getKey(t, testUserKeyFilename)
	assert.False(t, sshServer.authenticationHandler(context, pubKey))
	assert.Contains(t, hook.LastEntry().Message, "Invalid user")
}

func TestPublicKeyAuth_MissingFile(t *testing.T) {
	tempUser, err := user.Current()
	require.Nil(t, err)
	currentUser, err := lookupUser(tempUser.Username)
	require.Nil(t, err)

	require.Nil(t, err)
	context, cancel := newMockContext(currentUser)
	defer cancel()

	sshServer := SSHServer{Server: ssh.Server{}, logger: logger}

	pubKey := getKey(t, testUserKeyFilename)
	assert.False(t, sshServer.authorizedKeyHandler(context, pubKey))
	assert.Contains(t, hook.LastEntry().Message, "not found")
}

func TestShortLivedCerts_Success(t *testing.T) {
	context, cancel := newMockContext(mockUser)
	defer cancel()

	caCert := getKey(t, testCAFilename)
	sshServer := SSHServer{logger: log.CreateLogger(), caCert: caCert}

	userCert, ok := getKey(t, testUserCertFilename).(*gossh.Certificate)
	require.True(t, ok)
	assert.True(t, sshServer.shortLivedCertHandler(context, userCert))
}

func TestShortLivedCerts_CAsDontMatch(t *testing.T) {
	context, cancel := newMockContext(mockUser)
	defer cancel()

	caCert := getKey(t, testOtherCAFilename)
	sshServer := SSHServer{logger: logger, caCert: caCert}

	userCert, ok := getKey(t, testUserCertFilename).(*gossh.Certificate)
	require.True(t, ok)
	assert.False(t, sshServer.shortLivedCertHandler(context, userCert))
	assert.Equal(t, "CA certificate does not match user certificate signer", hook.LastEntry().Message)
}

func TestShortLivedCerts_InvalidPrincipal(t *testing.T) {
	context, cancel := newMockContext(&User{Username: "NotAUser"})
	defer cancel()

	caCert := getKey(t, testCAFilename)
	sshServer := SSHServer{logger: logger, caCert: caCert}

	userCert, ok := getKey(t, testUserCertFilename).(*gossh.Certificate)
	require.True(t, ok)
	assert.False(t, sshServer.shortLivedCertHandler(context, userCert))
	assert.Contains(t, hook.LastEntry().Message, "not in the set of valid principals for given certificate")
}

func getKey(t *testing.T, filename string) ssh.PublicKey {
	path := path.Join(testDir, filename)
	bytes, err := ioutil.ReadFile(path)
	require.Nil(t, err)
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(bytes)
	require.Nil(t, err)
	return pubKey
}

type mockSSHContext struct {
	context.Context
	*sync.Mutex
}

func newMockContext(user *User) (*mockSSHContext, context.CancelFunc) {
	innerCtx, cancel := context.WithCancel(context.Background())
	mockCtx := &mockSSHContext{innerCtx, &sync.Mutex{}}
	mockCtx.SetValue("sshUser", user)

	// This naming is confusing but we cant change it because this mocks the SSHContext struct in gliderlabs/ssh
	mockCtx.SetValue("user", user.Username)
	return mockCtx, cancel
}

func (ctx *mockSSHContext) SetValue(key, value interface{}) {
	ctx.Context = context.WithValue(ctx.Context, key, value)
}

func (ctx *mockSSHContext) User() string {
	return ctx.Value("user").(string)
}

func (ctx *mockSSHContext) SessionID() string {
	return ""
}

func (ctx *mockSSHContext) ClientVersion() string {
	return ""
}

func (ctx *mockSSHContext) ServerVersion() string {
	return ""
}

func (ctx *mockSSHContext) RemoteAddr() net.Addr {
	return nil
}

func (ctx *mockSSHContext) LocalAddr() net.Addr {
	return nil
}

func (ctx *mockSSHContext) Permissions() *ssh.Permissions {
	return nil
}
