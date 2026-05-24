// Command llm-catalog-service is the LLM model catalog + unified
// invoke endpoint. Every AIP-style flow (agents, copilots, retrieval
// chains) routes through this service so cost, audit, and rate
// limiting happen in one place regardless of upstream provider.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	awsclient "github.com/openfoundry/openfoundry-go/libs/aws-client"
	"github.com/openfoundry/openfoundry-go/libs/capabilities/probes"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/llm-catalog-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/llm-catalog-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/llm-catalog-service/internal/models"
	healthproviders "github.com/openfoundry/openfoundry-go/services/llm-catalog-service/internal/providers"
	"github.com/openfoundry/openfoundry-go/services/llm-catalog-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/llm-catalog-service/internal/server"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.FromEnv()
	if err != nil {
		slog.Error("config load failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if cfg.Service.Version == "dev" {
		cfg.Service.Version = version
	}

	log := observability.InitLogging(cfg.Service.Name, cfg.Service.Version)
	shutdownTracing, err := observability.InitTracing(ctx, cfg.Service.Name, cfg.Service.Version)
	if err != nil {
		log.Error("tracing init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	if cfg.DatabaseURL == "" {
		log.Error("DATABASE_URL unset — refusing to start")
		os.Exit(1)
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("pgx pool failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()
	if err := repo.Migrate(ctx, pool); err != nil {
		log.Error("migrations failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	metrics := observability.NewMetrics()
	store := &repo.PgStore{Pool: pool}
	providerRegistry := &handlers.ProviderRegistry{
		AnthropicAPIKey:  cfg.AnthropicAPIKey,
		AnthropicBaseURL: cfg.AnthropicBaseURL,
		OpenAIAPIKey:     cfg.OpenAIAPIKey,
		OpenAIBaseURL:    cfg.OpenAIBaseURL,
		OllamaBaseURL:    cfg.OllamaBaseURL,
		BedrockRegion:    cfg.BedrockRegion,
	}
	// Bedrock client is optional: when neither BEDROCK_REGION nor
	// OF_AWS__ENDPOINT_URL is set the catalog still accepts BEDROCK
	// rows for planning but Lookup returns ErrProviderUnimplemented.
	if cfg.BedrockRegion != "" || cfg.AWSEndpointURL != "" {
		bedrockClient, err := awsclient.BedrockRuntime(ctx, awsclient.Config{
			Region:      cfg.BedrockRegion,
			EndpointURL: cfg.AWSEndpointURL,
		})
		if err != nil {
			log.Warn("bedrock client init failed; BEDROCK provider disabled",
				slog.String("error", err.Error()))
		} else {
			providerRegistry.BedrockClient = bedrockClient
			log.Info("bedrock runtime client wired",
				slog.String("region", cfg.BedrockRegion),
				slog.Bool("custom_endpoint", cfg.AWSEndpointURL != ""))
		}
	}

	// Provider-health prober (B04 §AC#6). Runs in the background and
	// drives the /api/v1/llm/providers/health snapshot the UI badges
	// off. Skipped entirely when no upstream is configured (CI / unit
	// dev) so the service stays single-process.
	prober := buildProber(cfg)
	var providerHealth *handlers.ProviderHealth
	if prober != nil {
		go prober.Run(ctx)
		providerHealth = &handlers.ProviderHealth{Prober: prober}
	}

	deps := server.Deps{
		Catalog: &handlers.Catalog{Store: store},
		Invoke: &handlers.Invoke{
			Store:     store,
			Providers: providerRegistry,
			Limiter:   handlers.NewRateLimiter(cfg.RateLimitCapacity, cfg.RateLimitRefillPerSecond),
			Metrics:   handlers.NewInvokeMetrics(metrics),
			Logger:    log,
		},
		ProviderHealth: providerHealth,
		JWT:            authmw.NewJWTConfig(cfg.JWTSecret),
	}

	srv := server.New(cfg, deps, metrics, probes.Postgres("primary", pool))
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// buildProber returns a *Prober wired with the providers the operator
// has actually configured. Empty BaseURLs are skipped — a probe with
// no upstream is dead weight. Returns nil when nothing is configured
// so the /providers/health route is left unmounted.
func buildProber(cfg *config.Config) *healthproviders.Prober {
	targets := []healthproviders.ProbeTarget{}
	if cfg.OllamaBaseURL != "" {
		targets = append(targets, healthproviders.ProbeTarget{
			Provider: models.ProviderOllama, BaseURL: cfg.OllamaBaseURL,
		})
	}
	if cfg.OpenAIBaseURL != "" {
		t := healthproviders.ProbeTarget{Provider: models.ProviderOpenAI, BaseURL: cfg.OpenAIBaseURL}
		if cfg.OpenAIAPIKey != "" {
			t.Header = "Bearer " + cfg.OpenAIAPIKey
		}
		targets = append(targets, t)
	}
	if cfg.AzureBaseURL != "" {
		t := healthproviders.ProbeTarget{Provider: models.ProviderAzure, BaseURL: cfg.AzureBaseURL}
		if cfg.AzureAPIKey != "" {
			t.Header = cfg.AzureAPIKey
			t.HeaderName = "api-key"
		}
		targets = append(targets, t)
	}
	if cfg.AnthropicBaseURL != "" {
		t := healthproviders.ProbeTarget{Provider: models.ProviderAnthropic, BaseURL: cfg.AnthropicBaseURL}
		if cfg.AnthropicAPIKey != "" {
			t.Header = cfg.AnthropicAPIKey
			t.HeaderName = "x-api-key"
		}
		targets = append(targets, t)
	}
	if len(targets) == 0 {
		return nil
	}
	p := healthproviders.NewProber(targets)
	if cfg.ProviderHealthIntervalSeconds > 0 {
		p.Interval = time.Duration(cfg.ProviderHealthIntervalSeconds) * time.Second
	}
	if cfg.ProviderHealthDegradeAfterMS > 0 {
		p.DegradeAfter = time.Duration(cfg.ProviderHealthDegradeAfterMS) * time.Millisecond
	}
	return p
}
