package gocertifi

import "testing"

func TestGetCerts(t *testing.T) {
	cert_pool, err := CACerts()
	if (cert_pool == nil) || (err != nil) {
		t.Errorf("Failed to return the certificates.")
	}
}
