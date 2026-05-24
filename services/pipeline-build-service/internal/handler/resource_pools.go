package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// ResourcePoolRepository is the persistence seam for the
// /api/v1/resource-pools surface. The production implementation
// lives in services/pipeline-build-service/internal/postgres;
// tests inject a fake.
type ResourcePoolRepository interface {
	CreateResourcePool(ctx context.Context, req models.CreateResourcePoolRequest) (*models.ResourcePool, error)
	GetResourcePool(ctx context.Context, id uuid.UUID) (*models.ResourcePool, error)
	ListResourcePools(ctx context.Context, page, perPage int64) (*models.ListResourcePoolsResponse, error)
	UpdateResourcePool(ctx context.Context, id uuid.UUID, req models.UpdateResourcePoolRequest) (*models.ResourcePool, error)
	DeleteResourcePool(ctx context.Context, id uuid.UUID) error
}

// ErrResourcePoolNotFound + ErrResourcePoolNameTaken are the
// handler-side sentinels. The postgres package returns the same
// values via models so the handler can errors.Is them without
// importing postgres directly.
var (
	ErrResourcePoolNotFound  = errors.New("resource pool not found")
	ErrResourcePoolNameTaken = errors.New("resource pool name is already taken")
)

type resourcePoolSlot struct{ repo ResourcePoolRepository }

var resourcePoolRepoValue atomic.Value // stores *resourcePoolSlot

// SetResourcePoolRepository injects the production repo. Tests swap
// a fake and rely on the returned restore func to revert.
func SetResourcePoolRepository(repo ResourcePoolRepository) func() {
	previous, _ := resourcePoolRepoValue.Load().(*resourcePoolSlot)
	resourcePoolRepoValue.Store(&resourcePoolSlot{repo: repo})
	return func() { resourcePoolRepoValue.Store(previous) }
}

func currentResourcePoolRepository() (ResourcePoolRepository, bool) {
	slot, _ := resourcePoolRepoValue.Load().(*resourcePoolSlot)
	if slot == nil || slot.repo == nil {
		return nil, false
	}
	return slot.repo, true
}

func requireResourcePoolRepository(w http.ResponseWriter, detail string) (ResourcePoolRepository, bool) {
	repo, ok := currentResourcePoolRepository()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":  "resource_pool_repository_not_configured",
			"detail": detail,
		})
		return nil, false
	}
	return repo, true
}

// CreateResourcePool handles POST /api/v1/resource-pools.
func CreateResourcePool(w http.ResponseWriter, r *http.Request) {
	repo, ok := requireResourcePoolRepository(w, "resource-pool admin requires DATABASE_URL-backed repository wiring")
	if !ok {
		return
	}
	var body models.CreateResourcePoolRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "detail": err.Error()})
		return
	}
	pool, err := repo.CreateResourcePool(r.Context(), body)
	if err != nil {
		if errors.Is(err, ErrResourcePoolNameTaken) || isNameTakenError(err) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "name_taken", "detail": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "create_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, pool)
}

// ListResourcePools handles GET /api/v1/resource-pools.
func ListResourcePools(w http.ResponseWriter, r *http.Request) {
	repo, ok := requireResourcePoolRepository(w, "resource-pool list requires DATABASE_URL-backed repository wiring")
	if !ok {
		return
	}
	page, _ := strconv.ParseInt(r.URL.Query().Get("page"), 10, 64)
	perPage, _ := strconv.ParseInt(r.URL.Query().Get("per_page"), 10, 64)
	resp, err := repo.ListResourcePools(r.Context(), page, perPage)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetResourcePool handles GET /api/v1/resource-pools/{id}.
func GetResourcePool(w http.ResponseWriter, r *http.Request) {
	repo, ok := requireResourcePoolRepository(w, "resource-pool get requires DATABASE_URL-backed repository wiring")
	if !ok {
		return
	}
	id, ok := poolIDFromPath(w, r)
	if !ok {
		return
	}
	pool, err := repo.GetResourcePool(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrResourcePoolNotFound) || isNotFoundError(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource_pool_not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "get_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, pool)
}

// UpdateResourcePool handles PATCH /api/v1/resource-pools/{id}.
func UpdateResourcePool(w http.ResponseWriter, r *http.Request) {
	repo, ok := requireResourcePoolRepository(w, "resource-pool update requires DATABASE_URL-backed repository wiring")
	if !ok {
		return
	}
	id, ok := poolIDFromPath(w, r)
	if !ok {
		return
	}
	var body models.UpdateResourcePoolRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "detail": err.Error()})
		return
	}
	pool, err := repo.UpdateResourcePool(r.Context(), id, body)
	if err != nil {
		if errors.Is(err, ErrResourcePoolNotFound) || isNotFoundError(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource_pool_not_found"})
			return
		}
		if errors.Is(err, ErrResourcePoolNameTaken) || isNameTakenError(err) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "name_taken", "detail": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "update_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, pool)
}

// DeleteResourcePool handles DELETE /api/v1/resource-pools/{id}.
func DeleteResourcePool(w http.ResponseWriter, r *http.Request) {
	repo, ok := requireResourcePoolRepository(w, "resource-pool delete requires DATABASE_URL-backed repository wiring")
	if !ok {
		return
	}
	id, ok := poolIDFromPath(w, r)
	if !ok {
		return
	}
	if err := repo.DeleteResourcePool(r.Context(), id); err != nil {
		if errors.Is(err, ErrResourcePoolNotFound) || isNotFoundError(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource_pool_not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete_failed", "detail": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// poolIDFromPath parses the {id} chi path parameter as a UUID and
// emits a 400 response on miss.
func poolIDFromPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(chi.URLParam(r, "id"))
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "detail": "invalid resource pool id"})
		return uuid.Nil, false
	}
	return id, true
}

// isNotFoundError and isNameTakenError let the handler recognize
// postgres-package sentinels without importing the postgres package
// directly (which would create a cycle with this handler).
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "resource pool not found"
}

func isNameTakenError(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "resource pool name is already taken"
}
