package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/report-service/internal/generator"
)

type (
	GeneratorKind       string
	ScheduleCadence     string
	SectionKind         string
	DistributionChannel string
)

const (
	nowEngine = "openfoundry-report-local-v1"
)

var ErrNotFound = errors.New("report resource not found")

type ReportSection struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Kind        SectionKind    `json:"kind"`
	Query       string         `json:"query"`
	Description string         `json:"description"`
	Config      map[string]any `json:"config"`
}
type ReportTemplate struct {
	Title    string          `json:"title"`
	Subtitle string          `json:"subtitle"`
	Theme    string          `json:"theme"`
	Layout   string          `json:"layout"`
	Sections []ReportSection `json:"sections"`
}
type ReportSchedule struct {
	Cadence         ScheduleCadence `json:"cadence"`
	Expression      *string         `json:"expression"`
	Timezone        string          `json:"timezone"`
	AnchorTime      string          `json:"anchor_time"`
	IntervalMinutes *int            `json:"interval_minutes"`
	Enabled         bool            `json:"enabled"`
	NextRunAt       *string         `json:"next_run_at"`
}
type DistributionRecipient struct {
	ID      string              `json:"id"`
	Channel DistributionChannel `json:"channel"`
	Target  string              `json:"target"`
	Label   *string             `json:"label"`
	Config  map[string]any      `json:"config"`
}
type ReportDefinition struct {
	ID              string                  `json:"id"`
	Name            string                  `json:"name"`
	Description     string                  `json:"description"`
	Owner           string                  `json:"owner"`
	GeneratorKind   GeneratorKind           `json:"generator_kind"`
	DatasetName     string                  `json:"dataset_name"`
	Template        ReportTemplate          `json:"template"`
	Schedule        ReportSchedule          `json:"schedule"`
	Recipients      []DistributionRecipient `json:"recipients"`
	Tags            []string                `json:"tags"`
	Parameters      map[string]any          `json:"parameters"`
	Active          bool                    `json:"active"`
	LastGeneratedAt *string                 `json:"last_generated_at"`
	CreatedAt       string                  `json:"created_at"`
	UpdatedAt       string                  `json:"updated_at"`
}
type DistributionResult struct {
	Channel     DistributionChannel `json:"channel"`
	Target      string              `json:"target"`
	Status      string              `json:"status"`
	DeliveredAt string              `json:"delivered_at"`
	Detail      string              `json:"detail"`
}
type ReportPreviewHighlight struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Delta string `json:"delta"`
}
type ReportPreviewSection struct {
	SectionID string           `json:"section_id"`
	Title     string           `json:"title"`
	Kind      SectionKind      `json:"kind"`
	Summary   string           `json:"summary"`
	Rows      []map[string]any `json:"rows"`
}
type ReportExecutionPreview struct {
	Headline     string                   `json:"headline"`
	GeneratedFor string                   `json:"generated_for"`
	Engine       string                   `json:"engine"`
	Highlights   []ReportPreviewHighlight `json:"highlights"`
	Sections     []ReportPreviewSection   `json:"sections"`
}
type ReportArtifact struct {
	FileName   string `json:"file_name"`
	MimeType   string `json:"mime_type"`
	SizeBytes  int64  `json:"size_bytes"`
	StorageURL string `json:"storage_url"`
	Checksum   string `json:"checksum"`
}
type ReportExecutionMetrics struct {
	DurationMS     int64 `json:"duration_ms"`
	RowCount       int64 `json:"row_count"`
	SectionCount   int64 `json:"section_count"`
	RecipientCount int64 `json:"recipient_count"`
}
type ReportExecution struct {
	ID            string                 `json:"id"`
	ReportID      string                 `json:"report_id"`
	ReportName    string                 `json:"report_name"`
	Status        string                 `json:"status"`
	GeneratorKind GeneratorKind          `json:"generator_kind"`
	TriggeredBy   string                 `json:"triggered_by"`
	GeneratedAt   string                 `json:"generated_at"`
	CompletedAt   *string                `json:"completed_at"`
	Preview       ReportExecutionPreview `json:"preview"`
	Artifact      ReportArtifact         `json:"artifact"`
	Distributions []DistributionResult   `json:"distributions"`
	Metrics       ReportExecutionMetrics `json:"metrics"`
}
type ReportOverview struct {
	ReportCount     int              `json:"report_count"`
	ActiveSchedules int              `json:"active_schedules"`
	Executions24h   int              `json:"executions_24h"`
	GeneratorMix    []string         `json:"generator_mix"`
	LatestExecution *ReportExecution `json:"latest_execution"`
}
type GeneratorCatalogEntry struct {
	Kind         GeneratorKind `json:"kind"`
	DisplayName  string        `json:"display_name"`
	Engine       string        `json:"engine"`
	Extensions   []string      `json:"extensions"`
	Capabilities []string      `json:"capabilities"`
}
type DistributionChannelCatalogEntry struct {
	Channel             DistributionChannel `json:"channel"`
	DisplayName         string              `json:"display_name"`
	Description         string              `json:"description"`
	ConfigurationFields []string            `json:"configuration_fields"`
}
type ReportCatalog struct {
	Generators       []GeneratorCatalogEntry           `json:"generators"`
	DeliveryChannels []DistributionChannelCatalogEntry `json:"delivery_channels"`
}
type ScheduledRun struct {
	ReportID       string          `json:"report_id"`
	ReportName     string          `json:"report_name"`
	GeneratorKind  GeneratorKind   `json:"generator_kind"`
	NextRunAt      string          `json:"next_run_at"`
	RecipientCount int             `json:"recipient_count"`
	Cadence        ScheduleCadence `json:"cadence"`
}
type ScheduleBoard struct {
	ActiveSchedules  int               `json:"active_schedules"`
	PausedReports    int               `json:"paused_reports"`
	Upcoming         []ScheduledRun    `json:"upcoming"`
	RecentExecutions []ReportExecution `json:"recent_executions"`
}
type DownloadPayload struct {
	FileName       string `json:"file_name"`
	MimeType       string `json:"mime_type"`
	StorageURL     string `json:"storage_url"`
	PreviewExcerpt string `json:"preview_excerpt"`
	ReportName     string `json:"report_name"`
}

