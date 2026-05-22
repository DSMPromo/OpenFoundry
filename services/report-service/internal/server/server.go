// Package server wires the HTTP router, middleware and graceful
// shutdown for report-service.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/report-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/report-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/report-service/internal/repo"
)

type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	log        *slog.Logger
	pool       *pgxpool.Pool
}

type Option func(*options)

type options struct {
	store handlers.ReportStore
}

func WithReportStore(store handlers.ReportStore) Option {
	return func(o *options) { o.store = store }
}

func New(cfg *config.Config, metrics *observability.Metrics, log *slog.Logger, opts ...Option) (*Server, error) {
	if log == nil {
		log = slog.Default()
	}
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	store := o.store
	var pool *pgxpool.Pool
	if store == nil {
		var err error
		store, pool, err = buildStore(context.Background(), cfg)
		if err != nil {
			return nil, err
		}
	}

	jwtCfg := authmw.NewJWTConfig(cfg.JWT.Secret).
		WithIssuer(cfg.JWT.Issuer).
		WithAudience(cfg.JWT.Audience)

	r := BuildRouter(cfg, metrics, jwtCfg, store)

	s := &Server{
		cfg:  cfg,
		log:  log,
		pool: pool,
		httpServer: &http.Server{
			Addr:              cfg.Server.Addr,
			Handler:           r,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	return s, nil
}

func BuildRouter(cfg *config.Config, metrics *observability.Metrics, jwtCfg *authmw.JWTConfig, store handlers.ReportStore) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/healthz", handlers.Health(cfg.Service.Name, cfg.Service.Version))
	r.Get("/readyz", handlers.Ready(store))
	if metrics != nil {
		r.Method(http.MethodGet, "/metrics", metrics.Handler())
	}

	reports := handlers.NewReportsHandler(store)
	api := r.With(authmw.Middleware(jwtCfg))
	api.Route("/api/v1/reports", reports.Mount)
	return r
}

func buildStore(ctx context.Context, cfg *config.Config) (handlers.ReportStore, *pgxpool.Pool, error) {
	if cfg.Database.URL != "" {
		pool, err := pgxpool.New(ctx, cfg.Database.URL)
		if err != nil {
			return nil, nil, fmt.Errorf("pgx pool failed: %w", err)
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, nil, fmt.Errorf("postgres ping failed: %w", err)
		}
		if err := repo.Migrate(ctx, pool); err != nil {
			pool.Close()
			return nil, nil, fmt.Errorf("migrations failed: %w", err)
		}
		return repo.New(pool), pool, nil
	}
	if allowMemoryStore(cfg) {
		return handlers.NewMemoryReportStore(), nil, nil
	}
	return nil, nil, errors.New("DATABASE_URL or OF_DATABASE__URL is required unless OF_REPORT_ALLOW_MEMORY_STORE=true or OPENFOUNDRY_ENV is dev/test")
}

func allowMemoryStore(cfg *config.Config) bool {
	if cfg.Report.AllowMemoryStore {
		return true
	}
	env := strings.ToLower(strings.TrimSpace(cfg.Environment))
	return env == "dev" || env == "development" || env == "test" || env == "local"
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("listening", slog.String("addr", s.cfg.Server.Addr))
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errCh:
		return err
	}
}

func (s *Server) shutdown() error {
	timeout := 15 * time.Second
	if d, err := time.ParseDuration(s.cfg.Server.ShutdownTimeout); err == nil {
		timeout = d
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	s.log.Info("shutting down")
	err := s.httpServer.Shutdown(ctx)
	if s.pool != nil {
		s.pool.Close()
	}
	return err
}
