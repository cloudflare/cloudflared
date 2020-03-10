package h2mux

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChopEncodedHeaders(t *testing.T) {
	mockEncodedHeaders := make([]byte, 5)
	for i := range mockEncodedHeaders {
		mockEncodedHeaders[i] = byte(i)
	}
	chopped := chopEncodedHeaders(mockEncodedHeaders, 4)

	assert.Equal(t, 2, len(chopped))
	assert.Equal(t, []byte{0, 1, 2, 3}, chopped[0])
	assert.Equal(t, []byte{4}, chopped[1])
}

func TestChopEncodedEmptyHeaders(t *testing.T) {
	mockEncodedHeaders := make([]byte, 0)
	chopped := chopEncodedHeaders(mockEncodedHeaders, 3)

	assert.Equal(t, 0, len(chopped))
}
