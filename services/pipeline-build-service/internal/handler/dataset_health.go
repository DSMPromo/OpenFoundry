// B06 §AC#5: dataset Data Health surface.
//
// pipeline-expression evaluations call into RecordHealthEvent every
// time a per-row check (null-rate, value-range, ...) runs. The
// resulting row is the source of truth for the dataset's current
// health state and the trend history rendered by
// apps/web/src/routes/datasets/.

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthSeverity mirrors the migration's CHECK constraint.
type HealthSeverity string

const (
	HealthSeverityInfo     HealthSeverity = "info"
	HealthSeverityWarning  HealthSeverity = "warning"
	HealthSeverityError    HealthSeverity = "error"
	HealthSeverityCritical HealthSeverity = "critical"
)

// HealthStatus is the per-check outcome.
type HealthStatus string

const (
	HealthStatusPassing  HealthStatus = "passing"
	HealthStatusDegraded HealthStatus = "degraded"
)

// HealthEvent is one row of dataset_health_events. The HTTP responses
// expose the trend history; the latest event per (dataset, check_name)
// is also rolled up into DatasetHealthSummary.
type HealthEvent struct {
	ID          int64           `json:"id"`
	DatasetRID  string          `json:"dataset_rid"`
	SnapshotID  *int64          `json:"snapshot_id,omitempty"`
	CheckName   string          `json:"check_name"`
	Severity    HealthSeverity  `json:"severity"`
	Status      HealthStatus    `json:"status"`
	MetricName  *string         `json:"metric_name,omitempty"`
	MetricValue *float64        `json:"metric_value,omitempty"`
	Threshold   *float64        `json:"threshold,omitempty"`
	Message     *string         `json:"message,omitempty"`
	Details     json.RawMessage `json:"details,omitempty"`
	EvaluatedAt time.Time       `json:"evaluated_at"`
}

// RecordHealthEventRequest is the POST /internal/datasets/{rid}/health
// body — emitted by pipeline-expression evaluations.
type RecordHealthEventRequest struct {
	SnapshotID  *int64          `json:"snapshot_id,omitempty"`
	CheckName   string          `json:"check_name"`
	Severity    HealthSeverity  `json:"severity"`
	Status      HealthStatus    `json:"status"`
	MetricName  *string         `json:"metric_name,omitempty"`
	MetricValue *float64        `json:"metric_value,omitempty"`
	Threshold   *float64        `json:"threshold,omitempty"`
	Message     *string         `json:"message,omitempty"`
	Details     json.RawMessage `json:"details,omitempty"`
}

// DatasetHealthSummary is the GET /datasets/{rid}/health response.
//
//	Overall = degraded when ANY active check is degraded.
type DatasetHealthSummary struct {
	DatasetRID     string        `json:"dataset_rid"`
	Overall        HealthStatus  `json:"overall"`
	LatestPerCheck []HealthEvent `json:"latest_per_check"`
	RecentEvents   []HealthEvent `json:"recent_events"`
}

// HealthRepo wraps the SQL surface for dataset_health_events.
type HealthRepo struct {
	Pool *pgxpool.Pool
}

const healthCols = `id, dataset_rid, snapshot_id, check_name, severity,
                   status, metric_name, metric_value, threshold, message,
                   details, evaluated_at`

// RecordEvent inserts a row and returns it.
func (r *HealthRepo) RecordEvent(ctx context.Context, datasetRID string, body RecordHealthEventRequest) (*HealthEvent, error) {
	if datasetRID == "" {
		return nil, errors.New("dataset_rid required")
	}
	if body.CheckName == "" {
		return nil, errors.New("check_name required")
	}
	if body.Severity == "" {
		body.Severity = HealthSeverityInfo
	}
	if body.Status == "" {
		body.Status = HealthStatusPassing
	}
	details := body.Details
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	row := r.Pool.QueryRow(
		ctx,
		`INSERT INTO dataset_health_events
		   (dataset_rid, snapshot_id, check_name, severity, status,
		    metric_name, metric_value, threshold, message, details)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING `+healthCols,
		datasetRID, body.SnapshotID, body.CheckName, string(body.Severity), string(body.Status),
		body.MetricName, body.MetricValue, body.Threshold, body.Message, []byte(details),
	)
	return scanHealthEvent(row)
}

