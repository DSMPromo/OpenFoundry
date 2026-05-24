// Package config resolves llm-catalog-service env config.
//
// The Rust binary was `fn main(){}` with handlers/models/domain re-
// exported from libs/ai-kernel. The Go port is the canonical
// implementation: it owns the catalog table and the unified invoke
// endpoint, and reuses libs/ai-kernel-go/models for the upstream
// provider DTOs.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

// Config is the resolved env state for the service.
type Config struct {
	Service struct{ Name, Version string }
	Server  struct {
		Host string
		Port uint16
	}
	DatabaseURL                  string
	JWTSecret                    string
	CheckpointsPurposeServiceURL string

	// Provider API keys / base URLs. Each is optional; the
	// corresponding provider returns an error at invoke time when its
	// key is unset.
	AnthropicAPIKey  string
	AnthropicBaseURL string
	OpenAIAPIKey     string
	OpenAIBaseURL    string
	OllamaBaseURL    string
	// Azure OpenAI Service is distinct from OPENAI because the endpoint
	// scheme differs. See B04 §AC#3 for the demo pivot.
	AzureAPIKey  string
	AzureBaseURL string

	// Amazon Bedrock — region-scoped (model availability differs by
	// region). Empty region disables Bedrock; Lookup then returns
	// ErrProviderUnimplemented for BEDROCK models, matching the
	// pre-3a behavior. AWSEndpointURL is the LocalStack/dev-runtime
	// hook (OF_AWS__ENDPOINT_URL) shared with libs/aws-client.
	BedrockRegion  string
	AWSEndpointURL string

	// Provider-health probe (B04 §AC#6).
	ProviderHealthIntervalSeconds int
	ProviderHealthDegradeAfterMS  int

	// Per-(subject, model) token-bucket settings.
	// 0 disables rate limiting.
	RateLimitCapacity        float64
	RateLimitRefillPerSecond float64
}

// FromEnv builds a Config, applying production defaults for everything
// that is not strictly required.
func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "llm-catalog-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50095)
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	cfg.CheckpointsPurposeServiceURL = defaultStr(os.Getenv("CHECKPOINTS_PURPOSE_SERVICE_URL"), "http://localhost:50116")

	cfg.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	cfg.AnthropicBaseURL = os.Getenv("ANTHROPIC_BASE_URL")
	cfg.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	cfg.OpenAIBaseURL = os.Getenv("OPENAI_BASE_URL")
	cfg.OllamaBaseURL = os.Getenv("OLLAMA_BASE_URL")
	cfg.AzureAPIKey = os.Getenv("AZURE_OPENAI_API_KEY")
	cfg.AzureBaseURL = os.Getenv("AZURE_OPENAI_BASE_URL")
	cfg.BedrockRegion = defaultStr(os.Getenv("BEDROCK_REGION"), os.Getenv("OF_AWS__REGION"))
	cfg.AWSEndpointURL = os.Getenv("OF_AWS__ENDPOINT_URL")
	cfg.ProviderHealthIntervalSeconds = parseInt(os.Getenv("LLM_PROVIDER_HEALTH_INTERVAL_SECONDS"), 30)
	cfg.ProviderHealthDegradeAfterMS = parseInt(os.Getenv("LLM_PROVIDER_HEALTH_DEGRADE_AFTER_MS"), 2000)

	cfg.RateLimitCapacity = parseFloat(os.Getenv("LLM_RATE_LIMIT_CAPACITY"), 60)
	cfg.RateLimitRefillPerSecond = parseFloat(os.Getenv("LLM_RATE_LIMIT_REFILL_PER_SECOND"), 1)
	return cfg, nil
}

// MissingEnvError is returned for required-but-unset env vars.
type MissingEnvError struct{ Key string }

func (e *MissingEnvError) Error() string {
	return fmt.Sprintf("required environment variable %s is not set", e.Key)
}

// IsMissingEnv reports whether err is a *MissingEnvError.
func IsMissingEnv(err error) bool { var me *MissingEnvError; return errors.As(err, &me) }

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func parseUint16(v string, fallback uint16) uint16 {
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseUint(v, 10, 16)
	if err != nil {
		return fallback
	}
	return uint16(n)
}

func parseFloat(v string, fallback float64) float64 {
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func parseInt(v string, fallback int) int {
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
