package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	yaml "gopkg.in/yaml.v3"

	"github.com/cloudflare/cloudflared/validation"
)

var (
	// DefaultConfigFiles is the file names from which we attempt to read configuration.
	DefaultConfigFiles = []string{"config.yml", "config.yaml"}

	// DefaultUnixConfigLocation is the primary location to find a config file
	DefaultUnixConfigLocation = "/usr/local/etc/cloudflared"

	// DefaultUnixLogLocation is the primary location to find log files
	DefaultUnixLogLocation = "/var/log/cloudflared"

	// Launchd doesn't set root env variables, so there is default
	// Windows default config dir was ~/cloudflare-warp in documentation; let's keep it compatible
	defaultUserConfigDirs = []string{"~/.cloudflared", "~/.cloudflare-warp", "~/cloudflare-warp"}
	defaultNixConfigDirs  = []string{"/etc/cloudflared", DefaultUnixConfigLocation}

	ErrNoConfigFile = fmt.Errorf("Cannot determine default configuration path. No file %v in %v", DefaultConfigFiles, DefaultConfigSearchDirectories())
)

const (
	DefaultCredentialFile = "cert.pem"

	// BastionFlag is to enable bastion, or jump host, operation
	BastionFlag = "bastion"
)

// DefaultConfigDirectory returns the default directory of the config file
func DefaultConfigDirectory() string {
	if runtime.GOOS == "windows" {
		path := os.Getenv("CFDPATH")
		if path == "" {
			path = filepath.Join(os.Getenv("ProgramFiles(x86)"), "cloudflared")
			if _, err := os.Stat(path); os.IsNotExist(err) { // doesn't exist, so return an empty failure string
				return ""
			}
		}
		return path
	}
	return DefaultUnixConfigLocation
}

// DefaultLogDirectory returns the default directory for log files
func DefaultLogDirectory() string {
	if runtime.GOOS == "windows" {
		return DefaultConfigDirectory()
	}
	return DefaultUnixLogLocation
}

// DefaultConfigPath returns the default location of a config file
func DefaultConfigPath() string {
	dir := DefaultConfigDirectory()
	if dir == "" {
		return DefaultConfigFiles[0]
	}
	return filepath.Join(dir, DefaultConfigFiles[0])
}

// DefaultConfigSearchDirectories returns the default folder locations of the config
func DefaultConfigSearchDirectories() []string {
	dirs := make([]string, len(defaultUserConfigDirs))
	copy(dirs, defaultUserConfigDirs)
	if runtime.GOOS != "windows" {
		dirs = append(dirs, defaultNixConfigDirs...)
	}
	return dirs
}

// FileExists checks to see if a file exist at the provided path.
func FileExists(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// ignore missing files
			return false, nil
		}
		return false, err
	}
	_ = f.Close()
	return true, nil
}

// FindDefaultConfigPath returns the first path that contains a config file.
// If none of the combination of DefaultConfigSearchDirectories() and DefaultConfigFiles
// contains a config file, return empty string.
func FindDefaultConfigPath() string {
	for _, configDir := range DefaultConfigSearchDirectories() {
		for _, configFile := range DefaultConfigFiles {
			dirPath, err := homedir.Expand(configDir)
			if err != nil {
				continue
			}
			path := filepath.Join(dirPath, configFile)
			if ok, _ := FileExists(path); ok {
				return path
			}
		}
	}
	return ""
}

// FindOrCreateConfigPath returns the first path that contains a config file
// or creates one in the primary default path if it doesn't exist
func FindOrCreateConfigPath() string {
	path := FindDefaultConfigPath()

	if path == "" {
		// create the default directory if it doesn't exist
		path = DefaultConfigPath()
		if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
			return ""
		}

		// write a new config file out
		file, err := os.Create(path)
		if err != nil {
			return ""
		}
		defer file.Close()

		logDir := DefaultLogDirectory()
		_ = os.MkdirAll(logDir, os.ModePerm) // try and create it. Doesn't matter if it succeed or not, only byproduct will be no logs

		c := Root{
			LogDirectory: logDir,
		}
		if err := yaml.NewEncoder(file).Encode(&c); err != nil {
			return ""
		}
	}

	return path
}

// ValidateUnixSocket ensures --unix-socket param is used exclusively
// i.e. it fails if a user specifies both --url and --unix-socket
func ValidateUnixSocket(c *cli.Context) (string, error) {
	if c.IsSet("unix-socket") && (c.IsSet("url") || c.NArg() > 0) {
		return "", errors.New("--unix-socket must be used exclusivly.")
	}
	return c.String("unix-socket"), nil
}

// ValidateUrl will validate url flag correctness. It can be either from --url or argument
// Notice ValidateUnixSocket, it will enforce --unix-socket is not used with --url or argument
func ValidateUrl(c *cli.Context, allowURLFromArgs bool) (*url.URL, error) {
	var url = c.String("url")
	if allowURLFromArgs && c.NArg() > 0 {
		if c.IsSet("url") {
			return nil, errors.New("Specified origin urls using both --url and argument. Decide which one you want, I can only support one.")
		}
		url = c.Args().Get(0)
	}
	validUrl, err := validation.ValidateUrl(url)
	return validUrl, err
}

