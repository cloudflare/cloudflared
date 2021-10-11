package edgediscovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProtocolPercentage(t *testing.T) {
	_, err := ProtocolPercentage()
	assert.NoError(t, err)
}
