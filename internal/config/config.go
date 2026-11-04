// config.go - Carga de configuración desde YAML/env.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config del servicio.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Security SecurityConfig `yaml:"security"`
	Logging  LoggingConfig  `yaml:"logging"`
	Spidi    SpidiConfig    `yaml:"spidi"`
}

type ServerConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	ReadTimeout  int    `yaml:"read_timeout"`
	WriteTimeout int    `yaml:"write_timeout"`
}

type DatabaseConfig struct {
	Path     string `yaml:"path"`
	PoolSize int    `yaml:"pool_size"`
}

type SecurityConfig struct {
	MasterKey  string `yaml:"master_key"`
	RateLimit  int    `yaml:"rate_limit"`
	APIPrefix  string `yaml:"api_prefix"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"` // json / text
}

type SpidiConfig struct {
	MaxRequestsPerSec int `yaml:"max_requests_per_sec"`
	MaxRetries        int `yaml:"max_retries"`
}

// Default retorna la configuración por defecto.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host:         "0.0.0.0",
			Port:         8484,
			ReadTimeout:  30,
			WriteTimeout: 30,
		},
		Database: DatabaseConfig{
			Path:     "data/spidi.db",
			PoolSize: 10,
		},
		Security: SecurityConfig{
			RateLimit: 100,
			APIPrefix: "/",
		},
		Logging: LoggingConfig{
			Level:  "INFO",
			Format: "text",
		},
		Spidi: SpidiConfig{
			MaxRequestsPerSec: 10,
			MaxRetries:        3,
		},
	}
}

// Load carga la configuración desde archivo.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config: open: %w", err)
	}
	defer f.Close()

	if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
		return nil, fmt.Errorf("config: decode: %w", err)
	}
	return cfg, nil
}

// Validate valida la configuración.
func (c *Config) Validate() error {
	if c.Security.MasterKey == "" {
		if env := os.Getenv("SPIDI_MASTER_KEY"); env != "" {
			c.Security.MasterKey = env
		} else {
			return fmt.Errorf("config: security.master_key es requerida (o variable de entorno SPIDI_MASTER_KEY)")
		}
	}
	if c.Server.Port <= 0 {
		return fmt.Errorf("config: server.port debe ser positivo")
	}
	return nil
}

// BindAddr retorna la dirección de escucha.
func (c *Config) BindAddr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}
