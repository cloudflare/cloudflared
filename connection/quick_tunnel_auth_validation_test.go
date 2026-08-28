package connection

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateQuickTunnelAllowedMail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		values          []string
		expectedEmails  []string
		expectedDomains []string
	}{
		{name: "multiple without spaces", values: []string{"one@example.com,two@example.com"}, expectedEmails: []string{"one@example.com", "two@example.com"}},
		{name: "multiple with consistent spaces", values: []string{"one@example.com, two@example.com"}, expectedEmails: []string{"one@example.com", "two@example.com"}},
		{name: "multiple with inconsistent spaces", values: []string{" one@Example.com,two@example.com , THREE@example.com "}, expectedEmails: []string{"one@example.com", "two@example.com", "three@example.com"}},
		{name: "single with leading spaces", values: []string{" user@example.com"}, expectedEmails: []string{"user@example.com"}},
		{name: "single with trailing spaces", values: []string{"user@example.com "}, expectedEmails: []string{"user@example.com"}},
		{name: "single with surrounding spaces", values: []string{" User@Example.com "}, expectedEmails: []string{"user@example.com"}},
		{name: "repeated flags", values: []string{"first@example.com", "second@example.net,*@Example.org"}, expectedEmails: []string{"first@example.com", "second@example.net"}, expectedDomains: []string{"example.org"}},
		{name: "deduplicated", values: []string{"User@example.com,user@example.com", "*@Example.org,*@example.org"}, expectedEmails: []string{"user@example.com"}, expectedDomains: []string{"example.org"}},
		{name: "punycode IDN", values: []string{"jim@something.xn--fiqs8s,*@xn--fiqs8s"}, expectedEmails: []string{"jim@something.xn--fiqs8s"}, expectedDomains: []string{"xn--fiqs8s"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			emails, domains, err := validateQuickTunnelAllowedMail(test.values)
			require.NoError(t, err)
			assert.Len(t, emails, len(test.expectedEmails))
			assert.Len(t, domains, len(test.expectedDomains))
			for _, email := range test.expectedEmails {
				assert.Contains(t, emails, email)
			}
			for _, domain := range test.expectedDomains {
				assert.Contains(t, domains, domain)
			}
		})
	}
}

func TestValidateQuickTunnelAllowedMailRejectsMalformedEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []string
	}{
		{name: "empty"},
		{name: "blank", values: []string{" , "}},
		{name: "display name", values: []string{"User <user@example.com>"}},
		{name: "missing wildcard at", values: []string{"*example.com"}},
		{name: "double at", values: []string{"user@@example.com"}},
		{name: "invalid exact domain", values: []string{"user@-example.com"}},
		{name: "empty wildcard domain", values: []string{"*@"}},
		{name: "empty domain label", values: []string{"*@example..com"}},
		{name: "leading domain hyphen", values: []string{"*@-example.com"}},
		{name: "trailing domain hyphen", values: []string{"*@example-.com"}},
		{name: "invalid domain character", values: []string{"*@exam_ple.com"}},
		{name: "unicode IDN", values: []string{"jim@something.\u4e2d\u56fd"}},
		{name: "long domain label", values: []string{"*@" + strings.Repeat("a", 64) + ".com"}},
		{name: "long domain", values: []string{"*@" + strings.Repeat("a.", 126) + "aa"}},
		{name: "domain path", values: []string{"*@example.com/path"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := validateQuickTunnelAllowedMail(test.values)
			require.Error(t, err)
		})
	}
}

func TestValidateQuickTunnelAllowedMailRejectsEmptyRuleWithoutLeakingValues(t *testing.T) {
	t.Parallel()

	values := []string{"first@example.com,,second@example.com"}
	_, _, err := validateQuickTunnelAllowedMail(values)
	require.EqualError(t, err, "allowed mail rule 2 is empty")
	assert.NotContains(t, err.Error(), "example.com")
}

func TestValidateQuickTunnelAllowedMailReportsInvalidRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		values        []string
		expectedError string
	}{
		{name: "email", values: []string{"first@example.com", "private@example.com/path"}, expectedError: `allowed mail rule "private@example.com/path" is not a valid email address`},
		{name: "wildcard", values: []string{"first@example.com", "*@example.com/path"}, expectedError: `allowed mail rule "*@example.com/path" has an invalid wildcard domain`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := validateQuickTunnelAllowedMail(test.values)
			require.EqualError(t, err, test.expectedError)
		})
	}
}
