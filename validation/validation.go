package validation

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/pkg/errors"
	"golang.org/x/net/idna"
)

const (
	defaultScheme   = "http"
	accessDomain    = "cloudflareaccess.com"
	accessCertPath  = "/cdn-cgi/access/certs"
	accessJwtHeader = "Cf-access-jwt-assertion"
)

var (
	supportedProtocols = []string{"http", "https", "rdp", "ssh", "smb", "tcp"}
	validationTimeout  = time.Duration(30 * time.Second)
)

func ValidateHostname(hostname string) (string, error) {
	if hostname == "" {
		return "", nil
	}
	// users gives url(contains schema) not just hostname
	if strings.Contains(hostname, ":") || strings.Contains(hostname, "%3A") {
		unescapeHostname, err := url.PathUnescape(hostname)
		if err != nil {
			return "", fmt.Errorf("Hostname(actually a URL) %s has invalid escape characters %s", hostname, unescapeHostname)
		}
		hostnameToURL, err := url.Parse(unescapeHostname)
		if err != nil {
			return "", fmt.Errorf("Hostname(actually a URL) %s has invalid format %s", hostname, hostnameToURL)
		}
		asciiHostname, err := idna.ToASCII(hostnameToURL.Hostname())
		if err != nil {
			return "", fmt.Errorf("Hostname(actually a URL) %s has invalid ASCII encdoing %s", hostname, asciiHostname)
		}
		return asciiHostname, nil
	}

	asciiHostname, err := idna.ToASCII(hostname)
	if err != nil {
		return "", fmt.Errorf("Hostname %s has invalid ASCII encdoing %s", hostname, asciiHostname)
	}
	hostnameToURL, err := url.Parse(asciiHostname)
	if err != nil {
		return "", fmt.Errorf("Hostname %s is not valid", hostnameToURL)
	}
	return hostnameToURL.RequestURI(), nil

}

// ValidateUrl returns a validated version of `originUrl` with a scheme prepended (by default http://).
// Note: when originUrl contains a scheme, the path is removed:
//
//	ValidateUrl("https://localhost:8080/api/") => "https://localhost:8080"
//
// but when it does not, the path is preserved:
//
//	ValidateUrl("localhost:8080/api/") => "http://localhost:8080/api/"
//
// This is arguably a bug, but changing it might break some cloudflared users.
func ValidateUrl(originUrl string) (*url.URL, error) {
	urlStr, err := validateUrlString(originUrl)
	if err != nil {
		return nil, err
	}
	return url.Parse(urlStr)
}

func validateUrlString(originUrl string) (string, error) {
	if originUrl == "" {
		return "", fmt.Errorf("URL should not be empty")
	}

	if net.ParseIP(originUrl) != nil {
		return validateIP("", originUrl, "")
	} else if strings.HasPrefix(originUrl, "[") && strings.HasSuffix(originUrl, "]") {
		// ParseIP doesn't recoginze [::1]
		return validateIP("", originUrl[1:len(originUrl)-1], "")
	}

	host, port, err := net.SplitHostPort(originUrl)
	// user might pass in an ip address like 127.0.0.1
	if err == nil && net.ParseIP(host) != nil {
		return validateIP("", host, port)
	}

	unescapedUrl, err := url.PathUnescape(originUrl)
	if err != nil {
		return "", fmt.Errorf("URL %s has invalid escape characters %s", originUrl, unescapedUrl)
	}

	parsedUrl, err := url.Parse(unescapedUrl)
	if err != nil {
		return "", fmt.Errorf("URL %s has invalid format", originUrl)
	}

	// if the url is in the form of host:port, IsAbs() will think host is the schema
	var hostname string
	hasScheme := parsedUrl.IsAbs() && parsedUrl.Host != ""
	if hasScheme {
		err := validateScheme(parsedUrl.Scheme)
		if err != nil {
			return "", err
		}
		// The earlier check for ip address will miss the case http://[::1]
		// and http://[::1]:8080
		if net.ParseIP(parsedUrl.Hostname()) != nil {
			return validateIP(parsedUrl.Scheme, parsedUrl.Hostname(), parsedUrl.Port())
		}
		hostname, err = ValidateHostname(parsedUrl.Hostname())
		if err != nil {
			return "", fmt.Errorf("URL %s has invalid format", originUrl)
		}
		if parsedUrl.Port() != "" {
			return fmt.Sprintf("%s://%s", parsedUrl.Scheme, net.JoinHostPort(hostname, parsedUrl.Port())), nil
		}
		return fmt.Sprintf("%s://%s", parsedUrl.Scheme, hostname), nil
	} else {
		if host == "" {
			hostname, err = ValidateHostname(originUrl)
			if err != nil {
				return "", fmt.Errorf("URL no %s has invalid format", originUrl)
			}
			return fmt.Sprintf("%s://%s", defaultScheme, hostname), nil
		} else {
			hostname, err = ValidateHostname(host)
			if err != nil {
				return "", fmt.Errorf("URL %s has invalid format", originUrl)
			}
			// This is why the path is preserved when `originUrl` doesn't have a schema.
			// Using `parsedUrl.Port()` here, instead of `port`, would remove the path
			return fmt.Sprintf("%s://%s", defaultScheme, net.JoinHostPort(hostname, port)), nil
		}
	}

}

