//go:build ignore

// TODO: Remove the above build tag and include this test when we start compiling with Golang 1.10.0+

package tunnel

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Generated using `openssl req -newkey rsa:512 -nodes -x509 -days 3650`
var samplePEM = []byte(`
-----BEGIN CERTIFICATE-----
MIIB4DCCAYoCCQCb/H0EUrdXEjANBgkqhkiG9w0BAQsFADB3MQswCQYDVQQGEwJV
UzEOMAwGA1UECAwFVGV4YXMxDzANBgNVBAcMBkF1c3RpbjEZMBcGA1UECgwQQ2xv
dWRmbGFyZSwgSW5jLjEZMBcGA1UECwwQUHJvZHVjdCBTdHJhdGVneTERMA8GA1UE
AwwIVGVzdCBPbmUwHhcNMTgwNDI2MTYxMDUxWhcNMjgwNDIzMTYxMDUxWjB3MQsw
CQYDVQQGEwJVUzEOMAwGA1UECAwFVGV4YXMxDzANBgNVBAcMBkF1c3RpbjEZMBcG
A1UECgwQQ2xvdWRmbGFyZSwgSW5jLjEZMBcGA1UECwwQUHJvZHVjdCBTdHJhdGVn
eTERMA8GA1UEAwwIVGVzdCBPbmUwXDANBgkqhkiG9w0BAQEFAANLADBIAkEAwVQD
K0SJ25UFLznm2pU3zhzMEvpDEofHVNnCjk4mlDrtVop7PkKZ8pDEmuQANltUrxC8
yHBE2wXMv+GlH+bDtwIDAQABMA0GCSqGSIb3DQEBCwUAA0EAjVYQzozIFPkt/HRY
uUoZ8zEHIDICb0syFf5VAjm9AgTwIPzUmD+c5vl6LWDnxq7L45nLCzhhQ6YmiwDz
X7Wcyg==
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
MIIB4DCCAYoCCQDZfCdAJ+mwzDANBgkqhkiG9w0BAQsFADB3MQswCQYDVQQGEwJV
UzEOMAwGA1UECAwFVGV4YXMxDzANBgNVBAcMBkF1c3RpbjEZMBcGA1UECgwQQ2xv
dWRmbGFyZSwgSW5jLjEZMBcGA1UECwwQUHJvZHVjdCBTdHJhdGVneTERMA8GA1UE
AwwIVGVzdCBUd28wHhcNMTgwNDI2MTYxMTIwWhcNMjgwNDIzMTYxMTIwWjB3MQsw
CQYDVQQGEwJVUzEOMAwGA1UECAwFVGV4YXMxDzANBgNVBAcMBkF1c3RpbjEZMBcG
A1UECgwQQ2xvdWRmbGFyZSwgSW5jLjEZMBcGA1UECwwQUHJvZHVjdCBTdHJhdGVn
eTERMA8GA1UEAwwIVGVzdCBUd28wXDANBgkqhkiG9w0BAQEFAANLADBIAkEAoHKp
ROVK3zCSsH7ocYeyRAML4V7SFAbZcb4WIwDnE08oMBVRkQVcW5tqEkvG3RiClfzV
wZIJ3CfqKIeSNSDU9wIDAQABMA0GCSqGSIb3DQEBCwUAA0EAJw2gUbnPiq4C2p5b
iWzlA9Q7aKo+VQ4H7IZS7tTccr59nVjvH/TG3eWujpnocr4TOqW9M3CK1DF9mUGP
3pQ3Jg==
-----END CERTIFICATE-----
`)

var systemCertPoolSubjects []*pkix.Name

type certificateFixture struct {
	ou string
	cn string
}