// LatestPerCheck returns the most recent event per check_name for the
// given dataset.
func (r *HealthRepo) LatestPerCheck(ctx context.Context, datasetRID string) ([]HealthEvent, error) {
	rows, err := r.Pool.Query(
		ctx,
		`SELECT DISTINCT ON (check_name) `+healthCols+`
		   FROM dataset_health_events
		  WHERE dataset_rid = $1
		  ORDER BY check_name, evaluated_at DESC`,
		datasetRID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]HealthEvent, 0)
	for rows.Next() {
		e, err := scanHealthEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// Recent returns the last `limit` events for the dataset, newest first.
func (r *HealthRepo) Recent(ctx context.Context, datasetRID string, limit int) ([]HealthEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.Pool.Query(
		ctx,
		`SELECT `+healthCols+`
		   FROM dataset_health_events
		  WHERE dataset_rid = $1
		  ORDER BY evaluated_at DESC
		  LIMIT $2`,
		datasetRID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]HealthEvent, 0)
	for rows.Next() {
		e, err := scanHealthEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(...any) error
}

func scanHealthEvent(row rowScanner) (*HealthEvent, error) {
	var (
		e          HealthEvent
		snapshotID *int64
		metricName *string
		metricVal  *float64
		threshold  *float64
		message    *string
		details    []byte
	)
	err := row.Scan(
		&e.ID, &e.DatasetRID, &snapshotID, &e.CheckName, &e.Severity,
		&e.Status, &metricName, &metricVal, &threshold, &message,
		&details, &e.EvaluatedAt,
	)
	if err != nil {
		return nil, err
	}
	e.SnapshotID = snapshotID
	e.MetricName = metricName
	e.MetricValue = metricVal
	e.Threshold = threshold
	e.Message = message
	if len(details) > 0 {
		e.Details = json.RawMessage(details)
	}
	return &e, nil
}

// DatasetHealthHandlers is the chi handler shell.
type DatasetHealthHandlers struct {
	Repo *HealthRepo
}

// Get serves GET /api/v1/datasets/{rid}/health.
func (h *DatasetHealthHandlers) Get(w http.ResponseWriter, r *http.Request) {
	datasetRID := chi.URLParam(r, "rid")
	if datasetRID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dataset rid required"})
		return
	}
	latest, err := h.Repo.LatestPerCheck(r.Context(), datasetRID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	recent, err := h.Repo.Recent(r.Context(), datasetRID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	overall := HealthStatusPassing
	for _, e := range latest {
		if e.Status == HealthStatusDegraded {
			overall = HealthStatusDegraded
			break
		}
	}
	writeJSON(w, http.StatusOK, DatasetHealthSummary{
		DatasetRID:     datasetRID,
		Overall:        overall,
		LatestPerCheck: latest,
		RecentEvents:   recent,
	})
}

// Record serves POST /internal/datasets/{rid}/health. No auth — same
// network-layer restriction as other /internal/ producer surfaces.
func (h *DatasetHealthHandlers) Record(w http.ResponseWriter, r *http.Request) {
	datasetRID := chi.URLParam(r, "rid")
	if datasetRID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dataset rid required"})
		return
	}
	var body RecordHealthEventRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
		return
	}
	if body.Severity != "" && !validSeverity(body.Severity) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid severity %q", body.Severity)})
		return
	}
	if body.Status != "" && body.Status != HealthStatusPassing && body.Status != HealthStatusDegraded {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid status %q", body.Status)})
		return
	}
	event, err := h.Repo.RecordEvent(r.Context(), datasetRID, body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, event)
}

func validSeverity(s HealthSeverity) bool {
	switch s {
	case HealthSeverityInfo, HealthSeverityWarning, HealthSeverityError, HealthSeverityCritical:
		return true
	}
	return false
}
