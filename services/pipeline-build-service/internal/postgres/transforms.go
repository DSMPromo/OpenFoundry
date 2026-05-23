package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// CreateTransform persists a Python or SQL transform definition and
// returns the row as inserted (id, timestamps).
func (r *Repository) CreateTransform(ctx context.Context, req models.CreateTransformRequest, ownerID uuid.UUID) (*models.Transform, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("transform name is required")
	}
	if strings.TrimSpace(req.Source) == "" {
		return nil, fmt.Errorf("transform source is required")
	}
	language := models.NormalizeTransformLanguage(req.Language)
	if err := models.ValidateTransformLanguage(language); err != nil {
		return nil, err
	}
	config := json.RawMessage(`{}`)
	if len(req.Config) > 0 && !isEmptyJSON(req.Config) {
		config = req.Config
	}
	var t models.Transform
	row := r.db.QueryRow(ctx, `
INSERT INTO transforms (name, language, source, config, owner_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, language, source, config, owner_id, created_at, updated_at`,
		name, language, req.Source, config, nullableUUID(ownerID))
	if err := row.Scan(&t.ID, &t.Name, &t.Language, &t.Source, &t.Config, &t.OwnerID, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert transform: %w", err)
	}
	return &t, nil
}

// SavePipelineBuilderGraph writes the supplied JSON graph to the
// pipeline's draft_dag column and stamps draft_updated_at. Returns
// ErrPipelineNotFound when no row matches.
func (r *Repository) SavePipelineBuilderGraph(ctx context.Context, pipelineID uuid.UUID, graphJSON string) (*models.PipelineBuilderGraphSave, error) {
	if strings.TrimSpace(graphJSON) == "" {
		return nil, fmt.Errorf("graph_json is required")
	}
	if !json.Valid([]byte(graphJSON)) {
		return nil, fmt.Errorf("graph_json is not valid JSON")
	}
	now := time.Now().UTC()
	tag, err := r.db.Exec(ctx, `
UPDATE pipelines
SET draft_dag = $2::jsonb,
    draft_updated_at = $3,
    updated_at = $3
WHERE id = $1`, pipelineID, graphJSON, now)
	if err != nil {
		return nil, fmt.Errorf("update pipeline draft_dag: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, models.ErrPipelineNotFound
	}
	return &models.PipelineBuilderGraphSave{
		PipelineID:     pipelineID,
		DraftUpdatedAt: now,
		GraphJSON:      graphJSON,
	}, nil
}

// nullableUUID coerces the zero UUID to nil so the column stays NULL
// when no owner was supplied.
func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

// isEmptyJSON reports whether the raw payload is an empty / null /
// "{}" value the caller likely did not intend to override the default.
func isEmptyJSON(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null" || trimmed == "{}"
}