type listResponse struct {
	Items any `json:"items"`
}

type ReportStore interface {
	ListDefinitions(context.Context) ([]ReportDefinition, error)
	CreateDefinition(context.Context, ReportDefinition) (ReportDefinition, error)
	GetDefinition(context.Context, string) (*ReportDefinition, error)
	UpdateDefinition(context.Context, string, map[string]json.RawMessage) (ReportDefinition, error)
	SaveExecution(context.Context, ReportExecution) error
	ListExecutions(context.Context, string) ([]ReportExecution, error)
	GetExecution(context.Context, string) (*ReportExecution, error)
	Ping(context.Context) error
}

type MemoryReportStore struct {
	mu    sync.RWMutex
	defs  map[string]ReportDefinition
	execs map[string]ReportExecution
}

func NewMemoryReportStore() *MemoryReportStore {
	return &MemoryReportStore{defs: map[string]ReportDefinition{}, execs: map[string]ReportExecution{}}
}

// Ping reports the in-memory store as always reachable.
func (s *MemoryReportStore) Ping(context.Context) error { return nil }

func (s *MemoryReportStore) ListDefinitions(context.Context) ([]ReportDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ReportDefinition, 0, len(s.defs))
	for _, d := range s.defs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

func (s *MemoryReportStore) CreateDefinition(_ context.Context, d ReportDefinition) (ReportDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defs[d.ID] = d
	return d, nil
}

func (s *MemoryReportStore) GetDefinition(_ context.Context, id string) (*ReportDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.defs[id]
	if !ok {
		return nil, nil
	}
	return &d, nil
}

func (s *MemoryReportStore) UpdateDefinition(ctx context.Context, id string, patch map[string]json.RawMessage) (ReportDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.defs[id]
	if !ok {
		return ReportDefinition{}, ErrNotFound
	}
	ApplyDefinitionPatch(&d, patch)
	d.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.defs[id] = d
	return d, nil
}

func (s *MemoryReportStore) SaveExecution(_ context.Context, e ReportExecution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.execs[e.ID] = e
	if d, ok := s.defs[e.ReportID]; ok {
		d.LastGeneratedAt = &e.GeneratedAt
		d.UpdatedAt = e.GeneratedAt
		s.defs[d.ID] = d
	}
	return nil
}

