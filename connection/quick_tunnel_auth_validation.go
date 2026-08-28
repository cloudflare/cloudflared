package connection

import (
	"fmt"
	"net/mail"
	"strings"

	"golang.org/x/net/idna"
)

// validateQuickTunnelAllowedMail validates and normalizes exact email addresses and
// wildcard domains from one or more comma-separated values.
func validateQuickTunnelAllowedMail(values []string) (emails, wildcardDomains map[string]struct{}, err error) {
	emails, wildcardDomains = make(map[string]struct{}), make(map[string]struct{})
	for i, rawEntry := range strings.Split(strings.Join(values, ","), ",") {
		entry := normalizeQuickTunnelEmail(rawEntry)
		domain, isWildcard := strings.CutPrefix(entry, "*@")

		switch {
		case entry == "":
			return nil, nil, fmt.Errorf("allowed mail rule %d is empty", i+1)

		case isWildcard:
			if !isValidQuickTunnelEmailDomain(domain) {
				return nil, nil, fmt.Errorf(
					"allowed mail rule %q has an invalid wildcard domain",
					rawEntry,
				)
			}
			wildcardDomains[domain] = struct{}{}

		default:
			if !isValidQuickTunnelEmail(entry) {
				return nil, nil, fmt.Errorf(
					"allowed mail rule %q is not a valid email address",
					rawEntry,
				)
			}
			emails[entry] = struct{}{}
		}
	}

	return emails, wildcardDomains, nil
}

func isValidQuickTunnelEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	// ParseAddress accepts mailbox forms such as John Smith <jsmith@example.com>,
	// so require the input to be a bare email address.
	if err != nil || address.Address != email {
		return false
	}

	_, domain, ok := strings.Cut(email, "@")
	return ok && isValidQuickTunnelEmailDomain(domain)
}

func isValidQuickTunnelEmailDomain(domain string) bool {
	asciiDomain, err := idna.Registration.ToASCII(domain)
	// IDNs must be supplied in their ASCII punycode representation.
	return domain != "" && err == nil && asciiDomain == domain
}

func normalizeQuickTunnelEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
