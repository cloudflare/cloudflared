package tunnel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TesthostnameFromURI(t *testing.T) {
	assert.Equal(t, "ssh://awesome.warptunnels.horse:22", hostnameFromURI("ssh://awesome.warptunnels.horse:22"))
	assert.Equal(t, "ssh://awesome.warptunnels.horse:22", hostnameFromURI("ssh://awesome.warptunnels.horse"))
	assert.Equal(t, "rdp://localhost:3389", hostnameFromURI("rdp://localhost"))
	assert.Equal(t, "", hostnameFromURI("trash"))
	assert.Equal(t, "", hostnameFromURI("https://awesomesauce.com"))
}
