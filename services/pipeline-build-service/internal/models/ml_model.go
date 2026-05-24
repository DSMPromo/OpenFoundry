package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MLModel is the wire shape of a registered trained model. The fields mirror
// what Foundry's model registry exposes for pipeline authors: identity,
// framework + version, typed feature and output schemas, and an artifact /
// serving location.
type MLModel struct {
	ID           uuid.UUID      `json:"id"`
	Slug         string         `json:"slug"`
	DisplayName  string         `json:"display_name"`
	Description  string         `json:"description"`
	Framework    string         `json:"framework"`
	Version      string         `json:"version"`
	InputSchema  []MLModelField `json:"input_schema"`
	OutputSchema []MLModelField `json:"output_schema"`
	ArtifactURI  string         `json:"artifact_uri"`
	InferenceURL string         `json:"inference_url,omitempty"`
	OwnerID      *uuid.UUID     `json:"owner_id,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// MLModelField is one entry in the input or output schema of a model.
type MLModelField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// CreateMLModelRequest is the body for POST /ml-models.
type CreateMLModelRequest struct {
	Slug         string         `json:"slug"`
	DisplayName  string         `json:"display_name"`
	Description  string         `json:"description,omitempty"`
	Framework    string         `json:"framework,omitempty"`
	Version      string         `json:"version,omitempty"`
	InputSchema  []MLModelField `json:"input_schema,omitempty"`
	OutputSchema []MLModelField `json:"output_schema,omitempty"`
	ArtifactURI  string         `json:"artifact_uri,omitempty"`
	InferenceURL string         `json:"inference_url,omitempty"`
}

var supportedMLFrameworks = map[string]struct{}{
	"sklearn":    {},
	"pytorch":    {},
	"tensorflow": {},
	"onnx":       {},
	"xgboost":    {},
	"lightgbm":   {},
	"custom":     {},
}

// Validate enforces the basic shape rules: slug regex, mandatory name, and
// known framework if provided.
func (req *CreateMLModelRequest) Validate() error {
	req.Slug = strings.TrimSpace(req.Slug)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.Slug == "" {
		return errors.New("slug is required")
	}
	if !isMLModelSlugValid(req.Slug) {
		return fmt.Errorf("slug %q must match [a-z0-9][a-z0-9_-]*", req.Slug)
	}
	if req.DisplayName == "" {
		return errors.New("display_name is required")
	}
	if req.Framework == "" {
		req.Framework = "sklearn"
	}
	if _, ok := supportedMLFrameworks[strings.ToLower(req.Framework)]; !ok {
		return fmt.Errorf("unsupported framework %q", req.Framework)
	}
	if req.Version == "" {
		req.Version = "1.0.0"
	}
	return nil
}

func isMLModelSlugValid(slug string) bool {
	if slug == "" {
		return false
	}
	for index, r := range slug {
		if index == 0 {
			if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') {
				return false
			}
			continue
		}
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

// DecodeMLModelSchema is a helper used by repos / tests to round-trip the
// JSONB schema columns.
func DecodeMLModelSchema(raw json.RawMessage) ([]MLModelField, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []MLModelField{}, nil
	}
	var out []MLModelField
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode ml model schema: %w", err)
	}
	return out, nil
}