func validateScheme(scheme string) error {
	for _, protocol := range supportedProtocols {
		if scheme == protocol {
			return nil
		}
	}
	return fmt.Errorf("Currently Cloudflare Tunnel does not support %s protocol.", scheme)
}

func validateIP(scheme, host, port string) (string, error) {
	if scheme == "" {
		scheme = defaultScheme
	}
	if port != "" {
		return fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(host, port)), nil
	} else if strings.Contains(host, ":") {
		// IPv6
		return fmt.Sprintf("%s://[%s]", scheme, host), nil
	}
	return fmt.Sprintf("%s://%s", scheme, host), nil
}

// originURL shouldn't be a pointer, because this function might change the scheme
func ValidateHTTPService(originURL string, hostname string, transport http.RoundTripper) error {
	parsedURL, err := url.Parse(originURL)
	if err != nil {
		return err
	}

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: validationTimeout,
	}

	initialRequest, err := http.NewRequest("GET", parsedURL.String(), nil)
	if err != nil {
		return err
	}
	initialRequest.Host = hostname
	resp, initialErr := client.Do(initialRequest)
	if initialErr == nil {
		resp.Body.Close()
		return nil
	}

	// Attempt the same endpoint via the other protocol (http/https); maybe we have better luck?
	oldScheme := parsedURL.Scheme
	parsedURL.Scheme = toggleProtocol(oldScheme)

	secondRequest, err := http.NewRequest("GET", parsedURL.String(), nil)
	if err != nil {
		return err
	}
	secondRequest.Host = hostname
	resp, secondErr := client.Do(secondRequest)
	if secondErr == nil { // Worked this time--advise the user to switch protocols
		_ = resp.Body.Close()
		return errors.Errorf(
			"%s doesn't seem to work over %s, but does seem to work over %s. Reason: %v. Consider changing the origin URL to %v",
			parsedURL.Host,
			oldScheme,
			parsedURL.Scheme,
			initialErr,
			originURL,
		)
	}

	return initialErr
}

func toggleProtocol(httpProtocol string) string {
	switch httpProtocol {
	case "http":
		return "https"
	case "https":
		return "http"
	default:
		return httpProtocol
	}
}

// Access checks if a JWT from Cloudflare Access is valid.
type Access struct {
	verifier *oidc.IDTokenVerifier
}

func NewAccessValidator(ctx context.Context, domain, issuer, applicationAUD string) (*Access, error) {
	domainURL, err := validateUrlString(domain)
	if err != nil {
		return nil, err
	}

	issuerURL, err := validateUrlString(issuer)
	if err != nil {
		return nil, err
	}

	// An issuerURL from Cloudflare Access will always use HTTPS.
	issuerURL = strings.Replace(issuerURL, "http:", "https:", 1)

	keySet := oidc.NewRemoteKeySet(ctx, domainURL+accessCertPath)
	return &Access{oidc.NewVerifier(issuerURL, keySet, &oidc.Config{ClientID: applicationAUD})}, nil
}

func (a *Access) Validate(ctx context.Context, jwt string) error {
	token, err := a.verifier.Verify(ctx, jwt)

	if err != nil {
		return errors.Wrapf(err, "token is invalid: %s", jwt)
	}

	// Perform extra sanity checks, just to be safe.

	if token == nil {
		return fmt.Errorf("token is nil: %s", jwt)
	}

	if !strings.HasSuffix(token.Issuer, accessDomain) {
		return fmt.Errorf("token has non-cloudflare issuer of %s: %s", token.Issuer, jwt)
	}

	return nil
}

func (a *Access) ValidateRequest(ctx context.Context, r *http.Request) error {
	return a.Validate(ctx, r.Header.Get(accessJwtHeader))
}
