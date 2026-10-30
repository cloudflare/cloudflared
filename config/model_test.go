package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestForwarderHashIncludesIsFedramp ensures that toggling IsFedramp produces a
// different hash. The hash is what overwatch.AppManager uses to decide whether a
// forwarder from an updated config file is the same service as the running one,
// so a collision means the FedRAMP change is silently never applied.
func TestForwarderHashIncludesIsFedramp(t *testing.T) {
	commercial := Forwarder{
		URL:           "ssh.example.com",
		Listener:      "127.0.0.1:2222",
		TokenClientID: "id",
		TokenSecret:   "secret",
		Destination:   "destination",
		IsFedramp:     false,
	}
	fedramp := commercial
	fedramp.IsFedramp = true

	assert.NotEqual(t, commercial.Hash(), fedramp.Hash(), "changing IsFedramp must change the forwarder hash")
}
