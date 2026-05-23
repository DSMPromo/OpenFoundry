// Package server wires the HTTP surface for pipeline-build-service.
//
// URL grid mirrors the Rust router 1:1:
//
//   - /api/v1/builds                          (builds_v1)
//   - /api/v1/builds/{id}                     (builds_v1)
//   - /api/v1/builds/{id}/abort               (builds_v1)
//   - /api/v1/builds/{id}/jobs                (job_logs)
//   - /api/v1/jobs/{id}                       (job_logs)
//   - /api/v1/jobs/{id}/logs                  (job_logs)
//   - /api/v1/jobs/{id}/logs/stream           (job_logs SSE)
//   - /api/v1/pipelines                       (legacy CRUD)
//   - /api/v1/pipelines/_validate             (draft graph validation)
//   - /api/v1/pipelines/_schema-guidance      (draft join/union schema guidance)
//   - /api/v1/pipelines/aip/generate          (AIP transform generation)
//   - /api/v1/pipelines/transforms/catalog    (transform catalog)
//   - /api/v1/pipelines/{id}                  (legacy CRUD)
//   - /api/v1/pipelines/{id}/_validate        (persisted graph validation)
//   - /api/v1/pipelines/{id}/schema-guidance  (persisted join/union schema guidance)
//   - /api/v1/pipelines/{id}/nodes/{node_id}/preview (local preview)
//   - /api/v1/pipelines/geospatial/gpx/parse (GPX upload parse helper)
//   - /api/v1/pipelines/{id}/runs             (runs)
//   - /api/v1/pipelines/{id}/runs/{run_id}    (runs)
//   - /api/v1/pipelines/{id}/runs/{run_id}/retry  (runs)
//   - /api/v1/pipelines/{id}/runs/{run_id}/cancel (runs)
//   - /api/v1/dry-run/resolve                 (dry_run)
//   - /api/v1/dry-run/validate                (dry_run)
//   - /api/v1/execute                         (execute)
//   - /api/v1/data-integration/pipelines/{id}/runs                 (runs)
//   - /api/v1/data-integration/pipelines/{id}/runs/{run_id}        (runs)
//   - /api/v1/data-integration/pipelines/{id}/runs/{run_id}/retry  (runs)
//   - /api/v1/data-integration/builds                              (builds queue)
//   - /api/v1/data-integration/builds/_summary                     (builds queue)
//   - /api/v1/data-integration/builds/{run_id}/abort               (builds queue)
//   - /api/v1/data-integration/pipelines/_scheduler/run-due        (scheduler)
//   - /api/v1/data-integration/pipelines/{pipeline_rid}/dry-run-resolve (dry_run)
//   - /api/v1/data-integration/v1/schedules                          (schedules)
//   - /api/v1/data-integration/v1/schedules/{rid}                     (schedules)
//   - /api/v1/data-integration/spark-runs                          (spark_runs)
//   - /api/v1/data-integration/spark-runs/{id}                     (spark_runs)
//   - /api/v1/data-integration/pipelines/{id}/runs/{run_id}/spec   (spark_runs)
//   - /v1/builds, /v1/builds/{rid}, /v1/builds/{rid}/jobs          (Foundry builds)
//   - /v1/jobs/{rid}, /v1/jobs/{rid}/outputs                      (Foundry jobs)
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/jackc/pgx/v5/pgxpool"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	probespkg "github.com/openfoundry/openfoundry-go/libs/capabilities/probes"
	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
	"github.com/openfoundry/openfoundry-go/libs/observability"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/handler"
)

// Deps bundles optional dependencies the router can use. nil is fine
// — handlers that need a pool simply remain unmounted.
type Deps struct {
	// Pool backs the dataset-health handlers (B06 §AC#5). nil disables
	// the /datasets/{rid}/health surface; everything else mounts.
	Pool *pgxpool.Pool
}

func New(cfg *config.Config, m *observability.Metrics, probes ...capabilities.DependencyProbe) *http.Server {
	return NewWithDeps(cfg, m, Deps{}, probes...)
}

