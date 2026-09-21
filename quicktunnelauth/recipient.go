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
