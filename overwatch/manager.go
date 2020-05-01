package overwatch

// Service is the required functions for an object to be managed by the overwatch Manager
type Service interface {
	Name() string
	Type() string
	Hash() string
	Shutdown()
	Run() error
}

// Manager is based type to manage running services
type Manager interface {
	Add(Service)
	Remove(string)
	Services() []Service
}
