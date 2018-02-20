package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
)

const (
	helloKey = `
-----BEGIN EC PARAMETERS-----
BgUrgQQAIg==
-----END EC PARAMETERS-----
-----BEGIN EC PRIVATE KEY-----
MIGkAgEBBDAdyQBXfxTDCQSOT0HugmH9pVBtIw8t5dYvm6HxGlNq6P57v5GeN02Z
dH9FRl7+VSWgBwYFK4EEACKhZANiAATqpFzTxxV7D+/oqhKCTR6BEM9elTfKaRQE
FsLufcmaTMw/9tTwgpHKao/QsLKDTNbQhbSQLkcmpCQKlSGhl+pCrqNt/oYUAhav
UIwpwGiLCqGH/R2AqWLKRPOa/Rufs/U=
-----END EC PRIVATE KEY-----`

	helloCRT = `
-----BEGIN CERTIFICATE-----
MIICkDCCAhigAwIBAgIJAPtKfUjc2lwGMAkGByqGSM49BAEwgYoxCzAJBgNVBAYT
AlVTMQ4wDAYDVQQIDAVUZXhhczEPMA0GA1UEBwwGQXVzdGluMRkwFwYDVQQKDBBD
bG91ZGZsYXJlLCBJbmMuMT8wPQYDVQQDDDZDbG91ZGZsYXJlIEFyZ28gVHVubmVs
IFNhbXBsZSBIZWxsbyBTZXJ2ZXIgQ2VydGlmaWNhdGUwHhcNMTgwMjE1MjAxNjU5
WhcNMjgwMjEzMjAxNjU5WjCBijELMAkGA1UEBhMCVVMxDjAMBgNVBAgMBVRleGFz
MQ8wDQYDVQQHDAZBdXN0aW4xGTAXBgNVBAoMEENsb3VkZmxhcmUsIEluYy4xPzA9
BgNVBAMMNkNsb3VkZmxhcmUgQXJnbyBUdW5uZWwgU2FtcGxlIEhlbGxvIFNlcnZl
ciBDZXJ0aWZpY2F0ZTB2MBAGByqGSM49AgEGBSuBBAAiA2IABOqkXNPHFXsP7+iq
EoJNHoEQz16VN8ppFAQWwu59yZpMzD/21PCCkcpqj9CwsoNM1tCFtJAuRyakJAqV
IaGX6kKuo23+hhQCFq9QjCnAaIsKoYf9HYCpYspE85r9G5+z9aNJMEcwRQYDVR0R
BD4wPIIJbG9jYWxob3N0ggp3YXJwLWhlbGxvggt3YXJwMi1oZWxsb4cEfwAAAYcQ
AAAAAAAAAAAAAAAAAAAAATAJBgcqhkjOPQQBA2cAMGQCMHyVPufXZ6vQo6XRWRa0
dAwtfgesOdZVP2Wt+t5v8jOIQQh1IQXYk5GtyoZGSObjhQIwd1fRgAyKXaZt+1DV
ZtHTdf8pMvESfJsSd8AB1eQ6q+pAiRUYyaxcE1Mlo2YY5o+g
-----END CERTIFICATE-----`
)

func GetHelloCertificate() (tls.Certificate, error) {
	return tls.X509KeyPair([]byte(helloCRT), []byte(helloKey))
}

func GetHelloCertificateX509() (*x509.Certificate, error) {
	helloCertificate, err := GetHelloCertificate()
	if err != nil {
		return nil, err
	}

	return x509.ParseCertificate(helloCertificate.Certificate[0])
}
