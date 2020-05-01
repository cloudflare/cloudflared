// Package revoke provides functionality for checking the validity of
// a cert. Specifically, the temporal validity of the certificate is
// checked first, then any CRL in the cert is checked. OCSP is not
// supported at this time.
package revoke

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io/ioutil"
	"net/http"
	neturl "net/url"
	"time"

	"github.com/cloudflare/cfssl/log"
)

// HardFail determines whether the failure to check the revocation
// status of a certificate (i.e. due to network failure) causes
// verification to fail (a hard failure).
var HardFail = false

// TODO (kyle): figure out a good mechanism for OCSP; this requires
// presenting both the certificate and the issuer, and we don't have a
// good way at this time of getting the issuer.

// CRLSet associates a PKIX certificate list with the URL the CRL is
// fetched from.
var CRLSet = map[string]*pkix.CertificateList{}

// We can't handle LDAP certificates, so this checks to see if the
// URL string points to an LDAP resource so that we can ignore it.
func ldapURL(url string) bool {
	u, err := neturl.Parse(url)
	if err != nil {
		log.Warningf("invalid url %s: %v", url, err)
		return false
	}
	if u.Scheme == "ldap" {
		return true
	}
	return false
}

// revCheck should check the certificate for any revocations. It
// returns a pair of booleans: the first indicates whether the certificate
// is revoked, the second indicates whether the revocations were
// successfully checked.. This leads to the following combinations:
//
//	false, false: an error was encountered while checking revocations.
//
//	false, true:  the certificate was checked successfully and
//		      it is not revoked.
//
//      true, true:   the certificate was checked successfully and
//		      it is revoked.
func revCheck(cert *x509.Certificate) (revoked, ok bool) {
	for _, url := range cert.CRLDistributionPoints {
		if ldapURL(url) {
			log.Infof("skipping LDAP CRL: %s", url)
			continue
		}

		if revoked, ok := certIsRevokedCRL(cert, url); !ok {
			log.Warning("error checking revocation via CRL")
			if HardFail {
				return true, false
			}
			return false, false
		} else if revoked {
			log.Info("certificate is revoked via CRL")
			return true, true
		}
	}

	return false, true
}

// fetchCRL fetches and parses a CRL.
func fetchCRL(url string) (*pkix.CertificateList, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	} else if resp.StatusCode >= 300 {
		return nil, errors.New("failed to retrieve CRL")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	return x509.ParseCRL(body)
}

// check a cert against a specific CRL. Returns the same bool pair
// as revCheck.
func certIsRevokedCRL(cert *x509.Certificate, url string) (revoked, ok bool) {
	crl, ok := CRLSet[url]
	if ok && crl == nil {
		ok = false
		delete(CRLSet, url)
	}

	var shouldFetchCRL = true
	if ok {
		if !crl.HasExpired(time.Now()) {
			shouldFetchCRL = false
		}
	}

	if shouldFetchCRL {
		var err error
		crl, err = fetchCRL(url)
		if err != nil {
			log.Warningf("failed to fetch CRL: %v", err)
			return false, false
		}
		CRLSet[url] = crl
	}

	for _, revoked := range crl.TBSCertList.RevokedCertificates {
		if cert.SerialNumber.Cmp(revoked.SerialNumber) == 0 {
			log.Info("Serial number match: intermediate is revoked.")
			return true, true
		}
	}

	return false, true
}

// VerifyCertificate ensures that the certificate passed in hasn't
// expired and checks the CRL for the server.
func VerifyCertificate(cert *x509.Certificate) (revoked, ok bool) {
	if !time.Now().Before(cert.NotAfter) {
		log.Infof("Certificate expired %s\n", cert.NotAfter)
		return true, true
	} else if !time.Now().After(cert.NotBefore) {
		log.Infof("Certificate isn't valid until %s\n", cert.NotBefore)
		return true, true
	}

	return revCheck(cert)
}
