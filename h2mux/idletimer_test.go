package h2mux

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetry(t *testing.T) {
	timer := NewIdleTimer(time.Second, 2)
	assert.Equal(t, uint64(0), timer.RetryCount())
	ok := timer.Retry()
	assert.True(t, ok)
	assert.Equal(t, uint64(1), timer.RetryCount())
	ok = timer.Retry()
	assert.True(t, ok)
	assert.Equal(t, uint64(2), timer.RetryCount())
	ok = timer.Retry()
	assert.False(t, ok)
}

func TestMarkActive(t *testing.T) {
	timer := NewIdleTimer(time.Second, 2)
	assert.Equal(t, uint64(0), timer.RetryCount())
	ok := timer.Retry()
	assert.True(t, ok)
	assert.Equal(t, uint64(1), timer.RetryCount())
	timer.MarkActive()
	assert.Equal(t, uint64(0), timer.RetryCount())
}
