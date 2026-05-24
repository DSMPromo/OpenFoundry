package handlers_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
	cmruntime "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/runtime"
)

func TestConnectionJSONShape(t *testing.T) {
	t.Parallel()
	c := models.Connection{
		ID: uuid.New(), Name: "snowflake-prod",
		ConnectorType: "snowflake",
		Config:        json.RawMessage(`{"account":"x"}`),
		Status:        "disconnected",
		OwnerID:       uuid.New(),
		CreatedAt:     time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(c)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "name", "connector_type", "config", "status",
		"owner_id", "last_sync_at", "created_at", "updated_at",
	} {
		assert.Contains(t, view, k)
	}
}

func TestCreateConnectionRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("POST", "/connections",
		strings.NewReader(`{"name":"x","connector_type":"y"}`))
	rec := httptest.NewRecorder()
	h.CreateConnection(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestCreateConnectionRejectsEmptyFields(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/connections",
		strings.NewReader(`{"name":"","connector_type":""}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateConnection(rec, req)
	assert.Equal(t, 400, rec.Code)
}

func TestCreateRestAPISourceNormalizesOutboundWebhookModel(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	req := httptest.NewRequest(http.MethodPost, "/sources", strings.NewReader(`{
		"name":"Open-Meteo",
		"connector_type":"rest_api",
		"config":{
			"domain":"api.open-meteo.com",
			"auth":{"type":"none"},
			"runtime":{"worker":"foundry","timeout_ms":15000,"allowed_methods":["get","post"]},
			"permissions":{"invokable":true},
			"webhook":{"path":"/v1/forecast"}
		}
	}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), &authmw.Claims{Sub: owner}))
	rec := httptest.NewRecorder()

	h.CreateConnection(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	var body models.Connection
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	var cfg models.RESTAPISourceConfig
	require.NoError(t, json.Unmarshal(body.Config, &cfg))
	require.Equal(t, "Open-Meteo", body.Name)
	require.Equal(t, "rest_api", body.ConnectorType)
	require.Equal(t, "api.open-meteo.com", cfg.Domain)
	require.Equal(t, "https://api.open-meteo.com", cfg.BaseURL)
	require.Equal(t, "none", cfg.Auth.Type)
	require.Equal(t, "foundry", cfg.Runtime.Worker)
	require.Equal(t, 15000, cfg.Runtime.TimeoutMS)
	require.Equal(t, []string{"GET", "POST"}, cfg.Runtime.AllowedMethods)
	require.True(t, cfg.Permissions.Invokable)
	require.Equal(t, []string{"api.open-meteo.com"}, cfg.Permissions.AllowedEgressHosts)
	require.NotNil(t, cfg.Webhook)
	require.Equal(t, "GET", cfg.Webhook.Method)
	require.Equal(t, "/v1/forecast", cfg.Webhook.Path)
}

func TestCreateRestAPISourceRejectsInvalidDomain(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	req := httptest.NewRequest(http.MethodPost, "/sources", strings.NewReader(`{
		"name":"bad",
		"connector_type":"rest_api",
		"config":{"base_url":"ftp://example.com"}
	}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), &authmw.Claims{Sub: owner}))
	rec := httptest.NewRecorder()

	h.CreateConnection(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "http or https")
}

func TestUpdateRestAPISourceNormalizesConfig(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "rest_api"
	h := &handlers.Handlers{Repo: store}
	req := httptest.NewRequest(http.MethodPatch, "/sources/"+store.connections[0].ID.String(), strings.NewReader(`{
		"config":{
			"base_url":"https://api.example.com/",
			"auth":{"type":"bearer","secret_ref":"secret://weather-token"},
			"runtime":{"worker":"agent","timeout_ms":30000,"allowed_methods":["GET"]},
			"permissions":{"discoverable":true,"invokable":true,"allowed_egress_hosts":["api.example.com"]}
		}
	}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), &authmw.Claims{Sub: owner}))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", store.connections[0].ID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.UpdateConnection(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body models.Connection
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	var cfg models.RESTAPISourceConfig
	require.NoError(t, json.Unmarshal(body.Config, &cfg))
	require.Equal(t, "api.example.com", cfg.Domain)
	require.Equal(t, "https://api.example.com", cfg.BaseURL)
	require.Equal(t, "bearer", cfg.Auth.Type)
	require.Equal(t, "secret://weather-token", cfg.Auth.SecretRef)
	require.Equal(t, "agent", cfg.Runtime.Worker)
	require.Equal(t, []string{"GET"}, cfg.Runtime.AllowedMethods)
}

func TestWebhookHistoryRepositoryContract(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	store := newFakeStore(owner)
	sourceID := store.connections[0].ID
	now := time.Now().UTC()

	entry, err := store.AppendWebhookHistory(context.Background(), &models.CreateWebhookHistoryEntry{
		SourceID: sourceID,
		UserID:   owner,
		Status:   "succeeded",
		InputPolicy: models.WebhookHistoryInputPolicy{
			StoreOutputs: true,
			Visibility:   "hidden",
		},
		OutputParameters:   json.RawMessage(`{"temperature":84}`),
		StartedAt:          now.Add(-25 * time.Millisecond),
		FinishedAt:         now,
		RetentionExpiresAt: now.Add(time.Hour),
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, entry.ID)
	require.Positive(t, entry.DurationMS)

	expiredErr := "expired"
	_, err = store.AppendWebhookHistory(context.Background(), &models.CreateWebhookHistoryEntry{
		SourceID:           sourceID,
		UserID:             owner,
		Status:             "failed",
		Error:              &expiredErr,
		StartedAt:          now.Add(-2 * time.Hour),
		FinishedAt:         now.Add(-2 * time.Hour),
		RetentionExpiresAt: now.Add(-time.Hour),
	})
	require.NoError(t, err)

	items, err := store.ListWebhookHistory(context.Background(), sourceID, 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "succeeded", items[0].Status)
	assert.JSONEq(t, `{"temperature":84}`, string(items[0].OutputParameters))

	items, err = store.ListWebhookHistory(context.Background(), sourceID, 1)
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestInboundListenerRepositoryContract(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	store := newFakeStore(owner)
	sourceID := store.connections[0].ID
	objectTypeID := uuid.New()

	entry, err := store.AppendInboundListenerEvent(context.Background(), &models.CreateInboundListenerEvent{
		SourceID:          sourceID,
		ListenerID:        "trail-events",
		EventID:           "evt-1",
		Status:            "accepted",
		SignatureVerified: true,
		Payload:           json.RawMessage(`{"trail_id":"mule-deer"}`),
		Destination: models.InboundListenerDestinationConfig{
			Mode:         "object",
			ObjectTypeID: &objectTypeID,
		},
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, entry.ID)

	items, err := store.ListInboundListenerEvents(context.Background(), sourceID, 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "trail-events", items[0].ListenerID)
	assert.Equal(t, "evt-1", items[0].EventID)
	assert.True(t, items[0].SignatureVerified)
	assert.Equal(t, objectTypeID, *items[0].Destination.ObjectTypeID)
	assert.JSONEq(t, `{"trail_id":"mule-deer"}`, string(items[0].Payload))
}

func TestListConnectionsRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/connections", nil)
	rec := httptest.NewRecorder()
	h.ListConnections(rec, req)
	assert.Equal(t, 401, rec.Code)
}

type testConnectionAdapter struct {
	result      adapters.ConnectionTestResult
	err         error
	queryResult *adapters.Result
	queryErr    error
}

func (a testConnectionAdapter) TestConnection(_ context.Context, _ json.RawMessage) (adapters.ConnectionTestResult, error) {
	return a.result, a.err
}

func (a testConnectionAdapter) DiscoverSources(context.Context, *models.Connection, string) ([]adapters.Source, error) {
	return nil, adapters.ErrNotImplemented
}

func (a testConnectionAdapter) QueryVirtualTable(_ context.Context, _ *models.Connection, q *adapters.Query, _ string) (*adapters.Result, error) {
	if a.queryErr != nil {
		return nil, a.queryErr
	}
	if a.queryResult != nil {
		out := *a.queryResult
		if out.Selector == "" && q != nil {
			out.Selector = q.Selector
		}
		return &out, nil
	}
	return nil, adapters.ErrNotImplemented
}

func (a testConnectionAdapter) StreamArrow(context.Context, *models.Connection, *adapters.Query, string) (adapters.ArrowStream, error) {
	return adapters.EmptyArrowStream{}, adapters.ErrNotImplemented
}

func (a testConnectionAdapter) BuildIngestSpec(context.Context, *models.Connection, *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, adapters.ErrNotImplemented
}

func TestTestConnectionUsesAdapterResultAndUpdatesStatus(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "kafka"
	store.connections[0].Status = "disconnected"
	registry := adapters.NewRegistry()
	registry.MustRegister("kafka", adapters.SingletonFactory(testConnectionAdapter{result: adapters.ConnectionTestResult{
		Success:   true,
		Message:   "validated kafka catalog with 1 topic(s)",
		LatencyMS: 7,
		Details:   json.RawMessage(`{"mode":"catalog_backed","topic_count":1}`),
	}}))
	h := &handlers.Handlers{Repo: store, AdapterRegistry: registry}
	req := httptest.NewRequest(http.MethodPost, "/connections/"+store.connections[0].ID.String()+"/test", nil)
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), &authmw.Claims{Sub: owner}))
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", store.connections[0].ID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.TestConnection(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "connected", store.connections[0].Status)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, true, body["success"])
	assert.Equal(t, "validated kafka catalog with 1 topic(s)", body["message"])
	assert.Equal(t, float64(7), body["latency_ms"])
	assert.Equal(t, map[string]any{"mode": "catalog_backed", "topic_count": float64(1)}, body["details"])
}

func TestTestConnectionAdapterErrorMarksConnectionError(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "kafka"
	registry := adapters.NewRegistry()
	registry.MustRegister("kafka", adapters.SingletonFactory(testConnectionAdapter{err: assert.AnError}))
	h := &handlers.Handlers{Repo: store, AdapterRegistry: registry}
	req := httptest.NewRequest(http.MethodPost, "/connections/"+store.connections[0].ID.String()+"/test", nil)
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), &authmw.Claims{Sub: owner}))
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", store.connections[0].ID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.TestConnection(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "error", store.connections[0].Status)
	assert.Contains(t, rec.Body.String(), assert.AnError.Error())
}

type fakeStore struct {
	connections     []models.Connection
	syncJobs        map[uuid.UUID][]models.SyncJob
	exports         map[uuid.UUID][]models.DataExport
	mediaSyncs      map[uuid.UUID][]models.MediaSetSync
	runs            map[uuid.UUID][]models.SyncRun
	links           map[string]models.VirtualTableSourceLink
	vtables         map[string]models.VirtualTable
	polls           map[string][]models.PollHistoryRow
	registrations   map[uuid.UUID][]models.ConnectionRegistration
	policies        map[uuid.UUID][]models.SourcePolicyBindingResponse
	credentials     map[uuid.UUID][]models.CredentialResponse
	codeImports     map[uuid.UUID]models.SourceCodeImport
	governance      map[uuid.UUID]models.SourceGovernance
	auditEvents     map[uuid.UUID][]models.SourceGovernanceAuditEvent
	agents          []models.ConnectorAgent
	webhookHistory  map[uuid.UUID][]models.WebhookHistoryEntry
	listenerEvents  map[uuid.UUID][]models.InboundListenerEvent
	retryPolicies   map[uuid.UUID]models.SourceRetryPolicy
	retryFailures   map[uuid.UUID][]models.RetryRecoveryRunSummary
	mediaSyncRuns   map[uuid.UUID][]models.MediaSetSyncRun
	deadLetterSinks map[uuid.UUID]models.DeadLetterSink
	quarantine      map[uuid.UUID][]models.QuarantinedRecord
}