type UnvalidatedIngressRule struct {
	Hostname      string              `json:"hostname,omitempty"`
	Path          string              `json:"path,omitempty"`
	Service       string              `json:"service,omitempty"`
	OriginRequest OriginRequestConfig `yaml:"originRequest" json:"originRequest"`
}

// OriginRequestConfig is a set of optional fields that users may set to
// customize how cloudflared sends requests to origin services. It is used to set
// up general config that apply to all rules, and also, specific per-rule
// config.
// Note:
// - To specify a time.Duration in go-yaml, use e.g. "3s" or "24h".
// - To specify a time.Duration in json, use int64 of the nanoseconds
type OriginRequestConfig struct {
	// HTTP proxy timeout for establishing a new connection
	ConnectTimeout *CustomDuration `yaml:"connectTimeout" json:"connectTimeout,omitempty"`
	// HTTP proxy timeout for completing a TLS handshake
	TLSTimeout *CustomDuration `yaml:"tlsTimeout" json:"tlsTimeout,omitempty"`
	// HTTP proxy TCP keepalive duration
	TCPKeepAlive *CustomDuration `yaml:"tcpKeepAlive" json:"tcpKeepAlive,omitempty"`
	// HTTP proxy should disable "happy eyeballs" for IPv4/v6 fallback
	NoHappyEyeballs *bool `yaml:"noHappyEyeballs" json:"noHappyEyeballs,omitempty"`
	// HTTP proxy maximum keepalive connection pool size
	KeepAliveConnections *int `yaml:"keepAliveConnections" json:"keepAliveConnections,omitempty"`
	// HTTP proxy timeout for closing an idle connection
	KeepAliveTimeout *CustomDuration `yaml:"keepAliveTimeout" json:"keepAliveTimeout,omitempty"`
	// Sets the HTTP Host header for the local webserver.
	HTTPHostHeader *string `yaml:"httpHostHeader" json:"httpHostHeader,omitempty"`
	// Hostname on the origin server certificate.
	OriginServerName *string `yaml:"originServerName" json:"originServerName,omitempty"`
	// Path to the CA for the certificate of your origin.
	// This option should be used only if your certificate is not signed by Cloudflare.
	CAPool *string `yaml:"caPool" json:"caPool,omitempty"`
	// Disables TLS verification of the certificate presented by your origin.
	// Will allow any certificate from the origin to be accepted.
	// Note: The connection from your machine to Cloudflare's Edge is still encrypted.
	NoTLSVerify *bool `yaml:"noTLSVerify" json:"noTLSVerify,omitempty"`
	// Disables chunked transfer encoding.
	// Useful if you are running a WSGI server.
	DisableChunkedEncoding *bool `yaml:"disableChunkedEncoding" json:"disableChunkedEncoding,omitempty"`
	// Runs as jump host
	BastionMode *bool `yaml:"bastionMode" json:"bastionMode,omitempty"`
	// Listen address for the proxy.
	ProxyAddress *string `yaml:"proxyAddress" json:"proxyAddress,omitempty"`
	// Listen port for the proxy.
	ProxyPort *uint `yaml:"proxyPort" json:"proxyPort,omitempty"`
	// Valid options are 'socks' or empty.
	ProxyType *string `yaml:"proxyType" json:"proxyType,omitempty"`
	// IP rules for the proxy service
	IPRules []IngressIPRule `yaml:"ipRules" json:"ipRules,omitempty"`
	// Attempt to connect to origin with HTTP/2
	Http2Origin *bool `yaml:"http2Origin" json:"http2Origin,omitempty"`
}

type IngressIPRule struct {
	Prefix *string `yaml:"prefix" json:"prefix"`
	Ports  []int   `yaml:"ports" json:"ports"`
	Allow  bool    `yaml:"allow" json:"allow"`
}

type Configuration struct {
	TunnelID      string `yaml:"tunnel"`
	Ingress       []UnvalidatedIngressRule
	WarpRouting   WarpRoutingConfig   `yaml:"warp-routing"`
	OriginRequest OriginRequestConfig `yaml:"originRequest"`
	sourceFile    string
}

type WarpRoutingConfig struct {
	Enabled        bool            `yaml:"enabled" json:"enabled"`
	ConnectTimeout *CustomDuration `yaml:"connectTimeout" json:"connectTimeout,omitempty"`
	TCPKeepAlive   *CustomDuration `yaml:"tcpKeepAlive" json:"tcpKeepAlive,omitempty"`
}

type configFileSettings struct {
	Configuration `yaml:",inline"`
	// older settings will be aggregated into the generic map, should be read via cli.Context
	Settings map[string]interface{} `yaml:",inline"`
}

func (c *Configuration) Source() string {
	return c.sourceFile
}

func (c *configFileSettings) Int(name string) (int, error) {
	if raw, ok := c.Settings[name]; ok {
		if v, ok := raw.(int); ok {
			return v, nil
		}
		return 0, fmt.Errorf("expected int found %T for %s", raw, name)
	}
	return 0, nil
}

