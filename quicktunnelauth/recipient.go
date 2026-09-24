package quicktunnelauth

import (
	"errors"
	"fmt"
	"strings"
)

// QuickTunnelAuthRecipientPolicy contains normalized, process-local recipient
// rules for one protected Quick Tunnel.
type QuickTunnelAuthRecipientPolicy struct {
	emails          map[string]struct{}
	wildcardDomains map[string]struct{}
}

// NewQuickTunnelAuthRecipientPolicy validates and normalizes allowed email and
// wildcard-domain rules.
func NewQuickTunnelAuthRecipientPolicy(values []string) (*QuickTunnelAuthRecipientPolicy, error) {
	if len(values) == 0 {
		return nil, errors.New("at least one allowed mail rule is required")
	}

	emails, wildcardDomains, err := validateQuickTunnelAllowedMail(values)
	if err != nil {
		return nil, fmt.Errorf("validate allowed mail rules: %w", err)
	}
	return &QuickTunnelAuthRecipientPolicy{
		emails:          emails,
		wildcardDomains: wildcardDomains,
	}, nil
}

// AllowedRecipientCounts returns the number of exact email addresses and
// wildcard-domain rules without exposing the configured recipients.
func (p *QuickTunnelAuthRecipientPolicy) AllowedRecipientCounts() (emailAddresses, emailDomains int) {
	if p == nil {
		return 0, 0
	}
	return len(p.emails), len(p.wildcardDomains)
}

func (p *QuickTunnelAuthRecipientPolicy) allows(email string) bool {
	if p == nil {
		return false
	}

	normalizedEmail := normalizeQuickTunnelEmail(email)
	if !isValidQuickTunnelEmail(normalizedEmail) {
		return false
	}

	if _, ok := p.emails[normalizedEmail]; ok {
		return true
	}
	_, domain, _ := strings.Cut(normalizedEmail, "@")
	_, ok := p.wildcardDomains[domain]
	return ok
}
