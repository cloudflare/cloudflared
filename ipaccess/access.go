package ipaccess

import (
	"fmt"
	"net"
	"sort"
)

type Policy struct {
	defaultAllow bool
	rules        []Rule
}

type Rule struct {
	ipNet *net.IPNet
	ports []int
	allow bool
}

func NewPolicy(defaultAllow bool, rules []Rule) (*Policy, error) {
	for _, rule := range rules {
		if err := rule.Validate(); err != nil {
			return nil, err
		}
	}

	policy := Policy{
		defaultAllow: defaultAllow,
		rules:        rules,
	}

	return &policy, nil
}

func NewRuleByCIDR(prefix *string, ports []int, allow bool) (Rule, error) {
	if prefix == nil || len(*prefix) == 0 {
		return Rule{}, fmt.Errorf("no prefix provided")
	}

	_, ipnet, err := net.ParseCIDR(*prefix)
	if err != nil {
		return Rule{}, fmt.Errorf("unable to parse cidr: %s", *prefix)
	}

	return NewRule(ipnet, ports, allow)
}

func NewRule(ipnet *net.IPNet, ports []int, allow bool) (Rule, error) {
	rule := Rule{
		ipNet: ipnet,
		ports: ports,
		allow: allow,
	}
	return rule, rule.Validate()
}

func (r *Rule) Validate() error {
	if r.ipNet == nil {
		return fmt.Errorf("no ipnet set on the rule")
	}

	if len(r.ports) > 0 {
		sort.Ints(r.ports)
		for _, port := range r.ports {
			if port < 1 || port > 65535 {
				return fmt.Errorf("invalid port %d, needs to be between 1 and 65535", port)
			}
		}
	}

	return nil
}

func (h *Policy) Allowed(ip net.IP, port int) (bool, *Rule) {
	if len(h.rules) == 0 {
		return h.defaultAllow, nil
	}

	for _, rule := range h.rules {
		if rule.ipNet.Contains(ip) {
			if len(rule.ports) == 0 {
				return rule.allow, &rule
			} else if pos := sort.SearchInts(rule.ports, port); pos < len(rule.ports) && rule.ports[pos] == port {
				return rule.allow, &rule
			}
		}
	}

	return h.defaultAllow, nil
}

func (ipr *Rule) String() string {
	return fmt.Sprintf("prefix:%s/port:%s/allow:%t", ipr.ipNet, ipr.PortsString(), ipr.allow)
}

func (ipr *Rule) PortsString() string {
	if len(ipr.ports) > 0 {
		return fmt.Sprint(ipr.ports)
	}
	return "all"
}

func (ipr *Rule) Ports() []int {
	return ipr.ports
}

func (ipr *Rule) RulePolicy() bool {
	return ipr.allow
}

func (ipr *Rule) StringCIDR() string {
	return ipr.ipNet.String()
}
