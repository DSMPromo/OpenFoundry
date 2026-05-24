package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

type fakeMLRegistryRepo struct {
	mu     sync.Mutex
	models []models.MLModel
}

func (f *fakeMLRegistryRepo) ListMLModels(context.Context) ([]models.MLModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]models.MLModel(nil), f.models...), nil
}

func (f *fakeMLRegistryRepo) GetMLModel(_ context.Context, idOrSlug string) (*models.MLModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, m := range f.models {
		if m.ID.String() == idOrSlug || m.Slug == idOrSlug {
			out := f.models[i]
			return &out, nil
		}
	}
	return nil, errors.New("ml model not found")
}

func (f *fakeMLRegistryRepo) CreateMLModel(_ context.Context, req models.CreateMLModelRequest, ownerID *uuid.UUID) (*models.MLModel, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.models {
		if m.Slug == req.Slug {
			return nil, errors.New("slug already exists")
		}
	}
	m := models.MLModel{
		ID:           uuid.New(),
		Slug:         req.Slug,
		DisplayName:  req.DisplayName,
		Description:  req.Description,
		Framework:    req.Framework,
		Version:      req.Version,
		InputSchema:  req.InputSchema,
		OutputSchema: req.OutputSchema,
		ArtifactURI:  req.ArtifactURI,
		InferenceURL: req.InferenceURL,
		OwnerID:      ownerID,
	}
	if m.InputSchema == nil {
		m.InputSchema = []models.MLModelField{}
	}
	if m.OutputSchema == nil {
		m.OutputSchema = []models.MLModelField{}
	}
	f.models = append(f.models, m)
	return &m, nil
}

func (f *fakeMLRegistryRepo) DeleteMLModel(_ context.Context, id uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.models[:0]
	removed := false
	for _, m := range f.models {
		if m.ID == id {
			removed = true
			continue
		}
		out = append(out, m)
	}
	f.models = out
	return removed, nil
}

func TestCreateAndListMLModel(t *testing.T) {
	repo := &fakeMLRegistryRepo{}
	restore := SetMLModelRepository(repo)
	t.Cleanup(restore)
	owner := uuid.New()

	body, _ := json.Marshal(models.CreateMLModelRequest{
		Slug:         "demo",
		DisplayName:  "Demo",
		Framework:    "sklearn",
		InputSchema:  []models.MLModelField{{Name: "x", Type: "float"}},
		OutputSchema: []models.MLModelField{{Name: "y", Type: "float"}},
	})
	rr := httptest.NewRecorder()
	CreateMLModel(rr, requestWithAuth(http.MethodPost, "/ml-models", body, owner, nil))
	require.Equal(t, http.StatusCreated, rr.Code)

	rr = httptest.NewRecorder()
	ListMLModels(rr, requestWithAuth(http.MethodGet, "/ml-models", nil, owner, nil))
	require.Equal(t, http.StatusOK, rr.Code)
	var listed struct {
		Items []models.MLModel `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&listed))
	require.Len(t, listed.Items, 1)
	require.Equal(t, "demo", listed.Items[0].Slug)
}

func TestCreateMLModelValidationRejectsBadSlug(t *testing.T) {
	repo := &fakeMLRegistryRepo{}
	restore := SetMLModelRepository(repo)
	t.Cleanup(restore)
	owner := uuid.New()

	body, _ := json.Marshal(map[string]string{
		"slug":         "Has Uppercase Spaces",
		"display_name": "Bad",
	})
	rr := httptest.NewRecorder()
	CreateMLModel(rr, requestWithAuth(http.MethodPost, "/ml-models", body, owner, nil))
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestGetMLModelByIdAndBySlug(t *testing.T) {
	repo := &fakeMLRegistryRepo{}
	restore := SetMLModelRepository(repo)
	t.Cleanup(restore)
	owner := uuid.New()

	rr := httptest.NewRecorder()
	body, _ := json.Marshal(models.CreateMLModelRequest{Slug: "by-slug", DisplayName: "By slug"})
	CreateMLModel(rr, requestWithAuth(http.MethodPost, "/ml-models", body, owner, nil))
	require.Equal(t, http.StatusCreated, rr.Code)
	var created models.MLModel
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))

	// Lookup by slug.
	rr = httptest.NewRecorder()
	GetMLModel(rr, requestWithAuth(http.MethodGet, "/ml-models/by-slug", nil, owner, map[string]string{"id": "by-slug"}))
	require.Equal(t, http.StatusOK, rr.Code)

	// Lookup by id.
	rr = httptest.NewRecorder()
	GetMLModel(rr, requestWithAuth(http.MethodGet, "/ml-models/"+created.ID.String(), nil, owner, map[string]string{"id": created.ID.String()}))
	require.Equal(t, http.StatusOK, rr.Code)

	// Lookup missing.
	rr = httptest.NewRecorder()
	GetMLModel(rr, requestWithAuth(http.MethodGet, "/ml-models/nope", nil, owner, map[string]string{"id": "nope"}))
	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDeleteMLModelOwnerOnly(t *testing.T) {
	repo := &fakeMLRegistryRepo{}
	restore := SetMLModelRepository(repo)
	t.Cleanup(restore)
	owner := uuid.New()
	stranger := uuid.New()

	rr := httptest.NewRecorder()
	body, _ := json.Marshal(models.CreateMLModelRequest{Slug: "to-delete", DisplayName: "X"})
	CreateMLModel(rr, requestWithAuth(http.MethodPost, "/ml-models", body, owner, nil))
	require.Equal(t, http.StatusCreated, rr.Code)
	var created models.MLModel
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))

	// Stranger cannot delete.
	rr = httptest.NewRecorder()
	DeleteMLModel(rr, requestWithAuth(http.MethodDelete, "/ml-models/"+created.ID.String(), nil, stranger, map[string]string{"id": created.ID.String()}))
	require.Equal(t, http.StatusForbidden, rr.Code)

	// Owner can delete.
	rr = httptest.NewRecorder()
	DeleteMLModel(rr, requestWithAuth(http.MethodDelete, "/ml-models/"+created.ID.String(), nil, owner, map[string]string{"id": created.ID.String()}))
	require.Equal(t, http.StatusNoContent, rr.Code)
}

// Keep the bytes / sync imports honest if the test runner inlines them.
var (
	_ = bytes.NewReader
	_ = sync.Mutex{}
)
