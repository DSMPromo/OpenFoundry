package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// ErrMLModelNotFound is mapped to HTTP 404 by the handler.
var ErrMLModelNotFound = errors.New("ml model not found")

const mlModelSelectColumns = `id, slug, display_name, description, framework, version, input_schema, output_schema, artifact_uri, inference_url, owner_id, created_at, updated_at`

// ListMLModels returns all registered models ordered alphabetically by
// display_name (case-insensitive).
func (r *Repository) ListMLModels(ctx context.Context) ([]models.MLModel, error) {
	rows, err := r.db.Query(ctx, `SELECT `+mlModelSelectColumns+` FROM ml_models ORDER BY LOWER(display_name) ASC, slug ASC`)
	if err != nil {
		return nil, fmt.Errorf("list ml models: %w", err)
	}
	defer rows.Close()
	out := make([]models.MLModel, 0)
	for rows.Next() {
		model, err := scanMLModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, model)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ml models: %w", err)
	}
	return out, nil
}

// GetMLModel resolves a model by uuid OR slug. The unified lookup matches the
// way pipeline node configs reference models (UUID for stability, slug for
// developer ergonomics).
func (r *Repository) GetMLModel(ctx context.Context, idOrSlug string) (*models.MLModel, error) {
	if idOrSlug == "" {
		return nil, ErrMLModelNotFound
	}
	var (
		row     pgx.Row
		queried = `SELECT ` + mlModelSelectColumns + ` FROM ml_models WHERE `
	)
	if parsed, err := uuid.Parse(idOrSlug); err == nil {
		row = r.db.QueryRow(ctx, queried+`id = $1`, parsed)
	} else {
		row = r.db.QueryRow(ctx, queried+`slug = $1`, idOrSlug)
	}
	model, err := scanMLModel(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMLModelNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// CreateMLModel inserts a new model row. Slug conflicts return a wrapped
// error the handler maps to 409.
func (r *Repository) CreateMLModel(ctx context.Context, req models.CreateMLModelRequest, ownerID *uuid.UUID) (*models.MLModel, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if req.InputSchema == nil {
		req.InputSchema = []models.MLModelField{}
	}
	if req.OutputSchema == nil {
		req.OutputSchema = []models.MLModelField{}
	}
	inputSchema, err := json.Marshal(req.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("encode input_schema: %w", err)
	}
	outputSchema, err := json.Marshal(req.OutputSchema)
	if err != nil {
		return nil, fmt.Errorf("encode output_schema: %w", err)
	}
	id := uuid.New()
	row := r.db.QueryRow(
		ctx, `
		INSERT INTO ml_models (id, slug, display_name, description, framework, version, input_schema, output_schema, artifact_uri, inference_url, owner_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+mlModelSelectColumns,
		id, req.Slug, req.DisplayName, req.Description, req.Framework, req.Version, inputSchema, outputSchema, req.ArtifactURI, req.InferenceURL, ownerID,
	)
	model, err := scanMLModel(row)
	if err != nil {
		return nil, fmt.Errorf("insert ml model: %w", err)
	}
	return &model, nil
}

// DeleteMLModel removes a model row by id. Returns false when the row does
// not exist.
func (r *Repository) DeleteMLModel(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM ml_models WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("delete ml model: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

type mlModelScanner interface {
	Scan(dest ...any) error
}

func scanMLModel(row mlModelScanner) (models.MLModel, error) {
	var (
		m            models.MLModel
		inputSchema  []byte
		outputSchema []byte
	)
	if err := row.Scan(
		&m.ID, &m.Slug, &m.DisplayName, &m.Description, &m.Framework, &m.Version,
		&inputSchema, &outputSchema,
		&m.ArtifactURI, &m.InferenceURL, &m.OwnerID, &m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return models.MLModel{}, err
	}
	if parsed, err := models.DecodeMLModelSchema(inputSchema); err == nil {
		m.InputSchema = parsed
	} else {
		m.InputSchema = []models.MLModelField{}
	}
	if parsed, err := models.DecodeMLModelSchema(outputSchema); err == nil {
		m.OutputSchema = parsed
	} else {
		m.OutputSchema = []models.MLModelField{}
	}
	return m, nil
}
