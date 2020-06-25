package overwatch

import (
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockService struct {
	serviceName string
	serviceType string
	runError    error
}

func (s *mockService) Name() string {
	return s.serviceName
}

func (s *mockService) Type() string {
	return s.serviceType
}

func (s *mockService) Hash() string {
	h := md5.New()
	io.WriteString(h, s.serviceName)
	io.WriteString(h, s.serviceType)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (s *mockService) Shutdown() {
}

func (s *mockService) Run() error {
	return s.runError
}

func TestManagerAddAndRemove(t *testing.T) {
	m := NewAppManager(nil)

	first := &mockService{serviceName: "first", serviceType: "mock"}
	second := &mockService{serviceName: "second", serviceType: "mock"}
	m.Add(first)
	m.Add(second)
	assert.Len(t, m.Services(), 2, "expected 2 services in the list")

	m.Remove(first.Name())
	services := m.Services()
	assert.Len(t, services, 1, "expected 1 service in the list")
	assert.Equal(t, second.Hash(), services[0].Hash(), "hashes should match. Wrong service was removed")
}

func TestManagerDuplicate(t *testing.T) {
	m := NewAppManager(nil)

	first := &mockService{serviceName: "first", serviceType: "mock"}
	m.Add(first)
	m.Add(first)
	assert.Len(t, m.Services(), 1, "expected 1 service in the list")
}

func TestManagerErrorChannel(t *testing.T) {
	errChan := make(chan error)
	serviceCallback := func(t string, name string, err error) {
		errChan <- err
	}
	m := NewAppManager(serviceCallback)

	err := errors.New("test error")
	first := &mockService{serviceName: "first", serviceType: "mock", runError: err}
	m.Add(first)
	respErr := <-errChan
	assert.Equal(t, err, respErr, "errors don't match")
}