// NewWithDeps is the dependency-injected variant used by main.go.
func NewWithDeps(cfg *config.Config, m *observability.Metrics, deps Deps, probes ...capabilities.DependencyProbe) *http.Server {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           BuildRouterWithDeps(cfg, m, deps, probes...),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func BuildRouter(cfg *config.Config, m *observability.Metrics, probes ...capabilities.DependencyProbe) http.Handler {
	return BuildRouterWithDeps(cfg, m, Deps{}, probes...)
}

func BuildRouterWithDeps(cfg *config.Config, m *observability.Metrics, deps Deps, probes ...capabilities.DependencyProbe) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer)
	r.Use(chimw.Timeout(60 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	if m != nil {
		r.Method(http.MethodGet, "/metrics", m.Handler())
	}

	// Capability registry — see docs/agent-automation/AGENT-CAPABILITIES-ROADMAP.md (M1.1).
	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)
	caps.RegisterDependency(probespkg.PythonSidecar("python-sidecar", cfg.PythonSidecarBinary))
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	jwt := authmw.NewJWTConfig(cfg.JWTSecret)
	// Rust-compatible SparkApplication submission surface from the Rust /api/v1/pipeline nest.
	// Rust mounted these routes outside the JWT-protected data-integration/v1 groups;
	// the gateway can still enforce auth upstream.
	r.Route("/api/v1/pipeline", func(pipeline chi.Router) {
		pipeline.Post("/builds/run", handler.SubmitPipelineBuildRun)
		pipeline.Get("/builds/{run_id}/status", handler.GetPipelineBuildRunStatus)
	})

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))

		// Builds (v1).
		api.Get("/builds", handler.ListBuilds)
		api.Post("/builds", handler.CreateBuild)
		api.Get("/builds/{id}", handler.GetBuild)
		api.Post("/builds/{id}/abort", handler.AbortBuild)
		api.Get("/builds/{id}/jobs", handler.ListJobs)

		// Jobs + logs.
		api.Get("/jobs/{id}", handler.GetJob)
		api.Get("/jobs/{id}/logs", handler.ListJobLogs)
		api.Get("/jobs/{id}/logs/stream", handler.StreamJobLogs)

		// Dry-run + execute.
		api.Post("/dry-run/resolve", handler.DryRunResolve)
		api.Post("/dry-run/validate", handler.DryRunValidate)
		api.Post("/execute", handler.ExecutePipeline)

		// Pipelines (legacy CRUD + runs).
		api.Get("/pipelines", handler.ListPipelines)
		api.Post("/pipelines", handler.CreatePipeline)
		api.Post("/pipelines/_validate", handler.ValidatePipelineGraph)
		api.Post("/pipelines/_schema-guidance", handler.PipelineSchemaGuidance)
		api.Post("/pipelines/aip/generate", handler.PipelineAIPGenerate)
		api.Get("/pipelines/transforms/catalog", handler.ListPipelineTransformCatalog)
		api.Post("/pipelines/geospatial/gpx/parse", handler.ParseGPXUpload)
		// TransformService — proto/pipeline/transform.proto. Compile,
		// validate and preview are MVP linters today (response carries
		// `deferred=true` until the full runtime is wired); Register*
		// are full DB-backed implementations.
		api.Post("/pipelines/compile-transform", handler.CompileTransform)
		api.Post("/pipelines/validate-transform", handler.ValidateTransform)
		api.Post("/pipelines/preview-transform", handler.PreviewTransform)
		api.Post("/pipelines/register-python-transform", handler.RegisterPythonTransform)
		api.Post("/pipelines/register-sql-transform", handler.RegisterSqlTransform)
		api.Post("/pipelines/register-pipeline-builder-graph", handler.RegisterPipelineBuilderGraph)
		api.Get("/pipelines/{id}", handler.GetPipeline)
		api.Patch("/pipelines/{id}", handler.UpdatePipeline)
		api.Put("/pipelines/{id}", handler.UpdatePipeline)
		api.Delete("/pipelines/{id}", handler.DeletePipeline)
		api.Get("/pipelines/{id}/versions", handler.ListPipelineVersions)
		api.Post("/pipelines/{id}/publish", handler.PublishPipeline)
		api.Post("/pipelines/{id}/proposals", handler.CreatePipelineProposal)
		api.Post("/pipelines/{id}/versions/{version_id}/restore", handler.RestorePipelineVersion)
		api.Post("/pipelines/{id}/_validate", handler.ValidatePipelineByID)
		api.Post("/pipelines/{id}/schema-guidance", handler.PipelineSchemaGuidanceByID)
		api.Post("/pipelines/{id}/aip/generate", handler.PipelineAIPGenerateByID)
		api.Get("/pipelines/{id}/nodes/{node_id}/preview", handler.PreviewPipelineNode)
		api.Post("/pipelines/{id}/nodes/{node_id}/preview", handler.PreviewPipelineNode)

		// Access block (owner-only mutations; follow endpoints accept any
		// authenticated principal). Mirrors vertex-service link-share / grants.
		api.Get("/pipelines/{id}/link-share", handler.GetPipelineLinkShare)
		api.Put("/pipelines/{id}/link-share", handler.PutPipelineLinkShare)
		api.Get("/pipelines/{id}/grants", handler.ListPipelineGrants)
		api.Put("/pipelines/{id}/grants", handler.PutPipelineGrant)
		api.Delete("/pipelines/{id}/grants/{grant_id}", handler.DeletePipelineGrant)
		api.Get("/pipelines/{id}/followers/summary", handler.GetPipelineFollowerSummary)
		api.Post("/pipelines/{id}/followers", handler.FollowPipeline)
		api.Delete("/pipelines/{id}/followers", handler.UnfollowPipeline)

		api.Post("/pipelines/{id}/views", handler.RecordPipelineView)
		api.Get("/pipelines/{id}/views/summary", handler.GetPipelineViewSummary)

		api.Get("/pipelines/{id}/comments", handler.ListPipelineComments)
		api.Post("/pipelines/{id}/comments", handler.CreatePipelineComment)
		api.Delete("/pipelines/{id}/comments/{comment_id}", handler.DeletePipelineComment)

		api.Get("/compute-profiles", handler.ListComputeProfiles)

		api.Get("/ml-models", handler.ListMLModels)
		api.Post("/ml-models", handler.CreateMLModel)
		api.Get("/ml-models/{id}", handler.GetMLModel)
		api.Delete("/ml-models/{id}", handler.DeleteMLModel)

		api.Get("/pipelines/{id}/runs", handler.ListPipelineRuns)
		api.Post("/pipelines/{id}/runs", handler.TriggerPipelineRun)
		api.Get("/pipelines/{id}/runs/{run_id}", handler.GetPipelineRun)
		api.Post("/pipelines/{id}/runs/{run_id}/retry", handler.RetryPipelineRun)
		api.Post("/pipelines/{id}/runs/{run_id}/cancel", handler.CancelPipelineRun)

		// Rust-compatible data-integration route group plus SparkApplication helpers.
		api.Route("/data-integration", func(di chi.Router) {
			di.Get("/pipelines/{id}/runs", handler.ListPipelineRuns)
			di.Post("/pipelines/{id}/runs", handler.TriggerPipelineRun)
			di.Get("/pipelines/{id}/runs/{run_id}", handler.GetPipelineRun)
			di.Post("/pipelines/{id}/runs/{run_id}/retry", handler.RetryPipelineRun)
			di.Get("/builds", handler.ListDataIntegrationBuilds)
			di.Get("/builds/_summary", handler.DataIntegrationQueueSummary)
			di.Post("/builds/{run_id}/abort", handler.AbortDataIntegrationBuild)
			di.Post("/pipelines/_scheduler/run-due", handler.RunDueScheduledPipelines)
			di.Post("/pipelines/{pipeline_rid}/dry-run-resolve", handler.DryRunResolve)
			di.Get("/v1/schedules", handler.ListSchedules)
			di.Post("/v1/schedules", handler.CreateSchedule)
			di.Post("/v1/schedules/_scheduler/run-due", handler.DispatchDueSchedules)
			di.Post("/v1/schedules/_events", handler.RecordScheduleTriggerEvent)
			di.Get("/v1/schedules/{rid}", handler.GetSchedule)
			di.Patch("/v1/schedules/{rid}", handler.PatchSchedule)
			di.Delete("/v1/schedules/{rid}", handler.DeleteSchedule)
			di.Post("/v1/schedules/{rid}:pause", handler.PauseSchedule)
			di.Post("/v1/schedules/{rid}:resume", handler.ResumeSchedule)
			di.Post("/v1/schedules/{rid}:exempt-from-auto-pause", handler.SetScheduleAutoPauseExempt)
			di.Post("/v1/schedules/{rid}:run-now", handler.RunScheduleNow)
			di.Post("/v1/schedules/{rid}:convert-to-project-scope", handler.ConvertScheduleToProjectScope)
			di.Get("/v1/schedules/{rid}/runs", handler.ListScheduleRuns)
			di.Get("/v1/schedules/{rid}/versions", handler.ListScheduleVersions)
			di.Get("/v1/schedules/{rid}/versions/diff", handler.GetScheduleVersionDiff)
			di.Get("/v1/schedules/{rid}/versions/{version}", handler.GetScheduleVersion)

			di.Get("/spark-runs", handler.ListSparkRuns)
			di.Post("/spark-runs", handler.SubmitSparkRun)
			di.Get("/spark-runs/{id}", handler.GetSparkRun)
			di.Get("/pipelines/{id}/runs/{run_id}/spec", handler.GetSpecForRun)
		})
	})

	r.Route("/v1", func(v1 chi.Router) {
		v1.Use(authmw.Middleware(jwt))
		v1.Get("/builds", handler.ListBuildsV1)
		v1.Post("/builds", handler.CreateBuildV1)
		v1.Get("/builds/{rid}", handler.GetBuildV1)
		v1.Post("/builds/{rid}:abort", handler.AbortBuildV1)
		v1.Get("/builds/{rid}/jobs", handler.ListBuildJobsV1)
		v1.Get("/datasets/{rid}/builds", handler.ListDatasetBuildsV1)
		v1.Get("/jobs/{rid}", handler.GetJobV1)
		v1.Get("/jobs/{rid}/outputs", handler.GetJobOutputsV1)
		v1.Get("/jobs/{rid}/input-resolutions", handler.GetJobInputResolutionsV1)
		v1.Post("/job-specs/{kind}", handler.CreateJobSpecV1)
		v1.Get("/jobs/{rid}/logs", handler.ListJobLogsV1)
		v1.Post("/jobs/{rid}/logs", handler.EmitJobLogV1)
		v1.Get("/jobs/{rid}/logs/stream", handler.StreamJobLogsV1)
		v1.Get("/jobs/{rid}/logs/ws", handler.WSJobLogsV1)
	})

	// B06 §AC#5 — per-dataset check-evaluation history. The
	// `/health` (singular) endpoint stays on dataset-versioning-service
	// and renders the snapshot view; this surface adds the timeline
	// the UI Health tab needs to show trend + latest failures.
	if deps.Pool != nil {
		health := &handler.DatasetHealthHandlers{Repo: &handler.HealthRepo{Pool: deps.Pool}}
		r.Route("/api/v1/datasets", func(api chi.Router) {
			api.Use(authmw.Middleware(jwt))
			api.Get("/{rid}/health/events", health.Get)
		})
		// /internal/datasets/{rid}/health/events — no auth; producer
		// surface for pipeline-expression and the pipeline runners,
		// restricted at the network layer like other /internal/
		// endpoints.
		r.Post("/internal/datasets/{rid}/health/events", health.Record)
	}

	if _, err := caps.IngestChiRoutes(r, capabilities.IngestOptions{
		IDPrefix:  "pipeline-build",
		AuthPaths: []string{"/api/v1/pipeline", "/api/v1", "/v1"},
		Tags:      []string{"pipelines"},
	}); err != nil {
		panic("pipeline-build-service: capability ingest failed: " + err.Error())
	}

	return r
}
