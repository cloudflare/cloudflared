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
)

const (
	validPrincipal       = "testUser"
	testDir              = "testdata"
	testUserKeyFilename  = "id_rsa.pub"
	testCAFilename       = "ca.pub"
	testOtherCAFilename  = "other_ca.pub"
	testUserCertFilename = "id_rsa-cert.pub"
)

var logger, hook = test.NewNullLogger()

func TestMain(m *testing.M) {
	authorizedKeysDir = testUserKeyFilename
	logger.SetLevel(logrus.DebugLevel)
	code := m.Run()
	os.Exit(code)
}

func TestPublicKeyAuth_Success(t *testing.T) {
	context, cancel := newMockContext(validPrincipal)
	defer cancel()

	sshServer := SSHServer{getUserFunc: getMockUser}

	pubKey := getKey(t, testUserKeyFilename)
	assert.True(t, sshServer.authorizedKeyHandler(context, pubKey))
}

func TestPublicKeyAuth_MissingKey(t *testing.T) {
	context, cancel := newMockContext(validPrincipal)
	defer cancel()

	sshServer := SSHServer{logger: logger, getUserFunc: getMockUser}

	pubKey := getKey(t, testOtherCAFilename)
	assert.False(t, sshServer.authorizedKeyHandler(context, pubKey))
	assert.Contains(t, hook.LastEntry().Message, "Matching public key not found in")
}

func TestPublicKeyAuth_InvalidUser(t *testing.T) {
	context, cancel := newMockContext("notAUser")
	defer cancel()

	sshServer := SSHServer{logger: logger, getUserFunc: lookupUser}

	pubKey := getKey(t, testUserKeyFilename)
	assert.False(t, sshServer.authorizedKeyHandler(context, pubKey))
	assert.Contains(t, hook.LastEntry().Message, "Invalid user")
}

func TestPublicKeyAuth_MissingFile(t *testing.T) {
	currentUser, err := user.Current()
	require.Nil(t, err)
	context, cancel := newMockContext(currentUser.Username)
	defer cancel()

	sshServer := SSHServer{Server: ssh.Server{}, logger: logger, getUserFunc: lookupUser}

	pubKey := getKey(t, testUserKeyFilename)
	assert.False(t, sshServer.authorizedKeyHandler(context, pubKey))
	assert.Contains(t, hook.LastEntry().Message, "not found")
}

func TestShortLivedCerts_Success(t *testing.T) {
	context, cancel := newMockContext(validPrincipal)
	defer cancel()

	caCert := getKey(t, testCAFilename)
	sshServer := SSHServer{logger: log.CreateLogger(), caCert: caCert, getUserFunc: getMockUser}

	userCert := getKey(t, testUserCertFilename)
	assert.True(t, sshServer.shortLivedCertHandler(context, userCert))
}

func TestShortLivedCerts_CAsDontMatch(t *testing.T) {
	context, cancel := newMockContext(validPrincipal)
	defer cancel()

	caCert := getKey(t, testOtherCAFilename)
	sshServer := SSHServer{logger: logger, caCert: caCert, getUserFunc: getMockUser}

	userCert := getKey(t, testUserCertFilename)
	assert.False(t, sshServer.shortLivedCertHandler(context, userCert))
	assert.Equal(t, "CA certificate does not match user certificate signer", hook.LastEntry().Message)
}

func TestShortLivedCerts_UserDoesNotExist(t *testing.T) {
	context, cancel := newMockContext(validPrincipal)
	defer cancel()

	caCert := getKey(t, testCAFilename)
	sshServer := SSHServer{logger: logger, caCert: caCert, getUserFunc: lookupUser}

	userCert := getKey(t, testUserCertFilename)
	assert.False(t, sshServer.shortLivedCertHandler(context, userCert))
	assert.Contains(t, hook.LastEntry().Message, "Invalid user")
}

func TestShortLivedCerts_InvalidPrincipal(t *testing.T) {
	context, cancel := newMockContext("notAUser")
	defer cancel()

	caCert := getKey(t, testCAFilename)
	sshServer := SSHServer{logger: logger, caCert: caCert, getUserFunc: lookupUser}

	userCert := getKey(t, testUserCertFilename)
	assert.False(t, sshServer.shortLivedCertHandler(context, userCert))
	assert.Contains(t, hook.LastEntry().Message, "not in the set of valid principals for given certificate")
}

func getMockUser(_ string) (*User, error) {
	return &User{
		Username: validPrincipal,
		HomeDir:  testDir,
	}, nil

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

func newMockContext(user string) (*mockSSHContext, context.CancelFunc) {
	innerCtx, cancel := context.WithCancel(context.Background())
	mockCtx := &mockSSHContext{innerCtx, &sync.Mutex{}}
	mockCtx.SetValue("user", user)
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
