//+build !windows

package sshserver

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/gliderlabs/ssh"
	"github.com/pkg/errors"
	gossh "golang.org/x/crypto/ssh"
)

var (
	systemConfigPath  = "/etc/cloudflared/"
	authorizedKeysDir = ".cloudflared/authorized_keys"
)

func (s *SSHServer) authorizedKeyHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	sshUser, err := s.getUserFunc(ctx.User())
	if err != nil {
		s.logger.Debugf("Invalid user: %s", ctx.User())
		return false
	}

	authorizedKeysPath := path.Join(sshUser.HomeDir, authorizedKeysDir)
	if _, err := os.Stat(authorizedKeysPath); os.IsNotExist(err) {
		s.logger.Debugf("authorized_keys file %s not found", authorizedKeysPath)
		return false
	}

	authorizedKeysBytes, err := ioutil.ReadFile(authorizedKeysPath)
	if err != nil {
		s.logger.WithError(err).Errorf("Failed to load authorized_keys %s", authorizedKeysPath)
		return false
	}

	for len(authorizedKeysBytes) > 0 {

		// Skips invalid keys. Returns error if no valid keys remain.
		pubKey, _, _, rest, err := ssh.ParseAuthorizedKey(authorizedKeysBytes)
		authorizedKeysBytes = rest
		if err != nil {
			s.logger.Errorf("Invalid key(s) found in %s", authorizedKeysPath)
			return false
		}

		if ssh.KeysEqual(pubKey, key) {
			ctx.SetValue("sshUser", sshUser)
			return true
		}
	}
	s.logger.Debugf("Matching public key not found in %s", authorizedKeysPath)
	return false
}

func (s *SSHServer) shortLivedCertHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	userCert, ok := key.(*gossh.Certificate)
	if !ok {
		s.logger.Debug("Received key is not an SSH certificate")
		return false
	}

	if !ssh.KeysEqual(s.caCert, userCert.SignatureKey) {
		s.logger.Debug("CA certificate does not match user certificate signer")
		return false
	}

	checker := gossh.CertChecker{}
	if err := checker.CheckCert(ctx.User(), userCert); err != nil {
		s.logger.Debug(err)
		return false
	} else {
		sshUser, err := s.getUserFunc(ctx.User())
		if err != nil {
			s.logger.Debugf("Invalid user: %s", ctx.User())
			return false
		}
		ctx.SetValue("sshUser", sshUser)
	}
	return true
}

func getCACert() (ssh.PublicKey, error) {
	caCertPath := path.Join(systemConfigPath, "ca.pub")
	caCertBytes, err := ioutil.ReadFile(caCertPath)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("Failed to load CA certertificate %s", caCertPath))
	}
	caCert, _, _, _, err := ssh.ParseAuthorizedKey(caCertBytes)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to parse CA Certificate")
	}

	return caCert, nil
}
