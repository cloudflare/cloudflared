package tunnel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHostnameFromURI(t *testing.T) {
	assert.Equal(t, "awesome.warptunnels.horse:22", hostnameFromURI("ssh://awesome.warptunnels.horse:22"))
	assert.Equal(t, "awesome.warptunnels.horse:22", hostnameFromURI("ssh://awesome.warptunnels.horse"))
	assert.Equal(t, "awesome.warptunnels.horse:2222", hostnameFromURI("ssh://awesome.warptunnels.horse:2222"))
	assert.Equal(t, "localhost:3389", hostnameFromURI("rdp://localhost"))
	assert.Equal(t, "localhost:3390", hostnameFromURI("rdp://localhost:3390"))
	assert.Equal(t, "", hostnameFromURI("trash"))
	assert.Equal(t, "", hostnameFromURI("https://awesomesauce.com"))
}