func (s *MemoryReportStore) ListExecutions(_ context.Context, reportID string) ([]ReportExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []ReportExecution{}
	for _, e := range s.execs {
		if reportID == "" || e.ReportID == reportID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GeneratedAt > out[j].GeneratedAt })
	return out, nil
}

func (s *MemoryReportStore) GetExecution(_ context.Context, id string) (*ReportExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.execs[id]
	if !ok {
		return nil, nil
	}
	return &e, nil
}

type ReportsHandler struct{ Store ReportStore }

func NewReportsHandler(store ReportStore) *ReportsHandler {
	if store == nil {
		store = NewMemoryReportStore()
	}
	return &ReportsHandler{Store: store}
}

func (h *ReportsHandler) Mount(r chi.Router) {
	r.Get("/overview", h.Overview)
	r.Get("/catalog", h.Catalog)
	r.Get("/definitions", h.ListDefinitions)
	r.Post("/definitions", h.CreateDefinition)
	r.Patch("/definitions/{id}", h.UpdateDefinition)
	r.Post("/definitions/{id}/generate", h.Generate)
	r.Get("/definitions/{id}/history", h.History)
	r.Get("/schedules", h.Schedules)
	r.Get("/executions/{id}", h.GetExecution)
	r.Get("/executions/{id}/download", h.Download)
	r.Get("/executions/{id}/artifact", h.Artifact)
}

func (h *ReportsHandler) Overview(w http.ResponseWriter, r *http.Request) {
	defs, err := h.Store.ListDefinitions(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	execs, err := h.Store.ListExecutions(r.Context(), "")
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	active := 0
	mixMap := map[string]bool{}
	for _, d := range defs {
		if d.Active && d.Schedule.Enabled {
			active++
		}
		mixMap[string(d.GeneratorKind)] = true
	}
	mix := []string{}
	for k := range mixMap {
		mix = append(mix, k)
	}
	sort.Strings(mix)
	var latest *ReportExecution
	if len(execs) > 0 {
		latest = &execs[0]
	}
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	n24 := 0
	for _, e := range execs {
		if t, err := time.Parse(time.RFC3339Nano, e.GeneratedAt); err == nil && t.After(cutoff) {
			n24++
		}
	}
	writeJSON(w, http.StatusOK, ReportOverview{len(defs), active, n24, mix, latest})
}

func (h *ReportsHandler) Catalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, catalog())
}

func (h *ReportsHandler) ListDefinitions(w http.ResponseWriter, r *http.Request) {
	defs, err := h.Store.ListDefinitions(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, listResponse{Items: defs})
}

func (h *ReportsHandler) CreateDefinition(w http.ResponseWriter, r *http.Request) {
	var d ReportDefinition
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, 400, "invalid body")
		return
	}
	if err := validateDefinition(d); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	d.ID = uuid.NewString()
	d.CreatedAt = now
	d.UpdatedAt = now
	defaults(&d)
	out, err := h.Store.CreateDefinition(r.Context(), d)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, out)
}

func (h *ReportsHandler) UpdateDefinition(w http.ResponseWriter, r *http.Request) {
	var p map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, 400, "invalid body")
		return
	}
	id := chi.URLParam(r, "id")
	current, err := h.Store.GetDefinition(r.Context(), id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if current == nil {
		writeError(w, 404, "report definition not found")
		return
	}
	candidate := *current
	ApplyDefinitionPatch(&candidate, p)
	if err := validateDefinition(candidate); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	out, err := h.Store.UpdateDefinition(r.Context(), id, p)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, 404, "report definition not found")
			return
		}
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, out)
}

func (h *ReportsHandler) Generate(w http.ResponseWriter, r *http.Request) {
	d, err := h.Store.GetDefinition(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if d == nil {
		writeError(w, 404, "report definition not found")
		return
	}
	e, err := buildExecution(*d)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if err := h.Store.SaveExecution(r.Context(), e); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, e)
}

func (h *ReportsHandler) History(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.ListExecutions(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, listResponse{Items: out})
}