func newFakeStore(owner uuid.UUID) *fakeStore {
	conn := models.Connection{ID: uuid.New(), Name: "pg", ConnectorType: "postgresql", Config: json.RawMessage(`{}`), Status: "connected", OwnerID: owner, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	return &fakeStore{connections: []models.Connection{conn}, syncJobs: map[uuid.UUID][]models.SyncJob{}, exports: map[uuid.UUID][]models.DataExport{}, mediaSyncs: map[uuid.UUID][]models.MediaSetSync{}, runs: map[uuid.UUID][]models.SyncRun{}, links: map[string]models.VirtualTableSourceLink{}, vtables: map[string]models.VirtualTable{}, polls: map[string][]models.PollHistoryRow{}, registrations: map[uuid.UUID][]models.ConnectionRegistration{}, policies: map[uuid.UUID][]models.SourcePolicyBindingResponse{}, credentials: map[uuid.UUID][]models.CredentialResponse{}, codeImports: map[uuid.UUID]models.SourceCodeImport{}, governance: map[uuid.UUID]models.SourceGovernance{}, auditEvents: map[uuid.UUID][]models.SourceGovernanceAuditEvent{}, agents: []models.ConnectorAgent{}, webhookHistory: map[uuid.UUID][]models.WebhookHistoryEntry{}, listenerEvents: map[uuid.UUID][]models.InboundListenerEvent{}}
}

func (f *fakeStore) ListConnections(_ context.Context, ownerID *uuid.UUID) ([]models.Connection, error) {
	return f.connections, nil
}

func (f *fakeStore) GetConnection(_ context.Context, id uuid.UUID) (*models.Connection, error) {
	for i := range f.connections {
		if f.connections[i].ID == id {
			return &f.connections[i], nil
		}
	}
	return nil, nil
}

func (f *fakeStore) GetConnectionForOwner(_ context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.Connection, error) {
	for i := range f.connections {
		allowed, _ := f.CheckSourceRole(context.Background(), id, ownerID, models.SourceRoleView)
		if f.connections[i].ID == id && allowed {
			return &f.connections[i], nil
		}
	}
	return nil, nil
}

func (f *fakeStore) CreateConnection(_ context.Context, body *models.CreateConnectionRequest, ownerID uuid.UUID) (*models.Connection, error) {
	c := models.Connection{ID: uuid.New(), Name: body.Name, ConnectorType: body.ConnectorType, Config: body.Config, OwnerID: ownerID}
	return &c, nil
}

func (f *fakeStore) UpdateConnection(_ context.Context, id uuid.UUID, body *models.UpdateConnectionRequest) (*models.Connection, error) {
	for i := range f.connections {
		if f.connections[i].ID == id {
			if body.Status != nil {
				f.connections[i].Status = *body.Status
			}
			if body.Name != nil {
				f.connections[i].Name = *body.Name
			}
			if body.Config != nil {
				f.connections[i].Config = body.Config
			}
			return &f.connections[i], nil
		}
	}
	return nil, nil
}

func (f *fakeStore) DeleteConnection(_ context.Context, id uuid.UUID) (bool, error) {
	c, _ := f.GetConnection(context.Background(), id)
	return c != nil, nil
}

func (f *fakeStore) CheckSourceRole(_ context.Context, sourceID uuid.UUID, actorID uuid.UUID, role models.SourcePermissionRole) (bool, error) {
	conn, _ := f.GetConnection(context.Background(), sourceID)
	if conn == nil {
		return false, nil
	}
	if conn.OwnerID == actorID {
		return true, nil
	}
	now := time.Now().UTC()
	for _, grant := range f.governance[sourceID].PermissionGrants {
		if grant.PrincipalID != actorID.String() {
			continue
		}
		if grant.PrincipalType != "" && grant.PrincipalType != "user" && grant.PrincipalType != "service_account" {
			continue
		}
		if grant.ExpiresAt != nil && grant.ExpiresAt.Before(now) {
			continue
		}
		if models.SourceRolesAllow(grant.Roles, role) {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeStore) GetSourceGovernance(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID) (*models.SourceGovernance, error) {
	conn, _ := f.GetConnection(ctx, sourceID)
	if conn == nil {
		return nil, nil
	}
	allowed, _ := f.CheckSourceRole(ctx, sourceID, actorID, models.SourceRoleView)
	if !allowed {
		return nil, nil
	}
	current := f.governance[sourceID]
	if current.SourceID == uuid.Nil {
		current = models.SourceGovernance{SourceID: sourceID, Visibility: models.DefaultSourceVisibilityPolicy()}
	}
	current.SourceID = sourceID
	current.SourceRID = models.SourceRIDForConnection(sourceID)
	current.OwnerID = conn.OwnerID
	current.RoleDefinitions = models.SourcePermissionRoleDefinitions()
	current.Visibility = models.NormalizeSourceVisibilityPolicy(current.Visibility)
	current.Warnings = models.SourceGovernanceWarnings(current.Visibility)
	current.AuditEvents = append([]models.SourceGovernanceAuditEvent(nil), f.auditEvents[sourceID]...)
	current.OutputDatasetPermissions = append([]models.SourceOutputDatasetPermission(nil), current.OutputDatasetPermissions...)
	if len(current.AuditEvents) > 50 {
		current.AuditEvents = current.AuditEvents[:50]
	}
	if conn.OwnerID == actorID {
		current.EffectiveRoles = models.AllSourcePermissionRoles()
	} else {
		effective := []models.SourcePermissionRole{}
		now := time.Now().UTC()
		for _, grant := range current.PermissionGrants {
			if grant.PrincipalID == actorID.String() && (grant.ExpiresAt == nil || grant.ExpiresAt.After(now)) {
				effective = append(effective, grant.Roles...)
			}
		}
		current.EffectiveRoles = models.ExpandSourcePermissionRoles(effective)
	}
	current.EffectiveRoles = models.NormalizeSourcePermissionRoles(current.EffectiveRoles)
	current.PermissionGrants = models.NormalizeSourcePermissionGrants(current.PermissionGrants, sourceID, conn.OwnerID, time.Now().UTC())
	return &current, nil
}

func (f *fakeStore) UpdateSourceGovernance(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID, body *models.UpdateSourceGovernanceRequest) (*models.SourceGovernance, error) {
	allowed, _ := f.CheckSourceRole(ctx, sourceID, actorID, models.SourceRoleOwner)
	if !allowed {
		return nil, nil
	}
	conn, _ := f.GetConnection(ctx, sourceID)
	if conn == nil {
		return nil, nil
	}
	if body == nil {
		body = &models.UpdateSourceGovernanceRequest{}
	}
	now := time.Now().UTC()
	visibility := models.DefaultSourceVisibilityPolicy()
	if body.Visibility != nil {
		visibility = models.NormalizeSourceVisibilityPolicy(*body.Visibility)
	}
	current := models.SourceGovernance{
		SourceID:         sourceID,
		SourceRID:        models.SourceRIDForConnection(sourceID),
		OwnerID:          conn.OwnerID,
		PermissionGrants: models.NormalizeSourcePermissionGrants(body.PermissionGrants, sourceID, actorID, now),
		Visibility:       visibility,
	}
	f.governance[sourceID] = current
	_, _ = f.RecordSourceGovernanceAudit(ctx, models.RecordSourceGovernanceAuditRequest{
		SourceID:  sourceID,
		ActorID:   &actorID,
		EventType: "permission_change",
		Action:    "update_source_governance",
		Result:    "succeeded",
		Roles:     []models.SourcePermissionRole{models.SourceRoleOwner},
		Message:   "Source governance updated",
		Metadata:  map[string]any{"grant_count": len(current.PermissionGrants), "reason": strings.TrimSpace(body.Reason)},
	})
	return f.GetSourceGovernance(ctx, sourceID, actorID)
}

func (f *fakeStore) ListSourceGovernanceAudit(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID, limit int) ([]models.SourceGovernanceAuditEvent, error) {
	allowed, _ := f.CheckSourceRole(ctx, sourceID, actorID, models.SourceRoleView)
	if !allowed {
		return []models.SourceGovernanceAuditEvent{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items := append([]models.SourceGovernanceAuditEvent(nil), f.auditEvents[sourceID]...)
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (f *fakeStore) RecordSourceGovernanceAudit(ctx context.Context, body models.RecordSourceGovernanceAuditRequest) (*models.SourceGovernanceAuditEvent, error) {
	if conn, _ := f.GetConnection(ctx, body.SourceID); conn == nil {
		return nil, nil
	}
	body = models.NormalizeSourceGovernanceAuditRequest(body)
	event := models.SourceGovernanceAuditEvent{
		ID:                    uuid.New(),
		SourceID:              body.SourceID,
		ActorID:               body.ActorID,
		EventType:             body.EventType,
		Action:                body.Action,
		Result:                body.Result,
		PrincipalID:           body.PrincipalID,
		PrincipalType:         body.PrincipalType,
		Roles:                 body.Roles,
		Capability:            body.Capability,
		JobRID:                body.JobRID,
		DownstreamResourceRID: body.DownstreamResourceRID,
		Message:               body.Message,
		Metadata:              body.Metadata,
		CreatedAt:             time.Now().UTC(),
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	f.auditEvents[body.SourceID] = append([]models.SourceGovernanceAuditEvent{event}, f.auditEvents[body.SourceID]...)
	return &event, nil
}

func (f *fakeStore) ListSyncJobs(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.SyncJob, error) {
	if c, _ := f.GetConnectionForOwner(context.Background(), sourceID, ownerID); c == nil {
		return []models.SyncJob{}, nil
	}
	return f.syncJobs[sourceID], nil
}

func (f *fakeStore) GetSyncJob(_ context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.SyncJob, error) {
	for source, jobs := range f.syncJobs {
		if c, _ := f.GetConnectionForOwner(context.Background(), source, ownerID); c == nil {
			continue
		}
		for i := range jobs {
			if jobs[i].ID == id {
				return &jobs[i], nil
			}
		}
	}
	return nil, nil
}

func (f *fakeStore) CreateSyncJob(_ context.Context, body *models.CreateSyncJobRequest, ownerID uuid.UUID) (*models.SyncJob, error) {
	allowed, _ := f.CheckSourceRole(context.Background(), body.SourceID, ownerID, models.SourceRoleSyncCreate)
	if !allowed {
		return nil, nil
	}
	j := models.SyncJob{
		ID:              uuid.New(),
		SourceID:        body.SourceID,
		CapabilityType:  valueOr(body.CapabilityType, "batch_sync"),
		OutputKind:      valueOr(body.OutputKind, "dataset"),
		OutputDatasetID: body.OutputDatasetID,
		OutputStreamID:  body.OutputStreamID,
		SourceSelector:  body.SourceSelector,
		SourceTable:     body.SourceTable,
		SourceTopic:     body.SourceTopic,
		Schema:          body.Schema,
		WriteMode:       body.WriteMode,
		TransactionMode: body.TransactionMode,
		CdcSync:         body.CdcSync,
		FileGlob:        body.FileGlob,
		ScheduleCron:    body.ScheduleCron,
		CreatedAt:       time.Now().UTC(),
	}
	f.syncJobs[body.SourceID] = append([]models.SyncJob{j}, f.syncJobs[body.SourceID]...)
	return &j, nil
}

func (f *fakeStore) UpdateSyncJob(_ context.Context, id uuid.UUID, body *models.UpdateSyncJobRequest, ownerID uuid.UUID) (*models.SyncJob, error) {
	for source, jobs := range f.syncJobs {
		if c, _ := f.GetConnectionForOwner(context.Background(), source, ownerID); c == nil {
			continue
		}
		for i := range jobs {
			if jobs[i].ID == id {
				if body.OutputDatasetID != nil {
					jobs[i].OutputDatasetID = body.OutputDatasetID
				}
				if body.OutputStreamID != nil {
					jobs[i].OutputStreamID = body.OutputStreamID
				}
				if body.CdcSync != nil {
					jobs[i].CdcSync = body.CdcSync
				}
				if body.FileGlob != nil {
					jobs[i].FileGlob = body.FileGlob
				}
				if body.ScheduleCron != nil {
					jobs[i].ScheduleCron = body.ScheduleCron
				}
				f.syncJobs[source] = jobs
				return &jobs[i], nil
			}
		}
	}
	return nil, nil
}

func (f *fakeStore) RunSyncJob(_ context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.SyncRun, error) {
	if _, err := f.GetSyncJob(context.Background(), id, ownerID); err != nil {
		return nil, err
	} else if _, _ = f.GetSyncJob(context.Background(), id, ownerID); false {
	}
	job, _ := f.GetSyncJob(context.Background(), id, ownerID)
	if job == nil {
		return nil, nil
	}
	run := models.SyncRun{ID: uuid.New(), SyncDefID: id, Status: "running", StartedAt: time.Now().UTC()}
	f.runs[id] = append(f.runs[id], run)
	return &run, nil
}

func (f *fakeStore) ListSyncRuns(_ context.Context, syncID uuid.UUID, _ uuid.UUID) ([]models.SyncRun, error) {
	return f.runs[syncID], nil
}

func (f *fakeStore) ListDataExports(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.DataExport, error) {
	if c, _ := f.GetConnectionForOwner(context.Background(), sourceID, ownerID); c == nil {
		return []models.DataExport{}, nil
	}
	return f.exports[sourceID], nil
}

func (f *fakeStore) GetDataExport(_ context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.DataExport, error) {
	for source, exports := range f.exports {
		if c, _ := f.GetConnectionForOwner(context.Background(), source, ownerID); c == nil {
			continue
		}
		for i := range exports {
			if exports[i].ID == id {
				return &exports[i], nil
			}
		}
	}
	return nil, nil
}

func (f *fakeStore) CreateDataExport(_ context.Context, body *models.CreateDataExportRequest, ownerID uuid.UUID) (*models.DataExport, error) {
	allowed, _ := f.CheckSourceRole(context.Background(), body.SourceID, ownerID, models.SourceRoleExportCreate)
	if !allowed {
		return nil, nil
	}
	models.NormalizeCreateDataExportRequest(body)
	now := time.Now().UTC()
	status := models.DataExportStatusDraft
	if body.ScheduleCron != nil {
		status = models.DataExportStatusScheduled
	}
	createdBy := ownerID
	history := []models.DataExportHistoryEntry{{
		ID:        uuid.New(),
		Action:    "created",
		Status:    string(status),
		CreatedAt: now,
	}}
	export := models.DataExport{
		ID:               uuid.New(),
		SourceID:         body.SourceID,
		Name:             body.Name,
		ExportType:       body.ExportType,
		ExportMode:       body.ExportMode,
		InputDatasetID:   body.InputDatasetID,
		InputDatasetRID:  body.InputDatasetRID,
		InputStreamID:    body.InputStreamID,
		DestinationPath:  body.DestinationPath,
		DestinationTable: body.DestinationTable,
		DestinationTopic: body.DestinationTopic,
		ScheduleCron:     body.ScheduleCron,
		StartBehavior:    body.StartBehavior,
		StopBehavior:     body.StopBehavior,
		ExportControls:   body.ExportControls,
		Config:           body.Config,
		FileExport:       body.FileExport,
		TableExport:      body.TableExport,
		StreamingExport:  body.StreamingExport,
		Status:           status,
		Health:           models.DefaultDataExportHealth(),
		History:          history,
		CreatedBy:        &createdBy,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	export.Schedule = models.DataExportScheduleFor(export.ID, export.Name, export.ExportType, export.ScheduleCron, export.LastRunAt)
	f.exports[body.SourceID] = append([]models.DataExport{export}, f.exports[body.SourceID]...)
	return &export, nil
}

func decorateFakeDataExportBuildHistory(export models.DataExport, entry *models.DataExportHistoryEntry, triggeredAt time.Time) {
	buildID := models.NewDataExportBuildID()
	reportURL := models.DataExportBuildReportURL(buildID)
	entry.BuildID = &buildID
	entry.BuildReportURL = &reportURL
	entry.RetryAttempts = models.DataExportRetryAttempts(export.Config)
	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}
	entry.Metadata["build_id"] = buildID
	entry.Metadata["build_report_url"] = reportURL
	entry.Metadata["retry_attempts"] = entry.RetryAttempts
	entry.Metadata["build_system"] = "data-integration-build-schedules"
	schedule := models.DataExportScheduleFor(export.ID, export.Name, export.ExportType, export.ScheduleCron, &triggeredAt)
	if schedule != nil {
		entry.ScheduleTriggered = true
		entry.Metadata["triggered_by"] = "schedule"
		entry.Metadata["schedule"] = schedule
		entry.Metadata["schedule_rid"] = schedule.RID
		return
	}
	entry.Metadata["triggered_by"] = "manual"
}

func (f *fakeStore) UpdateDataExport(_ context.Context, id uuid.UUID, body *models.UpdateDataExportRequest, ownerID uuid.UUID) (*models.DataExport, error) {
	for source, exports := range f.exports {
		if c, _ := f.GetConnectionForOwner(context.Background(), source, ownerID); c == nil {
			continue
		}
		for i := range exports {
			if exports[i].ID == id {
				if body.Name != nil {
					exports[i].Name = strings.TrimSpace(*body.Name)
				}
				if body.ExportMode != nil {
					exports[i].ExportMode = *body.ExportMode
				}
				if body.ScheduleCron != nil {
					exports[i].ScheduleCron = body.ScheduleCron
				}
				if body.StartBehavior != nil {
					exports[i].StartBehavior = strings.TrimSpace(*body.StartBehavior)
				}
				if body.StopBehavior != nil {
					exports[i].StopBehavior = strings.TrimSpace(*body.StopBehavior)
				}
				if body.ExportControls != nil {
					exports[i].ExportControls = *body.ExportControls
				}
				if len(body.Config) > 0 {
					exports[i].Config = body.Config
				}
				if body.FileExport != nil {
					exports[i].FileExport = body.FileExport
				}
				if body.TableExport != nil {
					exports[i].TableExport = body.TableExport
				}
				if body.StreamingExport != nil {
					exports[i].StreamingExport = body.StreamingExport
				}
				exports[i].UpdatedAt = time.Now().UTC()
				exports[i].Schedule = models.DataExportScheduleFor(exports[i].ID, exports[i].Name, exports[i].ExportType, exports[i].ScheduleCron, exports[i].LastRunAt)
				f.exports[source] = exports
				return &exports[i], nil
			}
		}
	}
	return nil, nil
}

func (f *fakeStore) RunDataExport(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.DataExport, error) {
	current, _ := f.GetDataExport(ctx, id, ownerID)
	if current != nil && current.ExportType == models.DataExportTypeFile {
		settings := models.DefaultFileExportSettings(valueOr(current.DestinationPath, ""), current.ExportMode)
		if current.FileExport != nil {
			settings = *current.FileExport
		}
		now := time.Now().UTC()
		plan := models.BuildFileExportRunPlan(settings, valueOr(current.DestinationPath, ""), now)
		settings.LastSuccessfulAt = &now
		settings.LastSuccessfulTransactionID = plan.LastExportedTransactionID
		settings.FullReexportRequested = false
		for source, exports := range f.exports {
			for i := range exports {
				if exports[i].ID == id {
					msg := fmt.Sprintf("File export completed: %d file(s) written, %d skipped, %d bytes", plan.FilesWritten, plan.FilesSkipped, plan.BytesWritten)
					exports[i].Status = models.DataExportStatusSucceeded
					exports[i].Health = models.DataExportHealth{State: models.DataExportHealthHealthy, Message: &msg, LastCheckedAt: &now}
					exports[i].FileExport = &settings
					exports[i].LastRunAt = &now
					entry := models.DataExportHistoryEntry{
						ID:                         uuid.New(),
						Action:                     "run",
						Status:                     string(models.DataExportStatusSucceeded),
						Message:                    &msg,
						FilesWritten:               plan.FilesWritten,
						FilesSkipped:               plan.FilesSkipped,
						BytesWritten:               plan.BytesWritten,
						HighWatermarkTransactionID: plan.LastExportedTransactionID,
						FullReexport:               plan.FullReexport,
						CreatedAt:                  now,
					}
					decorateFakeDataExportBuildHistory(exports[i], &entry, now)
					exports[i].Schedule = models.DataExportScheduleFor(exports[i].ID, exports[i].Name, exports[i].ExportType, exports[i].ScheduleCron, exports[i].LastRunAt)
					exports[i].History = append([]models.DataExportHistoryEntry{entry}, exports[i].History...)
					f.exports[source] = exports
					return &exports[i], nil
				}
			}
		}
	}
	if current != nil && current.ExportType == models.DataExportTypeTable {
		settings := models.DefaultTableExportSettings(current.ExportMode)
		if current.TableExport != nil {
			settings = *current.TableExport
		}
		now := time.Now().UTC()
		plan := models.BuildTableExportRunPlan(settings, current.ExportMode, now)
		settings.LastSuccessfulAt = &now
		for source, exports := range f.exports {
			for i := range exports {
				if exports[i].ID == id {
					msg := fmt.Sprintf("Table export completed: %d row(s) written to %s using %s", plan.RowsWritten, valueOr(current.DestinationTable, ""), plan.ResolutionStrategy)
					exports[i].Status = models.DataExportStatusSucceeded
					exports[i].Health = models.DataExportHealth{State: models.DataExportHealthHealthy, Message: &msg, LastCheckedAt: &now}
					exports[i].TableExport = &settings
					exports[i].LastRunAt = &now
					entry := models.DataExportHistoryEntry{
						ID:                uuid.New(),
						Action:            "run",
						Status:            string(models.DataExportStatusSucceeded),
						Message:           &msg,
						RowsWritten:       plan.RowsWritten,
						TruncatePerformed: plan.TruncatePerformed,
						Metadata: map[string]any{
							"export_type":         "table",
							"resolution_strategy": plan.ResolutionStrategy,
						},
						CreatedAt: now,
					}
					decorateFakeDataExportBuildHistory(exports[i], &entry, now)
					exports[i].Schedule = models.DataExportScheduleFor(exports[i].ID, exports[i].Name, exports[i].ExportType, exports[i].ScheduleCron, exports[i].LastRunAt)
					exports[i].History = append([]models.DataExportHistoryEntry{entry}, exports[i].History...)
					f.exports[source] = exports
					return &exports[i], nil
				}
			}
		}
	}
	return f.transitionExport(ctx, id, ownerID, models.DataExportStatusSucceeded, models.DataExportHealthHealthy, "run")
}

func (f *fakeStore) StartDataExport(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.DataExport, error) {
	current, _ := f.GetDataExport(ctx, id, ownerID)
	if current != nil && current.ExportType == models.DataExportTypeStreaming {
		return f.setStreamingExportState(ctx, id, ownerID, true)
	}
	return f.transitionExport(ctx, id, ownerID, models.DataExportStatusRunning, models.DataExportHealthRunning, "started")
}

func (f *fakeStore) StopDataExport(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.DataExport, error) {
	current, _ := f.GetDataExport(ctx, id, ownerID)
	if current != nil && current.ExportType == models.DataExportTypeStreaming {
		return f.setStreamingExportState(ctx, id, ownerID, false)
	}
	return f.transitionExport(ctx, id, ownerID, models.DataExportStatusStopped, models.DataExportHealthHealthy, "stopped")
}

func (f *fakeStore) setStreamingExportState(_ context.Context, id uuid.UUID, ownerID uuid.UUID, running bool) (*models.DataExport, error) {
	now := time.Now().UTC()
	for source, exports := range f.exports {
		if c, _ := f.GetConnectionForOwner(context.Background(), source, ownerID); c == nil {
			continue
		}
		for i := range exports {
			if exports[i].ID == id {
				settings := models.DefaultStreamingExportSettings(exports[i].ScheduleCron != nil)
				if exports[i].StreamingExport != nil {
					settings = *exports[i].StreamingExport
				}
				models.NormalizeStreamingExportSettings(&settings, exports[i].ScheduleCron != nil)
				status := models.DataExportStatusStopped
				health := models.DataExportHealthHealthy
				action := "stopped"
				msg := "Streaming export stopped"
				entry := models.DataExportHistoryEntry{ID: uuid.New(), Action: action, Status: string(status), Message: &msg, CreatedAt: now}
				if running {
					plan := models.BuildStreamingExportStartPlan(settings, false, now)
					status = models.DataExportStatusRunning
					health = models.DataExportHealthRunning
					action = "started"
					msg = "Streaming export started"
					if plan.EffectiveStartOffset != nil {
						msg = fmt.Sprintf("Streaming export started from offset %s", *plan.EffectiveStartOffset)
					}
					settings.LastStartedAt = &now
					entry = models.DataExportHistoryEntry{
						ID:                 uuid.New(),
						Action:             action,
						Status:             string(status),
						Message:            &msg,
						LastExportedOffset: plan.EffectiveStartOffset,
						ReplayBehavior:     plan.ReplayBehavior,
						Metadata: map[string]any{
							"export_type":                  "streaming",
							"restart_from_previous_offset": plan.RestartFromPreviousOffset,
							"warnings":                     plan.Warnings,
						},
						StartedAt: &now,
						CreatedAt: now,
					}
				} else {
					records := int64(0)
					if settings.RecordsExportedEstimate != nil && *settings.RecordsExportedEstimate > 0 {
						records = *settings.RecordsExportedEstimate
					}
					offset := models.AdvanceStreamingExportOffset(settings)
					settings.LastExportedOffset = offset
					settings.LastStoppedAt = &now
					entry.RecordsExported = records
					entry.LastExportedOffset = offset
					entry.ReplayBehavior = settings.ReplayBehavior
					entry.FinishedAt = &now
				}
				models.NormalizeStreamingExportSettings(&settings, exports[i].ScheduleCron != nil)
				exports[i].Status = status
				exports[i].Health = models.DataExportHealth{State: health, Message: &msg, LastCheckedAt: &now}
				exports[i].StreamingExport = &settings
				if running {
					exports[i].LastRunAt = &now
				}
				exports[i].History = append([]models.DataExportHistoryEntry{entry}, exports[i].History...)
				exports[i].Schedule = models.DataExportScheduleFor(exports[i].ID, exports[i].Name, exports[i].ExportType, exports[i].ScheduleCron, exports[i].LastRunAt)
				exports[i].UpdatedAt = now
				f.exports[source] = exports
				return &exports[i], nil
			}
		}
	}
	return nil, nil
}

func (f *fakeStore) transitionExport(_ context.Context, id uuid.UUID, ownerID uuid.UUID, status models.DataExportStatus, health models.DataExportHealthState, action string) (*models.DataExport, error) {
	now := time.Now().UTC()
	for source, exports := range f.exports {
		if c, _ := f.GetConnectionForOwner(context.Background(), source, ownerID); c == nil {
			continue
		}
		for i := range exports {
			if exports[i].ID == id {
				exports[i].Status = status
				exports[i].Health = models.DataExportHealth{State: health, LastCheckedAt: &now}
				if status == models.DataExportStatusRunning || status == models.DataExportStatusSucceeded {
					exports[i].LastRunAt = &now
				}
				exports[i].History = append([]models.DataExportHistoryEntry{{ID: uuid.New(), Action: action, Status: string(status), CreatedAt: now}}, exports[i].History...)
				exports[i].Schedule = models.DataExportScheduleFor(exports[i].ID, exports[i].Name, exports[i].ExportType, exports[i].ScheduleCron, exports[i].LastRunAt)
				exports[i].UpdatedAt = now
				f.exports[source] = exports
				return &exports[i], nil
			}
		}
	}
	return nil, nil
}

func (f *fakeStore) CompleteSyncRun(_ context.Context, runID uuid.UUID, _ uuid.UUID, status string, bytesWritten int64, filesWritten int64, errMsg *string, ingestJobID *string, datasetVersionID *uuid.UUID, contentHash *string) (*models.SyncRun, error) {
	for syncID, runs := range f.runs {
		for i := range runs {
			if runs[i].ID == runID {
				now := time.Now().UTC()
				runs[i].Status = status
				runs[i].FinishedAt = &now
				runs[i].BytesWritten = bytesWritten
				runs[i].FilesWritten = filesWritten
				runs[i].Error = errMsg
				runs[i].IngestJobID = ingestJobID
				runs[i].DatasetVersionID = datasetVersionID
				runs[i].ContentHash = contentHash
				f.runs[syncID] = runs
				return &runs[i], nil
			}
		}
	}
	return nil, nil
}

func (f *fakeStore) PreviousDatasetVersionForHash(_ context.Context, syncDefID uuid.UUID, contentHash string) (*uuid.UUID, error) {
	for _, run := range f.runs[syncDefID] {
		if run.ContentHash != nil && *run.ContentHash == contentHash && run.DatasetVersionID != nil {
			return run.DatasetVersionID, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) RecordDatasetVersionOnRun(_ context.Context, runID uuid.UUID, datasetVersionID uuid.UUID, contentHash string) error {
	for syncID, runs := range f.runs {
		for i := range runs {
			if runs[i].ID == runID {
				runs[i].DatasetVersionID = &datasetVersionID
				runs[i].ContentHash = &contentHash
				f.runs[syncID] = runs
				return nil
			}
		}
	}
	return nil
}

func (f *fakeStore) ListCredentials(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.CredentialResponse, error) {
	allowed, _ := f.CheckSourceRole(context.Background(), sourceID, ownerID, models.SourceRoleCodeImport)
	if !allowed {
		return []models.CredentialResponse{}, nil
	}
	return append([]models.CredentialResponse(nil), f.credentials[sourceID]...), nil
}

func (f *fakeStore) SetCredential(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID, kind string, _ []byte, fingerprint string) (*models.CredentialResponse, error) {
	allowed, _ := f.CheckSourceRole(context.Background(), sourceID, ownerID, models.SourceRoleEdit)
	if !allowed {
		return nil, nil
	}
	credential := models.CredentialResponse{ID: uuid.New(), SourceID: sourceID, Kind: kind, Fingerprint: fingerprint, CreatedAt: time.Now().UTC()}
	f.credentials[sourceID] = append([]models.CredentialResponse{credential}, f.credentials[sourceID]...)
	return &credential, nil
}

func (f *fakeStore) ListConnectorAgents(_ context.Context, ownerID uuid.UUID) ([]models.ConnectorAgent, error) {
	out := []models.ConnectorAgent{}
	for _, agent := range f.agents {
		if agent.OwnerID == ownerID {
			out = append(out, agent)
		}
	}
	return out, nil
}

func (f *fakeStore) RegisterConnectorAgent(_ context.Context, body *models.RegisterAgentRequest, ownerID uuid.UUID) (*models.ConnectorAgent, error) {
	now := time.Now().UTC()
	for i := range f.agents {
		if f.agents[i].AgentURL == body.AgentURL {
			f.agents[i].Name = body.Name
			f.agents[i].OwnerID = ownerID
			f.agents[i].Status = "online"
			f.agents[i].Capabilities = body.Capabilities
			f.agents[i].Metadata = body.Metadata
			f.agents[i].Version = body.Version
			f.agents[i].Environment = body.Environment
			f.agents[i].Host = body.Host
			f.agents[i].ConnectedSources = body.ConnectedSources
			f.agents[i].SupportedConnectorCapabilities = body.SupportedConnectorCapabilities
			f.agents[i].AssignedProxyPolicies = body.AssignedProxyPolicies
			f.agents[i].ConnectionFailures = body.ConnectionFailures
			f.agents[i].LastHeartbeatAt = &now
			f.agents[i].UpdatedAt = now
			f.agents[i] = models.NormalizeConnectorAgent(f.agents[i])
			return &f.agents[i], nil
		}
	}
	agent := models.NormalizeConnectorAgent(models.ConnectorAgent{ID: uuid.New(), Name: body.Name, AgentURL: body.AgentURL, Version: body.Version, Environment: body.Environment, Host: body.Host, OwnerID: ownerID, Status: "online", Capabilities: body.Capabilities, Metadata: body.Metadata, ConnectedSources: body.ConnectedSources, SupportedConnectorCapabilities: body.SupportedConnectorCapabilities, AssignedProxyPolicies: body.AssignedProxyPolicies, ConnectionFailures: body.ConnectionFailures, LastHeartbeatAt: &now, CreatedAt: now, UpdatedAt: now})
	f.agents = append([]models.ConnectorAgent{agent}, f.agents...)
	return &f.agents[0], nil
}

func (f *fakeStore) HeartbeatConnectorAgent(_ context.Context, id uuid.UUID, body *models.AgentHeartbeatRequest, ownerID uuid.UUID) (*models.ConnectorAgent, error) {
	now := time.Now().UTC()
	for i := range f.agents {
		if f.agents[i].ID == id && f.agents[i].OwnerID == ownerID {
			f.agents[i].Status = "online"
			f.agents[i].Capabilities = body.Capabilities
			f.agents[i].Metadata = body.Metadata
			if body.Version != "" {
				f.agents[i].Version = body.Version
			}
			if body.Environment != "" {
				f.agents[i].Environment = body.Environment
			}
			if body.Host != "" {
				f.agents[i].Host = body.Host
			}
			if body.ConnectedSources != nil {
				f.agents[i].ConnectedSources = body.ConnectedSources
			}
			if body.SupportedConnectorCapabilities != nil {
				f.agents[i].SupportedConnectorCapabilities = body.SupportedConnectorCapabilities
			}
			if body.AssignedProxyPolicies != nil {
				f.agents[i].AssignedProxyPolicies = body.AssignedProxyPolicies
			}
			if body.ConnectionFailures != nil {
				f.agents[i].ConnectionFailures = body.ConnectionFailures
			}
			f.agents[i].LastHeartbeatAt = &now
			f.agents[i].UpdatedAt = now
			f.agents[i] = models.NormalizeConnectorAgent(f.agents[i])
			return &f.agents[i], nil
		}
	}
	return nil, nil
}

func (f *fakeStore) DeleteConnectorAgent(_ context.Context, id uuid.UUID, ownerID uuid.UUID) (bool, error) {
	for i := range f.agents {
		if f.agents[i].ID == id && f.agents[i].OwnerID == ownerID {
			f.agents = append(f.agents[:i], f.agents[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeStore) GetConnectorAgent(_ context.Context, id uuid.UUID) (*models.ConnectorAgent, error) {
	for i := range f.agents {
		if f.agents[i].ID == id {
			copy := f.agents[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) MigrateConnectionToFoundryWorker(_ context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.Connection, error) {
	for i := range f.connections {
		if f.connections[i].ID == id && f.connections[i].OwnerID == ownerID {
			copy := f.connections[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListSourcePolicies(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.SourcePolicyBindingResponse, error) {
	if c, _ := f.GetConnectionForOwner(context.Background(), sourceID, ownerID); c == nil {
		return []models.SourcePolicyBindingResponse{}, nil
	}
	return append([]models.SourcePolicyBindingResponse(nil), f.policies[sourceID]...), nil
}

func (f *fakeStore) AttachPolicy(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID, policyID uuid.UUID, kind string) (*models.SourcePolicyBindingResponse, error) {
	allowed, _ := f.CheckSourceRole(context.Background(), sourceID, ownerID, models.SourceRoleEdit)
	if !allowed {
		return nil, nil
	}
	binding := models.SourcePolicyBindingResponse{SourceID: sourceID, PolicyID: policyID, Kind: kind}
	items := f.policies[sourceID]
	for i := range items {
		if items[i].PolicyID == policyID {
			items[i] = binding
			f.policies[sourceID] = items
			return &items[i], nil
		}
	}
	f.policies[sourceID] = append(items, binding)
	return &binding, nil
}

func (f *fakeStore) DetachPolicy(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID, policyID uuid.UUID) (bool, error) {
	allowed, _ := f.CheckSourceRole(context.Background(), sourceID, ownerID, models.SourceRoleEdit)
	if !allowed {
		return false, nil
	}
	items := f.policies[sourceID]
	for i := range items {
		if items[i].PolicyID == policyID {
			f.policies[sourceID] = append(items[:i], items[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeStore) GetSourceCodeImport(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID) (*models.SourceCodeImport, error) {
	conn, _ := f.GetConnectionForOwner(context.Background(), sourceID, ownerID)
	if conn == nil {
		return nil, nil
	}
	importSettings := f.sourceCodeImportFor(conn)
	return &importSettings, nil
}

func (f *fakeStore) UpdateSourceCodeImport(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID, body *models.UpdateSourceCodeImportRequest) (*models.SourceCodeImport, error) {
	allowed, _ := f.CheckSourceRole(context.Background(), sourceID, ownerID, models.SourceRoleCodeImport)
	if !allowed {
		return nil, nil
	}
	conn, _ := f.GetConnection(context.Background(), sourceID)
	if conn == nil {
		return nil, nil
	}
	current := f.sourceCodeImportFor(conn)
	if body.Enabled != nil {
		current.Enabled = *body.Enabled
	}
	if body.FriendlyName != nil {
		current.FriendlyName = strings.TrimSpace(*body.FriendlyName)
	}
	if current.FriendlyName == "" {
		current.FriendlyName = conn.Name
	}
	if body.PythonIdentifier != nil {
		current.PythonIdentifier = models.PythonIdentifier(*body.PythonIdentifier, current.FriendlyName)
	}
	if current.PythonIdentifier == "" {
		current.PythonIdentifier = models.PythonIdentifier(current.FriendlyName, conn.Name)
	}
	if body.CodeRepositories != nil {
		current.CodeRepositories = models.NormalizeCodeRepositories(body.CodeRepositories, current.PythonIdentifier)
	}
	if body.ExportControls != nil {
		current.ExportControls = models.NormalizeExportControls(*body.ExportControls)
	}
	current.UpdatedAt = time.Now().UTC()
	current.GeneratedBinding = models.SourceBindingSnippet(current.SourceRID, current.FriendlyName, current.PythonIdentifier)
	current.ExternalTransformPatterns = models.ExternalTransformPatternsForSource(current.SourceRID, current.FriendlyName, current.PythonIdentifier, current.ExportControls)
	current.ComputeModuleAlternatives = models.ComputeModuleAlternativesForSource(current.SourceRID, current.FriendlyName, current.PythonIdentifier)
	current.BuildStartResolution = f.sourceCodeImportResolution(conn, current, nil)
	current.Warnings = current.BuildStartResolution.Warnings
	f.codeImports[sourceID] = current
	return &current, nil
}

func (f *fakeStore) ResolveSourceCodeImportBuildStart(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID, body *models.ResolveSourceCodeImportBuildRequest) (*models.SourceCodeImportBuildResolution, error) {
	allowed, _ := f.CheckSourceRole(context.Background(), sourceID, ownerID, models.SourceRoleCodeImport)
	if !allowed {
		return nil, nil
	}
	conn, _ := f.GetConnection(context.Background(), sourceID)
	if conn == nil {
		return nil, nil
	}
	current := f.sourceCodeImportFor(conn)
	if !current.Enabled {
		return nil, fmt.Errorf("source is not approved for code imports")
	}
	resolution := f.sourceCodeImportResolution(conn, current, body)
	return &resolution, nil
}

func (f *fakeStore) sourceCodeImportFor(conn *models.Connection) models.SourceCodeImport {
	if current, ok := f.codeImports[conn.ID]; ok {
		current.ExternalTransformPatterns = models.ExternalTransformPatternsForSource(current.SourceRID, current.FriendlyName, current.PythonIdentifier, current.ExportControls)
		current.ComputeModuleAlternatives = models.ComputeModuleAlternativesForSource(current.SourceRID, current.FriendlyName, current.PythonIdentifier)
		current.BuildStartResolution = f.sourceCodeImportResolution(conn, current, nil)
		current.Warnings = current.BuildStartResolution.Warnings
		return current
	}
	sourceRID := models.SourceRIDForConnection(conn.ID)
	friendlyName := conn.Name
	pythonIdentifier := models.PythonIdentifier(friendlyName, conn.ConnectorType)
	binding := models.SourceBindingSnippet(sourceRID, friendlyName, pythonIdentifier)
	current := models.SourceCodeImport{
		SourceID:                  conn.ID,
		SourceRID:                 sourceRID,
		SourceName:                conn.Name,
		ConnectorType:             conn.ConnectorType,
		FriendlyName:              friendlyName,
		PythonIdentifier:          pythonIdentifier,
		GeneratedBinding:          binding,
		CodeRepositories:          []models.CodeRepositorySourceImport{},
		ExportControls:            models.NormalizeExportControls(models.ExportControls{}),
		ExternalTransformPatterns: models.ExternalTransformPatternsForSource(sourceRID, friendlyName, pythonIdentifier, models.ExportControls{}),
		ComputeModuleAlternatives: models.ComputeModuleAlternativesForSource(sourceRID, friendlyName, pythonIdentifier),
		CreatedAt:                 conn.CreatedAt,
		UpdatedAt:                 conn.UpdatedAt,
	}
	current.BuildStartResolution = f.sourceCodeImportResolution(conn, current, nil)
	current.Warnings = current.BuildStartResolution.Warnings
	return current
}

func (f *fakeStore) sourceCodeImportResolution(conn *models.Connection, current models.SourceCodeImport, body *models.ResolveSourceCodeImportBuildRequest) models.SourceCodeImportBuildResolution {
	credentials := []models.SourceCredentialBinding{}
	for _, credential := range f.credentials[conn.ID] {
		credentials = append(credentials, models.SourceCredentialBinding{CredentialID: credential.ID, Kind: credential.Kind, Fingerprint: credential.Fingerprint, CreatedAt: credential.CreatedAt})
	}
	egress := []models.SourceEgressPolicyBinding{}
	for _, policy := range f.policies[conn.ID] {
		egress = append(egress, models.SourceEgressPolicyBinding{PolicyID: policy.PolicyID, Kind: policy.Kind})
	}
	hash := sha256.Sum256(conn.Config)
	usesFoundryInputs := false
	foundryInputs := []models.SourceCodeImportFoundryInput{}
	if body != nil {
		foundryInputs = body.FoundryInputs
		if body.UsesFoundryInputs != nil {
			usesFoundryInputs = *body.UsesFoundryInputs
		}
	}
	exportPolicyDecision := models.ResolveSourceCodeImportExportPolicy(current.ExportControls, usesFoundryInputs, foundryInputs)
	resolution := models.SourceCodeImportBuildResolution{
		SourceID:              conn.ID,
		SourceRID:             current.SourceRID,
		SourceName:            conn.Name,
		ConnectorType:         conn.ConnectorType,
		PythonIdentifier:      current.PythonIdentifier,
		FriendlyName:          current.FriendlyName,
		ResolvedAt:            time.Now().UTC(),
		SourceUpdatedAt:       conn.UpdatedAt,
		ConfigHash:            fmt.Sprintf("sha256:%x", hash[:]),
		CredentialBindings:    credentials,
		EgressPolicyBindings:  egress,
		ExportControls:        current.ExportControls,
		ExportPolicyDecision:  exportPolicyDecision,
		UsesLiveConfiguration: true,
		NoCodeChangeRequired:  true,
		GeneratedBinding:      models.SourceBindingSnippet(current.SourceRID, current.FriendlyName, current.PythonIdentifier),
		Warnings:              models.SourceCodeImportWarnings(current.Enabled, credentials, egress, current.ExportControls, exportPolicyDecision),
	}
	if body != nil {
		resolution.RepositoryRID = body.RepositoryRID
		resolution.BuildRID = body.BuildRID
		resolution.Branch = body.Branch
	}
	return resolution
}

func (f *fakeStore) EnableVirtualTableSource(_ context.Context, sourceRID string, body *models.EnableVirtualTableSourceRequest) (*models.VirtualTableSourceLink, error) {
	if body.Provider == "" {
		return nil, assert.AnError
	}
	l := models.VirtualTableSourceLink{SourceRID: sourceRID, Provider: body.Provider, VirtualTablesEnabled: true, ExportControls: []byte(`{}`), AutoRegisterTagFilters: []byte(`[]`), AutoRegisterFolderMirrorKind: "NESTED", AutoRegisterTableTagFilters: []string{}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	f.links[sourceRID] = l
	return &l, nil
}

func (f *fakeStore) DiscoverVirtualTableCatalog(_ context.Context, sourceRID string, path string) ([]models.DiscoveredEntry, error) {
	if _, ok := f.links[sourceRID]; !ok {
		return nil, nil
	}
	tableType := "TABLE"
	if path == "" {
		return []models.DiscoveredEntry{{DisplayName: "analytics", Path: "analytics", Kind: "database", Registrable: false}}, nil
	}
	if path == "analytics" {
		return []models.DiscoveredEntry{{DisplayName: "public", Path: "analytics/public", Kind: "schema", Registrable: false}}, nil
	}
	return []models.DiscoveredEntry{{DisplayName: "orders", Path: "analytics/public/orders", Kind: "table", Registrable: true, InferredTableType: &tableType}}, nil
}

func (f *fakeStore) CreateVirtualTable(_ context.Context, sourceRID string, actorID string, body *models.CreateVirtualTableRequest) (*models.VirtualTable, error) {
	if _, ok := f.links[sourceRID]; !ok {
		return nil, nil
	}
	loc, err := body.Locator.CanonicalJSON()
	if err != nil {
		return nil, err
	}
	name := body.Locator.DefaultDisplayName()
	if body.Name != nil {
		name = *body.Name
	}
	rid := "ri.foundry.main.virtual-table." + uuid.NewString()
	creator := actorID
	schema := body.SchemaInferred
	if len(schema) == 0 {
		schema = []byte(`[]`)
	}
	capabilities := body.Capabilities
	if len(capabilities) == 0 {
		capabilities = []byte(`{}`)
	}
	properties := body.Properties
	if len(properties) == 0 {
		properties = []byte(`{}`)
	}
	var props map[string]any
	if err := json.Unmarshal(properties, &props); err != nil {
		props = map[string]any{}
	}
	if props == nil {
		props = map[string]any{}
	}
	link := f.links[sourceRID]
	props["provider"] = link.Provider
	props["display_name"] = name
	props["external_reference"] = json.RawMessage(loc)
	props["save_location"] = map[string]any{"project_rid": body.ProjectRID, "parent_folder_rid": body.ParentFolderRID}
	props["source"] = map[string]any{"source_rid": sourceRID, "provider": link.Provider}
	props["schema"] = json.RawMessage(schema)
	owner := actorID
	if body.Owner != nil && strings.TrimSpace(*body.Owner) != "" {
		owner = strings.TrimSpace(*body.Owner)
	}
	props["owner"] = owner
	if len(body.Permissions) > 0 {
		props["permissions"] = json.RawMessage(body.Permissions)
	} else {
		props["permissions"] = map[string]any{"owners": []string{owner}, "readers": []string{}, "writers": []string{}, "admins": []string{}}
	}
	properties, _ = json.Marshal(props)
	v := models.VirtualTable{ID: uuid.New(), RID: rid, SourceRID: sourceRID, ProjectRID: body.ProjectRID, Name: name, Locator: loc, TableType: body.TableType, SchemaInferred: schema, Capabilities: capabilities, Markings: body.Markings, Properties: properties, CreatedBy: &creator, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	f.vtables[rid] = v
	return &v, nil
}

func (f *fakeStore) BulkRegisterVirtualTables(ctx context.Context, sourceRID string, actorID string, body *models.VirtualTableBulkRegisterRequest) (*models.VirtualTableBulkRegisterResponse, error) {
	if _, ok := f.links[sourceRID]; !ok {
		return nil, nil
	}
	out := &models.VirtualTableBulkRegisterResponse{Registered: []models.VirtualTable{}, Errors: []models.VirtualTableBulkError{}}
	for i := range body.Entries {
		entry := body.Entries[i]
		if entry.ProjectRID == "" {
			entry.ProjectRID = body.ProjectRID
		}
		if entry.Locator.Kind != "tabular" {
			out.Errors = append(out.Errors, models.VirtualTableBulkError{Name: entry.Locator.DefaultDisplayName(), Error: "bulk registration requires tabular locators"})
			continue
		}
		created, err := f.CreateVirtualTable(ctx, sourceRID, actorID, &entry)
		if err != nil {
			out.Errors = append(out.Errors, models.VirtualTableBulkError{Name: entry.Locator.DefaultDisplayName(), Error: err.Error()})
			continue
		}
		out.Registered = append(out.Registered, *created)
	}
	return out, nil
}

func (f *fakeStore) EnableVirtualTableAutoRegistration(_ context.Context, sourceRID string, body *models.EnableAutoRegistrationRequest) (*models.VirtualTableSourceLink, error) {
	link, ok := f.links[sourceRID]
	if !ok {
		return nil, nil
	}
	if strings.TrimSpace(body.ProjectName) == "" {
		return nil, assert.AnError
	}
	projectRID := "ri.foundry.main.project." + strings.ToLower(strings.ReplaceAll(body.ProjectName, " ", "-"))
	interval := int32(body.PollIntervalSeconds)
	layout := body.FolderMirrorKind
	if layout == "" {
		layout = "NESTED"
	}
	link.AutoRegisterEnabled = true
	link.AutoRegisterProjectRID = &projectRID
	link.AutoRegisterIntervalSeconds = &interval
	link.AutoRegisterFolderMirrorKind = layout
	link.AutoRegisterTableTagFilters = body.TableTagFilters
	f.links[sourceRID] = link
	return &link, nil
}

func (f *fakeStore) DisableVirtualTableAutoRegistration(_ context.Context, sourceRID string) error {
	link, ok := f.links[sourceRID]
	if ok {
		link.AutoRegisterEnabled = false
		f.links[sourceRID] = link
	}
	return nil
}

func (f *fakeStore) ScanVirtualTableAutoRegistrationNow(_ context.Context, sourceRID string) (*models.AutoRegistrationScanSummary, error) {
	link, ok := f.links[sourceRID]
	if !ok || !link.AutoRegisterEnabled {
		return nil, nil
	}
	return &models.AutoRegistrationScanSummary{}, nil
}

func (f *fakeStore) ListVirtualTables(_ context.Context, ownerID string, project, source, name, tableType string, _ int) ([]models.VirtualTable, error) {
	out := []models.VirtualTable{}
	for _, v := range f.vtables {
		if v.CreatedBy != nil && *v.CreatedBy == ownerID && (project == "" || v.ProjectRID == project) && (source == "" || v.SourceRID == source) && (name == "" || strings.Contains(strings.ToLower(v.Name), strings.ToLower(name))) && (tableType == "" || v.TableType == tableType) {
			out = append(out, v)
		}
	}
	return out, nil
}

func (f *fakeStore) GetVirtualTable(_ context.Context, rid string, ownerID string) (*models.VirtualTable, error) {
	v, ok := f.vtables[rid]
	if !ok || v.CreatedBy == nil || *v.CreatedBy != ownerID {
		return nil, nil
	}
	return &v, nil
}

func (f *fakeStore) SetVirtualTableUpdateDetection(_ context.Context, rid string, ownerID string, body *models.UpdateDetectionToggle) (*models.VirtualTable, error) {
	v, ok := f.vtables[rid]
	if !ok || v.CreatedBy == nil || *v.CreatedBy != ownerID {
		return nil, nil
	}
	interval := int32(body.IntervalSeconds)
	v.UpdateDetectionEnabled = body.Enabled
	if body.Enabled {
		v.UpdateDetectionIntervalSeconds = &interval
		next := time.Now().UTC()
		v.UpdateDetectionNextPollAt = &next
	} else {
		v.UpdateDetectionIntervalSeconds = nil
		v.UpdateDetectionNextPollAt = nil
	}
	v.UpdatedAt = time.Now().UTC()
	f.vtables[rid] = v
	return &v, nil
}

func (f *fakeStore) PollVirtualTableUpdateDetection(_ context.Context, rid string, ownerID string) (*models.PollResult, error) {
	v, ok := f.vtables[rid]
	if !ok || v.CreatedBy == nil || *v.CreatedBy != ownerID {
		return nil, nil
	}
	previous := v.LastObservedVersion
	observed := fakeObservedVersion(v)
	outcome := models.PollOutcomePotentialUpdate
	change := true
	if observed != nil {
		switch {
		case previous == nil:
			outcome = models.PollOutcomeInitial
		case *previous == *observed:
			outcome = models.PollOutcomeUnchanged
			change = false
		default:
			outcome = models.PollOutcomeChanged
		}
	}
	now := time.Now().UTC()
	v.LastObservedVersion = observed
	v.LastPolledAt = &now
	f.vtables[rid] = v
	history := models.PollHistoryRow{ID: uuid.New(), VirtualTableID: v.ID, PolledAt: now, ObservedVersion: observed, ChangeDetected: change, LatencyMS: 1}
	f.polls[rid] = append([]models.PollHistoryRow{history}, f.polls[rid]...)
	builds := fakeDownstreamBuilds(v, outcome)
	return &models.PollResult{VirtualTableRID: rid, Outcome: outcome, ObservedVersion: observed, PreviousVersion: previous, LatencyMS: 1, ChangeDetected: change, EventEmitted: outcome != models.PollOutcomeUnchanged, DownstreamBuilds: builds}, nil
}

func (f *fakeStore) ListVirtualTableUpdateDetectionHistory(_ context.Context, rid string, ownerID string, limit int) ([]models.PollHistoryRow, error) {
	v, ok := f.vtables[rid]
	if !ok || v.CreatedBy == nil || *v.CreatedBy != ownerID {
		return nil, nil
	}
	items := append([]models.PollHistoryRow(nil), f.polls[rid]...)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (f *fakeStore) GetVirtualTableLineage(_ context.Context, rid string, ownerID string) (*models.VirtualTableLineageResponse, error) {
	v, ok := f.vtables[rid]
	if !ok || v.CreatedBy == nil || *v.CreatedBy != ownerID {
		return nil, nil
	}
	pipelineRID := "ri.foundry.main.pipeline." + v.ID.String()
	datasetRID := "ri.foundry.main.dataset." + v.ID.String()
	objectRID := "ri.ontology.main.object-type." + strings.ToLower(strings.ReplaceAll(v.Name, " ", "-"))
	nodes := []models.VirtualTableLineageNode{
		{RID: v.SourceRID, Kind: "source", DisplayName: "Source " + v.SourceRID, Status: "active"},
		{RID: v.RID, Kind: "virtual_table", DisplayName: v.Name, Status: "active"},
		{RID: pipelineRID, Kind: "pipeline", DisplayName: v.Name + " pipeline", Status: "listening"},
		{RID: datasetRID, Kind: "dataset", DisplayName: v.Name + " dataset output", Status: "materialized"},
		{RID: objectRID, Kind: "object_type", DisplayName: v.Name + " object output", Status: "indexed"},
	}
	outcome := models.PollOutcomeInitial
	if len(f.polls[rid]) > 0 {
		if f.polls[rid][0].ChangeDetected {
			outcome = models.PollOutcomeChanged
		} else {
			outcome = models.PollOutcomeUnchanged
		}
	}
	return &models.VirtualTableLineageResponse{
		VirtualTableRID:        v.RID,
		SourceRID:              v.SourceRID,
		UpdateDetectionEnabled: v.UpdateDetectionEnabled,
		LastObservedVersion:    v.LastObservedVersion,
		Nodes:                  nodes,
		Edges: []models.VirtualTableLineageEdge{
			{FromRID: v.SourceRID, ToRID: v.RID, Kind: "backs"},
			{FromRID: v.RID, ToRID: pipelineRID, Kind: "pipeline_input"},
			{FromRID: pipelineRID, ToRID: datasetRID, Kind: "writes_dataset"},
			{FromRID: datasetRID, ToRID: objectRID, Kind: "indexes_object"},
		},
		DownstreamBuilds: fakeDownstreamBuilds(v, outcome),
	}, nil
}

func (f *fakeStore) ListRegistrations(_ context.Context, sourceID uuid.UUID) ([]models.ConnectionRegistration, error) {
	return f.registrations[sourceID], nil
}

func (f *fakeStore) UpsertRegistration(_ context.Context, sourceID uuid.UUID, source models.DiscoveredSource, mode string, autoSync bool, updateDetection bool, targetDatasetID *uuid.UUID, metadata json.RawMessage) (*models.ConnectionRegistration, error) {
	if len(metadata) == 0 || string(metadata) == "null" {
		metadata = []byte(`{}`)
	}
	for i := range f.registrations[sourceID] {
		if f.registrations[sourceID][i].Selector == source.Selector {
			f.registrations[sourceID][i].DisplayName = source.DisplayName
			f.registrations[sourceID][i].SourceKind = source.SourceKind
			f.registrations[sourceID][i].RegistrationMode = mode
			f.registrations[sourceID][i].AutoSync = autoSync
			f.registrations[sourceID][i].UpdateDetection = updateDetection
			f.registrations[sourceID][i].TargetDatasetID = targetDatasetID
			f.registrations[sourceID][i].Metadata = metadata
			f.registrations[sourceID][i].UpdatedAt = time.Now().UTC()
			return &f.registrations[sourceID][i], nil
		}
	}
	reg := models.ConnectionRegistration{ID: uuid.New(), ConnectionID: sourceID, Selector: source.Selector, DisplayName: source.DisplayName, SourceKind: source.SourceKind, RegistrationMode: mode, AutoSync: autoSync, UpdateDetection: updateDetection, TargetDatasetID: targetDatasetID, LastSourceSignature: source.SourceSignature, Metadata: metadata, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	f.registrations[sourceID] = append([]models.ConnectionRegistration{reg}, f.registrations[sourceID]...)
	return &reg, nil
}

func (f *fakeStore) GetRegistration(_ context.Context, sourceID uuid.UUID, registrationID uuid.UUID) (*models.ConnectionRegistration, error) {
	for i := range f.registrations[sourceID] {
		if f.registrations[sourceID][i].ID == registrationID {
			return &f.registrations[sourceID][i], nil
		}
	}
	return nil, nil
}

func (f *fakeStore) DeleteRegistration(_ context.Context, sourceID uuid.UUID, registrationID uuid.UUID) (bool, error) {
	regs := f.registrations[sourceID]
	for i := range regs {
		if regs[i].ID == registrationID {
			f.registrations[sourceID] = append(regs[:i], regs[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeStore) UpdateConnectionConfig(_ context.Context, id uuid.UUID, config json.RawMessage) (*models.Connection, error) {
	for i := range f.connections {
		if f.connections[i].ID == id {
			f.connections[i].Config = config
			f.connections[i].UpdatedAt = time.Now().UTC()
			return &f.connections[i], nil
		}
	}
	return nil, nil
}

func (f *fakeStore) AppendWebhookHistory(_ context.Context, body *models.CreateWebhookHistoryEntry) (*models.WebhookHistoryEntry, error) {
	now := time.Now().UTC()
	if body.StartedAt.IsZero() {
		body.StartedAt = now
	}
	if body.FinishedAt.IsZero() {
		body.FinishedAt = now
	}
	if body.RetentionExpiresAt.IsZero() {
		body.RetentionExpiresAt = body.FinishedAt.Add(30 * 24 * time.Hour)
	}
	durationMS := body.FinishedAt.Sub(body.StartedAt).Milliseconds()
	if durationMS < 0 {
		durationMS = 0
	}
	entry := models.WebhookHistoryEntry{
		ID:                 uuid.New(),
		SourceID:           body.SourceID,
		UserID:             body.UserID,
		Status:             body.Status,
		HTTPStatus:         body.HTTPStatus,
		InputPolicy:        body.InputPolicy,
		Inputs:             cloneTestRawMessage(body.Inputs),
		OutputParameters:   cloneTestRawMessage(body.OutputParameters),
		Error:              body.Error,
		CallCount:          body.CallCount,
		StartedAt:          body.StartedAt,
		FinishedAt:         body.FinishedAt,
		DurationMS:         durationMS,
		RetentionExpiresAt: body.RetentionExpiresAt,
		CreatedAt:          now,
	}
	f.webhookHistory[body.SourceID] = append([]models.WebhookHistoryEntry{entry}, f.webhookHistory[body.SourceID]...)
	return &entry, nil
}

func (f *fakeStore) ListWebhookHistory(_ context.Context, sourceID uuid.UUID, limit int) ([]models.WebhookHistoryEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	now := time.Now().UTC()
	out := []models.WebhookHistoryEntry{}
	for _, entry := range f.webhookHistory[sourceID] {
		if !entry.RetentionExpiresAt.IsZero() && entry.RetentionExpiresAt.Before(now) {
			continue
		}
		out = append(out, entry)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeStore) AppendInboundListenerEvent(_ context.Context, body *models.CreateInboundListenerEvent) (*models.InboundListenerEvent, error) {
	now := time.Now().UTC()
	entry := models.InboundListenerEvent{
		ID:                uuid.New(),
		SourceID:          body.SourceID,
		ListenerID:        body.ListenerID,
		EventID:           body.EventID,
		Status:            body.Status,
		SignatureVerified: body.SignatureVerified,
		Payload:           cloneTestRawMessage(body.Payload),
		Headers:           cloneTestRawMessage(body.Headers),
		Destination:       body.Destination,
		CreatedAt:         now,
	}
	if entry.Status == "" {
		entry.Status = "accepted"
	}
	f.listenerEvents[body.SourceID] = append([]models.InboundListenerEvent{entry}, f.listenerEvents[body.SourceID]...)
	return &entry, nil
}

func (f *fakeStore) ListInboundListenerEvents(_ context.Context, sourceID uuid.UUID, limit int) ([]models.InboundListenerEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := []models.InboundListenerEvent{}
	for _, entry := range f.listenerEvents[sourceID] {
		out = append(out, entry)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func cloneTestRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func valueOr(ptr *string, fallback string) string {
	if ptr == nil || strings.TrimSpace(*ptr) == "" {
		return fallback
	}
	return strings.TrimSpace(*ptr)
}

func (f *fakeStore) ListIcebergNamespaces(_ context.Context) ([]models.Connection, error) {
	out := []models.Connection{}
	for _, c := range f.connections {
		for _, r := range f.registrations[c.ID] {
			var meta map[string]any
			_ = json.Unmarshal(r.Metadata, &meta)
			if meta["supports_zero_copy"] == true {
				out = append(out, c)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeStore) GetIcebergConnection(_ context.Context, namespace string) (*models.Connection, error) {
	for i := range f.connections {
		if f.connections[i].Name == namespace || strings.NewReplacer("-", "-", " ", "_", ".", "_").Replace(f.connections[i].Name) == namespace {
			return &f.connections[i], nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListIcebergTables(_ context.Context, connectionID uuid.UUID) ([]models.ConnectionRegistration, error) {
	out := []models.ConnectionRegistration{}
	for _, r := range f.registrations[connectionID] {
		var meta map[string]any
		_ = json.Unmarshal(r.Metadata, &meta)
		if meta["supports_zero_copy"] == true {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStore) ListMediaSetSyncs(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.MediaSetSync, error) {
	if c, _ := f.GetConnectionForOwner(context.Background(), sourceID, ownerID); c == nil {
		return []models.MediaSetSync{}, nil
	}
	return f.mediaSyncs[sourceID], nil
}

func (f *fakeStore) GetMediaSetSync(_ context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.MediaSetSync, error) {
	for source, syncs := range f.mediaSyncs {
		if c, _ := f.GetConnectionForOwner(context.Background(), source, ownerID); c == nil {
			continue
		}
		for i := range syncs {
			if syncs[i].ID == id {
				return &syncs[i], nil
			}
		}
	}
	return nil, nil
}

func (f *fakeStore) CreateMediaSetSync(_ context.Context, sourceID uuid.UUID, body *models.CreateMediaSetSyncRequest, ownerID uuid.UUID) (*models.MediaSetSync, error) {
	allowed, _ := f.CheckSourceRole(context.Background(), sourceID, ownerID, models.SourceRoleSyncCreate)
	if !allowed {
		return nil, nil
	}
	m := models.MediaSetSync{ID: uuid.New(), SourceID: sourceID, Kind: body.Kind, TargetMediaSetRID: body.TargetMediaSetRID, Subfolder: strings.Trim(body.Subfolder, "/"), Filters: body.Filters, ScheduleCron: body.ScheduleCron, CreatedAt: time.Now().UTC()}
	f.mediaSyncs[sourceID] = append([]models.MediaSetSync{m}, f.mediaSyncs[sourceID]...)
	return &m, nil
}

func (f *fakeStore) UpdateMediaSetSync(_ context.Context, id uuid.UUID, body *models.UpdateMediaSetSyncRequest, ownerID uuid.UUID) (*models.MediaSetSync, error) {
	for source, syncs := range f.mediaSyncs {
		allowed, _ := f.CheckSourceRole(context.Background(), source, ownerID, models.SourceRoleEdit)
		if !allowed {
			continue
		}
		for i := range syncs {
			if syncs[i].ID == id {
				if body.Kind != nil {
					syncs[i].Kind = *body.Kind
				}
				if body.TargetMediaSetRID != nil {
					syncs[i].TargetMediaSetRID = *body.TargetMediaSetRID
				}
				if body.Subfolder != nil {
					syncs[i].Subfolder = strings.Trim(*body.Subfolder, "/")
				}
				if body.Filters != nil {
					syncs[i].Filters = *body.Filters
				}
				if body.ScheduleCron != nil {
					syncs[i].ScheduleCron = body.ScheduleCron
				}
				f.mediaSyncs[source] = syncs
				return &syncs[i], nil
			}
		}
	}
	return nil, nil
}

// SDC.40 — minimal in-memory retry policy + failure store for handler tests.
func (f *fakeStore) GetSourceRetryPolicy(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID) (*models.SourceRetryPolicy, error) {
	if allowed, _ := f.CheckSourceRole(context.Background(), sourceID, ownerID, models.SourceRoleView); !allowed {
		return nil, nil
	}
	if f.retryPolicies == nil {
		return nil, nil
	}
	policy, ok := f.retryPolicies[sourceID]
	if !ok {
		return nil, nil
	}
	return &policy, nil
}

func (f *fakeStore) UpsertSourceRetryPolicy(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID, actorID *string, policy models.SourceRetryPolicy) (*models.SourceRetryPolicy, error) {
	if allowed, _ := f.CheckSourceRole(context.Background(), sourceID, ownerID, models.SourceRoleEdit); !allowed {
		return nil, nil
	}
	now := time.Now().UTC()
	normalized := models.NormalizeSourceRetryPolicy(policy, sourceID, now)
	normalized.UpdatedBy = actorID
	normalized.UpdatedAt = now
	if f.retryPolicies == nil {
		f.retryPolicies = map[uuid.UUID]models.SourceRetryPolicy{}
	}
	f.retryPolicies[sourceID] = normalized
	return &normalized, nil
}

func (f *fakeStore) ListSyncRunFailuresForSource(_ context.Context, sourceID uuid.UUID, ownerID uuid.UUID, limit int) ([]models.RetryRecoveryRunSummary, error) {
	if allowed, _ := f.CheckSourceRole(context.Background(), sourceID, ownerID, models.SourceRoleView); !allowed {
		return nil, nil
	}
	if f.retryFailures == nil {
		return nil, nil
	}
	failures := f.retryFailures[sourceID]
	if limit > 0 && len(failures) > limit {
		failures = failures[:limit]
	}
	return failures, nil
}

func (f *fakeStore) RecordMediaSetSyncRun(_ context.Context, syncID uuid.UUID, ownerID uuid.UUID, run models.MediaSetSyncRun) (*models.MediaSetSyncRun, error) {
	// Find owning source via the in-memory media sync map.
	var sourceID uuid.UUID
	for src, syncs := range f.mediaSyncs {
		for _, sync := range syncs {
			if sync.ID == syncID {
				sourceID = src
				break
			}
		}
	}
	if sourceID != uuid.Nil {
		if allowed, _ := f.CheckSourceRole(context.Background(), sourceID, ownerID, models.SourceRoleUse); !allowed {
			return nil, nil
		}
	}
	run.ID = uuid.New()
	run.SyncDefID = syncID
	if f.mediaSyncRuns == nil {
		f.mediaSyncRuns = map[uuid.UUID][]models.MediaSetSyncRun{}
	}
	f.mediaSyncRuns[syncID] = append([]models.MediaSetSyncRun{run}, f.mediaSyncRuns[syncID]...)
	return &run, nil
}

func (f *fakeStore) ListMediaSetSyncRuns(_ context.Context, syncID uuid.UUID, _ uuid.UUID, limit int) ([]models.MediaSetSyncRun, error) {
	if f.mediaSyncRuns == nil {
		return []models.MediaSetSyncRun{}, nil
	}
	runs := f.mediaSyncRuns[syncID]
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

func (f *fakeStore) MediaSetSyncUsageForSource(_ context.Context, sourceID uuid.UUID, _ uuid.UUID) (map[uuid.UUID]models.MediaSetSyncUsageSummary, error) {
	out := map[uuid.UUID]models.MediaSetSyncUsageSummary{}
	syncs := f.mediaSyncs[sourceID]
	for _, sync := range syncs {
		runs := f.mediaSyncRuns[sync.ID]
		if len(runs) == 0 {
			continue
		}
		summary := models.MediaSetSyncUsageSummary{SyncDefID: sync.ID, RunCount: uint32(len(runs))}
		for _, run := range runs {
			summary.TotalAcceptedFiles += uint64(run.AcceptedFiles)
			summary.TotalBytesAccepted += run.BytesAccepted
			summary.TotalDispatchErrors += uint64(run.DispatchErrors)
			summary.TotalSchemaMismatch += uint64(run.SchemaMismatched)
		}
		latest := runs[0]
		summary.LastRunAt = &latest.StartedAt
		s := latest.Status
		summary.LastStatus = &s
		summary.LastErrorMessage = latest.ErrorMessage
		out[sync.ID] = summary
	}
	return out, nil
}

// SDC.47 — minimal in-memory dead-letter sink + quarantine record store for handler tests.
func (f *fakeStore) GetDeadLetterSink(_ context.Context, syncDefID uuid.UUID, _ uuid.UUID) (*models.DeadLetterSink, error) {
	if f.deadLetterSinks == nil {
		return nil, nil
	}
	sink, ok := f.deadLetterSinks[syncDefID]
	if !ok {
		return nil, nil
	}
	return &sink, nil
}

func (f *fakeStore) UpsertDeadLetterSink(_ context.Context, syncDefID uuid.UUID, _ uuid.UUID, actorID *string, req models.UpdateDeadLetterSinkRequest) (*models.DeadLetterSink, error) {
	now := time.Now().UTC()
	sink := models.DeadLetterSink{
		SyncDefID:      syncDefID,
		Kind:           req.Kind,
		TargetRID:      strings.TrimSpace(req.TargetRID),
		RetentionDays:  req.RetentionDays,
		RedactionRules: req.RedactionRules,
		UpdatedBy:      actorID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if f.deadLetterSinks == nil {
		f.deadLetterSinks = map[uuid.UUID]models.DeadLetterSink{}
	}
	f.deadLetterSinks[syncDefID] = sink
	return &sink, nil
}

func (f *fakeStore) RecordQuarantinedRecord(_ context.Context, syncDefID uuid.UUID, _ uuid.UUID, body models.RecordQuarantineRequest, sink models.DeadLetterSink, recordedAt time.Time) (*models.QuarantinedRecord, error) {
	redactedPayload, redactedHeaders := models.ApplyDeadLetterRedaction(body.Payload, body.Headers, sink.RedactionRules)
	record := models.QuarantinedRecord{
		ID:              uuid.New(),
		SyncDefID:       syncDefID,
		RunID:           body.RunID,
		FailureCategory: body.FailureCategory,
		ErrorMessage:    body.ErrorMessage,
		RecordKey:       body.RecordKey,
		RedactedPayload: redactedPayload,
		RedactedHeaders: redactedHeaders,
		RecordedAt:      recordedAt,
		ExpiresAt:       models.QuarantineExpiryFor(sink, recordedAt),
	}
	if f.quarantine == nil {
		f.quarantine = map[uuid.UUID][]models.QuarantinedRecord{}
	}
	f.quarantine[syncDefID] = append([]models.QuarantinedRecord{record}, f.quarantine[syncDefID]...)
	return &record, nil
}

func (f *fakeStore) ListQuarantinedRecords(_ context.Context, syncDefID uuid.UUID, _ uuid.UUID, category models.QuarantineFailureCategory, limit int) ([]models.QuarantinedRecord, error) {
	records := f.quarantine[syncDefID]
	if category != "" {
		filtered := make([]models.QuarantinedRecord, 0)
		for _, r := range records {
			if r.FailureCategory == category {
				filtered = append(filtered, r)
			}
		}
		records = filtered
	}
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (f *fakeStore) MarkQuarantinedRecordsForReplay(_ context.Context, syncDefID uuid.UUID, _ uuid.UUID, actorID *string, recordIDs []uuid.UUID, now time.Time) (int, error) {
	wanted := map[uuid.UUID]bool{}
	for _, id := range recordIDs {
		wanted[id] = true
	}
	records := f.quarantine[syncDefID]
	count := 0
	for i := range records {
		if wanted[records[i].ID] && records[i].ExpiresAt.After(now) {
			records[i].ReplayRequestedAt = &now
			records[i].ReplayRequestedBy = actorID
			count++
		}
	}
	f.quarantine[syncDefID] = records
	return count, nil
}

type fakeRuntime struct {
	report *models.MediaSetSyncExecutionReport
	err    error
	called bool
}

func (f *fakeRuntime) ExecuteMediaSetSync(_ context.Context, _ *models.MediaSetSync, _ *models.RunMediaSetSyncRequest, _ string) (*models.MediaSetSyncExecutionReport, error) {
	f.called = true
	return f.report, f.err
}

func authedReq(method, target, body string, sub uuid.UUID) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	return req.WithContext(authmw.ContextWithClaims(context.Background(), &authmw.Claims{Sub: sub}))
}

func withRouteParam(req *http.Request, key, val string) *http.Request {
	rctx := chi.RouteContext(req.Context())
	if rctx == nil {
		rctx = chi.NewRouteContext()
	}
	rctx.URLParams.Add(key, val)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func readRawProperty(t *testing.T, raw json.RawMessage, key string) json.RawMessage {
	t.Helper()
	var props map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &props))
	value, ok := props[key]
	require.True(t, ok, "missing property %s", key)
	return value
}

func readStringProperty(t *testing.T, raw json.RawMessage, key string) string {
	t.Helper()
	var value string
	require.NoError(t, json.Unmarshal(readRawProperty(t, raw, key), &value))
	return value
}

func fakeObservedVersion(v models.VirtualTable) *string {
	var caps models.Capabilities
	_ = json.Unmarshal(v.Capabilities, &caps)
	if !caps.Versioning {
		return nil
	}
	var props map[string]json.RawMessage
	if json.Unmarshal(v.Properties, &props) == nil {
		if raw, ok := props["source_version"]; ok {
			var value string
			if json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
				return &value
			}
		}
	}
	value := "sha256:" + v.ID.String()
	return &value
}

func fakeDownstreamBuilds(v models.VirtualTable, outcome models.PollOutcome) []models.VirtualTableDownstreamBuildPlan {
	action := "triggered"
	reason := "source-side update detected"
	if outcome == models.PollOutcomeUnchanged {
		action = "skipped"
		reason = "observed source version is unchanged"
	}
	if outcome == models.PollOutcomePotentialUpdate {
		reason = "source does not expose a comparable version; conservative update event emitted"
	}
	return []models.VirtualTableDownstreamBuildPlan{
		{TargetRID: "ri.foundry.main.pipeline." + v.ID.String(), TargetKind: "pipeline", DisplayName: v.Name + " pipeline", Action: action, Reason: reason},
		{TargetRID: "ri.foundry.main.dataset." + v.ID.String(), TargetKind: "dataset", DisplayName: v.Name + " dataset output", Action: action, Reason: reason},
		{TargetRID: "ri.ontology.main.object-type." + strings.ToLower(strings.ReplaceAll(v.Name, " ", "-")), TargetKind: "object_type", DisplayName: v.Name + " object output", Action: action, Reason: reason},
	}
}

func listenerHMAC(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestConnectorAgentHandlersRegisterHeartbeatAndDelete(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	sourceID := store.connections[0].ID
	policyID := uuid.New()

	req := authedReq(http.MethodPost, "/agents", fmt.Sprintf(`{
		"name":"Edge bridge",
		"agent_url":"https://agent.local:8443",
		"version":"1.2.3",
		"environment":"prod",
		"host":"edge-01.internal",
		"capabilities":{"connectors":["postgres"],"connector_capabilities":{"postgres":["batch_sync","cdc_sync"]}},
		"metadata":{"region":"eu"},
		"connected_sources":[{"source_id":%q,"source_name":"Postgres warehouse","connector_type":"postgres","status":"connected"}],
		"assigned_proxy_policies":[{"policy_id":%q,"source_id":%q,"policy_name":"warehouse proxy","proxy_mode":"http_connect","status":"active"}]
	}`, sourceID.String(), policyID.String(), sourceID.String()), owner)
	rec := httptest.NewRecorder()
	h.RegisterConnectorAgent(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created models.ConnectorAgent
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.Equal(t, "online", created.Status)
	require.Equal(t, "1.2.3", created.Version)
	require.Equal(t, "prod", created.Environment)
	require.Equal(t, "edge-01.internal", created.Host)
	require.Len(t, created.ConnectedSources, 1)
	require.Len(t, created.AssignedProxyPolicies, 1)
	require.Equal(t, "healthy", created.Health.State)
	require.NotNil(t, created.LastHeartbeatAt)

	req = authedReq(http.MethodGet, "/agents", ``, owner)
	rec = httptest.NewRecorder()
	h.ListConnectorAgents(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var listed models.ListResponse[models.ConnectorAgent]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed.Items, 1)

	req = withRouteParam(authedReq(http.MethodPost, "/agents/"+created.ID.String()+"/heartbeat", fmt.Sprintf(`{
		"version":"1.2.4",
		"capabilities":{"connectors":["postgres","mysql"]},
		"metadata":{"region":"eu"},
		"connection_failures":[{"source_id":%q,"source_name":"Postgres warehouse","policy_id":%q,"code":"agent_proxy_403","message":"Proxy rejected host outside source configuration","retryable":false}]
	}`, sourceID.String(), policyID.String()), owner), "id", created.ID.String())
	rec = httptest.NewRecorder()
	h.HeartbeatConnectorAgent(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "mysql")
	var heartbeat models.ConnectorAgent
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &heartbeat))
	require.Equal(t, "1.2.4", heartbeat.Version)
	require.Equal(t, "error", heartbeat.Health.State)
	require.Contains(t, heartbeat.Health.Message, "Proxy rejected")
	require.Len(t, heartbeat.ConnectionFailures, 1)

	req = withRouteParam(authedReq(http.MethodDelete, "/agents/"+created.ID.String(), ``, owner), "id", created.ID.String())
	rec = httptest.NewRecorder()
	h.DeleteConnectorAgent(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Empty(t, store.agents)
}

func TestRegisterConnectorAgentRejectsInvalidPayloads(t *testing.T) {
	owner := uuid.New()
	h := &handlers.Handlers{Repo: newFakeStore(owner)}

	for name, body := range map[string]string{
		"missing name":     `{"agent_url":"https://agent.local"}`,
		"invalid url":      `{"name":"agent","agent_url":"agent.local"}`,
		"array metadata":   `{"name":"agent","agent_url":"https://agent.local","metadata":[]}`,
		"bad capabilities": `{"name":"agent","agent_url":"https://agent.local","capabilities":{`,
	} {
		t.Run(name, func(t *testing.T) {
			req := authedReq(http.MethodPost, "/agents", body, owner)
			rec := httptest.NewRecorder()
			h.RegisterConnectorAgent(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

func TestSourcePolicyBindingHandlersMatchRustContract(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	sourceID := store.connections[0].ID
	policyID := uuid.New()
	h := &handlers.Handlers{Repo: store}

	req := withRouteParam(authedReq(http.MethodPost, "/sources/"+sourceID.String()+"/egress-policies", `{"policy_id":"`+policyID.String()+`"}`, owner), "id", sourceID.String())
	rec := httptest.NewRecorder()
	h.AttachPolicy(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var attached models.SourcePolicyBindingResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &attached))
	require.Equal(t, sourceID, attached.SourceID)
	require.Equal(t, policyID, attached.PolicyID)
	require.Equal(t, "direct", attached.Kind)
	require.Len(t, store.auditEvents[sourceID], 1)
	require.Equal(t, "egress_policy_attached", store.auditEvents[sourceID][0].Action)
	require.Equal(t, "network_egress", store.auditEvents[sourceID][0].Capability)
	require.Equal(t, true, store.auditEvents[sourceID][0].Metadata["potential_data_export"])
	require.Equal(t, []string{"networkEgress", "dataExport", "managementPermissions"}, store.auditEvents[sourceID][0].Metadata["audit_categories"])

	req = withRouteParam(authedReq(http.MethodPost, "/sources/"+sourceID.String()+"/egress-policies", `{"policy_id":"`+policyID.String()+`","kind":"agent_proxy"}`, owner), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.AttachPolicy(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.policies[sourceID], 1)
	require.Equal(t, "agent_proxy", store.policies[sourceID][0].Kind)

	req = withRouteParam(authedReq(http.MethodGet, "/sources/"+sourceID.String()+"/egress-policies", ``, owner), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.ListSourcePolicies(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var listed []models.SourcePolicyBindingResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed, 1)
	require.Equal(t, "agent_proxy", listed[0].Kind)

	req = withRouteParam(authedReq(http.MethodPost, "/sources/"+sourceID.String()+"/egress-policies", `{"policy_id":"`+policyID.String()+`","kind":"same_region_bucket"}`, owner), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.AttachPolicy(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.policies[sourceID], 1)
	require.Equal(t, "same_region_bucket", store.policies[sourceID][0].Kind)

	req = withRouteParam(withRouteParam(authedReq(http.MethodDelete, "/sources/"+sourceID.String()+"/egress-policies/"+policyID.String(), ``, owner), "source_id", sourceID.String()), "policy_id", policyID.String())
	rec = httptest.NewRecorder()
	h.DetachPolicy(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Empty(t, store.policies[sourceID])
	require.Equal(t, "egress_policy_detached", store.auditEvents[sourceID][0].Action)
	require.Equal(t, policyID.String(), store.auditEvents[sourceID][0].Metadata["policy_id"])
}

func TestAttachPolicyRejectsEdgeCases(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	sourceID := store.connections[0].ID
	h := &handlers.Handlers{Repo: store}

	for name, body := range map[string]string{
		"nil policy id":     `{"policy_id":"00000000-0000-0000-0000-000000000000"}`,
		"unsupported kind":  `{"policy_id":"` + uuid.NewString() + `","kind":"bucket_endpoint"}`,
		"malformed payload": `{`,
	} {
		t.Run(name, func(t *testing.T) {
			req := withRouteParam(authedReq(http.MethodPost, "/sources/"+sourceID.String()+"/egress-policies", body, owner), "id", sourceID.String())
			rec := httptest.NewRecorder()
			h.AttachPolicy(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

func TestSourceGovernanceRolesVisibilityAndAudit(t *testing.T) {
	owner := uuid.New()
	worker := uuid.New()
	viewer := uuid.New()
	store := newFakeStore(owner)
	sourceID := store.connections[0].ID
	h := &handlers.Handlers{Repo: store}

	body := fmt.Sprintf(`{
		"reason":"delegate governed sync creation",
		"permission_grants":[
			{"principal_id":%q,"principal_type":"service_account","principal_name":"pipeline-bot","roles":["source_view","source_use","sync_create"],"reason":"pipeline owner"},
			{"principal_id":%q,"principal_type":"user","principal_name":"read-only viewer","roles":["source_view"]}
		],
		"visibility":{
			"source_visibility_roles":["source_view","source_edit","source_owner"],
			"credential_visibility_roles":["code_import","source_edit","source_owner"],
			"external_sample_visibility_roles":["source_use","source_edit","source_owner"],
			"output_dataset_permission_roles":["dataset:view","dataset:edit"],
			"credential_values_visible":true,
			"external_samples_persisted":true,
			"output_dataset_permissions_enforced":true,
			"output_dataset_permission_system":"openfoundry_datasets"
		}
	}`, worker.String(), viewer.String())
	req := withRouteParam(authedReq(http.MethodPatch, "/sources/"+sourceID.String()+"/permissions", body, owner), "id", sourceID.String())
	rec := httptest.NewRecorder()
	h.UpdateSourceGovernance(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var updated models.SourceGovernance
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	require.Len(t, updated.RoleDefinitions, len(models.AllSourcePermissionRoles()))
	require.Len(t, updated.PermissionGrants, 2)
	assert.False(t, updated.Visibility.CredentialValuesVisible)
	assert.False(t, updated.Visibility.ExternalSamplesPersisted)
	assert.True(t, updated.Visibility.OutputDatasetPermissionsEnforced)
	assert.False(t, models.SourceRoleListContains(updated.Visibility.CredentialVisibilityRoles, models.SourceRoleView))
	assert.False(t, models.SourceRoleListContains(updated.Visibility.ExternalSampleVisibilityRoles, models.SourceRoleView))

	req = withRouteParam(authedReq(http.MethodGet, "/sources/"+sourceID.String()+"/permissions", "", worker), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.GetSourceGovernance(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var workerView models.SourceGovernance
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &workerView))
	assert.True(t, sourceRoleSet(workerView.EffectiveRoles)[models.SourceRoleView])
	assert.True(t, sourceRoleSet(workerView.EffectiveRoles)[models.SourceRoleUse])
	assert.True(t, sourceRoleSet(workerView.EffectiveRoles)[models.SourceRoleSyncCreate])
	assert.False(t, sourceRoleSet(workerView.EffectiveRoles)[models.SourceRoleCodeImport])

	store.credentials[sourceID] = []models.CredentialResponse{{ID: uuid.New(), SourceID: sourceID, Kind: "api_key", Fingerprint: "sha256:redacted", CreatedAt: time.Now().UTC()}}
	req = withRouteParam(authedReq(http.MethodGet, "/sources/"+sourceID.String()+"/credentials", "", viewer), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.ListCredentials(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "code_import")

	outputDataset := uuid.New()
	req = authedReq(http.MethodPost, "/syncs", fmt.Sprintf(`{"source_id":%q,"output_dataset_id":%q}`, sourceID.String(), outputDataset.String()), worker)
	rec = httptest.NewRecorder()
	h.CreateSyncJob(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var syncJob models.SyncJob
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &syncJob))
	assert.Equal(t, sourceID, syncJob.SourceID)

	req = withRouteParam(authedReq(http.MethodPost, "/connections/"+sourceID.String()+"/test", "", worker), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.TestConnection(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	req = withRouteParam(authedReq(http.MethodGet, "/sources/"+sourceID.String()+"/audit?limit=10", "", owner), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.ListSourceGovernanceAudit(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var audit models.ListResponse[models.SourceGovernanceAuditEvent]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &audit))
	actions := map[string]bool{}
	for _, event := range audit.Items {
		actions[event.Action] = true
	}
	assert.True(t, actions["update_source_governance"])
	assert.True(t, actions["sync_created"])
	assert.True(t, actions["connection_tested"])
}

func TestGetSourceHealthAggregatesDataConnectionFailures(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	sourceID := store.connections[0].ID
	now := time.Now().UTC()
	store.connections[0].Status = "error"
	store.connections[0].Config = json.RawMessage(`{"worker":"agent"}`)
	expired := now.Add(-1 * time.Hour)
	store.credentials[sourceID] = []models.CredentialResponse{{
		ID:               uuid.New(),
		SourceID:         sourceID,
		Kind:             "api_key",
		Fingerprint:      "sha256:redacted",
		ValidationStatus: "failed",
		LastValidatedAt:  &now,
		ExpiresAt:        &expired,
		CreatedAt:        now.Add(-24 * time.Hour),
	}}
	heartbeat := now.Add(-5 * time.Minute)
	store.agents = []models.ConnectorAgent{{
		ID:               uuid.New(),
		Name:             "edge",
		OwnerID:          owner,
		Status:           "online",
		Capabilities:     json.RawMessage(`{}`),
		Metadata:         json.RawMessage(`{}`),
		ConnectedSources: []models.AgentConnectedSource{{SourceID: sourceID, SourceName: "pg", ConnectorType: "postgresql", Status: "connected"}},
		ConnectionFailures: []models.AgentConnectionFailure{{
			SourceID:   sourceID,
			Code:       "agent_proxy_403",
			Message:    "Proxy rejected host",
			Retryable:  false,
			OccurredAt: &now,
		}},
		LastHeartbeatAt: &heartbeat,
	}}
	streamID := "ri.foundry.main.stream.orders"
	sourceTable := "orders"
	syncID := uuid.New()
	schedule := "0 * * * *"
	store.syncJobs[sourceID] = []models.SyncJob{{
		ID:             syncID,
		SourceID:       sourceID,
		CapabilityType: "cdc_sync",
		OutputKind:     "stream",
		OutputStreamID: &streamID,
		SourceTable:    &sourceTable,
		ScheduleCron:   &schedule,
		CdcSync: &models.CdcSyncSettings{
			InputKind:                "relational_connector",
			SourceTable:              "orders",
			PrimaryKeyColumns:        []string{},
			OrderingColumn:           "",
			OutputStreamID:           &streamID,
			OutputStreamLocation:     "",
			StartPosition:            "initial_snapshot",
			SourceDatabaseCDCEnabled: false,
			SourceTableCDCEnabled:    false,
		},
		CreatedAt: now.Add(-48 * time.Hour),
	}}
	runErr := "checkpoint failed"
	store.runs[syncID] = []models.SyncRun{{
		ID:        uuid.New(),
		SyncDefID: syncID,
		Status:    "failed",
		StartedAt: now.Add(-2 * time.Hour),
		Error:     &runErr,
	}}
	exportErr := "destination column UPDATED_AT type mismatch"
	destinationTable := "public.orders"
	store.exports[sourceID] = []models.DataExport{{
		ID:               uuid.New(),
		SourceID:         sourceID,
		Name:             "orders export",
		ExportType:       models.DataExportTypeTable,
		ExportMode:       models.DataExportModeTableMirror,
		DestinationTable: &destinationTable,
		ScheduleCron:     &schedule,
		Status:           models.DataExportStatusFailed,
		Health:           models.DataExportHealth{State: models.DataExportHealthError, Message: &exportErr, LastCheckedAt: &now},
		TableExport: &models.TableExportSettings{
			ValidationIssues: []models.TableExportValidationIssue{{Code: "type_mismatch", Severity: "error", Message: exportErr}},
		},
		History:   []models.DataExportHistoryEntry{{ID: uuid.New(), Status: "failed", ErrorMessage: &exportErr, CreatedAt: now}},
		CreatedAt: now.Add(-24 * time.Hour),
		UpdatedAt: now,
	}}
	webhookErr := "upstream timeout"
	store.webhookHistory[sourceID] = []models.WebhookHistoryEntry{{
		ID:                 uuid.New(),
		SourceID:           sourceID,
		UserID:             owner,
		Status:             "failed",
		Error:              &webhookErr,
		StartedAt:          now.Add(-5 * time.Minute),
		FinishedAt:         now.Add(-4 * time.Minute),
		RetentionExpiresAt: now.Add(24 * time.Hour),
		CreatedAt:          now.Add(-5 * time.Minute),
	}}
	ownerString := owner.String()
	store.vtables["ri.foundry.main.virtual-table.orders"] = models.VirtualTable{
		ID:                                 uuid.New(),
		RID:                                "ri.foundry.main.virtual-table.orders",
		SourceRID:                          models.SourceRIDForConnection(sourceID),
		ProjectRID:                         "ri.foundry.main.project.analytics",
		Name:                               "orders",
		UpdateDetectionEnabled:             true,
		UpdateDetectionConsecutiveFailures: 3,
		CreatedBy:                          &ownerString,
		CreatedAt:                          now.Add(-24 * time.Hour),
		UpdatedAt:                          now,
	}
	h := &handlers.Handlers{Repo: store}

	req := withRouteParam(authedReq(http.MethodGet, "/sources/"+sourceID.String()+"/health", "", owner), "id", sourceID.String())
	rec := httptest.NewRecorder()
	h.GetSourceHealth(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var summary models.DataConnectionHealthSummary
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &summary))
	require.Equal(t, models.DataConnectionHealthCritical, summary.State)
	codes := map[string]bool{}
	for _, check := range summary.Checks {
		codes[check.Code] = true
	}
	for _, code := range []string{
		"source_status",
		"agent_health",
		"network_policy_missing",
		"credential_expired",
		"credential_validation_failed",
		"sync_recent_failure",
		"stream_checkpoint_failure",
		"cdc_metadata_invalid",
		"export_recent_failure",
		"destination_schema_mismatch",
		"webhook_recent_failures",
		"virtual_table_update_detection_failed",
	} {
		assert.True(t, codes[code], "expected health code %s", code)
	}
	assert.GreaterOrEqual(t, summary.Counts.Critical, 8)
	assert.Contains(t, summary.Surfaces, models.DataConnectionHealthSurfaceCDC)
	assert.Contains(t, summary.Surfaces, models.DataConnectionHealthSurfaceSchedule)
}

func sourceRoleSet(roles []models.SourcePermissionRole) map[models.SourcePermissionRole]bool {
	out := map[models.SourcePermissionRole]bool{}
	for _, role := range roles {
		out[role] = true
	}
	return out
}

func TestSourceCodeImportHandlersGenerateBindingsAndResolveLiveBuildStart(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	sourceID := store.connections[0].ID
	h := &handlers.Handlers{Repo: store}

	req := withRouteParam(authedReq(http.MethodGet, "/sources/"+sourceID.String()+"/code-imports", "", owner), "id", sourceID.String())
	rec := httptest.NewRecorder()
	h.GetSourceCodeImport(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var initial models.SourceCodeImport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &initial))
	require.False(t, initial.Enabled)
	require.Equal(t, models.SourceRIDForConnection(sourceID), initial.SourceRID)
	require.Contains(t, initial.GeneratedBinding.CodeSnippet, `Source("`+models.SourceRIDForConnection(sourceID)+`")`)
	require.Contains(t, initial.GeneratedBinding.SourcePanelURL, sourceID.String())

	req = withRouteParam(authedReq(http.MethodPost, "/sources/"+sourceID.String()+"/code-imports:resolve-build-start", `{}`, owner), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.ResolveSourceCodeImportBuildStart(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "not approved")

	_, err := store.SetCredential(context.Background(), sourceID, owner, "api_key", []byte("secret"), "sha256:credential")
	require.NoError(t, err)
	policyID := uuid.New()
	_, err = store.AttachPolicy(context.Background(), sourceID, owner, policyID, "agent_proxy")
	require.NoError(t, err)

	body := `{
		"enabled": true,
		"friendly_name": "Warehouse Orders",
		"python_identifier": "orders_source",
		"code_repositories": [{
			"repository_rid": "ri.code.repo.orders",
			"repository_name": "Orders transforms",
			"file_path": "transforms/orders.py",
			"imported_name": "orders_source"
		}],
		"export_controls": {
			"allowed_markings": ["public"],
			"allowed_organizations": ["operations"]
		}
	}`
	req = withRouteParam(authedReq(http.MethodPatch, "/sources/"+sourceID.String()+"/code-imports", body, owner), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.UpdateSourceCodeImport(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var updated models.SourceCodeImport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	require.True(t, updated.Enabled)
	require.Equal(t, "Warehouse Orders", updated.FriendlyName)
	require.Equal(t, "orders_source", updated.PythonIdentifier)
	require.Len(t, updated.CodeRepositories, 1)
	require.Equal(t, "Orders transforms · transforms/orders.py", updated.CodeRepositories[0].RenderedDisplay)
	require.Equal(t, "/code-repos/ri.code.repo.orders", updated.CodeRepositories[0].RenderedLink)
	require.False(t, updated.ExportControls.AllowFoundryInputs)
	require.Contains(t, updated.GeneratedBinding.CodeSnippet, "def compute(orders_source")
	require.Contains(t, updated.GeneratedBinding.CodeSnippet, `orders_source=Source("`+models.SourceRIDForConnection(sourceID)+`")`)
	require.NotEmpty(t, updated.ExternalTransformPatterns)
	alternatives := map[string]bool{}
	examples := map[string]bool{}
	for _, pattern := range updated.ExternalTransformPatterns {
		require.Contains(t, pattern.CodeSnippet, models.SourceRIDForConnection(sourceID), pattern.ID)
		for _, alt := range pattern.AlternativeFor {
			alternatives[alt] = true
		}
		examples[pattern.ExampleKind] = true
	}
	for _, alt := range []string{"batch_sync", "file_export", "table_batch_sync", "table_export", "media_sync_handoff", "virtual_table_registration", "virtual_media_registration"} {
		assert.True(t, alternatives[alt], "missing external transform alternative %s", alt)
	}
	for _, example := range []string{"rest_api", "database", "buffered_parquet", "csv_export", "lightweight_transform", "agent_proxy"} {
		assert.True(t, examples[example], "missing external transform example %s", example)
	}
	require.NotEmpty(t, updated.ComputeModuleAlternatives)
	computeAlternatives := map[string]bool{}
	computeBlockers := map[string]bool{}
	for _, alternative := range updated.ComputeModuleAlternatives {
		require.Equal(t, "blocked", alternative.Status)
		require.Equal(t, "long_running_compute_module", alternative.RuntimeKind)
		require.Contains(t, alternative.CodeSketch, models.SourceRIDForConnection(sourceID), alternative.ID)
		computeAlternatives[alternative.AlternativeFor] = true
		for _, blocker := range alternative.Blockers {
			computeBlockers[blocker] = true
		}
	}
	for _, alt := range []string{"streaming_sync", "streaming_export", "cdc_sync", "webhook"} {
		assert.True(t, computeAlternatives[alt], "missing compute module alternative %s", alt)
	}
	for _, blocker := range []string{"compute_module_runtime", "compute_module_deployment_contract", "compute_module_source_import_contract"} {
		assert.True(t, computeBlockers[blocker], "missing compute module blocker %s", blocker)
	}

	newConfig := json.RawMessage(`{"host":"new.example.com","port":443}`)
	_, err = store.UpdateConnection(context.Background(), sourceID, &models.UpdateConnectionRequest{Config: newConfig})
	require.NoError(t, err)
	expectedHash := sha256.Sum256(newConfig)

	req = withRouteParam(authedReq(http.MethodPost, "/sources/"+sourceID.String()+"/code-imports:resolve-build-start", `{"repository_rid":"ri.code.repo.orders","build_rid":"ri.build.1","branch":"main"}`, owner), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.ResolveSourceCodeImportBuildStart(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resolved models.SourceCodeImportBuildResolution
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resolved))
	require.Equal(t, "ri.code.repo.orders", *resolved.RepositoryRID)
	require.Equal(t, "main", *resolved.Branch)
	require.Equal(t, fmt.Sprintf("sha256:%x", expectedHash[:]), resolved.ConfigHash)
	require.Len(t, resolved.CredentialBindings, 1)
	require.Equal(t, "sha256:credential", resolved.CredentialBindings[0].Fingerprint)
	require.Len(t, resolved.EgressPolicyBindings, 1)
	require.Equal(t, policyID, resolved.EgressPolicyBindings[0].PolicyID)
	require.Equal(t, []string{"public"}, resolved.ExportControls.AllowedMarkings)
	require.Equal(t, "not_applicable", resolved.ExportPolicyDecision.Status)
	require.True(t, resolved.ExportPolicyDecision.BuildAllowed)
	require.True(t, resolved.UsesLiveConfiguration)
	require.True(t, resolved.NoCodeChangeRequired)

	req = withRouteParam(authedReq(http.MethodPost, "/sources/"+sourceID.String()+"/code-imports:resolve-build-start", `{
		"repository_rid":"ri.code.repo.orders",
		"uses_foundry_inputs": true,
		"foundry_inputs": [{
			"rid": "ri.foundry.main.dataset.orders",
			"display_name": "Orders dataset",
			"resource_type": "dataset",
			"markings": ["public"],
			"organizations": ["operations"]
		}]
	}`, owner), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.ResolveSourceCodeImportBuildStart(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resolved))
	require.Equal(t, "blocked", resolved.ExportPolicyDecision.Status)
	require.False(t, resolved.ExportPolicyDecision.BuildAllowed)
	require.True(t, resolved.ExportPolicyDecision.OwnerApprovalRequired)
	assert.Contains(t, warningCodes(resolved.ExportPolicyDecision.BlockingReasons), "source-export-controls-disabled")

	originalSnippet := resolved.GeneratedBinding.CodeSnippet
	req = withRouteParam(authedReq(http.MethodPatch, "/sources/"+sourceID.String()+"/code-imports", `{"export_controls":{"allow_foundry_inputs":true,"allowed_markings":["restricted"],"allowed_organizations":["operations"]}}`, owner), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.UpdateSourceCodeImport(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	req = withRouteParam(authedReq(http.MethodPost, "/sources/"+sourceID.String()+"/code-imports:resolve-build-start", `{
		"uses_foundry_inputs": true,
		"foundry_inputs": [{
			"rid": "ri.foundry.main.dataset.orders",
			"display_name": "Orders dataset",
			"resource_type": "dataset",
			"markings": ["public"],
			"organizations": ["operations"]
		}]
	}`, owner), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.ResolveSourceCodeImportBuildStart(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resolved))
	require.Equal(t, "blocked", resolved.ExportPolicyDecision.Status)
	require.False(t, resolved.ExportPolicyDecision.BuildAllowed)
	assert.Contains(t, warningCodes(resolved.ExportPolicyDecision.BlockingReasons), "source-export-controls-marking-denied")

	req = withRouteParam(authedReq(http.MethodPost, "/sources/"+sourceID.String()+"/code-imports:resolve-build-start", `{
		"uses_foundry_inputs": true,
		"foundry_inputs": [{
			"rid": "ri.foundry.main.dataset.orders",
			"display_name": "Orders dataset",
			"resource_type": "dataset",
			"markings": ["restricted"],
			"organizations": ["operations"]
		}]
	}`, owner), "id", sourceID.String())
	rec = httptest.NewRecorder()
	h.ResolveSourceCodeImportBuildStart(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resolved))
	require.Equal(t, []string{"restricted"}, resolved.ExportControls.AllowedMarkings)
	require.True(t, resolved.ExportControls.AllowFoundryInputs)
	require.Equal(t, "allowed", resolved.ExportPolicyDecision.Status)
	require.True(t, resolved.ExportPolicyDecision.BuildAllowed)
	require.Equal(t, originalSnippet, resolved.GeneratedBinding.CodeSnippet)
	require.True(t, resolved.NoCodeChangeRequired)
}

func TestCreateListGetUpdateSyncJobAndRun(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID
	out := uuid.New()
	req := authedReq("POST", "/syncs", `{"source_id":"`+source.String()+`","output_dataset_id":"`+out.String()+`","file_glob":"*.csv"}`, owner)
	rec := httptest.NewRecorder()
	h.CreateSyncJob(rec, req)
	require.Equal(t, 201, rec.Code)
	var created models.SyncJob
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, source, created.SourceID)

	req = withRouteParam(authedReq("GET", "/sources/"+source.String()+"/syncs", "", owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.ListSyncJobs(rec, req)
	require.Equal(t, 200, rec.Code)
	var list []models.SyncJob
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)

	req = withRouteParam(authedReq("GET", "/syncs/"+created.ID.String(), "", owner), "sync_id", created.ID.String())
	rec = httptest.NewRecorder()
	h.GetSyncJob(rec, req)
	assert.Equal(t, 200, rec.Code)
	cron := "0 * * * *"
	req = withRouteParam(authedReq("PATCH", "/syncs/"+created.ID.String(), `{"schedule_cron":"`+cron+`"}`, owner), "sync_id", created.ID.String())
	rec = httptest.NewRecorder()
	h.UpdateSyncJob(rec, req)
	assert.Equal(t, 200, rec.Code)
	req = withRouteParam(authedReq("POST", "/syncs/"+created.ID.String()+"/run", "", owner), "sync_id", created.ID.String())
	rec = httptest.NewRecorder()
	h.RunSyncJob(rec, req)
	require.Equal(t, 202, rec.Code)
	assert.Contains(t, rec.Body.String(), "running")
}

func warningCodes(warnings []models.SourceCodeImportWarning) []string {
	codes := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		codes = append(codes, warning.Code)
	}
	return codes
}

func TestCreateListRunDataExportResource(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID
	body := `{
		"name":"Orders table export",
		"export_type":"table",
		"input_dataset_rid":"ri.foundry.main.dataset.orders",
		"destination_table":"public.orders_export",
		"schedule_cron":"0 * * * *",
		"export_controls":{"allowed_markings":["public"],"allowed_organizations":["operations"]},
		"config":{"batch_size":1000,"retry_attempts":2},
		"table_export":{
			"input_parquet_backed":true,
			"destination_table_exists":true,
			"truncate_permission":true,
			"row_count_estimate":2,
			"dataset_schema":[
				{"name":"ORDER_ID","foundry_type":"BIGINT","external_type":"BIGINT","nullable":false},
				{"name":"UPDATED_AT","foundry_type":"TIMESTAMP","external_type":"TIMESTAMP","nullable":true}
			],
			"destination_schema":[
				{"name":"ORDER_ID","foundry_type":"BIGINT","external_type":"BIGINT","nullable":false},
				{"name":"UPDATED_AT","foundry_type":"TIMESTAMP","external_type":"TIMESTAMP","nullable":true}
			]
		}
	}`
	req := withRouteParam(authedReq(http.MethodPost, "/sources/"+source.String()+"/exports", body, owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.CreateDataExport(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created models.DataExport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, models.DataExportTypeTable, created.ExportType)
	assert.Equal(t, models.DataExportModeTableMirror, created.ExportMode)
	assert.Equal(t, models.DataExportStatusScheduled, created.Status)
	require.NotNil(t, created.Schedule)
	assert.Equal(t, "0 * * * *", created.Schedule.Cron)
	assert.Equal(t, "data-integration-build-schedules", created.Schedule.BuildSystem)
	assert.Equal(t, "scheduled", created.StartBehavior)
	require.NotNil(t, created.DestinationTable)
	assert.Equal(t, "public.orders_export", *created.DestinationTable)
	assert.Equal(t, []string{"public"}, created.ExportControls.AllowedMarkings)
	require.NotNil(t, created.TableExport)
	assert.True(t, created.TableExport.ExactColumnMatch)

	req = withRouteParam(authedReq(http.MethodGet, "/sources/"+source.String()+"/exports", "", owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.ListDataExports(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list []models.DataExport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)

	req = withRouteParam(authedReq(http.MethodPost, "/exports/"+created.ID.String()+"/run", "", owner), "id", created.ID.String())
	rec = httptest.NewRecorder()
	h.RunDataExport(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var ran models.DataExport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &ran))
	assert.Equal(t, models.DataExportStatusSucceeded, ran.Status)
	assert.Equal(t, models.DataExportHealthHealthy, ran.Health.State)
	require.NotEmpty(t, ran.History)
	assert.Equal(t, "run", ran.History[0].Action)
	assert.EqualValues(t, 2, ran.History[0].RowsWritten)
	assert.True(t, ran.History[0].TruncatePerformed)
	assert.True(t, ran.History[0].ScheduleTriggered)
	assert.EqualValues(t, 2, ran.History[0].RetryAttempts)
	require.NotNil(t, ran.History[0].BuildID)
	require.NotNil(t, ran.History[0].BuildReportURL)
	assert.Contains(t, *ran.History[0].BuildReportURL, "/builds/")
}

func TestStreamingDataExportUsesStartStop(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "kafka"
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID
	body := `{
		"name":"Telemetry stream export",
		"export_type":"streaming",
		"input_stream_id":"stream://telemetry",
		"destination_topic":"ops.telemetry",
		"schedule_cron":"*/5 * * * *",
		"start_behavior":"start_immediately",
		"stop_behavior":"manual",
		"streaming_export":{
			"replay_behavior":"export_replayed_records",
			"start_offset":"previous_export_offset",
			"last_exported_offset":"42",
			"schedule_restart_enabled":true,
			"records_exported_estimate":3,
			"replayed_records_detected":true
		}
	}`
	req := withRouteParam(authedReq(http.MethodPost, "/sources/"+source.String()+"/exports", body, owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.CreateDataExport(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created models.DataExport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, models.DataExportModeStreamingContinuous, created.ExportMode)
	assert.Equal(t, models.DataExportStatusScheduled, created.Status)
	require.NotNil(t, created.StreamingExport)
	assert.True(t, created.StreamingExport.RestartFromPreviousOffset)
	assert.Equal(t, "export_replayed_records", created.StreamingExport.ReplayBehavior)
	assert.Equal(t, "replay_duplicate_risk", created.StreamingExport.Warnings[0].Code)

	req = withRouteParam(authedReq(http.MethodPost, "/exports/"+created.ID.String()+"/run", "", owner), "id", created.ID.String())
	rec = httptest.NewRecorder()
	h.RunDataExport(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "start/stop")

	req = withRouteParam(authedReq(http.MethodPost, "/exports/"+created.ID.String()+"/start", "", owner), "id", created.ID.String())
	rec = httptest.NewRecorder()
	h.StartDataExport(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"running"`)
	var started models.DataExport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &started))
	require.NotEmpty(t, started.History)
	assert.Equal(t, "started", started.History[0].Action)
	require.NotNil(t, started.History[0].LastExportedOffset)
	assert.Equal(t, "42", *started.History[0].LastExportedOffset)
	require.NotNil(t, started.StreamingExport)
	require.NotNil(t, started.StreamingExport.LastStartedAt)

	req = withRouteParam(authedReq(http.MethodPost, "/exports/"+created.ID.String()+"/stop", "", owner), "id", created.ID.String())
	rec = httptest.NewRecorder()
	h.StopDataExport(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"stopped"`)
	var stopped models.DataExport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &stopped))
	require.NotNil(t, stopped.StreamingExport)
	require.NotNil(t, stopped.StreamingExport.LastExportedOffset)
	assert.Equal(t, "45", *stopped.StreamingExport.LastExportedOffset)
	assert.EqualValues(t, 3, stopped.History[0].RecordsExported)
}

func TestCreateDataExportValidatesConnectorAndDestination(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID

	req := withRouteParam(authedReq(http.MethodPost, "/sources/"+source.String()+"/exports", `{"export_type":"file","input_dataset_rid":"ri.dataset.orders","destination_path":"s3://bucket/orders"}`, owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.CreateDataExport(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "connector does not support file_export")

	req = withRouteParam(authedReq(http.MethodPost, "/sources/"+source.String()+"/exports", `{"export_type":"table","input_dataset_rid":"ri.dataset.orders"}`, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.CreateDataExport(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "destination_table required")

	store.connections[0].ConnectorType = "kafka"
	req = withRouteParam(authedReq(http.MethodPost, "/sources/"+source.String()+"/exports", `{
		"export_type":"streaming",
		"input_stream_id":"stream://orders",
		"destination_topic":"orders.out",
		"streaming_export":{"replay_behavior":"skip_replayed_records","start_offset":"explicit"}
	}`, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.CreateDataExport(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "start_offset_value")
}

func TestTableDataExportValidatesSchemaModesAndTruncate(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID
	validSettings := `"table_export":{
		"input_parquet_backed":true,
		"destination_table_exists":true,
		"truncate_permission":true,
		"row_count_estimate":5,
		"dataset_schema":[
			{"name":"ORDER_ID","foundry_type":"BIGINT","external_type":"BIGINT","nullable":false},
			{"name":"AMOUNT","foundry_type":"DECIMAL","external_type":"NUMERIC","nullable":true}
		],
		"destination_schema":[
			{"name":"ORDER_ID","foundry_type":"BIGINT","external_type":"BIGINT","nullable":false},
			{"name":"AMOUNT","foundry_type":"DECIMAL","external_type":"NUMERIC","nullable":true}
		]
	}`

	req := withRouteParam(authedReq(http.MethodPost, "/sources/"+source.String()+"/exports", `{
		"export_type":"table",
		"export_mode":"full_snapshot",
		"input_dataset_rid":"ri.dataset.orders",
		"destination_table":"public.orders_export",
		`+strings.Replace(validSettings, `"truncate_permission":true,`, `"truncate_permission":false,`, 1)+`
	}`, owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.CreateDataExport(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created models.DataExport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.NotNil(t, created.TableExport)
	assert.False(t, created.TableExport.TruncatePermission)
	assert.Empty(t, created.TableExport.ValidationIssues)

	req = withRouteParam(authedReq(http.MethodPost, "/exports/"+created.ID.String()+"/run", "", owner), "id", created.ID.String())
	rec = httptest.NewRecorder()
	h.RunDataExport(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var ran models.DataExport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &ran))
	assert.EqualValues(t, 5, ran.History[0].RowsWritten)
	assert.False(t, ran.History[0].TruncatePerformed)

	req = withRouteParam(authedReq(http.MethodPost, "/sources/"+source.String()+"/exports", `{
		"export_type":"table",
		"export_mode":"mirror",
		"input_dataset_rid":"ri.dataset.orders",
		"destination_table":"public.orders_export",
		`+strings.Replace(validSettings, `"truncate_permission":true,`, `"truncate_permission":false,`, 1)+`
	}`, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.CreateDataExport(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "truncate_permission")

	req = withRouteParam(authedReq(http.MethodPost, "/sources/"+source.String()+"/exports", `{
		"export_type":"table",
		"input_dataset_rid":"ri.dataset.orders",
		"destination_table":"public.orders_export",
		"table_export":{
			"input_parquet_backed":true,
			"destination_table_exists":true,
			"truncate_permission":true,
			"dataset_schema":[{"name":"ORDER_ID","foundry_type":"ARRAY<STRING>","external_type":"ARRAY","nullable":false}],
			"destination_schema":[{"name":"order_id","foundry_type":"BIGINT","external_type":"BIGINT","nullable":false}]
		}
	}`, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.CreateDataExport(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "nested")
	assert.Contains(t, rec.Body.String(), "exactly match")
}

func TestFileDataExportTracksModifiedFilesOverwriteAndFullReexport(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "s3"
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID
	body := `{
		"name":"Raw orders files",
		"export_type":"file",
		"input_dataset_rid":"ri.foundry.main.dataset.orders-raw",
		"destination_path":"s3://exports-bucket/foundry/orders",
		"file_export":{
			"overwrite_behavior":"overwrite_existing",
			"destination_subfolder":"orders",
			"source_files":[
				{"path":"part-000.parquet","size_bytes":128,"modified_at":"2026-05-13T00:00:00Z","transaction_id":"tx-1"},
				{"path":"part-001.parquet","size_bytes":256,"modified_at":"2026-05-13T00:01:00Z","transaction_id":"tx-2"}
			]
		}
	}`
	req := withRouteParam(authedReq(http.MethodPost, "/sources/"+source.String()+"/exports", body, owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.CreateDataExport(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created models.DataExport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.NotNil(t, created.FileExport)
	assert.Equal(t, "modified_since_last_success", created.FileExport.IncrementalPolicy)
	assert.Equal(t, "overwrite_existing", created.FileExport.OverwriteBehavior)
	assert.NotEmpty(t, created.FileExport.DestinationSubfolderGuidance)

	req = withRouteParam(authedReq(http.MethodPost, "/exports/"+created.ID.String()+"/run", "", owner), "id", created.ID.String())
	rec = httptest.NewRecorder()
	h.RunDataExport(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var firstRun models.DataExport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &firstRun))
	require.NotEmpty(t, firstRun.History)
	assert.EqualValues(t, 2, firstRun.History[0].FilesWritten)
	assert.EqualValues(t, 384, firstRun.History[0].BytesWritten)
	require.NotNil(t, firstRun.FileExport)
	require.NotNil(t, firstRun.FileExport.LastSuccessfulTransactionID)
	assert.Equal(t, "tx-2", *firstRun.FileExport.LastSuccessfulTransactionID)

	req = withRouteParam(authedReq(http.MethodPost, "/exports/"+created.ID.String()+"/run", "", owner), "id", created.ID.String())
	rec = httptest.NewRecorder()
	h.RunDataExport(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var secondRun models.DataExport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &secondRun))
	assert.EqualValues(t, 0, secondRun.History[0].FilesWritten)
	assert.EqualValues(t, 2, secondRun.History[0].FilesSkipped)

	req = withRouteParam(authedReq(http.MethodPatch, "/exports/"+created.ID.String(), `{"file_export":{"overwrite_behavior":"overwrite_existing","full_reexport_requested":true,"source_files":[{"path":"part-000.parquet","size_bytes":128,"modified_at":"2026-05-13T00:00:00Z","transaction_id":"tx-1"},{"path":"part-001.parquet","size_bytes":256,"modified_at":"2026-05-13T00:01:00Z","transaction_id":"tx-2"}]}}`, owner), "id", created.ID.String())
	rec = httptest.NewRecorder()
	h.UpdateDataExport(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	req = withRouteParam(authedReq(http.MethodPost, "/exports/"+created.ID.String()+"/run", "", owner), "id", created.ID.String())
	rec = httptest.NewRecorder()
	h.RunDataExport(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var fullRun models.DataExport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &fullRun))
	assert.EqualValues(t, 2, fullRun.History[0].FilesWritten)
	assert.True(t, fullRun.History[0].FullReexport)
	require.NotNil(t, fullRun.FileExport)
	assert.False(t, fullRun.FileExport.FullReexportRequested)
}

func TestCreateCdcSyncJobCapturesChangelogMetadata(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID
	body := `{
		"source_id":"` + source.String() + `",
		"capability_type":"cdc_sync",
		"output_kind":"stream",
		"output_stream_id":"stream://orders-cdc",
		"source_selector":"warehouse.public.orders",
		"source_table":"orders",
		"schema":[
			{"name":"order_id","source_type":"uuid","foundry_type":"String","nullable":false},
			{"name":"commit_lsn","source_type":"text","foundry_type":"String","nullable":false},
			{"name":"is_deleted","source_type":"boolean","foundry_type":"Boolean","nullable":false}
		],
		"cdc_sync":{
			"input_kind":"relational_connector",
			"source_database":"warehouse",
			"source_schema":"public",
			"source_table":"orders",
			"primary_key_columns":["order_id"],
			"ordering_column":"commit_lsn",
			"deletion_column":"is_deleted",
			"output_stream_id":"stream://orders-cdc",
			"output_stream_location":"stream://orders-cdc",
			"schema":[
				{"name":"order_id","source_type":"uuid","foundry_type":"String","nullable":false},
				{"name":"commit_lsn","source_type":"text","foundry_type":"String","nullable":false},
				{"name":"is_deleted","source_type":"boolean","foundry_type":"Boolean","nullable":false}
			],
			"start_position":"initial_snapshot",
			"source_database_cdc_enabled":true,
			"source_table_cdc_enabled":true,
			"changelog_input_validated":false,
			"connector_metadata":{"connector_type":"postgresql","snapshot_mode":"initial"}
		}
	}`
	req := authedReq(http.MethodPost, "/syncs", body, owner)
	rec := httptest.NewRecorder()
	h.CreateSyncJob(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created models.SyncJob
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "cdc_sync", created.CapabilityType)
	assert.Equal(t, "stream", created.OutputKind)
	require.NotNil(t, created.CdcSync)
	assert.Equal(t, []string{"order_id"}, created.CdcSync.PrimaryKeyColumns)
	assert.Equal(t, "commit_lsn", created.CdcSync.OrderingColumn)
	assert.Nil(t, created.OutputDatasetID)

	req = withRouteParam(authedReq(http.MethodPost, "/syncs/"+created.ID.String()+"/run", "", owner), "sync_id", created.ID.String())
	rec = httptest.NewRecorder()
	h.RunSyncJob(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "long-running runtime")
}

func TestCreateCdcSyncJobRequiresSourceChangelogReadiness(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID
	body := `{
		"source_id":"` + source.String() + `",
		"capability_type":"cdc_sync",
		"output_kind":"stream",
		"output_stream_id":"stream://orders-cdc",
		"cdc_sync":{
			"input_kind":"relational_connector",
			"source_table":"orders",
			"primary_key_columns":["order_id"],
			"ordering_column":"commit_lsn",
			"output_stream_location":"stream://orders-cdc",
			"schema":[
				{"name":"order_id","source_type":"uuid","foundry_type":"String","nullable":false},
				{"name":"commit_lsn","source_type":"text","foundry_type":"String","nullable":false}
			],
			"start_position":"initial_snapshot",
			"source_database_cdc_enabled":false,
			"source_table_cdc_enabled":false,
			"changelog_input_validated":false,
			"connector_metadata":{"connector_type":"postgresql"}
		}
	}`
	req := authedReq(http.MethodPost, "/syncs", body, owner)
	rec := httptest.NewRecorder()
	h.CreateSyncJob(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "source database must expose changelog data")
	assert.Contains(t, rec.Body.String(), "source table must expose changelog data")
}

func TestCreateListGetVirtualTable(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	sourceRID := "ri.foundry.main.source." + uuid.NewString()
	req := withRouteParam(authedReq("POST", "/sources/enable", `{"provider":"BIGQUERY"}`, owner), "source_rid", sourceRID)
	rec := httptest.NewRecorder()
	h.EnableVirtualTableSource(rec, req)
	require.Equal(t, 200, rec.Code)
	body := `{"project_rid":"ri.project.main","locator":{"kind":"tabular","database":"db","schema":"public","table":"orders"},"table_type":"TABLE"}`
	req = withRouteParam(authedReq("POST", "/sources/virtual-tables", body, owner), "source_rid", sourceRID)
	rec = httptest.NewRecorder()
	h.CreateVirtualTable(rec, req)
	require.Equal(t, 201, rec.Code)
	var created models.VirtualTable
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "orders", created.Name)
	req = authedReq("GET", "/virtual-tables", "", owner)
	rec = httptest.NewRecorder()
	h.ListVirtualTables(rec, req)
	require.Equal(t, 200, rec.Code)
	var list models.ListVirtualTablesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	req = withRouteParam(authedReq("GET", "/virtual-tables/"+created.RID, "", owner), "rid", created.RID)
	rec = httptest.NewRecorder()
	h.GetVirtualTable(rec, req)
	assert.Equal(t, 200, rec.Code)
}

func TestVirtualTableRegistrationStoresMetadataAndFilters(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	sourceRID := "ri.foundry.main.source." + uuid.NewString()
	req := withRouteParam(authedReq("POST", "/sources/enable", `{"provider":"SNOWFLAKE"}`, owner), "source_rid", sourceRID)
	rec := httptest.NewRecorder()
	h.EnableVirtualTableSource(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	body := `{
		"project_rid":"ri.project.finance",
		"parent_folder_rid":"ri.compass.main.folder.curated",
		"name":"Finance Orders",
		"owner":"finance-platform",
		"locator":{"kind":"tabular","database":"FINANCE","schema":"PUBLIC","table":"ORDERS"},
		"table_type":"TABLE",
		"schema_inferred":[{"name":"ORDER_ID","source_type":"NUMBER","inferred_type":"long","nullable":false}],
		"capabilities":{"read":true,"write":true,"incremental":true,"versioning":false,"compute_pushdown":"snowpark","snapshot_supported":true,"append_only_supported":true,"foundry_compute":{"python_single_node":true,"python_spark":true,"pipeline_builder_single_node":false,"pipeline_builder_spark":true}},
		"permissions":{"owners":["finance-platform"],"readers":["finance-analysts"],"writers":[],"admins":[]},
		"markings":["finance"]
	}`
	req = withRouteParam(authedReq("POST", "/sources/virtual-tables", body, owner), "source_rid", sourceRID)
	rec = httptest.NewRecorder()
	h.CreateVirtualTable(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created models.VirtualTable
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "Finance Orders", created.Name)
	assert.JSONEq(t, `[{"name":"ORDER_ID","source_type":"NUMBER","inferred_type":"long","nullable":false}]`, string(created.SchemaInferred))
	assert.JSONEq(t, `{"owners":["finance-platform"],"readers":["finance-analysts"],"writers":[],"admins":[]}`, string(readRawProperty(t, created.Properties, "permissions")))
	assert.JSONEq(t, `{"kind":"tabular","database":"FINANCE","schema":"PUBLIC","table":"ORDERS"}`, string(readRawProperty(t, created.Properties, "external_reference")))
	assert.Equal(t, "finance-platform", readStringProperty(t, created.Properties, "owner"))

	req = authedReq("GET", "/virtual-tables?name=finance&type=TABLE", "", owner)
	rec = httptest.NewRecorder()
	h.ListVirtualTables(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var list models.ListVirtualTablesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
}

func TestVirtualTableDiscoverBulkAndAutoRegistration(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	sourceRID := "ri.foundry.main.source." + uuid.NewString()
	req := withRouteParam(authedReq("POST", "/sources/enable", `{"provider":"BIGQUERY"}`, owner), "source_rid", sourceRID)
	rec := httptest.NewRecorder()
	h.EnableVirtualTableSource(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	req = withRouteParam(authedReq("GET", "/sources/"+sourceRID+"/virtual-tables/discover?path=analytics/public", "", owner), "source_rid", sourceRID)
	rec = httptest.NewRecorder()
	h.DiscoverVirtualTableCatalog(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "orders")

	bulk := `{"project_rid":"ri.project.bulk","entries":[
		{"locator":{"kind":"tabular","database":"analytics","schema":"public","table":"orders"},"table_type":"TABLE"},
		{"locator":{"kind":"tabular","database":"analytics","schema":"public","table":"customers"},"table_type":"TABLE"}
	]}`
	req = withRouteParam(authedReq("POST", "/sources/"+sourceRID+"/virtual-tables/bulk-register", bulk, owner), "source_rid", sourceRID)
	rec = httptest.NewRecorder()
	h.BulkRegisterVirtualTables(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var bulkResponse models.VirtualTableBulkRegisterResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &bulkResponse))
	require.Len(t, bulkResponse.Registered, 2)
	require.Empty(t, bulkResponse.Errors)

	auto := `{"project_name":"Warehouse mirror","folder_mirror_kind":"FLAT","table_tag_filters":[],"poll_interval_seconds":3600}`
	req = withRouteParam(authedReq("POST", "/sources/"+sourceRID+"/auto-registration", auto, owner), "source_rid", sourceRID)
	rec = httptest.NewRecorder()
	h.EnableVirtualTableAutoRegistration(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var link models.VirtualTableSourceLink
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &link))
	assert.True(t, link.AutoRegisterEnabled)
	assert.Equal(t, "FLAT", link.AutoRegisterFolderMirrorKind)

	req = withRouteParam(authedReq("POST", "/sources/"+sourceRID+"/auto-registration:scan-now", `{}`, owner), "source_rid", sourceRID)
	rec = httptest.NewRecorder()
	h.ScanVirtualTableAutoRegistrationNow(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"added":0`)

	req = withRouteParam(authedReq("DELETE", "/sources/"+sourceRID+"/auto-registration", "", owner), "source_rid", sourceRID)
	rec = httptest.NewRecorder()
	h.DisableVirtualTableAutoRegistration(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestVirtualTableQueryUsesAdapterAndReportsPushdown(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "bigquery"
	sourceRID := store.connections[0].ID.String()
	row := json.RawMessage(`{"ORDER_ID":1,"AMOUNT":42.5}`)
	registry := adapters.NewRegistry()
	registry.MustRegister("bigquery", adapters.SingletonFactory(testConnectionAdapter{
		queryResult: &adapters.Result{
			Mode:     "zero_copy",
			Columns:  []string{"ORDER_ID", "AMOUNT"},
			RowCount: 1,
			Rows:     []json.RawMessage{row},
			Metadata: json.RawMessage(`{"adapter":"test_bigquery"}`),
		},
	}))
	h := &handlers.Handlers{Repo: store, AdapterRegistry: registry}

	req := withRouteParam(authedReq("POST", "/sources/enable", `{"provider":"BIGQUERY"}`, owner), "source_rid", sourceRID)
	rec := httptest.NewRecorder()
	h.EnableVirtualTableSource(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := `{
		"project_rid":"ri.project.query",
		"locator":{"kind":"tabular","database":"analytics","schema":"public","table":"orders"},
		"table_type":"TABLE",
		"schema_inferred":[{"name":"ORDER_ID","source_type":"INTEGER","inferred_type":"integer","nullable":false},{"name":"AMOUNT","source_type":"NUMERIC","inferred_type":"decimal","nullable":true}],
		"capabilities":{"read":true,"write":true,"incremental":true,"versioning":false,"compute_pushdown":"ibis","snapshot_supported":true,"append_only_supported":true,"foundry_compute":{"python_single_node":true,"python_spark":true,"pipeline_builder_single_node":false,"pipeline_builder_spark":true}}
	}`
	req = withRouteParam(authedReq("POST", "/sources/virtual-tables", body, owner), "source_rid", sourceRID)
	rec = httptest.NewRecorder()
	h.CreateVirtualTable(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created models.VirtualTable
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	query := `{"columns":["ORDER_ID","AMOUNT"],"filters":["AMOUNT > 10"],"limit":25}`
	req = withRouteParam(authedReq("POST", "/virtual-tables/"+created.RID+"/query", query, owner), "rid", created.RID)
	rec = httptest.NewRecorder()
	h.QueryVirtualTable(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var response models.VirtualTableQueryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Equal(t, "analytics.public.orders", response.Selector)
	assert.Equal(t, "source_system", response.ComputeLocation)
	require.NotNil(t, response.Pushdown)
	assert.False(t, response.Pushdown.UsesCopiedDataset)
	assert.True(t, response.Pushdown.DirectQuery)
	assert.Contains(t, response.Pushdown.PushedOperations, "filter")
	assert.Empty(t, response.Pushdown.FoundryOperations)
	require.Len(t, response.Rows, 1)
	assert.JSONEq(t, `{"ORDER_ID":1,"AMOUNT":42.5}`, string(response.Rows[0]))
	assert.Contains(t, string(response.Metadata), `"uses_copied_dataset":false`)
	assert.Contains(t, string(response.Metadata), `"adapter":"test_bigquery"`)
	assert.NotEmpty(t, response.Limitations)
}

func TestVirtualTableQueryAdapterUnavailableReturnsExplicitError(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	sourceRID := store.connections[0].ID.String()
	h := &handlers.Handlers{Repo: store, AdapterRegistry: adapters.NewRegistry()}

	req := withRouteParam(authedReq("POST", "/sources/enable", `{"provider":"AMAZON_S3"}`, owner), "source_rid", sourceRID)
	rec := httptest.NewRecorder()
	h.EnableVirtualTableSource(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := `{"project_rid":"ri.project.query","locator":{"kind":"file","bucket":"warehouse","prefix":"orders","format":"parquet"},"table_type":"PARQUET_FILES","schema_inferred":[{"name":"path","source_type":"STRING","inferred_type":"string","nullable":false}],"capabilities":{"read":true,"write":true,"incremental":false,"versioning":false,"compute_pushdown":null,"snapshot_supported":false,"append_only_supported":false,"foundry_compute":{"python_single_node":false,"python_spark":false,"pipeline_builder_single_node":false,"pipeline_builder_spark":false}}}`
	req = withRouteParam(authedReq("POST", "/sources/virtual-tables", body, owner), "source_rid", sourceRID)
	rec = httptest.NewRecorder()
	h.CreateVirtualTable(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created models.VirtualTable
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	req = withRouteParam(authedReq("POST", "/virtual-tables/"+created.RID+"/query", `{"requires_foundry_compute":true,"limit":2}`, owner), "rid", created.RID)
	rec = httptest.NewRecorder()
	h.QueryVirtualTable(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "virtual table adapter unavailable for real preview")
}

func TestVirtualTableQueryErrNotImplementedReturnsServiceUnavailable(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "bigquery"
	sourceRID := store.connections[0].ID.String()
	registry := adapters.NewRegistry()
	registry.MustRegister("bigquery", adapters.SingletonFactory(testConnectionAdapter{queryErr: adapters.ErrNotImplemented}))
	h := &handlers.Handlers{Repo: store, AdapterRegistry: registry}

	req := withRouteParam(authedReq("POST", "/sources/enable", `{"provider":"BIGQUERY"}`, owner), "source_rid", sourceRID)
	rec := httptest.NewRecorder()
	h.EnableVirtualTableSource(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := `{"project_rid":"ri.project.query","locator":{"kind":"tabular","database":"analytics","schema":"public","table":"orders"},"table_type":"TABLE","schema_inferred":[{"name":"ORDER_ID","source_type":"INTEGER","inferred_type":"integer","nullable":false}],"capabilities":{"read":true,"write":true,"incremental":true,"versioning":false,"snapshot_supported":true,"append_only_supported":true}}`
	req = withRouteParam(authedReq("POST", "/sources/virtual-tables", body, owner), "source_rid", sourceRID)
	rec = httptest.NewRecorder()
	h.CreateVirtualTable(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created models.VirtualTable
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	req = withRouteParam(authedReq("POST", "/virtual-tables/"+created.RID+"/query", `{"limit":2}`, owner), "rid", created.RID)
	rec = httptest.NewRecorder()
	h.QueryVirtualTable(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "virtual table adapter unavailable for real preview")
}

func TestVirtualTableMetadataPreviewIsExplicitlyDegraded(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	sourceRID := "ri.foundry.main.source." + uuid.NewString()
	store.links[sourceRID] = models.VirtualTableSourceLink{SourceRID: sourceRID, Provider: "METADATA", VirtualTablesEnabled: true}
	created, err := store.CreateVirtualTable(context.Background(), sourceRID, owner.String(), &models.CreateVirtualTableRequest{
		ProjectRID:     "ri.project.metadata",
		Locator:        models.Locator{Kind: "tabular", Database: "analytics", Schema: "public", Table: "orders"},
		TableType:      "TABLE",
		SchemaInferred: json.RawMessage(`[{"name":"ORDER_ID","source_type":"INTEGER","inferred_type":"integer","nullable":false}]`),
		Capabilities:   json.RawMessage(`{"read":true}`),
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	req := withRouteParam(authedReq("POST", "/virtual-tables/"+created.RID+"/query", `{"limit":2}`, owner), "rid", created.RID)
	rec := httptest.NewRecorder()
	h.QueryVirtualTable(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var response models.VirtualTableQueryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.True(t, response.Degraded)
	assert.Equal(t, "metadata", response.Source)
	require.NotEmpty(t, response.Limitations)
	assert.Equal(t, 2, response.RowCount)
	assert.Contains(t, string(response.Metadata), `"source":"metadata"`)
	assert.Contains(t, string(response.Metadata), "no remote source data was read")
}

func TestVirtualTableUpdateDetectionSkipsUnchangedDownstreamBuildsAndShowsLineage(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	sourceRID := "ri.foundry.main.source." + uuid.NewString()
	req := withRouteParam(authedReq("POST", "/sources/enable", `{"provider":"DATABRICKS"}`, owner), "source_rid", sourceRID)
	rec := httptest.NewRecorder()
	h.EnableVirtualTableSource(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	body := `{
		"project_rid":"ri.project.lineage",
		"name":"Orders VT",
		"locator":{"kind":"tabular","database":"main","schema":"sales","table":"orders"},
		"table_type":"MANAGED_DELTA",
		"schema_inferred":[{"name":"ORDER_ID","source_type":"BIGINT","inferred_type":"long","nullable":false}],
		"capabilities":{"read":true,"write":false,"incremental":true,"versioning":true,"compute_pushdown":"pyspark","snapshot_supported":true,"append_only_supported":true,"foundry_compute":{"python_single_node":true,"python_spark":true,"pipeline_builder_single_node":false,"pipeline_builder_spark":true}},
		"properties":{"source_version":"delta-version-7"}
	}`
	req = withRouteParam(authedReq("POST", "/sources/virtual-tables", body, owner), "source_rid", sourceRID)
	rec = httptest.NewRecorder()
	h.CreateVirtualTable(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created models.VirtualTable
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	req = withRouteParam(authedReq("PATCH", "/virtual-tables/"+created.RID+"/update-detection", `{"enabled":true,"interval_seconds":3600}`, owner), "rid", created.RID)
	rec = httptest.NewRecorder()
	h.SetVirtualTableUpdateDetection(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var updated models.VirtualTable
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	assert.True(t, updated.UpdateDetectionEnabled)

	req = withRouteParam(authedReq("POST", "/virtual-tables/"+created.RID+"/update-detection:poll-now", `{}`, owner), "rid", created.RID)
	rec = httptest.NewRecorder()
	h.PollVirtualTableUpdateDetectionNow(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var first models.PollResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &first))
	assert.Equal(t, models.PollOutcomeInitial, first.Outcome)
	assert.True(t, first.EventEmitted)
	require.NotEmpty(t, first.DownstreamBuilds)
	assert.Equal(t, "triggered", first.DownstreamBuilds[0].Action)

	req = withRouteParam(authedReq("POST", "/virtual-tables/"+created.RID+"/update-detection:poll-now", `{}`, owner), "rid", created.RID)
	rec = httptest.NewRecorder()
	h.PollVirtualTableUpdateDetectionNow(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var second models.PollResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &second))
	assert.Equal(t, models.PollOutcomeUnchanged, second.Outcome)
	assert.False(t, second.EventEmitted)
	require.NotEmpty(t, second.DownstreamBuilds)
	assert.Equal(t, "skipped", second.DownstreamBuilds[0].Action)
	assert.Contains(t, second.DownstreamBuilds[0].Reason, "unchanged")

	req = withRouteParam(authedReq("GET", "/virtual-tables/"+created.RID+"/update-detection/history", "", owner), "rid", created.RID)
	rec = httptest.NewRecorder()
	h.ListVirtualTableUpdateDetectionHistory(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "delta-version-7")

	req = withRouteParam(authedReq("GET", "/virtual-tables/"+created.RID+"/lineage", "", owner), "rid", created.RID)
	rec = httptest.NewRecorder()
	h.GetVirtualTableLineage(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var lineage models.VirtualTableLineageResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &lineage))
	assert.True(t, lineage.UpdateDetectionEnabled)
	require.NotEmpty(t, lineage.DownstreamBuilds)
	assert.Equal(t, "skipped", lineage.DownstreamBuilds[0].Action)
	assert.Contains(t, rec.Body.String(), "pipeline")
	assert.Contains(t, rec.Body.String(), "dataset")
	assert.Contains(t, rec.Body.String(), "object_type")
}

func TestSyncAndVirtualValidationErrors(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	req := authedReq("POST", "/syncs", `{}`, owner)
	rec := httptest.NewRecorder()
	h.CreateSyncJob(rec, req)
	assert.Equal(t, 400, rec.Code)
	req = withRouteParam(authedReq("POST", "/sources/virtual-tables", `{"project_rid":"p","locator":{"kind":"bad"},"table_type":"TABLE"}`, owner), "source_rid", "missing")
	rec = httptest.NewRecorder()
	h.CreateVirtualTable(rec, req)
	assert.Equal(t, 404, rec.Code)
}

func TestSyncAndVirtualAuthTenantIsolation(t *testing.T) {
	owner := uuid.New()
	intruder := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID
	out := uuid.New()
	created, err := store.CreateSyncJob(context.Background(), &models.CreateSyncJobRequest{SourceID: source, OutputDatasetID: &out}, owner)
	require.NoError(t, err)
	req := withRouteParam(authedReq("GET", "/syncs/"+created.ID.String(), "", intruder), "sync_id", created.ID.String())
	rec := httptest.NewRecorder()
	h.GetSyncJob(rec, req)
	assert.Equal(t, 404, rec.Code)
	req = httptest.NewRequest("GET", "/virtual-tables", nil)
	rec = httptest.NewRecorder()
	h.ListVirtualTables(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestCreateListGetUpdateMediaSetSync(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "s3" // SDC.41: media sync requires a supported connector
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID
	body := `{"kind":"MEDIA_SET_SYNC","target_media_set_rid":"ri.foundry.main.media_set.` + uuid.NewString() + `","subfolder":"images","filters":{"path_glob":"*.png","file_size_limit":1024},"schedule_cron":"0 * * * *"}`
	req := withRouteParam(authedReq("POST", "/sources/"+source.String()+"/media-set-syncs", body, owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.CreateMediaSetSync(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created models.MediaSetSync
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, source, created.SourceID)
	assert.Equal(t, models.MediaSetSyncKindCopy, created.Kind)

	req = withRouteParam(authedReq("GET", "/sources/"+source.String()+"/media-set-syncs", "", owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.ListMediaSetSyncs(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var list []models.MediaSetSyncWithUsage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)

	req = withRouteParam(authedReq("GET", "/media-set-syncs/"+created.ID.String(), "", owner), "sync_id", created.ID.String())
	rec = httptest.NewRecorder()
	h.GetMediaSetSync(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	patch := `{"kind":"VIRTUAL_MEDIA_SET_SYNC","subfolder":"archive"}`
	req = withRouteParam(authedReq("PATCH", "/media-set-syncs/"+created.ID.String(), patch, owner), "sync_id", created.ID.String())
	rec = httptest.NewRecorder()
	h.UpdateMediaSetSync(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var updated models.MediaSetSync
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	assert.Equal(t, models.MediaSetSyncKindVirtual, updated.Kind)
	assert.Equal(t, "archive", updated.Subfolder)
}

func TestMediaSetSyncValidationErrors(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID
	req := withRouteParam(authedReq("POST", "/sources/"+source.String()+"/media-set-syncs", `{"kind":"MEDIA_SET_SYNC","target_media_set_rid":"bad","filters":{"file_size_limit":0},"schedule_cron":"bad cron"}`, owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.CreateMediaSetSync(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "target_media_set_rid")
	assert.Contains(t, rec.Body.String(), "file_size_limit")

	created, err := store.CreateMediaSetSync(context.Background(), source, &models.CreateMediaSetSyncRequest{Kind: models.MediaSetSyncKindCopy, TargetMediaSetRID: "ri.foundry.main.media_set." + uuid.NewString()}, owner)
	require.NoError(t, err)
	badKind := `{"kind":"BAD"}`
	req = withRouteParam(authedReq("PATCH", "/media-set-syncs/"+created.ID.String(), badKind, owner), "sync_id", created.ID.String())
	rec = httptest.NewRecorder()
	h.UpdateMediaSetSync(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMediaSetSyncAuthTenantIsolation(t *testing.T) {
	owner := uuid.New()
	intruder := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "s3"
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID
	created, err := store.CreateMediaSetSync(context.Background(), source, &models.CreateMediaSetSyncRequest{Kind: models.MediaSetSyncKindCopy, TargetMediaSetRID: "ri.foundry.main.media_set." + uuid.NewString()}, owner)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/sources/"+source.String()+"/media-set-syncs", nil)
	req = withRouteParam(req, "id", source.String())
	rec := httptest.NewRecorder()
	h.ListMediaSetSyncs(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	req = withRouteParam(authedReq("GET", "/media-set-syncs/"+created.ID.String(), "", intruder), "sync_id", created.ID.String())
	rec = httptest.NewRecorder()
	h.GetMediaSetSync(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	req = withRouteParam(authedReq("POST", "/sources/"+source.String()+"/media-set-syncs", `{"kind":"MEDIA_SET_SYNC","target_media_set_rid":"ri.foundry.main.media_set.x"}`, intruder), "id", source.String())
	rec = httptest.NewRecorder()
	h.CreateMediaSetSync(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateMediaSetSyncRejectsUnsupportedConnector(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	// default connector is postgresql which is not media-sync supported
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID
	body := `{"kind":"MEDIA_SET_SYNC","target_media_set_rid":"ri.foundry.main.media_set.` + uuid.NewString() + `","subfolder":"images"}`
	req := withRouteParam(authedReq("POST", "/sources/"+source.String()+"/media-set-syncs", body, owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.CreateMediaSetSync(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "connector does not support media set sync")
	assert.Contains(t, rec.Body.String(), "postgresql")
}

func TestRunMediaSetSyncPersistsHistoryAndListsRuns(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "s3"
	source := store.connections[0].ID
	created, err := store.CreateMediaSetSync(context.Background(), source, &models.CreateMediaSetSyncRequest{
		Kind: models.MediaSetSyncKindCopy, TargetMediaSetRID: "ri.foundry.main.media_set." + uuid.NewString(),
	}, owner)
	require.NoError(t, err)
	runtime := &fakeRuntime{report: &models.MediaSetSyncExecutionReport{
		Stats:      models.SyncStats{Accepted: 2, Skipped: 1, SchemaMismatched: 0},
		Dispatched: 2,
	}}
	h := &handlers.Handlers{Repo: store, MediaSetRuntime: runtime}

	runBody := `{"source_files":[{"path":"a.png","size_bytes":100,"mime_type":"image/png"},{"path":"b.png","size_bytes":200,"mime_type":"image/png"}]}`
	req := withRouteParam(authedReq("POST", "/media-set-syncs/"+created.ID.String()+"/run", runBody, owner), "sync_id", created.ID.String())
	rec := httptest.NewRecorder()
	h.RunMediaSetSync(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	// History endpoint surfaces the persisted run.
	req = withRouteParam(authedReq("GET", "/media-set-syncs/"+created.ID.String()+"/runs", "", owner), "sync_id", created.ID.String())
	rec = httptest.NewRecorder()
	h.ListMediaSetSyncRuns(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var history models.ListResponse[models.MediaSetSyncRun]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &history))
	require.Len(t, history.Items, 1)
	assert.Equal(t, models.MediaSetSyncRunStatusSucceeded, history.Items[0].Status)
	assert.ElementsMatch(t, []string{"a.png", "b.png"}, history.Items[0].SelectedPaths)
	assert.Equal(t, uint32(2), history.Items[0].AcceptedFiles)

	// Listing media set syncs now exposes the usage rollup.
	req = withRouteParam(authedReq("GET", "/sources/"+source.String()+"/media-set-syncs", "", owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.ListMediaSetSyncs(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var list []models.MediaSetSyncWithUsage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)
	require.NotNil(t, list[0].Usage)
	assert.Equal(t, uint32(1), list[0].Usage.RunCount)
}

func TestRunMediaSetSyncRecordsFailedRunOnRuntimeError(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "s3"
	source := store.connections[0].ID
	created, err := store.CreateMediaSetSync(context.Background(), source, &models.CreateMediaSetSyncRequest{
		Kind: models.MediaSetSyncKindCopy, TargetMediaSetRID: "ri.foundry.main.media_set." + uuid.NewString(),
	}, owner)
	require.NoError(t, err)
	rt := &fakeRuntime{err: &handlers.RuntimeError{Kind: handlers.RuntimeDispatch, Msg: "media-sets-service unavailable"}}
	h := &handlers.Handlers{Repo: store, MediaSetRuntime: rt}

	req := withRouteParam(authedReq("POST", "/media-set-syncs/"+created.ID.String()+"/run", `{"source_files":[{"path":"a.png","size_bytes":10,"mime_type":"image/png"}]}`, owner), "sync_id", created.ID.String())
	rec := httptest.NewRecorder()
	h.RunMediaSetSync(rec, req)
	assert.Equal(t, http.StatusBadGateway, rec.Code)

	// The failed run is still persisted with the runtime error.
	req = withRouteParam(authedReq("GET", "/media-set-syncs/"+created.ID.String()+"/runs", "", owner), "sync_id", created.ID.String())
	rec = httptest.NewRecorder()
	h.ListMediaSetSyncRuns(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var history models.ListResponse[models.MediaSetSyncRun]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &history))
	require.Len(t, history.Items, 1)
	assert.Equal(t, models.MediaSetSyncRunStatusFailed, history.Items[0].Status)
	require.NotNil(t, history.Items[0].ErrorMessage)
	assert.Contains(t, *history.Items[0].ErrorMessage, "media-sets-service unavailable")
}

func TestRunMediaSetSyncRuntimeErrorMapping(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	source := store.connections[0].ID
	created, err := store.CreateMediaSetSync(context.Background(), source, &models.CreateMediaSetSyncRequest{Kind: models.MediaSetSyncKindCopy, TargetMediaSetRID: "ri.foundry.main.media_set." + uuid.NewString()}, owner)
	require.NoError(t, err)
	rt := &fakeRuntime{err: &handlers.RuntimeError{Kind: handlers.RuntimeDispatch, Msg: "media-sets-service returned HTTP 500"}}
	h := &handlers.Handlers{Repo: store, MediaSetRuntime: rt}

	req := withRouteParam(authedReq("POST", "/media-set-syncs/"+created.ID.String()+"/run", `{"source_files":[{"path":"a.png","size_bytes":1,"mime_type":"image/png"}]}`, owner), "sync_id", created.ID.String())
	rec := httptest.NewRecorder()
	h.RunMediaSetSync(rec, req)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.True(t, rt.called)
}

func TestRunMediaSetSyncRuntimeSuccess(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	source := store.connections[0].ID
	created, err := store.CreateMediaSetSync(context.Background(), source, &models.CreateMediaSetSyncRequest{Kind: models.MediaSetSyncKindCopy, TargetMediaSetRID: "ri.foundry.main.media_set." + uuid.NewString()}, owner)
	require.NoError(t, err)
	h := &handlers.Handlers{Repo: store, MediaSetRuntime: &fakeRuntime{report: &models.MediaSetSyncExecutionReport{Stats: models.SyncStats{Accepted: 1}, Dispatched: 1}}}

	req := withRouteParam(authedReq("POST", "/media-set-syncs/"+created.ID.String()+"/run", `{"source_files":[{"path":"a.png","size_bytes":1,"mime_type":"image/png"}]}`, owner), "sync_id", created.ID.String())
	rec := httptest.NewRecorder()
	h.RunMediaSetSync(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), "dispatched")
}

func TestHTTPMediaSetRuntimeDispatchesAcceptedFiles(t *testing.T) {
	seen := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	limit := uint64(10)
	glob := "*.png"
	sync := &models.MediaSetSync{
		Kind:              models.MediaSetSyncKindCopy,
		TargetMediaSetRID: "ri.foundry.main.media_set.x",
		Filters: models.MediaSetSyncFilters{
			PathGlob:      &glob,
			FileSizeLimit: &limit,
		},
	}
	req := &models.RunMediaSetSyncRequest{SourceFiles: []models.SourceFile{
		{Path: "ok.png", SizeBytes: 1, MimeType: "image/png"},
		{Path: "too-large.png", SizeBytes: 11, MimeType: "image/png"},
		{Path: "notes.txt", SizeBytes: 1, MimeType: "text/plain"},
	}, AllowedMIMETypes: []string{"image/png"}}
	rt := &handlers.HTTPMediaSetRuntime{MediaSetsBaseURL: srv.URL, Client: srv.Client()}
	report, err := rt.ExecuteMediaSetSync(context.Background(), sync, req, "Bearer token")
	require.NoError(t, err)
	require.Equal(t, uint32(1), report.Dispatched)
	require.Equal(t, uint32(1), report.Stats.Accepted)
	require.Equal(t, uint32(2), report.Stats.Skipped)
	require.Equal(t, []string{"/media-sets/ri.foundry.main.media_set.x/items/upload-url"}, seen)
}

func TestCatalogSurfaceMatchesGoldenFixtures(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	cases := []struct {
		name   string
		handle http.HandlerFunc
		golden string
	}{
		{name: "catalog", handle: h.GetConnectorCatalog, golden: "testdata/catalog.golden.json"},
		{name: "contracts", handle: h.GetConnectorContracts, golden: "testdata/contracts.golden.json"},
		{name: "streaming_sources", handle: h.ListStreamingSources, golden: "testdata/streaming_sources.golden.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.handle(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			require.Equal(t, http.StatusOK, rec.Code)
			assertJSONGolden(t, tc.golden, rec.Body.Bytes())
		})
	}
}

func TestCatalogIncludesAllRustConnectorModules(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	(&handlers.Handlers{}).GetConnectorContracts(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var catalog models.ConnectorContractCatalog
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &catalog))
	byType := map[string]models.ConnectorContractProfile{}
	for _, connector := range catalog.Connectors {
		byType[connector.ConnectorType] = connector
	}
	for _, connectorType := range []string{"azure_blob", "bigquery", "csv", "databricks", "excel", "gcs", "generic", "graphql", "iot", "jdbc", "json", "kafka", "kinesis", "ldap", "mssql", "mysql", "odbc", "onelake", "open_table_catalog", "oracle", "parquet", "postgresql", "power_bi", "rest_api", "s3", "salesforce", "sap", "sftp", "snowflake", "tableau"} {
		require.Contains(t, byType, connectorType)
	}
}

func TestConnectorCapabilityMatrixEndpointIncludesImplementedAndLimitedConnectors(t *testing.T) {
	rec := httptest.NewRecorder()
	(&handlers.Handlers{}).GetConnectorCapabilityMatrix(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		CapabilityMatrix []models.ConnectorCapabilityMatrix `json:"capability_matrix"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	byType := map[string]models.ConnectorCapabilityMatrix{}
	for _, capability := range body.CapabilityMatrix {
		byType[capability.ConnectorType] = capability
	}

	mysql := byType["mysql"]
	assert.True(t, mysql.DiscoverSources)
	assert.True(t, mysql.QueryVirtualTable)
	assert.True(t, mysql.StreamArrow)
	assert.True(t, mysql.BuildIngestSpec)

	ldap := byType["ldap"]
	assert.False(t, ldap.DiscoverSources)
	assert.False(t, ldap.QueryVirtualTable)
	assert.NotEmpty(t, ldap.Limitations)

	oracle := byType["oracle"]
	assert.True(t, oracle.DiscoverSources)
	assert.True(t, oracle.QueryVirtualTable)
	assert.False(t, oracle.StreamArrow)
	assert.False(t, oracle.BuildIngestSpec)
	assert.NotEmpty(t, oracle.Limitations)
}

func TestConnectionCapabilitiesCombineContractConfigAndPolicy(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "snowflake"
	store.connections[0].Config = json.RawMessage(`{"account":"acct","private_key":"pk","cursor_field":"updated_at","zero_copy":true}`)
	h := &handlers.Handlers{Repo: store, Config: handlers.RuntimeConfig{AllowedEgressHosts: []string{"snowflake.example.com"}}}

	r := chi.NewRouter()
	r.Get("/api/v1/data-connection/sources/{id}/capabilities", h.GetConnectionCapabilities)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/data-connection/sources/"+store.connections[0].ID.String()+"/capabilities", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got models.ConnectionCapabilityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "snowflake", got.ConnectorType)
	require.Equal(t, "warehouse_zero_copy", got.Contract.TemplateFamily)
	require.True(t, got.Capabilities.SupportsZeroCopy)
	require.True(t, got.Capabilities.SupportsIncremental)
	require.True(t, got.Capabilities.ConfigInferred.HasPrivateKey)
	require.True(t, got.Capabilities.ConfigInferred.HasIncrementalCursor)
	require.True(t, got.Capabilities.PrivateNetworkEgressAllowed)
	require.False(t, got.Capabilities.RequiresPrivateNetworkAgent)
	require.Contains(t, got.Capabilities.Workers, "agent")
	require.Contains(t, got.Capabilities.ConfigKeys, "private_key")
}

func assertJSONGolden(t *testing.T, golden string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	var wantJSON any
	var gotJSON any
	require.NoError(t, json.Unmarshal(want, &wantJSON))
	require.NoError(t, json.Unmarshal(got, &gotJSON))
	assert.Equal(t, wantJSON, gotJSON)
}

func TestRegistrationHandlerFlow(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	source := store.connections[0].ID
	store.connections[0].Config = json.RawMessage(`{"tables":[{"selector":"public.orders","display_name":"Orders","source_kind":"table","supports_zero_copy":true}]}`)
	h := &handlers.Handlers{Repo: store}

	req := withRouteParam(authedReq(http.MethodPost, "/discover", ``, owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.DiscoverRegistrations(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "public.orders")

	body := `{"registrations":[{"selector":"public.orders","registration_mode":"zero_copy","auto_sync":true}]}`
	req = withRouteParam(authedReq(http.MethodPost, "/bulk/preview", body, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.BulkRegisterPreview(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "matched")

	req = withRouteParam(authedReq(http.MethodPost, "/bulk", body, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.BulkRegister(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	reg := store.registrations[source][0]

	req = withRouteParam(authedReq(http.MethodGet, "/registrations", ``, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.ListRegistrations(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	req = withRouteParam(withRouteParam(authedReq(http.MethodPost, "/query", `{"limit":1}`, owner), "source_id", source.String()), "registration_id", reg.ID.String())
	rec = httptest.NewRecorder()
	h.QueryRegistration(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	req = withRouteParam(withRouteParam(authedReq(http.MethodPost, "/query/arrow", `{"limit":1}`, owner), "source_id", source.String()), "registration_id", reg.ID.String())
	rec = httptest.NewRecorder()
	h.QueryRegistrationArrow(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/vnd.apache.arrow.stream", rec.Header().Get("Content-Type"))

	req = withRouteParam(withRouteParam(authedReq(http.MethodDelete, "/registrations/"+reg.ID.String(), ``, owner), "source_id", source.String()), "registration_id", reg.ID.String())
	rec = httptest.NewRecorder()
	h.DeleteRegistration(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestAutoRegistrationHandlers(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	source := store.connections[0].ID
	h := &handlers.Handlers{Repo: store}

	req := withRouteParam(authedReq(http.MethodPut, "/auto", `{"enabled":true,"registration_mode":"sync","selectors":["pg"]}`, owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.UpdateAutoRegistration(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	req = withRouteParam(authedReq(http.MethodGet, "/auto/status", ``, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.AutoRegisterStatus(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "settings")

	req = withRouteParam(authedReq(http.MethodPost, "/auto", `{}`, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.AutoRegister(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotEmpty(t, store.registrations[source])
}

func TestConnectionWebhookAndIcebergHandlers(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID

	req := withRouteParam(authedReq(http.MethodPost, "/test", ``, owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.TestConnection(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output_parameters":{"ok":true}}`))
	}))
	defer srv.Close()
	store.connections[0].ConnectorType = "webhook"
	store.connections[0].Config = json.RawMessage(`{"url":"` + srv.URL + `","method":"POST"}`)
	req = withRouteParam(authedReq(http.MethodPost, "/webhooks/"+source.String()+"/invoke", `{"inputs":{"x":1}}`, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.InvokeWebhook(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "output_parameters")

	srvWeather := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/forecast", r.URL.Path)
		if r.URL.Query().Has("latitude") {
			assert.Equal(t, "40.016353", r.URL.Query().Get("latitude"))
			assert.Equal(t, "-105.34458", r.URL.Query().Get("longitude"))
			assert.Equal(t, "temperature_2m,wind_speed_10m,relative_humidity_2m", r.URL.Query().Get("current"))
		}
		_, _ = w.Write([]byte(`{"current":{"temperature_2m":84,"wind_speed_10m":4.8,"relative_humidity_2m":62}}`))
	}))
	defer srvWeather.Close()
	store.connections[0].ConnectorType = "rest_api"
	store.connections[0].Config = json.RawMessage(`{
		"base_url":"` + srvWeather.URL + `",
		"auth":{"type":"none"},
		"runtime":{"worker":"foundry","timeout_ms":5000},
		"permissions":{"invokable":true},
		"webhook":{
			"method":"GET",
			"path":"/v1/forecast",
			"inputs":[
				{"id":"latitude","type":"number","required":true},
				{"id":"longitude","type":"number","required":true}
			],
			"calls":[{
				"id":"weather",
				"method":"GET",
				"path":"/v1/forecast",
				"query_params":{
					"latitude":"{{latitude}}",
					"longitude":"{{longitude}}",
					"current":"temperature_2m,wind_speed_10m,relative_humidity_2m"
				}
			}],
			"outputs":[
				{"id":"temperature","type":"number","extractor":{"from_call":"weather","path":"/current/temperature_2m"}},
				{"id":"wind_speed","type":"number","extractor":{"from_call":"weather","path":"/current/wind_speed_10m"}},
				{"id":"humidity","type":"number","extractor":{"from_call":"weather","path":"/current/relative_humidity_2m"}}
			],
			"history":{"enabled":true,"retention_days":14,"store_outputs":true}
		}
	}`)
	req = withRouteParam(authedReq(http.MethodPost, "/webhooks/"+source.String()+"/invoke", `{"inputs":{"latitude":40.016353,"longitude":-105.34458}}`, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.InvokeWebhook(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"temperature":84`)
	assert.Contains(t, rec.Body.String(), `"wind_speed":4.8`)
	assert.Contains(t, rec.Body.String(), `"humidity":62`)
	var weatherResp models.InvokeWebhookResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &weatherResp))
	var weatherHistory map[string]any
	require.NoError(t, json.Unmarshal(weatherResp.History, &weatherHistory))
	assert.Equal(t, "succeeded", weatherHistory["status"])
	assert.Equal(t, true, weatherHistory["stored"])
	assert.NotEmpty(t, weatherHistory["entry_id"])

	req = withRouteParam(authedReq(http.MethodGet, "/webhooks/"+source.String()+"/history?limit=10", ``, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.ListWebhookHistory(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var historyList models.ListResponse[models.WebhookHistoryEntry]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &historyList))
	require.NotEmpty(t, historyList.Items)
	weatherEntry := historyList.Items[0]
	assert.Equal(t, source, weatherEntry.SourceID)
	assert.Equal(t, owner, weatherEntry.UserID)
	assert.Equal(t, "succeeded", weatherEntry.Status)
	assert.NotNil(t, weatherEntry.HTTPStatus)
	assert.Equal(t, uint16(http.StatusOK), *weatherEntry.HTTPStatus)
	assert.True(t, weatherEntry.InputPolicy.StoreOutputs)
	assert.False(t, weatherEntry.InputPolicy.StoreInputs)
	assert.Nil(t, weatherEntry.Inputs)
	assert.JSONEq(t, `{"temperature":84,"wind_speed":4.8,"humidity":62}`, string(weatherEntry.OutputParameters))
	assert.WithinDuration(t, time.Now().UTC().Add(14*24*time.Hour), weatherEntry.RetentionExpiresAt, time.Minute)

	store.connections[0].Config = json.RawMessage(`{
		"base_url":"` + srvWeather.URL + `",
		"auth":{"type":"none"},
		"runtime":{"worker":"foundry","allowed_methods":["GET"]},
		"permissions":{"invokable":false},
		"webhook":{"method":"GET","path":"/v1/forecast"}
	}`)
	req = withRouteParam(authedReq(http.MethodPost, "/webhooks/"+source.String()+"/invoke", `{"inputs":{}}`, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.InvokeWebhook(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	store.connections[0].Config = json.RawMessage(`{
		"base_url":"` + srvWeather.URL + `",
		"auth":{"type":"none"},
		"runtime":{"worker":"foundry","allowed_methods":["POST"]},
		"permissions":{"invokable":true},
		"webhook":{"method":"GET","path":"/v1/forecast"}
	}`)
	req = withRouteParam(authedReq(http.MethodPost, "/webhooks/"+source.String()+"/invoke", `{"inputs":{}}`, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.InvokeWebhook(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "method GET")

	req = withRouteParam(authedReq(http.MethodGet, "/webhooks/"+source.String()+"/history?limit=1", ``, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.ListWebhookHistory(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &historyList))
	require.Len(t, historyList.Items, 1)
	assert.Equal(t, "failed", historyList.Items[0].Status)
	require.NotNil(t, historyList.Items[0].Error)
	assert.Contains(t, *historyList.Items[0].Error, "method GET")
	assert.Nil(t, historyList.Items[0].Inputs)

	srvLarge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "super-secret", r.URL.Query().Get("api_key"))
		_, _ = w.Write([]byte(`{"large":"this payload is intentionally larger than the configured limit"}`))
	}))
	defer srvLarge.Close()
	store.connections[0].Config = json.RawMessage(`{
		"base_url":"` + srvLarge.URL + `",
		"auth":{"type":"api_key","query_param":"api_key","value":"super-secret"},
		"runtime":{"worker":"foundry","allowed_methods":["GET"]},
		"permissions":{"invokable":true},
		"webhook":{
			"method":"GET",
			"path":"/v1/forecast",
			"limits":{"max_response_bytes":16}
		}
	}`)
	req = withRouteParam(authedReq(http.MethodPost, "/webhooks/"+source.String()+"/invoke", `{"inputs":{}}`, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.InvokeWebhook(rec, req)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "max_response_bytes")
	assert.NotContains(t, rec.Body.String(), "super-secret")

	store.connections[0].Config = json.RawMessage(`{
		"base_url":"` + srvWeather.URL + `",
		"auth":{"type":"none"},
		"runtime":{"worker":"foundry","allowed_methods":["GET"]},
		"permissions":{"invokable":true},
		"webhook":{
			"method":"GET",
			"path":"/v1/forecast",
			"rate_limit":{"max_requests":1,"per_seconds":60}
		}
	}`)
	req = withRouteParam(authedReq(http.MethodPost, "/webhooks/"+source.String()+"/invoke", `{"inputs":{}}`, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.InvokeWebhook(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	req = withRouteParam(authedReq(http.MethodPost, "/webhooks/"+source.String()+"/invoke", `{"inputs":{}}`, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.InvokeWebhook(rec, req)
	require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())

	req = withRouteParam(authedReq(http.MethodPost, "/webhooks/"+source.String()+"/invoke", `{"inputs":{}}`, uuid.New()), "id", source.String())
	rec = httptest.NewRecorder()
	h.InvokeWebhook(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	trailObjectTypeID := uuid.New()
	store.connections[0].ConnectorType = "rest_api"
	store.connections[0].Config = json.RawMessage(`{
		"base_url":"https://ingest.example.test",
		"auth":{"type":"none"},
		"runtime":{"worker":"foundry"},
		"permissions":{"invokable":true},
		"listener":{
			"id":"trail-events",
			"type":"https",
			"enabled":true,
			"auth":{"type":"hmac_sha256","header":"X-OpenFoundry-Signature","secret":"listener-secret"},
			"destination":{"mode":"object","object_type_id":"` + trailObjectTypeID.String() + `"},
			"limits":{"max_payload_bytes":4096}
		}
	}`)
	listenerPayload := `{"event_id":"evt-trail-1","trail_id":"mule-deer","distance_miles":8.8}`
	req = withRouteParam(withRouteParam(httptest.NewRequest(http.MethodPost, "/api/v1/data-connection/sources/"+source.String()+"/listeners/trail-events/events", strings.NewReader(listenerPayload)), "source_id", source.String()), "listener_id", "trail-events")
	req.Header.Set("X-OpenFoundry-Signature", listenerHMAC("listener-secret", listenerPayload))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ReceiveInboundListener(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var listenerResp models.ReceiveInboundListenerResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listenerResp))
	assert.Equal(t, source, listenerResp.SourceID)
	assert.Equal(t, "trail-events", listenerResp.ListenerID)
	assert.True(t, listenerResp.SignatureVerified)
	assert.Equal(t, "object", listenerResp.Destination.Mode)
	assert.Equal(t, trailObjectTypeID, *listenerResp.Destination.ObjectTypeID)

	req = withRouteParam(authedReq(http.MethodGet, "/api/v1/data-connection/sources/"+source.String()+"/listener-events?limit=10", ``, owner), "id", source.String())
	rec = httptest.NewRecorder()
	h.ListInboundListenerEvents(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var listenerHistory models.ListResponse[models.InboundListenerEvent]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listenerHistory))
	require.Len(t, listenerHistory.Items, 1)
	assert.Equal(t, "evt-trail-1", listenerHistory.Items[0].EventID)
	assert.JSONEq(t, listenerPayload, string(listenerHistory.Items[0].Payload))
	assert.Contains(t, string(listenerHistory.Items[0].Headers), "[redacted]")

	req = withRouteParam(withRouteParam(httptest.NewRequest(http.MethodPost, "/api/v1/data-connection/sources/"+source.String()+"/listeners/trail-events/events", strings.NewReader(listenerPayload)), "source_id", source.String()), "listener_id", "trail-events")
	req.Header.Set("X-OpenFoundry-Signature", "sha256=bad")
	rec = httptest.NewRecorder()
	h.ReceiveInboundListener(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())

	store.connections[0].ConnectorType = "postgresql"
	store.registrations[source] = []models.ConnectionRegistration{{ID: uuid.New(), ConnectionID: source, Selector: "public.orders", DisplayName: "Orders", SourceKind: "table", RegistrationMode: "zero_copy", Metadata: json.RawMessage(`{"supports_zero_copy":true}`)}}
	req = authedReq(http.MethodGet, "/iceberg/v1/config", ``, owner)
	rec = httptest.NewRecorder()
	h.IcebergGetConfig(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	req = authedReq(http.MethodGet, "/iceberg/v1/namespaces", ``, owner)
	rec = httptest.NewRecorder()
	h.IcebergListNamespaces(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	req = withRouteParam(authedReq(http.MethodGet, "/iceberg/v1/namespaces/pg", ``, owner), "namespace", "pg")
	rec = httptest.NewRecorder()
	h.IcebergGetNamespace(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	req = withRouteParam(authedReq(http.MethodGet, "/iceberg/v1/namespaces/pg/tables", ``, owner), "namespace", "pg")
	rec = httptest.NewRecorder()
	h.IcebergListTables(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	req = withRouteParam(withRouteParam(authedReq(http.MethodGet, "/iceberg/v1/namespaces/pg/tables/public.orders", ``, owner), "namespace", "pg"), "table", "public.orders")
	rec = httptest.NewRecorder()
	h.IcebergLoadTable(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

type fakeIngestionPort struct {
	requests []cmruntime.IngestionRequest
	result   cmruntime.IngestionResult
	err      error
}

func (f *fakeIngestionPort) Dispatch(_ context.Context, req cmruntime.IngestionRequest) (*cmruntime.IngestionResult, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	result := f.result
	if result.IngestJobID == "" {
		result.IngestJobID = "ingest-" + req.RunID.String()
	}
	if result.Payload == nil {
		result.Payload = req.Materialized
	}
	if result.BytesWritten == 0 {
		result.BytesWritten = int64(len(result.Payload))
	}
	if result.FilesWritten == 0 {
		result.FilesWritten = 1
	}
	return &result, nil
}

type fakeDatasetVersioningPort struct {
	requests []cmruntime.DatasetVersionRequest
	id       uuid.UUID
	err      error
}

func (f *fakeDatasetVersioningPort) Register(_ context.Context, req cmruntime.DatasetVersionRequest) (*cmruntime.DatasetVersionResult, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	id := f.id
	if id == uuid.Nil {
		id = uuid.New()
	}
	return &cmruntime.DatasetVersionResult{DatasetVersionID: id}, nil
}

func TestRunSyncJobDispatchesIngestionAndRegistersDatasetVersion(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	output := uuid.New()
	job, err := store.CreateSyncJob(context.Background(), &models.CreateSyncJobRequest{SourceID: store.connections[0].ID, OutputDatasetID: &output}, owner)
	require.NoError(t, err)
	ingestion := &fakeIngestionPort{result: cmruntime.IngestionResult{RowsWritten: 7, Payload: []byte(`{"rows":7}`)}}
	versionID := uuid.New()
	versions := &fakeDatasetVersioningPort{id: versionID}
	h := &handlers.Handlers{Repo: store, IngestionRuntime: ingestion, DatasetVersioning: versions}

	req := withRouteParam(authedReq(http.MethodPost, "/syncs/"+job.ID.String()+"/run", "", owner), "sync_id", job.ID.String())
	rec := httptest.NewRecorder()
	h.RunSyncJob(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	var run models.SyncRun
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &run))
	assert.Equal(t, "succeeded", run.Status)
	assert.NotNil(t, run.FinishedAt)
	assert.NotNil(t, run.IngestJobID)
	assert.Equal(t, versionID, *run.DatasetVersionID)
	assert.NotEmpty(t, *run.ContentHash)
	require.Len(t, ingestion.requests, 1)
	assert.Equal(t, job.ID, ingestion.requests[0].SyncDefID)
	require.Len(t, versions.requests, 1)
	require.NotNil(t, job.OutputDatasetID)
	assert.Equal(t, *job.OutputDatasetID, versions.requests[0].OutputDatasetID)
	assert.Equal(t, *run.ContentHash, versions.requests[0].ContentHash)

	req = withRouteParam(authedReq(http.MethodGet, "/syncs/"+job.ID.String()+"/runs", "", owner), "sync_id", job.ID.String())
	rec = httptest.NewRecorder()
	h.ListRuns(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var runs []models.SyncRun
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &runs))
	require.Len(t, runs, 1)
	assert.Equal(t, "succeeded", runs[0].Status)
}

func TestRunSyncJobReusesDatasetVersionForSameContentHash(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	output := uuid.New()
	job, err := store.CreateSyncJob(context.Background(), &models.CreateSyncJobRequest{SourceID: store.connections[0].ID, OutputDatasetID: &output}, owner)
	require.NoError(t, err)
	payload := []byte(`stable-payload`)
	ingestion := &fakeIngestionPort{result: cmruntime.IngestionResult{Payload: payload, BytesWritten: int64(len(payload)), FilesWritten: 1}}
	versions := &fakeDatasetVersioningPort{id: uuid.New()}
	h := &handlers.Handlers{Repo: store, IngestionRuntime: ingestion, DatasetVersioning: versions}

	for range 2 {
		req := withRouteParam(authedReq(http.MethodPost, "/syncs/"+job.ID.String()+"/run", "", owner), "sync_id", job.ID.String())
		rec := httptest.NewRecorder()
		h.RunSyncJob(rec, req)
		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	}
	require.Len(t, versions.requests, 1, "second run should reuse the recorded dataset version for the same content hash")
	runs := store.runs[job.ID]
	require.Len(t, runs, 2)
	require.NotNil(t, runs[0].DatasetVersionID)
	require.NotNil(t, runs[1].DatasetVersionID)
	assert.Equal(t, *runs[0].DatasetVersionID, *runs[1].DatasetVersionID)
}

func TestGetVirtualMediaHandoffReportsBlockedDescriptor(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "s3"
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID

	req := withRouteParam(authedReq("GET", "/sources/"+source.String()+"/virtual-media-handoff", "", owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.GetVirtualMediaHandoff(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var descriptor models.VirtualMediaHandoffDescriptor
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &descriptor))
	assert.Equal(t, "blocked", descriptor.Status)
	assert.NotEmpty(t, descriptor.BlockedReason)
	require.Len(t, descriptor.Handoffs, 3)
	for _, handoff := range descriptor.Handoffs {
		assert.Equal(t, "blocked", handoff.Status)
		assert.Contains(t, handoff.Blockers, "media_sets_virtual_item_semantics")
	}
	assert.NotEmpty(t, descriptor.Delegation.Schema)
}

func TestDeadLetterSinkAndQuarantineRoundtrip(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	store.connections[0].ConnectorType = "postgresql"
	source := store.connections[0].ID
	output := uuid.New()
	syncJob, err := store.CreateSyncJob(context.Background(), &models.CreateSyncJobRequest{SourceID: source, OutputDatasetID: &output}, owner)
	require.NoError(t, err)
	h := &handlers.Handlers{Repo: store}

	// 1. Update the dead-letter sink with a redaction rule.
	putBody := `{"kind":"dataset","target_rid":"ri.datasets.main.dlq-` + syncJob.ID.String() + `","retention_days":7,"redaction_rules":[{"field":"email","replacement":"[REDACTED]"}]}`
	req := withRouteParam(authedReq("PUT", "/syncs/"+syncJob.ID.String()+"/dead-letter", putBody, owner), "sync_id", syncJob.ID.String())
	rec := httptest.NewRecorder()
	h.UpdateDeadLetterSink(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// 2. GET returns the persisted sink.
	getReq := withRouteParam(authedReq("GET", "/syncs/"+syncJob.ID.String()+"/dead-letter", "", owner), "sync_id", syncJob.ID.String())
	getRec := httptest.NewRecorder()
	h.GetDeadLetterSink(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)
	var sink models.DeadLetterSink
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &sink))
	assert.Equal(t, models.DeadLetterSinkKindDataset, sink.Kind)
	assert.Equal(t, 7, sink.RetentionDays)
	require.Len(t, sink.RedactionRules, 1)

	// 3. Record a quarantine entry and verify redaction is applied.
	recordBody := `{"failure_category":"schema_validation","error_message":"missing field email","payload":{"id":"abc","email":"user@example.com"}}`
	recReq := withRouteParam(authedReq("POST", "/syncs/"+syncJob.ID.String()+"/quarantine", recordBody, owner), "sync_id", syncJob.ID.String())
	recRec := httptest.NewRecorder()
	h.RecordQuarantinedRecord(recRec, recReq)
	require.Equal(t, http.StatusCreated, recRec.Code, recRec.Body.String())
	var record models.QuarantinedRecord
	require.NoError(t, json.Unmarshal(recRec.Body.Bytes(), &record))
	assert.Equal(t, "[REDACTED]", record.RedactedPayload["email"], "email should be redacted: %+v", record.RedactedPayload)
	assert.Equal(t, models.QuarantineFailureSchemaValidation, record.FailureCategory)

	// 4. List quarantine returns summary with by-category counts.
	listReq := withRouteParam(authedReq("GET", "/syncs/"+syncJob.ID.String()+"/quarantine", "", owner), "sync_id", syncJob.ID.String())
	listRec := httptest.NewRecorder()
	h.ListQuarantinedRecords(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code)
	var summary models.QuarantineSummary
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &summary))
	assert.Equal(t, 1, summary.Total)
	assert.Equal(t, 1, summary.ByCategory[models.QuarantineFailureSchemaValidation])

	// 5. Replay marks the record as pending replay.
	replayBody := `{"record_ids":["` + record.ID.String() + `"],"reason":"schema fix shipped"}`
	replayReq := withRouteParam(authedReq("POST", "/syncs/"+syncJob.ID.String()+"/quarantine:replay", replayBody, owner), "sync_id", syncJob.ID.String())
	replayRec := httptest.NewRecorder()
	h.ReplayQuarantinedRecords(replayRec, replayReq)
	require.Equal(t, http.StatusAccepted, replayRec.Code, replayRec.Body.String())
	var plan models.QuarantineReplayPlan
	require.NoError(t, json.Unmarshal(replayRec.Body.Bytes(), &plan))
	assert.Equal(t, 1, plan.RecordsMatched)
	assert.Empty(t, plan.BlockingReasons)
}

func TestRecordQuarantineRequiresErrorMessage(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	source := store.connections[0].ID
	output := uuid.New()
	syncJob, err := store.CreateSyncJob(context.Background(), &models.CreateSyncJobRequest{SourceID: source, OutputDatasetID: &output}, owner)
	require.NoError(t, err)
	h := &handlers.Handlers{Repo: store}

	req := withRouteParam(authedReq("POST", "/syncs/"+syncJob.ID.String()+"/quarantine", `{"payload":{}}`, owner), "sync_id", syncJob.ID.String())
	rec := httptest.NewRecorder()
	h.RecordQuarantinedRecord(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUpdateDeadLetterSinkRejectsInvalidPayload(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	source := store.connections[0].ID
	output := uuid.New()
	syncJob, err := store.CreateSyncJob(context.Background(), &models.CreateSyncJobRequest{SourceID: source, OutputDatasetID: &output}, owner)
	require.NoError(t, err)
	h := &handlers.Handlers{Repo: store}

	body := `{"kind":"queue","target_rid":"not-a-rid","retention_days":999}`
	req := withRouteParam(authedReq("PUT", "/syncs/"+syncJob.ID.String()+"/dead-letter", body, owner), "sync_id", syncJob.ID.String())
	rec := httptest.NewRecorder()
	h.UpdateDeadLetterSink(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "kind must be dataset or stream")
}

func TestComputeStreamReplayPlanRequiresAckForActiveExport(t *testing.T) {
	owner := uuid.New()
	h := &handlers.Handlers{}
	body := `{
		"stream_id": "stream-1",
		"reason": "Drain after schema fix",
		"from_offset": 100,
		"to_offset": 200,
		"earliest_offset": 0,
		"latest_offset": 500,
		"exports": [{"export_id": "exp-1", "status": "running"}],
		"consumers": [{"consumer_id": "c1", "idempotency_mode": "duplicate_tolerant"}]
	}`
	req := authedReq("POST", "/streams/replay-plan:compute", body, owner)
	rec := httptest.NewRecorder()
	h.ComputeStreamReplayPlan(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var plan models.StreamReplayPlan
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &plan))
	assert.Equal(t, "blocked", plan.Status)
	assert.True(t, plan.ConfirmationRequired)
	assert.Contains(t, plan.AcknowledgementsMissing, "ack_streaming_export_exp-1")
	require.NotNil(t, plan.EstimatedRecords)
	assert.Equal(t, int64(101), *plan.EstimatedRecords)
}

func TestComputeStreamReplayPlanRejectsMissingStreamID(t *testing.T) {
	owner := uuid.New()
	h := &handlers.Handlers{}
	req := authedReq("POST", "/streams/replay-plan:compute", `{"reason":"x"}`, owner)
	rec := httptest.NewRecorder()
	h.ComputeStreamReplayPlan(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "stream_id")
}

func TestComputeStreamMetricsSnapshotReturnsRatesAndBreakdowns(t *testing.T) {
	owner := uuid.New()
	h := &handlers.Handlers{}
	body := `{
		"stream_id": "stream-1",
		"stream_name": "events",
		"window": "1m",
		"ingested_records": 600,
		"ingested_bytes": 6000,
		"consumed_records": 300,
		"consumed_bytes": 3000,
		"stream_lag_records": 200,
		"hot_buffer_records": 1000,
		"consumers": [
			{"id": "c1", "name": "consumer-1", "lag": 50, "records_read": 60},
			{"id": "c2", "name": "consumer-2", "lag": 200, "records_read": 240}
		],
		"streaming_exports": [
			{"export_id": "e1", "duplicate_risk": true, "records_exported": 300, "bytes_exported": 3000}
		]
	}`
	req := authedReq("POST", "/streams/metrics:compute", body, owner)
	rec := httptest.NewRecorder()
	h.ComputeStreamMetricsSnapshot(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var snapshot models.StreamMetricsSnapshot
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &snapshot))
	assert.Equal(t, "stream-1", snapshot.StreamID)
	assert.Equal(t, float64(10), snapshot.Ingestion.RecordsPerSecond)
	assert.Equal(t, float64(5), snapshot.Consumption.RecordsPerSecond)
	require.Len(t, snapshot.Consumers, 2)
	assert.Equal(t, "c2", snapshot.Consumers[0].ConsumerID, "consumers sort by lag desc")
	require.Len(t, snapshot.StreamingExports, 1)
	assert.True(t, snapshot.StreamingExports[0].DuplicateRisk)
	assert.NotEmpty(t, snapshot.Warnings, "duplicate risk export should emit a warning")
}

func TestComputeStreamMetricsSnapshotRejectsMissingStreamID(t *testing.T) {
	owner := uuid.New()
	h := &handlers.Handlers{}
	req := authedReq("POST", "/streams/metrics:compute", `{"window":"1m"}`, owner)
	rec := httptest.NewRecorder()
	h.ComputeStreamMetricsSnapshot(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "stream_id")
}

func TestListConnectorCapabilityPacksReturnsAllFamilies(t *testing.T) {
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/data-connection/capability-packs", nil)
	rec := httptest.NewRecorder()
	h.ListConnectorCapabilityPacks(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp models.ListResponse[models.ConnectorCapabilityPack]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	connectorTypes := map[string]bool{}
	for _, pack := range resp.Items {
		connectorTypes[pack.ConnectorType] = true
	}
	for _, required := range []string{"postgresql", "kafka", "s3", "snowflake", "rest_api", "foundry_to_foundry"} {
		assert.True(t, connectorTypes[required], "missing %s pack", required)
	}
}

func TestGetConnectorCapabilityPack(t *testing.T) {
	h := &handlers.Handlers{}
	req := withRouteParam(httptest.NewRequest("GET", "/data-connection/capability-packs/postgresql", nil), "connector_type", "postgresql")
	rec := httptest.NewRecorder()
	h.GetConnectorCapabilityPack(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var pack models.ConnectorCapabilityPack
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pack))
	assert.Equal(t, "postgresql", pack.ConnectorType)
	assert.True(t, pack.Capabilities.CdcSync)
	assert.Equal(t, "relational_connector", pack.CdcInputKind)
}

func TestGetConnectorCapabilityPackUnknownConnector(t *testing.T) {
	h := &handlers.Handlers{}
	req := withRouteParam(httptest.NewRequest("GET", "/data-connection/capability-packs/does-not-exist", nil), "connector_type", "does-not-exist")
	rec := httptest.NewRecorder()
	h.GetConnectorCapabilityPack(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetListenerInboundDescriptorReportsBlockedAggregate(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID

	req := withRouteParam(authedReq("GET", "/sources/"+source.String()+"/listener-descriptor", "", owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.GetListenerInboundDescriptor(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var descriptor models.ListenerInboundDescriptor
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &descriptor))
	assert.Equal(t, "blocked", descriptor.Status)
	require.Len(t, descriptor.Capabilities, 4)
	assert.NotEmpty(t, descriptor.BlockedReason)
	assert.NotEmpty(t, descriptor.AvailableSurfaces)
	assert.Equal(t, "listener", descriptor.Recommendation.Kind)
	facets := map[models.ListenerInboundFacet]bool{}
	for _, c := range descriptor.Capabilities {
		facets[c.Facet] = true
	}
	for _, expected := range []models.ListenerInboundFacet{
		models.ListenerInboundFacetSchemaMapping,
		models.ListenerInboundFacetAuthStrategy,
		models.ListenerInboundFacetReplayIdempotency,
		models.ListenerInboundFacetDeadLetter,
	} {
		assert.Truef(t, facets[expected], "missing facet %s", expected)
	}
}

func TestGetListenerInboundDescriptorRequiresAuth(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner)
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID

	req := withRouteParam(httptest.NewRequest("GET", "/sources/"+source.String()+"/listener-descriptor", nil), "id", source.String())
	rec := httptest.NewRecorder()
	h.GetListenerInboundDescriptor(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetVirtualMediaHandoffReportsNotSupportedForOtherConnectors(t *testing.T) {
	owner := uuid.New()
	store := newFakeStore(owner) // default connector is postgresql
	h := &handlers.Handlers{Repo: store}
	source := store.connections[0].ID

	req := withRouteParam(authedReq("GET", "/sources/"+source.String()+"/virtual-media-handoff", "", owner), "id", source.String())
	rec := httptest.NewRecorder()
	h.GetVirtualMediaHandoff(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var descriptor models.VirtualMediaHandoffDescriptor
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &descriptor))
	assert.Equal(t, "not_supported", descriptor.Status)
	assert.Empty(t, descriptor.Handoffs)
}
