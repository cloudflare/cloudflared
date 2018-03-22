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
MIGkAgEBBDBGGfwhIJdiUiJUVIItqJjEIMmlXxsMa8TQeer47+g+cIZ466rgg8EK
+Mdn6BY48GCgBwYFK4EEACKhZANiAASW//A9iDbPKg3OLkn7yJqLer32g9I5lBKR
tPc/zBubQLLz9lAaYI6AOQiJXhGr5JkKmQfi1sYHK5rJITPFy4W8Et4hHLdazDZH
WnEd+TStQABFUjrhtqXPWmGKcly0pOE=
-----END EC PRIVATE KEY-----`

	helloCRT = `
-----BEGIN CERTIFICATE-----
MIICiDCCAg6gAwIBAgIJAJ/FfkBTtbuIMAkGByqGSM49BAEwfzELMAkGA1UEBhMC
VVMxDjAMBgNVBAgMBVRleGFzMQ8wDQYDVQQHDAZBdXN0aW4xGTAXBgNVBAoMEENs
b3VkZmxhcmUsIEluYy4xNDAyBgNVBAMMK0FyZ28gVHVubmVsIFNhbXBsZSBIZWxs
byBTZXJ2ZXIgQ2VydGlmaWNhdGUwHhcNMTgwMzE5MjMwNTMyWhcNMjgwMzE2MjMw
NTMyWjB/MQswCQYDVQQGEwJVUzEOMAwGA1UECAwFVGV4YXMxDzANBgNVBAcMBkF1
c3RpbjEZMBcGA1UECgwQQ2xvdWRmbGFyZSwgSW5jLjE0MDIGA1UEAwwrQXJnbyBU
dW5uZWwgU2FtcGxlIEhlbGxvIFNlcnZlciBDZXJ0aWZpY2F0ZTB2MBAGByqGSM49
AgEGBSuBBAAiA2IABJb/8D2INs8qDc4uSfvImot6vfaD0jmUEpG09z/MG5tAsvP2
UBpgjoA5CIleEavkmQqZB+LWxgcrmskhM8XLhbwS3iEct1rMNkdacR35NK1AAEVS
OuG2pc9aYYpyXLSk4aNXMFUwUwYDVR0RBEwwSoIJbG9jYWxob3N0ghFjbG91ZGZs
YXJlZC1oZWxsb4ISY2xvdWRmbGFyZWQyLWhlbGxvhwR/AAABhxAAAAAAAAAAAAAA
AAAAAAABMAkGByqGSM49BAEDaQAwZgIxAPxkdghH6y8xLMnY9Bom3Llf4NYM6yB9
PD1YsaNUJTsxjTk3YY1Jsp+yzK0yUKtTZwIxAPcdvqCF2/iR9H288pCT1TgtO0a9
cJL9RY1lq7DIGN37v1ZXReWaD+3hNokY8NriVg==
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