func TestMain(m *testing.M) {
	systemCertPool, err := x509.SystemCertPool()
	if isUnrecoverableError(err) {
		os.Exit(1)
	}

	if systemCertPool == nil {
		// On Windows, let's just assume the system cert pool was empty
		systemCertPool = x509.NewCertPool()
	}

	systemCertPoolSubjects, err = getCertPoolSubjects(systemCertPool)
	if err != nil {
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func TestLoadOriginCertPoolJustSystemPool(t *testing.T) {
	certPoolSubjects := loadCertPoolSubjects(t, nil)
	extraSubjects := subjectSubtract(systemCertPoolSubjects, certPoolSubjects)

	// Remove extra subjects from the cert pool
	var filteredSystemCertPoolSubjects []*pkix.Name

	t.Log(extraSubjects)

OUTER:
	for _, subject := range certPoolSubjects {
		for _, extraSubject := range extraSubjects {
			if subject == extraSubject {
				t.Log(extraSubject)
				continue OUTER
			}
		}

		filteredSystemCertPoolSubjects = append(filteredSystemCertPoolSubjects, subject)
	}

	assert.Equal(t, len(filteredSystemCertPoolSubjects), len(systemCertPoolSubjects))

	difference := subjectSubtract(systemCertPoolSubjects, filteredSystemCertPoolSubjects)
	assert.Equal(t, 0, len(difference))
}

func TestLoadOriginCertPoolCFCertificates(t *testing.T) {
	certPoolSubjects := loadCertPoolSubjects(t, nil)

	extraSubjects := subjectSubtract(systemCertPoolSubjects, certPoolSubjects)

	expected := []*certificateFixture{
		{ou: "CloudFlare Origin SSL ECC Certificate Authority"},
		{ou: "CloudFlare Origin SSL Certificate Authority"},
		{cn: "origin-pull.cloudflare.net"},
		{cn: "Argo Tunnel Sample Hello Server Certificate"},
	}

	assertFixturesMatchSubjects(t, expected, extraSubjects)
}

func TestLoadOriginCertPoolWithExtraPEMs(t *testing.T) {
	certPoolWithoutPEMSubjects := loadCertPoolSubjects(t, nil)
	certPoolWithPEMSubjects := loadCertPoolSubjects(t, samplePEM)

	difference := subjectSubtract(certPoolWithoutPEMSubjects, certPoolWithPEMSubjects)

	assert.Equal(t, 2, len(difference))

	expected := []*certificateFixture{
		{cn: "Test One"},
		{cn: "Test Two"},
	}

	assertFixturesMatchSubjects(t, expected, difference)
}

func loadCertPoolSubjects(t *testing.T, originCAPoolPEM []byte) []*pkix.Name {
	certPool, err := loadOriginCertPool(originCAPoolPEM)
	if isUnrecoverableError(err) {
		t.Fatal(err)
	}
	assert.NotEmpty(t, certPool.Subjects())
	certPoolSubjects, err := getCertPoolSubjects(certPool)
	if err != nil {
		t.Fatal(err)
	}

	return certPoolSubjects
}

func assertFixturesMatchSubjects(t *testing.T, fixtures []*certificateFixture, subjects []*pkix.Name) {
	assert.Equal(t, len(fixtures), len(subjects))

	for _, fixture := range fixtures {
		found := false
		for _, subject := range subjects {
			found = found || fixtureMatchesSubjectPredicate(fixture, subject)
		}

		if !found {
			t.Fail()
		}
	}
}

func fixtureMatchesSubjectPredicate(fixture *certificateFixture, subject *pkix.Name) bool {
	cnMatch := true
	if fixture.cn != "" {
		cnMatch = fixture.cn == subject.CommonName
	}

	ouMatch := true
	if fixture.ou != "" {
		ouMatch = len(subject.OrganizationalUnit) > 0 && fixture.ou == subject.OrganizationalUnit[0]
	}

	return cnMatch && ouMatch
}

func subjectSubtract(left []*pkix.Name, right []*pkix.Name) []*pkix.Name {
	var difference []*pkix.Name

	var found bool
	for _, r := range right {
		found = false
		for _, l := range left {
			if (*l).String() == (*r).String() {
				found = true
			}
		}

		if !found {
			difference = append(difference, r)
		}
	}

	return difference
}

func getCertPoolSubjects(certPool *x509.CertPool) ([]*pkix.Name, error) {
	var subjects []*pkix.Name

	for _, subject := range certPool.Subjects() {
		var sequence pkix.RDNSequence
		_, err := asn1.Unmarshal(subject, &sequence)
		if err != nil {
			return nil, err
		}

		name := pkix.Name{}
		name.FillFromRDNSequence(&sequence)

		subjects = append(subjects, &name)
	}

	return subjects, nil
}

func isUnrecoverableError(err error) bool {
	return err != nil && err.Error() != "crypto/x509: system root pool is not available on Windows"
}

func TestTestIPBindable(t *testing.T) {
	assert.Nil(t, testIPBindable(nil))

	// Public services - if one of these IPs is on the machine, the test environment is too weird
	assert.NotNil(t, testIPBindable(net.ParseIP("8.8.8.8")))
	assert.NotNil(t, testIPBindable(net.ParseIP("1.1.1.1")))

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for i, addr := range addrs {
		if i >= 3 {
			break
		}
		ip := addr.(*net.IPNet).IP
		assert.Nil(t, testIPBindable(ip))
	}
}
