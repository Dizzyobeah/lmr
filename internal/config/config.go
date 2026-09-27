// Package config provides configuration loading and validation for the Local Model Router.
package config

import (
	"fmt"
	"strings"
	"time"
)

// Config is the root configuration structure.
type Config struct {
	// Server contains HTTP server settings.
	Server ServerConfig `mapstructure:"server"`

	// Ollama contains Ollama connection settings.
	Ollama OllamaConfig `mapstructure:"ollama"`

	// Auth contains authentication settings.
	Auth AuthConfig `mapstructure:"auth"`

	// Keystore contains API key storage settings.
	Keystore KeystoreConfig `mapstructure:"keystore"`

	// Admin contains admin UI settings.
	Admin AdminConfig `mapstructure:"admin"`

	// Routing contains model routing settings.
	Routing RoutingConfig `mapstructure:"routing"`

	// Metrics contains Prometheus metrics settings.
	Metrics MetricsConfig `mapstructure:"metrics"`

	// Logging contains logging settings.
	Logging LoggingConfig `mapstructure:"logging"`
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	// Host is the address to bind to.
	Host string `mapstructure:"host"`

	// Port is the port to listen on.
	Port int `mapstructure:"port"`

	// ReadTimeout is the maximum duration for reading the entire request.
	ReadTimeout time.Duration `mapstructure:"read_timeout"`

	// WriteTimeout is the maximum duration before timing out writes of the response.
	WriteTimeout time.Duration `mapstructure:"write_timeout"`

	// IdleTimeout is the maximum amount of time to wait for the next request.
	IdleTimeout time.Duration `mapstructure:"idle_timeout"`
}

