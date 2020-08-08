package tunnel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHostFromURI(t *testing.T) {
	assert.Equal(t, "awesome.warptunnels.horse:22", hostFromURI("ssh://awesome.warptunnels.horse:22"))
	assert.Equal(t, "awesome.warptunnels.horse:22", hostFromURI("ssh://awesome.warptunnels.horse"))
	assert.Equal(t, "awesome.warptunnels.horse:2222", hostFromURI("ssh://awesome.warptunnels.horse:2222"))
	assert.Equal(t, "localhost:3389", hostFromURI("rdp://localhost"))
	assert.Equal(t, "localhost:3390", hostFromURI("rdp://localhost:3390"))
	assert.Equal(t, "", hostFromURI("trash"))
	assert.Equal(t, "", hostFromURI("https://awesomesauce.com"))
}
