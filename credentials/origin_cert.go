package credentials

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudflare/cloudflared/config"
	"github.com/mitchellh/go-homedir"
)

const (
	DefaultCredentialFile = "cert.pem"
	OriginCertFlag        = "origincert"
)

type namedTunnelToken struct {
	ZoneID    string `json:"zoneID"`
	AccountID string `json:"accountID"`
	APIToken  string `json:"apiToken"`
}

type OriginCert struct {
	ZoneID    string
	APIToken  string
	AccountID string
}

// FindDefaultOriginCertPath returns the first path that contains a cert.pem file. If none of the
// DefaultConfigSearchDirectories contains a cert.pem file, return empty string
func FindDefaultOriginCertPath() string {
	for _, defaultConfigDir := range config.DefaultConfigSearchDirectories() {
		originCertPath, _ := homedir.Expand(filepath.Join(defaultConfigDir, DefaultCredentialFile))
		if ok := fileExists(originCertPath); ok {
			return originCertPath
		}
	}
	return ""
}

func decodeOriginCert(blocks []byte) (*OriginCert, error) {
	if len(blocks) == 0 {
		return nil, fmt.Errorf("Cannot decode empty certificate")
	}
	originCert := OriginCert{}
	block, rest := pem.Decode(blocks)
	for {
		if block == nil {
			break
		}
		switch block.Type {
		case "PRIVATE KEY", "CERTIFICATE":
			// this is for legacy purposes.
			break
		case "ARGO TUNNEL TOKEN":
			if originCert.ZoneID != "" || originCert.APIToken != "" {
				return nil, fmt.Errorf("Found multiple tokens in the certificate")
			}
			// The token is a string,
			// Try the newer JSON format
			ntt := namedTunnelToken{}
			if err := json.Unmarshal(block.Bytes, &ntt); err == nil {
				originCert.ZoneID = ntt.ZoneID
				originCert.APIToken = ntt.APIToken
				originCert.AccountID = ntt.AccountID
			}
		default:
			return nil, fmt.Errorf("Unknown block %s in the certificate", block.Type)
		}
		block, rest = pem.Decode(rest)
	}

	if originCert.ZoneID == "" || originCert.APIToken == "" {
		return nil, fmt.Errorf("Missing token in the certificate")
	}

	return &originCert, nil
}

func readOriginCert(originCertPath string) ([]byte, error) {
	originCert, err := os.ReadFile(originCertPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s to load origin certificate", originCertPath)
	}

	return originCert, nil
}

// FindOriginCert will check to make sure that the certificate exists at the specified file path.
func FindOriginCert(originCertPath string) (string, error) {
	if originCertPath == "" {
		return "", fmt.Errorf("client didn't specify origincert path")
	}
	var err error
	originCertPath, err = homedir.Expand(originCertPath)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path %s", originCertPath)
	}
	// Check that the user has acquired a certificate using the login command
	ok := fileExists(originCertPath)
	if !ok {
		return "", fmt.Errorf("cannot find a valid certificate at the path %s", originCertPath)
	}

	return originCertPath, nil
}

// FileExists checks to see if a file exist at the provided path.
func fileExists(path string) bool {
	fileStat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !fileStat.IsDir()
}
