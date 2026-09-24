package quicktunnelauth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQuickTunnelAuthRecipientPolicy(t *testing.T) {
	t.Parallel()

	policy, err := NewQuickTunnelAuthRecipientPolicy([]string{
		"Visitor@Example.com",
		"*@Allowed.example",
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		email   string
		allowed bool
	}{
		{name: "exact", email: "visitor@example.com", allowed: true},
		{name: "normalized exact", email: " Visitor@Example.com ", allowed: true},
		{name: "wildcard domain", email: "other@allowed.example", allowed: true},
		{name: "wildcard does not include subdomains", email: "other@sub.allowed.example"},
		{name: "wildcard rejects empty local part", email: "@allowed.example"},
		{name: "different address", email: "other@example.com"},
		{name: "invalid address", email: "not-an-email"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.allowed, policy.allows(test.email))
		})
	}
}

func TestQuickTunnelAuthRecipientPolicyAllowedRecipientCounts(t *testing.T) {
	t.Parallel()

	policy, err := NewQuickTunnelAuthRecipientPolicy([]string{
		"visitor@example.com",
		"Visitor@Example.com",
		"other@example.com",
		"*@allowed.example",
	})
	require.NoError(t, err)

	emailAddresses, emailDomains := policy.AllowedRecipientCounts()
	assert.Equal(t, 2, emailAddresses)
	assert.Equal(t, 1, emailDomains)

	var nilPolicy *QuickTunnelAuthRecipientPolicy
	emailAddresses, emailDomains = nilPolicy.AllowedRecipientCounts()
	assert.Zero(t, emailAddresses)
	assert.Zero(t, emailDomains)
}

func TestNewQuickTunnelAuthRecipientPolicyRejectsInvalidRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []string
	}{
		{name: "missing"},
		{name: "empty", values: []string{""}},
		{name: "invalid", values: []string{"not-an-email"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			policy, err := NewQuickTunnelAuthRecipientPolicy(test.values)
			require.Error(t, err)
			assert.Nil(t, policy)
		})
	}
}
