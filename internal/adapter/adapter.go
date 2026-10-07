package adapter

import "errors"

var (
	ErrServerNotRunning = errors.New("web server is not running")
	ErrInvalidConfig    = errors.New("web server configuration test failed")
)

// WebServerAdapter defines the lifecycle and configuration contract for web servers.
type WebServerAdapter interface {
	Name() string
	Start() error
	Stop() error
	ApplyConfig(domain string, rootPath string) error
	RemoveConfig(domain string) error
	ProvisionSSL(domain string) error
	TestConfig() error
}
