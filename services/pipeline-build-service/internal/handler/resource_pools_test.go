package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

var _ = context.Background // keeps the import while tests evolve

// fakeResourcePoolRepo captures last-call inputs + returns canned
// outputs, mirroring the test seam used by transforms_test.go.
type fakeResourcePoolRepo struct {
	gotCreate  models.CreateResourcePoolRequest
	gotUpdate  models.UpdateResourcePoolRequest
	gotID      uuid.UUID
	gotPage    int64
	gotPerPage int64
	createOut  *models.ResourcePool
	getOut     *models.ResourcePool
	listOut    *models.ListResourcePoolsResponse
	updateOut  *models.ResourcePool
	err        error
}

func (f *fakeResourcePoolRepo) CreateResourcePool(_ context.Context, req models.CreateResourcePoolRequest) (*models.ResourcePool, error) {
	f.gotCreate = req
	if f.err != nil {
		return nil, f.err
	}
	if f.createOut != nil {
		return f.createOut, nil
	}
	return &models.ResourcePool{ID: uuid.New(), Name: req.Name}, nil
}

func (f *fakeResourcePoolRepo) GetResourcePool(_ context.Context, id uuid.UUID) (*models.ResourcePool, error) {
	f.gotID = id
	if f.err != nil {
		return nil, f.err
	}
	return f.getOut, nil
}

func (f *fakeResourcePoolRepo) ListResourcePools(_ context.Context, page, perPage int64) (*models.ListResourcePoolsResponse, error) {
	f.gotPage, f.gotPerPage = page, perPage
	if f.listOut != nil {
		return f.listOut, nil
	}
	return &models.ListResourcePoolsResponse{Data: []models.ResourcePool{}, Page: page, PerPage: perPage}, nil
}

func (f *fakeResourcePoolRepo) UpdateResourcePool(_ context.Context, id uuid.UUID, req models.UpdateResourcePoolRequest) (*models.ResourcePool, error) {
	f.gotID, f.gotUpdate = id, req
	if f.err != nil {
		return nil, f.err
	}
	return f.updateOut, nil
}

func (f *fakeResourcePoolRepo) DeleteResourcePool(_ context.Context, id uuid.UUID) error {
	f.gotID = id
	return f.err
}

func routeWith(method, path string, body any, handler http.HandlerFunc) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	r := chi.NewRouter()
	r.Method(method, path, handler)
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestCreateResourcePool(t *testing.T) {
	fake := &fakeResourcePoolRepo{createOut: &models.ResourcePool{ID: uuid.New(), Name: "gpu-small", Priority: 200}}
	restore := SetResourcePoolRepository(fake)
	defer restore()

	rec := routeWith(http.MethodPost, "/api/v1/resource-pools",
		map[string]any{"name": "gpu-small", "priority": 200, "max_cpu_cores": 8},
		CreateResourcePool)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.gotCreate.Name != "gpu-small" || fake.gotCreate.Priority == nil || *fake.gotCreate.Priority != 200 {
		t.Errorf("create routing: %+v", fake.gotCreate)
	}
	if fake.gotCreate.MaxCPUCores == nil || *fake.gotCreate.MaxCPUCores != 8 {
		t.Errorf("max_cpu_cores routing: %+v", fake.gotCreate.MaxCPUCores)
	}
}

func TestCreateResourcePool409OnNameTaken(t *testing.T) {
	restore := SetResourcePoolRepository(&fakeResourcePoolRepo{err: ErrResourcePoolNameTaken})
	defer restore()
	rec := routeWith(http.MethodPost, "/api/v1/resource-pools",
		map[string]any{"name": "default"}, CreateResourcePool)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestCreateResourcePool503WhenRepoMissing(t *testing.T) {
	restore := SetResourcePoolRepository(nil)
	defer restore()
	rec := routeWith(http.MethodPost, "/api/v1/resource-pools",
		map[string]any{"name": "x"}, CreateResourcePool)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestListResourcePoolsPropagatesPagination(t *testing.T) {
	fake := &fakeResourcePoolRepo{listOut: &models.ListResourcePoolsResponse{
		Data: []models.ResourcePool{{Name: "a"}, {Name: "b"}}, Total: 2, Page: 2, PerPage: 25,
	}}
	restore := SetResourcePoolRepository(fake)
	defer restore()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/resource-pools?page=2&per_page=25", nil)
	rec := httptest.NewRecorder()
	ListResourcePools(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if fake.gotPage != 2 || fake.gotPerPage != 25 {
		t.Errorf("pagination routing: page=%d per_page=%d", fake.gotPage, fake.gotPerPage)
	}
	var got models.ListResourcePoolsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Total != 2 || len(got.Data) != 2 {
		t.Errorf("list body: %+v", got)
	}
}

func TestGetResourcePool404OnMissing(t *testing.T) {
	restore := SetResourcePoolRepository(&fakeResourcePoolRepo{err: ErrResourcePoolNotFound})
	defer restore()
	r := chi.NewRouter()
	r.Get("/resource-pools/{id}", GetResourcePool)
	req := httptest.NewRequest(http.MethodGet, "/resource-pools/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetResourcePool400OnBadUUID(t *testing.T) {
	restore := SetResourcePoolRepository(&fakeResourcePoolRepo{})
	defer restore()
	r := chi.NewRouter()
	r.Get("/resource-pools/{id}", GetResourcePool)
	req := httptest.NewRequest(http.MethodGet, "/resource-pools/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUpdateResourcePoolPropagatesClearFlag(t *testing.T) {
	id := uuid.New()
	fake := &fakeResourcePoolRepo{updateOut: &models.ResourcePool{ID: id, Name: "default"}}
	restore := SetResourcePoolRepository(fake)
	defer restore()

	r := chi.NewRouter()
	r.Patch("/resource-pools/{id}", UpdateResourcePool)
	body, _ := json.Marshal(map[string]any{"max_cpu_cores_clear": true, "priority": 50})
	req := httptest.NewRequest(http.MethodPatch, "/resource-pools/"+id.String(), bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !fake.gotUpdate.MaxCPUCoresClear {
		t.Errorf("clear flag not propagated: %+v", fake.gotUpdate)
	}
	if fake.gotUpdate.Priority == nil || *fake.gotUpdate.Priority != 50 {
		t.Errorf("priority not propagated: %+v", fake.gotUpdate.Priority)
	}
}

func TestDeleteResourcePool204(t *testing.T) {
	restore := SetResourcePoolRepository(&fakeResourcePoolRepo{})
	defer restore()
	r := chi.NewRouter()
	r.Delete("/resource-pools/{id}", DeleteResourcePool)
	req := httptest.NewRequest(http.MethodDelete, "/resource-pools/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}
