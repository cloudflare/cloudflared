package logger

// MockWriteManager does nothing and is provided for testing purposes
type MockWriteManager struct {
}

// NewMockWriteManager creates an OutputManager that does nothing for testing purposes
func NewMockWriteManager() OutputManager {
	return &MockWriteManager{}
}

// Append is a mock stub
func (m *MockWriteManager) Append(data []byte, target LogOutput) {
}

// Shutdown is a mock stub
func (m *MockWriteManager) Shutdown() {
}
