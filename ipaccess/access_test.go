package ipaccess

import (
	"bytes"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRuleCreation(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("1.1.1.1/24")

	_, err := NewRule(nil, []int{80}, false)
	assert.Error(t, err, "expected error as no ipnet provided")

	_, err = NewRule(ipnet, []int{65536, 80}, false)
	assert.Error(t, err, "expected error as port higher than 65535")

	_, err = NewRule(ipnet, []int{80, -1}, false)
	assert.Error(t, err, "expected error as port less than 0")

	rule, err := NewRule(ipnet, []int{443, 80}, false)
	assert.NoError(t, err)
	assert.True(t, ipnet.IP.Equal(rule.ipNet.IP) && bytes.Compare(ipnet.Mask, rule.ipNet.Mask) == 0, "ipnet expected to be %+v, got: %+v", ipnet, rule.ipNet)
	assert.True(t, len(rule.ports) == 2 && rule.ports[0] == 80 && rule.ports[1] == 443, "expected ports to be sorted")
}

func TestRuleCreationByCIDR(t *testing.T) {
	var cidr *string
	_, err := NewRuleByCIDR(cidr, []int{80}, false)
	assert.Error(t, err, "expected error as cidr is nil")

	badCidr := "1.1.1.1"
	cidr = &badCidr
	_, err = NewRuleByCIDR(cidr, []int{80}, false)
	assert.Error(t, err, "expected error as the cidr is bad")

	goodCidr := "1.1.1.1/24"
	_, ipnet, _ := net.ParseCIDR("1.1.1.0/24")
	cidr = &goodCidr
	rule, err := NewRuleByCIDR(cidr, []int{80}, false)
	assert.NoError(t, err)
	assert.True(t, ipnet.IP.Equal(rule.ipNet.IP) && bytes.Compare(ipnet.Mask, rule.ipNet.Mask) == 0, "ipnet expected to be %+v, got: %+v", ipnet, rule.ipNet)
}

func TestRulesNoRules(t *testing.T) {
	ip, _, _ := net.ParseCIDR("1.2.3.4/24")

	policy, _ := NewPolicy(true, []Rule{})

	allowed, rule := policy.Allowed(ip, 80)
	assert.True(t, allowed, "expected to be allowed as no rules and default allow")
	assert.Nil(t, rule, "expected to be nil as no rules")

	policy, _ = NewPolicy(false, []Rule{})

	allowed, rule = policy.Allowed(ip, 80)
	assert.False(t, allowed, "expected to be denied as no rules and default deny")
	assert.Nil(t, rule, "expected to be nil as no rules")
}

func TestRulesMatchIPAndPort(t *testing.T) {
	ip1, ipnet1, _ := net.ParseCIDR("1.2.3.4/24")
	ip2, _, _ := net.ParseCIDR("2.3.4.5/24")

	rule1, _ := NewRule(ipnet1, []int{80, 443}, true)
	rules := []Rule{
		rule1,
	}

	policy, _ := NewPolicy(false, rules)

	allowed, rule := policy.Allowed(ip1, 80)
	assert.True(t, allowed, "expected to be allowed as matching rule")
	assert.True(t, rule.ipNet == ipnet1, "expected to match ipnet1")

	allowed, rule = policy.Allowed(ip2, 80)
	assert.False(t, allowed, "expected to be denied as no matching rule")
	assert.Nil(t, rule, "expected to be nil")
}

func TestRulesMatchIPAndPort2(t *testing.T) {
	ip1, ipnet1, _ := net.ParseCIDR("1.2.3.4/24")
	ip2, ipnet2, _ := net.ParseCIDR("2.3.4.5/24")

	rule1, _ := NewRule(ipnet1, []int{53, 80}, false)
	rule2, _ := NewRule(ipnet2, []int{53, 80}, true)
	rules := []Rule{
		rule1,
		rule2,
	}

	policy, _ := NewPolicy(false, rules)

	allowed, rule := policy.Allowed(ip1, 80)
	assert.False(t, allowed, "expected to be denied as matching rule")
	assert.True(t, rule.ipNet == ipnet1, "expected to match ipnet1")

	allowed, rule = policy.Allowed(ip2, 80)
	assert.True(t, allowed, "expected to be allowed as matching rule")
	assert.True(t, rule.ipNet == ipnet2, "expected to match ipnet1")

	allowed, rule = policy.Allowed(ip2, 81)
	assert.False(t, allowed, "expected to be denied as no matching rule")
	assert.Nil(t, rule, "expected to be nil")
}
