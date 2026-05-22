// Package config loads the report-service configuration via koanf,
// following the same precedence as the rest of the OpenFoundry Go
// services (defaults → image YAML → CONFIG_FILE override → OF_* env).
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Service struct {
		Name    string `koanf:"name"`
		Version string `koanf:"version"`
	} `koanf:"service"`

	Server struct {
		Addr            string `koanf:"addr"`
		ShutdownTimeout string `koanf:"shutdown_timeout"`
	} `koanf:"server"`

	JWT struct {
		Secret   string `koanf:"secret"`
		Issuer   string `koanf:"issuer"`
		Audience string `koanf:"audience"`
	} `koanf:"jwt"`

	Telemetry struct {
		OTLPEndpoint string `koanf:"otlp_endpoint"`
		LogFormat    string `koanf:"log_format"`
	} `koanf:"telemetry"`

	Database struct {
		URL string `koanf:"url"`
	} `koanf:"database"`

	Report struct {
		AllowMemoryStore  bool `koanf:"allow_memory_store"`
		SchedulerDisabled bool `koanf:"scheduler_disabled"`
	} `koanf:"report"`

	Environment string `koanf:"environment"`
	Milestone   string `koanf:"milestone"`
}

func Load(defaultsPath, envPath string) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(file.Provider(defaultsPath), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("load defaults from %s: %w", defaultsPath, err)
	}
	if envPath != "" {
		if err := k.Load(file.Provider(envPath), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("load env config from %s: %w", envPath, err)
		}
	}
	envProvider := env.Provider("OF_", ".", func(s string) string {
		return strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(s, "OF_"), "__", "."))
	})
	if err := k.Load(envProvider, nil); err != nil {
		return nil, fmt.Errorf("load env overrides: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if cfg.Database.URL == "" {
		cfg.Database.URL = os.Getenv("DATABASE_URL")
	}
	if cfg.Environment == "" {
		cfg.Environment = os.Getenv("OPENFOUNDRY_ENV")
	}
	if !cfg.Report.AllowMemoryStore {
		cfg.Report.AllowMemoryStore = strings.EqualFold(os.Getenv("OF_REPORT_ALLOW_MEMORY_STORE"), "true")
	}
	return &cfg, nil
}
