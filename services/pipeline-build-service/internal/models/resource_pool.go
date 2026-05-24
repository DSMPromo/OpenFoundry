package models

import (
	"time"

	"github.com/google/uuid"
)

// ResourcePool is the `resource_pools` table row. A pool carves
// out a slice of the cluster's distributed-compute capacity. The
// dispatcher (TASKS_COMPUTE_PIPELINES.md Task A3) admits a build
// into a pool only when none of the `max_*` ceilings would be
// crossed; otherwise the build stays QUEUED with reason
// WAITING_FOR_RESOURCES.
type ResourcePool struct {
	ID                  uuid.UUID  `json:"id"`
	Name                string     `json:"name"`
	Description         string     `json:"description"`
	MaxCPUCores         *int       `json:"max_cpu_cores,omitempty"`
	MaxMemoryGB         *int       `json:"max_memory_gb,omitempty"`
	MaxConcurrentBuilds *int       `json:"max_concurrent_builds,omitempty"`
	Priority            int        `json:"priority"`
	ProjectID           *uuid.UUID `json:"project_id,omitempty"`
	ClusterTarget       string     `json:"cluster_target"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// CreateResourcePoolRequest is the wire body accepted by
// POST /api/v1/resource-pools.
type CreateResourcePoolRequest struct {
	Name                string     `json:"name"`
	Description         *string    `json:"description,omitempty"`
	MaxCPUCores         *int       `json:"max_cpu_cores,omitempty"`
	MaxMemoryGB         *int       `json:"max_memory_gb,omitempty"`
	MaxConcurrentBuilds *int       `json:"max_concurrent_builds,omitempty"`
	Priority            *int       `json:"priority,omitempty"`
	ProjectID           *uuid.UUID `json:"project_id,omitempty"`
	ClusterTarget       *string    `json:"cluster_target,omitempty"`
}

// UpdateResourcePoolRequest is the body accepted by
// PATCH /api/v1/resource-pools/{id}. Every field is a pointer so
// the JSON encoder can distinguish "not supplied" from "explicit
// zero / null"; the repo only writes fields whose pointer is non-nil.
//
// `MaxCPUCoresClear` (and peers) is a separate explicit flag so the
// caller can null out a previously-set cap; supplying a non-nil
// *int sets a value, supplying ClearFoo=true unsets it.
type UpdateResourcePoolRequest struct {
	Name                *string `json:"name,omitempty"`
	Description         *string `json:"description,omitempty"`
	MaxCPUCores         *int    `json:"max_cpu_cores,omitempty"`
	MaxCPUCoresClear    bool    `json:"max_cpu_cores_clear,omitempty"`
	MaxMemoryGB         *int    `json:"max_memory_gb,omitempty"`
	MaxMemoryGBClear    bool    `json:"max_memory_gb_clear,omitempty"`
	MaxConcurrentBuilds *int    `json:"max_concurrent_builds,omitempty"`
	MaxConcurrentClear  bool    `json:"max_concurrent_builds_clear,omitempty"`
	Priority            *int    `json:"priority,omitempty"`
	ClusterTarget       *string `json:"cluster_target,omitempty"`
}

// ListResourcePoolsResponse paginates the catalog. List handlers in
// pipeline-build-service emit `data`+`total`+`page`+`per_page`
// (see Pipeline list shape), so resource pools follow the same.
type ListResourcePoolsResponse struct {
	Data    []ResourcePool `json:"data"`
	Total   int64          `json:"total"`
	Page    int64          `json:"page"`
	PerPage int64          `json:"per_page"`
}
