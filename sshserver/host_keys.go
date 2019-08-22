//+build !windows

package sshserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/gliderlabs/ssh"
	"github.com/pkg/errors"
)

const (
	rsaFilename   = "ssh_host_rsa_key"
	ecdsaFilename = "ssh_host_ecdsa_key"
)

func (s *SSHServer) configureHostKeys() error {
	if _, err := os.Stat(systemConfigPath); os.IsNotExist(err) {
		if err := os.MkdirAll(systemConfigPath, 0755); err != nil {
			return errors.Wrap(err, fmt.Sprintf("Error creating %s directory", systemConfigPath))
		}
	}

	if err := s.configureHostKey(s.ensureECDSAKeyExists); err != nil {
		return err
	}

	if err := s.configureHostKey(s.ensureRSAKeyExists); err != nil {
		return err
	}

	return nil
}

func (s *SSHServer) configureHostKey(keyFunc func() (string, error)) error {
	path, err := keyFunc()
	if err != nil {
		return err
	}

	if err := s.SetOption(ssh.HostKeyFile(path)); err != nil {
		return errors.Wrap(err, "Could not set SSH host key")
	}
	return nil
}

func (s *SSHServer) ensureRSAKeyExists() (string, error) {
	keyPath := filepath.Join(systemConfigPath, rsaFilename)
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return "", errors.Wrap(err, "Error generating RSA host key")
		}

		privateKey := &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		}

		if err = writePrivateKey(keyPath, privateKey); err != nil {
			return "", err
		}

		s.logger.Debug("Created new RSA SSH host key: ", keyPath)
	}
	return keyPath, nil
}

func (s *SSHServer) ensureECDSAKeyExists() (string, error) {
	keyPath := filepath.Join(systemConfigPath, ecdsaFilename)
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return "", errors.Wrap(err, "Error generating ECDSA host key")
		}

		keyBytes, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return "", errors.Wrap(err, "Error marshalling ECDSA key")
		}

		privateKey := &pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: keyBytes,
		}

		if err = writePrivateKey(keyPath, privateKey); err != nil {
			return "", err
		}

		s.logger.Debug("Created new ECDSA SSH host key: ", keyPath)
	}
	return keyPath, nil
}

func writePrivateKey(keyPath string, privateKey *pem.Block) error {
	if err := ioutil.WriteFile(keyPath, pem.EncodeToMemory(privateKey), 0600); err != nil {
		return errors.Wrap(err, fmt.Sprintf("Error writing host key to %s", keyPath))
	}
	return nil
}
