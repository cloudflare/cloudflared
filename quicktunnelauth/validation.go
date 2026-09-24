package quicktunnelauth

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"golang.org/x/net/idna"
)

const (
	quickTunnelAuthMaxAllowedMailRules     = 100
	quickTunnelAuthMaxAllowedMailRuleBytes = 320
)

// validateQuickTunnelAllowedMail validates and normalizes exact email addresses and
// wildcard domains from one or more comma-separated values.
func validateQuickTunnelAllowedMail(values []string) (emails, wildcardDomains map[string]struct{}, err error) {
	if len(values) == 0 {
		return nil, nil, errors.New("allowed mail rule 1 is empty")
	}

	emails, wildcardDomains = make(map[string]struct{}), make(map[string]struct{})
	for ruleIndex, rawEntry := range strings.Split(strings.Join(values, ","), ",") {
		rulePosition := ruleIndex + 1
		if rulePosition > quickTunnelAuthMaxAllowedMailRules {
			return nil, nil, fmt.Errorf("allowed mail rules exceed the %d-entry limit", quickTunnelAuthMaxAllowedMailRules)
		}
		if len(rawEntry) > quickTunnelAuthMaxAllowedMailRuleBytes {
			return nil, nil, fmt.Errorf(
				"allowed mail rule %d exceeds the %d-byte limit",
				rulePosition,
				quickTunnelAuthMaxAllowedMailRuleBytes,
			)
		}

		entry := normalizeQuickTunnelEmail(rawEntry)
		domain, isWildcard := strings.CutPrefix(entry, "*@")

		switch {
		case entry == "":
			return nil, nil, fmt.Errorf("allowed mail rule %d is empty", rulePosition)

		case isWildcard:
			if !isValidQuickTunnelEmailDomain(domain) {
				return nil, nil, fmt.Errorf("allowed mail rule %d has an invalid wildcard domain", rulePosition)
			}
			wildcardDomains[domain] = struct{}{}

		default:
			if !isValidQuickTunnelEmail(entry) {
				return nil, nil, fmt.Errorf("allowed mail rule %d is not a valid email address", rulePosition)
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
