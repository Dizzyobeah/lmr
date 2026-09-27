package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Load loads configuration from file and environment variables.
// The configPath parameter specifies the path to the configuration file.
// If empty, it will look for config.yaml in the current directory.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Configure viper
	v.SetConfigType("yaml")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Bind environment variables explicitly for sensitive values
	v.BindEnv("auth.encryption_key", "AUTH_ENCRYPTION_KEY")
	v.BindEnv("admin.password", "ADMIN_PASSWORD")
	v.BindEnv("ollama.endpoint", "OLLAMA_ENDPOINT")

	// Load config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.AddConfigPath("/etc/local-model-router")
	}

	// Read config file (ignore if not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		// Config file not found is okay, we'll use defaults and env vars
	}

	// Unmarshal into config struct
	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// LoadFromFile loads configuration from a specific file path.
func LoadFromFile(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", path)
	}
	return Load(path)
}

// setDefaults sets default values in viper.
func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.read_timeout", "30s")
	v.SetDefault("server.write_timeout", "120s")
	v.SetDefault("server.idle_timeout", "60s")

	// Ollama defaults
	v.SetDefault("ollama.endpoint", "http://localhost:11434")
	v.SetDefault("ollama.timeout", "120s")
	v.SetDefault("ollama.max_connections", 100)
	v.SetDefault("ollama.refresh_interval", "5m")

	// Auth defaults
	v.SetDefault("auth.enabled", true)

	// Keystore defaults
	v.SetDefault("keystore.path", "./data/keys.db")

	// Admin defaults
	v.SetDefault("admin.enabled", true)
	v.SetDefault("admin.username", "admin")
	v.SetDefault("admin.path", "/admin")
	v.SetDefault("admin.session_ttl", "24h")
	v.SetDefault("admin.recent_requests_limit", 1000)

	// Routing defaults
	v.SetDefault("routing.default_model", "auto")

	// Metrics defaults
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.path", "/metrics")
	v.SetDefault("metrics.detailed", true)

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("logging.log_bodies", false)
}

// MustLoad loads configuration and panics on error.
// This is useful for initialization where configuration errors should be fatal.
func MustLoad(configPath string) *Config {
	cfg, err := Load(configPath)
	if err != nil {
		panic(fmt.Sprintf("failed to load configuration: %v", err))
	}
	return cfg
}