func (h *ReportsHandler) Schedules(w http.ResponseWriter, r *http.Request) {
	defs, err := h.Store.ListDefinitions(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	execs, err := h.Store.ListExecutions(r.Context(), "")
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	b := ScheduleBoard{RecentExecutions: execs}
	if len(b.RecentExecutions) > 10 {
		b.RecentExecutions = b.RecentExecutions[:10]
	}
	for _, d := range defs {
		if d.Active && d.Schedule.Enabled && d.Schedule.NextRunAt != nil {
			b.ActiveSchedules++
			b.Upcoming = append(b.Upcoming, ScheduledRun{d.ID, d.Name, d.GeneratorKind, *d.Schedule.NextRunAt, len(d.Recipients), d.Schedule.Cadence})
		} else if !d.Active || !d.Schedule.Enabled {
			b.PausedReports++
		}
	}
	writeJSON(w, 200, b)
}

func (h *ReportsHandler) GetExecution(w http.ResponseWriter, r *http.Request) {
	e, err := h.Store.GetExecution(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if e == nil {
		writeError(w, 404, "report execution not found")
		return
	}
	writeJSON(w, 200, e)
}

func (h *ReportsHandler) Download(w http.ResponseWriter, r *http.Request) {
	e, err := h.Store.GetExecution(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if e == nil {
		writeError(w, 404, "report execution not found")
		return
	}
	writeJSON(w, 200, DownloadPayload{FileName: e.Artifact.FileName, MimeType: e.Artifact.MimeType, StorageURL: e.Artifact.StorageURL, PreviewExcerpt: e.Preview.Headline, ReportName: e.ReportName})
}

func defaults(d *ReportDefinition) {
	if d.Tags == nil {
		d.Tags = []string{}
	}
	if d.Parameters == nil {
		d.Parameters = map[string]any{}
	}
	if d.Recipients == nil {
		d.Recipients = []DistributionRecipient{}
	}
	if d.Template.Sections == nil {
		d.Template.Sections = []ReportSection{}
	}
	if d.Template.Title == "" {
		d.Template.Title = d.Name
	}
	if d.Schedule.Timezone == "" {
		d.Schedule.Timezone = "UTC"
	}
	if d.Schedule.Cadence == "" {
		d.Schedule.Cadence = "manual"
	}
}

func ApplyDefinitionPatch(d *ReportDefinition, p map[string]json.RawMessage) {
	for k, v := range p {
		switch k {
		case "name":
			_ = json.Unmarshal(v, &d.Name)
		case "description":
			_ = json.Unmarshal(v, &d.Description)
		case "owner":
			_ = json.Unmarshal(v, &d.Owner)
		case "generator_kind":
			_ = json.Unmarshal(v, &d.GeneratorKind)
		case "dataset_name":
			_ = json.Unmarshal(v, &d.DatasetName)
		case "template":
			_ = json.Unmarshal(v, &d.Template)
		case "schedule":
			_ = json.Unmarshal(v, &d.Schedule)
		case "recipients":
			_ = json.Unmarshal(v, &d.Recipients)
		case "tags":
			_ = json.Unmarshal(v, &d.Tags)
		case "parameters":
			_ = json.Unmarshal(v, &d.Parameters)
		case "active":
			_ = json.Unmarshal(v, &d.Active)
		}
	}
	defaults(d)
}

func validateDefinition(d ReportDefinition) error {
	if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Owner) == "" || strings.TrimSpace(d.DatasetName) == "" {
		return errors.New("name, owner and dataset_name are required")
	}
	if !validGenerator(d.GeneratorKind) {
		return errors.New("unsupported generator_kind")
	}
	return nil
}

func validGenerator(g GeneratorKind) bool {
	switch g {
	case "pdf", "excel", "csv", "html", "pptx":
		return true
	}
	return false
}

func mime(g GeneratorKind) string {
	switch g {
	case "pdf":
		return "application/pdf"
	case "excel":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "csv":
		return "text/csv"
	case "pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	default:
		return "text/html"
	}
}

func ext(g GeneratorKind) string {
	if g == "excel" {
		return "xlsx"
	}
	return string(g)
}

func buildExecution(d ReportDefinition) (ReportExecution, error) {
	start := time.Now()
	gen := start.UTC().Format(time.RFC3339Nano)
	sections := make([]ReportPreviewSection, 0, len(d.Template.Sections))
	for _, s := range d.Template.Sections {
		row := map[string]any{"dataset": d.DatasetName, "query": s.Query, "parameters": d.Parameters}
		sections = append(sections, ReportPreviewSection{s.ID, s.Title, s.Kind, "Preview generated from report definition and parameters.", []map[string]any{row}})
	}
	if len(sections) == 0 {
		sections = append(sections, ReportPreviewSection{"summary", "Summary", "narrative", "Preview generated from report definition and parameters.", []map[string]any{{"dataset": d.DatasetName, "parameters": d.Parameters}}})
	}
	id := uuid.NewString()
	file := strings.ReplaceAll(strings.ToLower(d.Name), " ", "-") + "." + ext(d.GeneratorKind)
	completed := time.Now().UTC().Format(time.RFC3339Nano)
	dist := make([]DistributionResult, 0, len(d.Recipients))
	for _, r := range d.Recipients {
		dist = append(dist, DistributionResult{r.Channel, r.Target, "skipped", completed, "external distribution is not configured for manual local generation"})
	}
	e := ReportExecution{
		ID: id, ReportID: d.ID, ReportName: d.Name, Status: "succeeded",
		GeneratorKind: d.GeneratorKind, TriggeredBy: "manual", GeneratedAt: gen, CompletedAt: &completed,
		Preview: ReportExecutionPreview{
			Headline:     d.Template.Title,
			GeneratedFor: d.Owner,
			Engine:       nowEngine,
			Highlights:   []ReportPreviewHighlight{{"Dataset", d.DatasetName, "0"}, {"Sections", fmt.Sprint(len(sections)), "0"}},
			Sections:     sections,
		},
		Artifact: ReportArtifact{
			FileName:   file,
			MimeType:   mime(d.GeneratorKind),
			StorageURL: "/api/v1/reports/executions/" + id + "/artifact",
		},
		Distributions: dist,
	}
	// Render the artifact now so the recorded size and checksum describe
	// real bytes. The generator is deterministic, so the same bytes are
	// reproduced when the artifact is downloaded.
	data, err := generator.Render(string(d.GeneratorKind), reportDocument(e))
	if err != nil {
		return ReportExecution{}, fmt.Errorf("render %s artifact: %w", d.GeneratorKind, err)
	}
	sum := sha256.Sum256(data)
	e.Artifact.SizeBytes = int64(len(data))
	e.Artifact.Checksum = hex.EncodeToString(sum[:])
	e.Metrics = ReportExecutionMetrics{
		DurationMS:     time.Since(start).Milliseconds(),
		RowCount:       int64(len(sections)),
		SectionCount:   int64(len(sections)),
		RecipientCount: int64(len(d.Recipients)),
	}
	return e, nil
}

func catalog() ReportCatalog {
	return ReportCatalog{Generators: []GeneratorCatalogEntry{{"pdf", "PDF", nowEngine, []string{"pdf"}, []string{"preview", "artifact_metadata", "artifact_download"}}, {"excel", "Excel", nowEngine, []string{"xlsx"}, []string{"preview", "artifact_metadata", "artifact_download"}}, {"csv", "CSV", nowEngine, []string{"csv"}, []string{"preview", "artifact_metadata", "artifact_download"}}, {"html", "HTML", nowEngine, []string{"html"}, []string{"preview", "artifact_metadata", "artifact_download"}}, {"pptx", "PowerPoint", nowEngine, []string{"pptx"}, []string{"preview", "artifact_metadata", "artifact_download"}}}, DeliveryChannels: []DistributionChannelCatalogEntry{{"email", "Email", "Email delivery requires a configured delivery worker.", []string{"target"}}, {"s3", "S3", "Object storage delivery requires configured storage credentials.", []string{"bucket", "prefix"}}, {"slack", "Slack", "Slack delivery requires an app webhook.", []string{"webhook"}}, {"teams", "Teams", "Teams delivery requires an app webhook.", []string{"webhook"}}, {"webhook", "Webhook", "Webhook delivery requires a configured outbound dispatcher.", []string{"url"}}}}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
