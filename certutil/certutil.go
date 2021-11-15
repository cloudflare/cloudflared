package certutil

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
)

type namedTunnelToken struct {
	ZoneID     string `json:"zoneID"`
	AccountID  string `json:"accountID"`
	ServiceKey string `json:"serviceKey"`
}

type OriginCert struct {
	PrivateKey interface{}
	Cert       *x509.Certificate
	ZoneID     string
	ServiceKey string
	AccountID  string
}

func DecodeOriginCert(blocks []byte) (*OriginCert, error) {
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
		case "PRIVATE KEY":
			if originCert.PrivateKey != nil {
				return nil, fmt.Errorf("Found multiple private key in the certificate")
			}
			// RSA private key
			privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("Cannot parse private key")
			}
			originCert.PrivateKey = privateKey
		case "CERTIFICATE":
			if originCert.Cert != nil {
				return nil, fmt.Errorf("Found multiple certificates in the certificate")
			}
			cert, err := x509.ParseCertificates(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("Cannot parse certificate")
			} else if len(cert) > 1 {
				return nil, fmt.Errorf("Found multiple certificates in the certificate")
			}
			originCert.Cert = cert[0]
		case "WARP TOKEN", "ARGO TUNNEL TOKEN":
			if originCert.ZoneID != "" || originCert.ServiceKey != "" {
				return nil, fmt.Errorf("Found multiple tokens in the certificate")
			}
			// The token is a string,
			// Try the newer JSON format
			ntt := namedTunnelToken{}
			if err := json.Unmarshal(block.Bytes, &ntt); err == nil {
				originCert.ZoneID = ntt.ZoneID
				originCert.ServiceKey = ntt.ServiceKey
				originCert.AccountID = ntt.AccountID
			} else {
				// Try the older format, where the zoneID and service key are separated by
				// a new line character
				token := string(block.Bytes)
				s := strings.Split(token, "\n")
				if len(s) != 2 {
					return nil, fmt.Errorf("Cannot parse token")
				}
				originCert.ZoneID = s[0]
				originCert.ServiceKey = s[1]
			}
		default:
			return nil, fmt.Errorf("Unknown block %s in the certificate", block.Type)
		}
		block, rest = pem.Decode(rest)
	}

	if originCert.PrivateKey == nil {
		return nil, fmt.Errorf("Missing private key in the certificate")
	} else if originCert.Cert == nil {
		return nil, fmt.Errorf("Missing certificate in the certificate")
	} else if originCert.ZoneID == "" || originCert.ServiceKey == "" {
		return nil, fmt.Errorf("Missing token in the certificate")
	}

	return &originCert, nil
}
