package certutil

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
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