// OllamaConfig contains Ollama connection settings.
type OllamaConfig struct {
	// Endpoint is the Ollama API URL.
	Endpoint string `mapstructure:"endpoint"`

	// Timeout is the request timeout for Ollama API calls.
	Timeout time.Duration `mapstructure:"timeout"`

	// MaxConnections is the maximum number of connections to Ollama.
	MaxConnections int `mapstructure:"max_connections"`

	// RefreshInterval is how often to refresh the model list.
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`
}

// AuthConfig contains authentication settings.
type AuthConfig struct {
	// Enabled determines if authentication is required.
	Enabled bool `mapstructure:"enabled"`

	// EncryptionKey is the key used to encrypt sensitive data (from env var).
	// This should be a 32-byte hex-encoded string.
	EncryptionKey string `mapstructure:"encryption_key"`
}

// KeystoreConfig contains API key storage settings.
type KeystoreConfig struct {
	// Path is the path to the SQLite database file.
	Path string `mapstructure:"path"`
}

// AdminConfig contains admin UI settings.
type AdminConfig struct {
	// Enabled determines if the admin UI is available.
	Enabled bool `mapstructure:"enabled"`

	// Username is the admin username.
	Username string `mapstructure:"username"`

	// Password is the admin password (from env var).
	Password string `mapstructure:"password"`

	// Path is the URL path prefix for the admin UI.
	Path string `mapstructure:"path"`

	// SessionTTL is the admin session duration.
	SessionTTL time.Duration `mapstructure:"session_ttl"`

	// RecentRequestsLimit is the number of recent requests to keep in memory.
	RecentRequestsLimit int `mapstructure:"recent_requests_limit"`
}

// RoutingConfig contains model routing settings.
type RoutingConfig struct {
	// DefaultModel is the model to use when auto-routing can't determine a preference.
	DefaultModel string `mapstructure:"default_model"`

	// ForceRouting, when true, makes the router ignore the model named in a
	// request and always select the best model based on content classification.
	// This lets clients such as OpenCode treat the router as a single virtual
	// model: configure it once and never pick a model again.
	ForceRouting bool `mapstructure:"force_routing"`

	// VirtualModelName is the model name advertised by the /v1/models endpoint
	// when ForceRouting is enabled. Clients select this single model and all
	// their requests are routed intelligently. Defaults to "auto".
	VirtualModelName string `mapstructure:"virtual_model_name"`

	// VirtualModelDescription is a human-readable description for the virtual
	// model. It is currently informational only.
	VirtualModelDescription string `mapstructure:"virtual_model_description"`

	// Capabilities maps task types to model preferences.
	Capabilities map[string]CapabilityConfig `mapstructure:"capabilities"`

	// Rules contains custom routing rules.
	Rules []RoutingRule `mapstructure:"rules"`
}

// CapabilityConfig contains settings for a task capability.
type CapabilityConfig struct {
	// Keywords are words that trigger this capability.
	Keywords []string `mapstructure:"keywords"`

	// Patterns are regex patterns that trigger this capability.
	Patterns []string `mapstructure:"patterns"`

	// PreferModels is an ordered list of preferred models for this capability.
	PreferModels []string `mapstructure:"prefer_models"`

	// MaxInputTokens is the maximum input length to consider for this capability.
	// Requests shorter than this may be routed to faster models.
	MaxInputTokens int `mapstructure:"max_input_tokens"`
}

// RoutingRule is a custom routing rule.
type RoutingRule struct {
	// Name is a human-readable name for the rule.
	Name string `mapstructure:"name"`

	// Pattern is a regex pattern to match against the request content.
	Pattern string `mapstructure:"pattern"`

	// Model is the model to route to if the pattern matches.
	Model string `mapstructure:"model"`

	// Priority determines the order of rule evaluation (higher = first).
	Priority int `mapstructure:"priority"`
}

// MetricsConfig contains Prometheus metrics settings.
type MetricsConfig struct {
	// Enabled determines if metrics are collected and exposed.
	Enabled bool `mapstructure:"enabled"`

	// Path is the URL path for the metrics endpoint.
	Path string `mapstructure:"path"`

	// Detailed enables more detailed per-model metrics.
	Detailed bool `mapstructure:"detailed"`
}

// LoggingConfig contains logging settings.
type LoggingConfig struct {
	// Level is the minimum log level (debug, info, warn, error).
	Level string `mapstructure:"level"`

	// Format is the log format (json, text).
	Format string `mapstructure:"format"`

	// LogBodies enables logging of request/response bodies (careful: may contain sensitive data).
	LogBodies bool `mapstructure:"log_bodies"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:         "0.0.0.0",
			Port:         8080,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 120 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		Ollama: OllamaConfig{
			Endpoint:        "http://localhost:11434",
			Timeout:         120 * time.Second,
			MaxConnections:  100,
			RefreshInterval: 5 * time.Minute,
		},
		Auth: AuthConfig{
			Enabled: true,
		},
		Keystore: KeystoreConfig{
			Path: "./data/keys.db",
		},
		Admin: AdminConfig{
			Enabled:             true,
			Username:            "admin",
			Path:                "/admin",
			SessionTTL:          24 * time.Hour,
			RecentRequestsLimit: 1000,
		},
		Routing: RoutingConfig{
			DefaultModel:            "auto",
			ForceRouting:            true,
			VirtualModelName:        "auto",
			VirtualModelDescription: "Intelligent router that selects the best model per request",
			Capabilities: map[string]CapabilityConfig{
				"code": {
					Keywords:     []string{"code", "function", "implement", "debug", "refactor", "fix bug"},
					Patterns:     []string{"\\.py$", "\\.go$", "\\.js$", "\\.ts$", "\\.java$", "\\.rs$", "\\.cpp$"},
					PreferModels: []string{"deepseek-coder", "codellama", "qwen2.5-coder", "starcoder2"},
				},
				"reasoning": {
					Keywords:     []string{"explain", "why", "how does", "analyze", "compare", "reason", "think"},
					PreferModels: []string{"deepseek-r1", "llama3.2:70b", "llama3.2", "mixtral"},
				},
				"simple": {
					Keywords:       []string{"what is", "define", "list", "quick", "briefly"},
					PreferModels:   []string{"phi3", "llama3.2:1b", "gemma2:2b", "tinyllama"},
					MaxInputTokens: 50,
				},
				"creative": {
					Keywords:     []string{"story", "poem", "imagine", "write about", "creative", "fiction"},
					PreferModels: []string{"llama3.2", "mistral", "neural-chat"},
				},
				"math": {
					Keywords:     []string{"calculate", "solve", "equation", "integral", "derivative", "math"},
					PreferModels: []string{"deepseek-math", "wizard-math", "llama3.2"},
				},
			},
			Rules: []RoutingRule{},
		},
		Metrics: MetricsConfig{
			Enabled:  true,
			Path:     "/metrics",
			Detailed: true,
		},
		Logging: LoggingConfig{
			Level:     "info",
			Format:    "json",
			LogBodies: false,
		},
	}
}

// Validate validates the configuration and returns an error if invalid.
func (c *Config) Validate() error {
	var errors []string

	// Server validation
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		errors = append(errors, "server.port must be between 1 and 65535")
	}

	// Ollama validation
	if c.Ollama.Endpoint == "" {
		errors = append(errors, "ollama.endpoint is required")
	}

	// Auth validation
	if c.Auth.Enabled && c.Auth.EncryptionKey == "" {
		errors = append(errors, "auth.encryption_key is required when auth is enabled (set AUTH_ENCRYPTION_KEY env var)")
	}
	if c.Auth.EncryptionKey != "" && len(c.Auth.EncryptionKey) != 64 {
		errors = append(errors, "auth.encryption_key must be a 64-character hex string (32 bytes)")
	}

	// Admin validation
	if c.Admin.Enabled {
		if c.Admin.Username == "" {
			errors = append(errors, "admin.username is required when admin is enabled")
		}
		if c.Admin.Password == "" {
			errors = append(errors, "admin.password is required when admin is enabled (set ADMIN_PASSWORD env var)")
		}
	}

	// Logging validation
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[c.Logging.Level] {
		errors = append(errors, "logging.level must be one of: debug, info, warn, error")
	}

	validFormats := map[string]bool{"json": true, "text": true}
	if !validFormats[c.Logging.Format] {
		errors = append(errors, "logging.format must be one of: json, text")
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration errors:\n  - %s", strings.Join(errors, "\n  - "))
	}

	return nil
}

// Address returns the server address in host:port format.
func (c *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
