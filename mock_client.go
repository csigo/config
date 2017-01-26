package config

import (
	"regexp"

	"github.com/stretchr/testify/mock"
)

// MockClient is the mock struct
type MockClient struct {
	mock.Mock
}

// ConfigInfo is the mock function.
func (m *MockClient) ConfigInfo() ConfigInfo {
	args := m.Called()
	return args.Get(0).(ConfigInfo)
}

// Get is the mock function.
func (m *MockClient) Get(path string) ([]byte, error) {
	args := m.Called(path)
	return args.Get(0).([]byte), args.Error(1)
}

// AddListener is the mock function.
func (m *MockClient) AddListener(pathRegEx *regexp.Regexp) *chan ModifiedFile {
	args := m.Called(pathRegEx)
	return args.Get(0).(*chan ModifiedFile)
}

// RemoveListener is the mock function.
func (m *MockClient) RemoveListener(ch *chan ModifiedFile) {
	m.Called(ch)
}

// Stop is to stop client
func (m *MockClient) Stop() error {
	args := m.Called()
	return args.Error(0)
}

// List is the mock function.
func (m *MockClient) List(path string) (map[string][]byte, error) {
	args := m.Called(path)
	return args.Get(0).(map[string][]byte), args.Error(1)
}

// Watch watches change of a file and invokes callback. It also invokes callback for the
func (m *MockClient) Watch(path string, callback func([]byte) error, errChan chan<- error) error {
	args := m.Called(path, callback, errChan)
	return args.Error(0)
}