func (c *configFileSettings) Duration(name string) (time.Duration, error) {
	if raw, ok := c.Settings[name]; ok {
		switch v := raw.(type) {
		case time.Duration:
			return v, nil
		case string:
			return time.ParseDuration(v)
		}
		return 0, fmt.Errorf("expected duration found %T for %s", raw, name)
	}
	return 0, nil
}

func (c *configFileSettings) Float64(name string) (float64, error) {
	if raw, ok := c.Settings[name]; ok {
		if v, ok := raw.(float64); ok {
			return v, nil
		}
		return 0, fmt.Errorf("expected float found %T for %s", raw, name)
	}
	return 0, nil
}

func (c *configFileSettings) String(name string) (string, error) {
	if raw, ok := c.Settings[name]; ok {
		if v, ok := raw.(string); ok {
			return v, nil
		}
		return "", fmt.Errorf("expected string found %T for %s", raw, name)
	}
	return "", nil
}

func (c *configFileSettings) StringSlice(name string) ([]string, error) {
	if raw, ok := c.Settings[name]; ok {
		if slice, ok := raw.([]interface{}); ok {
			strSlice := make([]string, len(slice))
			for i, v := range slice {
				str, ok := v.(string)
				if !ok {
					return nil, fmt.Errorf("expected string, found %T for %v", i, v)
				}
				strSlice[i] = str
			}
			return strSlice, nil
		}
		return nil, fmt.Errorf("expected string slice found %T for %s", raw, name)
	}
	return nil, nil
}

func (c *configFileSettings) IntSlice(name string) ([]int, error) {
	if raw, ok := c.Settings[name]; ok {
		if slice, ok := raw.([]interface{}); ok {
			intSlice := make([]int, len(slice))
			for i, v := range slice {
				str, ok := v.(int)
				if !ok {
					return nil, fmt.Errorf("expected int, found %T for %v ", v, v)
				}
				intSlice[i] = str
			}
			return intSlice, nil
		}
		if v, ok := raw.([]int); ok {
			return v, nil
		}
		return nil, fmt.Errorf("expected int slice found %T for %s", raw, name)
	}
	return nil, nil
}

func (c *configFileSettings) Generic(name string) (cli.Generic, error) {
	return nil, errors.New("option type Generic not supported")
}

func (c *configFileSettings) Bool(name string) (bool, error) {
	if raw, ok := c.Settings[name]; ok {
		if v, ok := raw.(bool); ok {
			return v, nil
		}
		return false, fmt.Errorf("expected boolean found %T for %s", raw, name)
	}
	return false, nil
}

var configuration configFileSettings

func GetConfiguration() *Configuration {
	return &configuration.Configuration
}

// ReadConfigFile returns InputSourceContext initialized from the configuration file.
// On repeat calls returns with the same file, returns without reading the file again; however,
// if value of "config" flag changes, will read the new config file
func ReadConfigFile(c *cli.Context, log *zerolog.Logger) (settings *configFileSettings, warnings string, err error) {
	configFile := c.String("config")
	if configuration.Source() == configFile || configFile == "" {
		if configuration.Source() == "" {
			return nil, "", ErrNoConfigFile
		}
		return &configuration, "", nil
	}

	log.Debug().Msgf("Loading configuration from %s", configFile)
	file, err := os.Open(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			err = ErrNoConfigFile
		}
		return nil, "", err
	}
	defer file.Close()
	if err := yaml.NewDecoder(file).Decode(&configuration); err != nil {
		if err == io.EOF {
			log.Error().Msgf("Configuration file %s was empty", configFile)
			return &configuration, "", nil
		}
		return nil, "", errors.Wrap(err, "error parsing YAML in config file at "+configFile)
	}
	configuration.sourceFile = configFile

	// Parse it again, with strict mode, to find warnings.
	if file, err := os.Open(configFile); err == nil {
		decoder := yaml.NewDecoder(file)
		decoder.KnownFields(true)
		var unusedConfig configFileSettings
		if err := decoder.Decode(&unusedConfig); err != nil {
			warnings = err.Error()
		}
	}

	return &configuration, warnings, nil
}

// A CustomDuration is a Duration that has custom serialization for JSON.
// JSON in Javascript assumes that int fields are 32 bits and Duration fields are deserialized assuming that numbers
// are in nanoseconds, which in 32bit integers limits to just 2 seconds.
// This type assumes that when serializing/deserializing from JSON, that the number is in seconds, while it maintains
// the YAML serde assumptions.
type CustomDuration struct {
	time.Duration
}

func (s CustomDuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Duration.Seconds())
}

func (s *CustomDuration) UnmarshalJSON(data []byte) error {
	seconds, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return err
	}

	s.Duration = time.Duration(seconds * int64(time.Second))
	return nil
}

func (s *CustomDuration) MarshalYAML() (interface{}, error) {
	return s.Duration.String(), nil
}

func (s *CustomDuration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	return unmarshal(&s.Duration)
}
