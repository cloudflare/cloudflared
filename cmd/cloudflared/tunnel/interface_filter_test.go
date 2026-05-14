package tunnel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsVirtualInterface(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		// Virtual interfaces that should be filtered
		{"br-1744e4cf9e20", true},
		{"docker0", true},
		{"docker1", true},
		{"veth1234abc", true},
		{"virbr0", true},
		{"vboxnet0", true},
		{"vmnet1", true},
		{"lxcbr0", true},
		{"lxdbr0", true},
		{"cni0", true},
		{"flannel.1", true},
		{"cali1234", true},
		{"weave", true},
		{"podman0", true},

		// Physical interfaces that should not be filtered
		{"eth0", false},
		{"enp6s0", false},
		{"ens192", false},
		{"eno1", false},
		{"wlan0", false},
		{"wlp3s0", false},
		{"lo", false},
		{"bond0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVirtualInterface(tt.name)
			assert.Equal(t, tt.expected, result, "isVirtualInterface(%q)", tt.name)
		})
	}
}

func TestIsPhysicalInterface(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		// Physical interfaces that should be prioritized (Linux)
		{"eth0", true},
		{"eth1", true},
		{"enp6s0", true},
		{"enp0s25", true},
		{"ens192", true},
		{"ens33", true},
		{"eno1", true},
		{"eno2", true},
		{"wlan0", true},
		{"wlan1", true},
		{"wlp3s0", true},
		{"wlp2s0", true},

		// Physical interfaces (macOS)
		{"en0", true},
		{"en1", true},
		{"en5", true},

		// Non-physical interfaces
		{"lo", false},
		{"lo0", false},
		{"docker0", false},
		{"br-abc123", false},
		{"veth1234", false},
		{"bond0", false},
		{"tun0", false},
		{"tap0", false},
		{"utun0", false},
		{"bridge0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPhysicalInterface(tt.name)
			assert.Equal(t, tt.expected, result, "isPhysicalInterface(%q)", tt.name)
		})
	}
}
