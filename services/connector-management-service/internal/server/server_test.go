package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/server"
)

var errReadyTest = errors.New("db unavailable")

type routerStore struct {
	owner  uuid.UUID
	source uuid.UUID
	agent  uuid.UUID
}

func newRouterStore() *routerStore {
	return &routerStore{owner: uuid.New(), source: uuid.New(), agent: uuid.New()}
}

func (s *routerStore) ListConnections(context.Context, *uuid.UUID) ([]models.Connection, error) {
	return []models.Connection{{ID: s.source, Name: "pg", ConnectorType: "postgres", OwnerID: s.owner, Config: json.RawMessage(`{}`)}}, nil
}

func (s *routerStore) GetConnection(context.Context, uuid.UUID) (*models.Connection, error) {
	return &models.Connection{ID: s.source, Name: "pg", ConnectorType: "postgres", OwnerID: s.owner, Config: json.RawMessage(`{}`)}, nil
}

func (s *routerStore) GetConnectionForOwner(context.Context, uuid.UUID, uuid.UUID) (*models.Connection, error) {
	return &models.Connection{ID: s.source, Name: "pg", ConnectorType: "postgres", OwnerID: s.owner, Config: json.RawMessage(`{}`)}, nil
}

func (s *routerStore) CreateConnection(context.Context, *models.CreateConnectionRequest, uuid.UUID) (*models.Connection, error) {
	return &models.Connection{ID: uuid.New(), Name: "created", ConnectorType: "postgres", OwnerID: s.owner, Config: json.RawMessage(`{}`)}, nil
}

func (s *routerStore) UpdateConnection(context.Context, uuid.UUID, *models.UpdateConnectionRequest) (*models.Connection, error) {
	return &models.Connection{ID: s.source, Name: "updated", ConnectorType: "postgres", OwnerID: s.owner, Config: json.RawMessage(`{}`)}, nil
}

func (s *routerStore) DeleteConnection(context.Context, uuid.UUID) (bool, error) { return true, nil }

func (s *routerStore) CheckSourceRole(context.Context, uuid.UUID, uuid.UUID, models.SourcePermissionRole) (bool, error) {
	return true, nil
}

func (s *routerStore) GetSourceGovernance(context.Context, uuid.UUID, uuid.UUID) (*models.SourceGovernance, error) {
	return &models.SourceGovernance{
		SourceID:                 s.source,
		SourceRID:                models.SourceRIDForConnection(s.source),
		OwnerID:                  s.owner,
		RoleDefinitions:          models.SourcePermissionRoleDefinitions(),
		EffectiveRoles:           models.AllSourcePermissionRoles(),
		PermissionGrants:         []models.SourcePermissionGrant{},
		Visibility:               models.DefaultSourceVisibilityPolicy(),
		OutputDatasetPermissions: []models.SourceOutputDatasetPermission{},
		AuditEvents:              []models.SourceGovernanceAuditEvent{},
	}, nil
}

func (s *routerStore) UpdateSourceGovernance(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID, _ *models.UpdateSourceGovernanceRequest) (*models.SourceGovernance, error) {
	return s.GetSourceGovernance(ctx, sourceID, actorID)
}

func (s *routerStore) ListSourceGovernanceAudit(context.Context, uuid.UUID, uuid.UUID, int) ([]models.SourceGovernanceAuditEvent, error) {
	return []models.SourceGovernanceAuditEvent{}, nil
}

func (s *routerStore) RecordSourceGovernanceAudit(context.Context, models.RecordSourceGovernanceAuditRequest) (*models.SourceGovernanceAuditEvent, error) {
	now := time.Now().UTC()
	return &models.SourceGovernanceAuditEvent{ID: uuid.New(), SourceID: s.source, EventType: "source_use", Action: "test", Result: "succeeded", CreatedAt: now}, nil
}

func (s *routerStore) ListSyncJobs(context.Context, uuid.UUID, uuid.UUID) ([]models.SyncJob, error) {
	return []models.SyncJob{}, nil
}

func (s *routerStore) GetSyncJob(context.Context, uuid.UUID, uuid.UUID) (*models.SyncJob, error) {
	output := uuid.New()
	return &models.SyncJob{ID: uuid.New(), SourceID: s.source, OutputDatasetID: &output, OutputKind: "dataset"}, nil
}

func (s *routerStore) CreateSyncJob(context.Context, *models.CreateSyncJobRequest, uuid.UUID) (*models.SyncJob, error) {
	output := uuid.New()
	return &models.SyncJob{ID: uuid.New(), SourceID: s.source, OutputDatasetID: &output, OutputKind: "dataset"}, nil
}

func (s *routerStore) UpdateSyncJob(context.Context, uuid.UUID, *models.UpdateSyncJobRequest, uuid.UUID) (*models.SyncJob, error) {
	output := uuid.New()
	return &models.SyncJob{ID: uuid.New(), SourceID: s.source, OutputDatasetID: &output, OutputKind: "dataset"}, nil
}

func (s *routerStore) RunSyncJob(context.Context, uuid.UUID, uuid.UUID) (*models.SyncRun, error) {
	return &models.SyncRun{ID: uuid.New(), SyncDefID: uuid.New(), Status: "running"}, nil
}

func (s *routerStore) ListSyncRuns(context.Context, uuid.UUID, uuid.UUID) ([]models.SyncRun, error) {
	return []models.SyncRun{}, nil
}

func (s *routerStore) ListDataExports(context.Context, uuid.UUID, uuid.UUID) ([]models.DataExport, error) {
	return []models.DataExport{}, nil
}

func (s *routerStore) GetDataExport(context.Context, uuid.UUID, uuid.UUID) (*models.DataExport, error) {
	return &models.DataExport{ID: uuid.New(), SourceID: s.source, Name: "export", ExportType: models.DataExportTypeTable, ExportMode: models.DataExportModeTableMirror, Status: models.DataExportStatusDraft, Config: json.RawMessage(`{}`), Health: models.DefaultDataExportHealth(), History: []models.DataExportHistoryEntry{}}, nil
}

func (s *routerStore) CreateDataExport(context.Context, *models.CreateDataExportRequest, uuid.UUID) (*models.DataExport, error) {
	return &models.DataExport{ID: uuid.New(), SourceID: s.source, Name: "export", ExportType: models.DataExportTypeTable, ExportMode: models.DataExportModeTableMirror, DestinationTable: stringPtr("public.orders_export"), Status: models.DataExportStatusDraft, Config: json.RawMessage(`{}`), Health: models.DefaultDataExportHealth(), History: []models.DataExportHistoryEntry{}}, nil
}

func (s *routerStore) UpdateDataExport(context.Context, uuid.UUID, *models.UpdateDataExportRequest, uuid.UUID) (*models.DataExport, error) {
	return &models.DataExport{ID: uuid.New(), SourceID: s.source, Name: "export", ExportType: models.DataExportTypeTable, ExportMode: models.DataExportModeTableMirror, DestinationTable: stringPtr("public.orders_export"), Status: models.DataExportStatusDraft, Config: json.RawMessage(`{}`), Health: models.DefaultDataExportHealth(), History: []models.DataExportHistoryEntry{}}, nil
}

func (s *routerStore) RunDataExport(context.Context, uuid.UUID, uuid.UUID) (*models.DataExport, error) {
	now := time.Now().UTC()
	return &models.DataExport{ID: uuid.New(), SourceID: s.source, Name: "export", ExportType: models.DataExportTypeTable, ExportMode: models.DataExportModeTableMirror, Status: models.DataExportStatusSucceeded, Health: models.DataExportHealth{State: models.DataExportHealthHealthy, LastCheckedAt: &now}, LastRunAt: &now, Config: json.RawMessage(`{}`), History: []models.DataExportHistoryEntry{}}, nil
}

func (s *routerStore) StartDataExport(context.Context, uuid.UUID, uuid.UUID) (*models.DataExport, error) {
	now := time.Now().UTC()
	return &models.DataExport{ID: uuid.New(), SourceID: s.source, Name: "stream export", ExportType: models.DataExportTypeStreaming, ExportMode: models.DataExportModeStreamingContinuous, Status: models.DataExportStatusRunning, Health: models.DataExportHealth{State: models.DataExportHealthRunning, LastCheckedAt: &now}, LastRunAt: &now, Config: json.RawMessage(`{}`), History: []models.DataExportHistoryEntry{}}, nil
}

func (s *routerStore) StopDataExport(context.Context, uuid.UUID, uuid.UUID) (*models.DataExport, error) {
	now := time.Now().UTC()
	return &models.DataExport{ID: uuid.New(), SourceID: s.source, Name: "stream export", ExportType: models.DataExportTypeStreaming, ExportMode: models.DataExportModeStreamingContinuous, Status: models.DataExportStatusStopped, Health: models.DataExportHealth{State: models.DataExportHealthHealthy, LastCheckedAt: &now}, Config: json.RawMessage(`{}`), History: []models.DataExportHistoryEntry{}}, nil
}

func (s *routerStore) ListCredentials(context.Context, uuid.UUID, uuid.UUID) ([]models.CredentialResponse, error) {
	return []models.CredentialResponse{}, nil
}

func (s *routerStore) SetCredential(context.Context, uuid.UUID, uuid.UUID, string, []byte, string) (*models.CredentialResponse, error) {
	return &models.CredentialResponse{ID: uuid.New(), SourceID: s.source, Kind: "api_key", Fingerprint: "abc"}, nil
}

func (s *routerStore) ListConnectorAgents(context.Context, uuid.UUID) ([]models.ConnectorAgent, error) {
	return []models.ConnectorAgent{{ID: s.agent, Name: "edge", AgentURL: "https://agent.local", OwnerID: s.owner, Status: "online", Capabilities: json.RawMessage(`{}`), Metadata: json.RawMessage(`{}`)}}, nil
}

func (s *routerStore) RegisterConnectorAgent(context.Context, *models.RegisterAgentRequest, uuid.UUID) (*models.ConnectorAgent, error) {
	return &models.ConnectorAgent{ID: s.agent, Name: "edge", AgentURL: "https://agent.local", OwnerID: s.owner, Status: "online", Capabilities: json.RawMessage(`{}`), Metadata: json.RawMessage(`{}`)}, nil
}

func (s *routerStore) HeartbeatConnectorAgent(context.Context, uuid.UUID, *models.AgentHeartbeatRequest, uuid.UUID) (*models.ConnectorAgent, error) {
	now := time.Now().UTC()
	return &models.ConnectorAgent{ID: s.agent, Name: "edge", AgentURL: "https://agent.local", OwnerID: s.owner, Status: "online", Capabilities: json.RawMessage(`{}`), Metadata: json.RawMessage(`{}`), LastHeartbeatAt: &now}, nil
}

func (s *routerStore) DeleteConnectorAgent(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}

func (s *routerStore) GetConnectorAgent(context.Context, uuid.UUID) (*models.ConnectorAgent, error) {
	return &models.ConnectorAgent{ID: s.agent, Name: "edge", AgentURL: "https://agent.local", OwnerID: s.owner, Status: "online", Capabilities: json.RawMessage(`{}`), Metadata: json.RawMessage(`{}`)}, nil
}

func (s *routerStore) MigrateConnectionToFoundryWorker(context.Context, uuid.UUID, uuid.UUID) (*models.Connection, error) {
	return &models.Connection{ID: s.source, Name: "edge", ConnectorType: "rest_api", OwnerID: s.owner, Status: "online", Config: json.RawMessage(`{"worker":"foundry"}`)}, nil
}

func (s *routerStore) ListSourcePolicies(context.Context, uuid.UUID, uuid.UUID) ([]models.SourcePolicyBindingResponse, error) {
	return []models.SourcePolicyBindingResponse{}, nil
}

func (s *routerStore) AttachPolicy(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) (*models.SourcePolicyBindingResponse, error) {
	return &models.SourcePolicyBindingResponse{SourceID: s.source, PolicyID: uuid.New(), Kind: "direct"}, nil
}

func (s *routerStore) DetachPolicy(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}

func (s *routerStore) GetSourceCodeImport(context.Context, uuid.UUID, uuid.UUID) (*models.SourceCodeImport, error) {
	sourceRID := models.SourceRIDForConnection(s.source)
	binding := models.SourceBindingSnippet(sourceRID, "pg", "pg")
	resolution := models.SourceCodeImportBuildResolution{
		SourceID:              s.source,
		SourceRID:             sourceRID,
		SourceName:            "pg",
		ConnectorType:         "postgres",
		PythonIdentifier:      "pg",
		FriendlyName:          "pg",
		ResolvedAt:            time.Now().UTC(),
		SourceUpdatedAt:       time.Now().UTC(),
		ConfigHash:            "sha256:test",
		CredentialBindings:    []models.SourceCredentialBinding{},
		EgressPolicyBindings:  []models.SourceEgressPolicyBinding{},
		ExportControls:        models.ExportControls{},
		ExportPolicyDecision:  models.ResolveSourceCodeImportExportPolicy(models.ExportControls{}, false, nil),
		UsesLiveConfiguration: true,
		NoCodeChangeRequired:  true,
		GeneratedBinding:      binding,
	}
	return &models.SourceCodeImport{
		SourceID:                  s.source,
		SourceRID:                 sourceRID,
		SourceName:                "pg",
		ConnectorType:             "postgres",
		Enabled:                   true,
		FriendlyName:              "pg",
		PythonIdentifier:          "pg",
		GeneratedBinding:          binding,
		CodeRepositories:          []models.CodeRepositorySourceImport{},
		ExportControls:            models.ExportControls{},
		ExternalTransformPatterns: models.ExternalTransformPatternsForSource(sourceRID, "pg", "pg", models.ExportControls{}),
		ComputeModuleAlternatives: models.ComputeModuleAlternativesForSource(sourceRID, "pg", "pg"),
		BuildStartResolution:      resolution,
		CreatedAt:                 time.Now().UTC(),
		UpdatedAt:                 time.Now().UTC(),
	}, nil
}

func (s *routerStore) UpdateSourceCodeImport(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, _ *models.UpdateSourceCodeImportRequest) (*models.SourceCodeImport, error) {
	return s.GetSourceCodeImport(ctx, sourceID, ownerID)
}

func (s *routerStore) ResolveSourceCodeImportBuildStart(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, _ *models.ResolveSourceCodeImportBuildRequest) (*models.SourceCodeImportBuildResolution, error) {
	v, err := s.GetSourceCodeImport(ctx, sourceID, ownerID)
	if err != nil || v == nil {
		return nil, err
	}
	return &v.BuildStartResolution, nil
}

func (s *routerStore) ListMediaSetSyncs(context.Context, uuid.UUID, uuid.UUID) ([]models.MediaSetSync, error) {
	return []models.MediaSetSync{}, nil
}

func (s *routerStore) GetMediaSetSync(context.Context, uuid.UUID, uuid.UUID) (*models.MediaSetSync, error) {
	return &models.MediaSetSync{ID: uuid.New(), SourceID: s.source, Kind: models.MediaSetSyncKindCopy, TargetMediaSetRID: "ri.foundry.main.media_set.x"}, nil
}

func (s *routerStore) CreateMediaSetSync(context.Context, uuid.UUID, *models.CreateMediaSetSyncRequest, uuid.UUID) (*models.MediaSetSync, error) {
	return &models.MediaSetSync{ID: uuid.New(), SourceID: s.source, Kind: models.MediaSetSyncKindCopy, TargetMediaSetRID: "ri.foundry.main.media_set.x"}, nil
}

func (s *routerStore) UpdateMediaSetSync(context.Context, uuid.UUID, *models.UpdateMediaSetSyncRequest, uuid.UUID) (*models.MediaSetSync, error) {
	return &models.MediaSetSync{ID: uuid.New(), SourceID: s.source, Kind: models.MediaSetSyncKindCopy, TargetMediaSetRID: "ri.foundry.main.media_set.x"}, nil
}

func (s *routerStore) EnableVirtualTableSource(context.Context, string, *models.EnableVirtualTableSourceRequest) (*models.VirtualTableSourceLink, error) {
	return &models.VirtualTableSourceLink{SourceRID: "ri.source", Provider: "BIGQUERY", VirtualTablesEnabled: true, AutoRegisterTagFilters: json.RawMessage(`[]`), AutoRegisterFolderMirrorKind: "NESTED", AutoRegisterTableTagFilters: []string{}}, nil
}

func (s *routerStore) DiscoverVirtualTableCatalog(context.Context, string, string) ([]models.DiscoveredEntry, error) {
	return []models.DiscoveredEntry{{DisplayName: "orders", Path: "analytics/public/orders", Kind: "table", Registrable: true}}, nil
}

func (s *routerStore) CreateVirtualTable(context.Context, string, string, *models.CreateVirtualTableRequest) (*models.VirtualTable, error) {
	return &models.VirtualTable{RID: "ri.virtual", SourceRID: "ri.source", CreatedBy: stringPtr(s.owner.String())}, nil
}

func (s *routerStore) BulkRegisterVirtualTables(context.Context, string, string, *models.VirtualTableBulkRegisterRequest) (*models.VirtualTableBulkRegisterResponse, error) {
	return &models.VirtualTableBulkRegisterResponse{Registered: []models.VirtualTable{{RID: "ri.virtual", SourceRID: "ri.source", CreatedBy: stringPtr(s.owner.String())}}, Errors: []models.VirtualTableBulkError{}}, nil
}

func (s *routerStore) EnableVirtualTableAutoRegistration(context.Context, string, *models.EnableAutoRegistrationRequest) (*models.VirtualTableSourceLink, error) {
	interval := int32(3600)
	projectRID := "ri.foundry.main.project.mirror"
	return &models.VirtualTableSourceLink{SourceRID: "ri.source", Provider: "BIGQUERY", VirtualTablesEnabled: true, AutoRegisterEnabled: true, AutoRegisterProjectRID: &projectRID, AutoRegisterIntervalSeconds: &interval, AutoRegisterTagFilters: json.RawMessage(`[]`), AutoRegisterFolderMirrorKind: "NESTED", AutoRegisterTableTagFilters: []string{}}, nil
}

func (s *routerStore) DisableVirtualTableAutoRegistration(context.Context, string) error {
	return nil
}

func (s *routerStore) ScanVirtualTableAutoRegistrationNow(context.Context, string) (*models.AutoRegistrationScanSummary, error) {
	return &models.AutoRegistrationScanSummary{}, nil
}

func (s *routerStore) ListVirtualTables(context.Context, string, string, string, string, string, int) ([]models.VirtualTable, error) {
	return []models.VirtualTable{}, nil
}

func (s *routerStore) GetVirtualTable(context.Context, string, string) (*models.VirtualTable, error) {
	return &models.VirtualTable{RID: "ri.virtual", SourceRID: "ri.source", CreatedBy: stringPtr(s.owner.String())}, nil
}

func (s *routerStore) SetVirtualTableUpdateDetection(context.Context, string, string, *models.UpdateDetectionToggle) (*models.VirtualTable, error) {
	interval := int32(3600)
	return &models.VirtualTable{RID: "ri.virtual", SourceRID: "ri.source", CreatedBy: stringPtr(s.owner.String()), UpdateDetectionEnabled: true, UpdateDetectionIntervalSeconds: &interval}, nil
}

func (s *routerStore) PollVirtualTableUpdateDetection(context.Context, string, string) (*models.PollResult, error) {
	return &models.PollResult{VirtualTableRID: "ri.virtual", Outcome: models.PollOutcomeInitial, ChangeDetected: true, EventEmitted: true}, nil
}

func (s *routerStore) ListVirtualTableUpdateDetectionHistory(context.Context, string, string, int) ([]models.PollHistoryRow, error) {
	return []models.PollHistoryRow{}, nil
}

func (s *routerStore) GetVirtualTableLineage(context.Context, string, string) (*models.VirtualTableLineageResponse, error) {
	return &models.VirtualTableLineageResponse{VirtualTableRID: "ri.virtual", SourceRID: "ri.source", Nodes: []models.VirtualTableLineageNode{{RID: "ri.source", Kind: "source"}, {RID: "ri.virtual", Kind: "virtual_table"}}, Edges: []models.VirtualTableLineageEdge{{FromRID: "ri.source", ToRID: "ri.virtual", Kind: "backs"}}}, nil
}

func (s *routerStore) ListRegistrations(context.Context, uuid.UUID) ([]models.ConnectionRegistration, error) {
	return []models.ConnectionRegistration{{ID: uuid.New(), ConnectionID: s.source, Selector: "pg", DisplayName: "pg", SourceKind: "postgres", RegistrationMode: "zero_copy", Metadata: json.RawMessage(`{"supports_zero_copy":true}`)}}, nil
}

func (s *routerStore) UpsertRegistration(context.Context, uuid.UUID, models.DiscoveredSource, string, bool, bool, *uuid.UUID, json.RawMessage) (*models.ConnectionRegistration, error) {
	return &models.ConnectionRegistration{ID: uuid.New(), ConnectionID: s.source, Selector: "pg", DisplayName: "pg", SourceKind: "postgres", RegistrationMode: "sync", Metadata: json.RawMessage(`{"supports_zero_copy":true}`)}, nil
}

func (s *routerStore) GetRegistration(context.Context, uuid.UUID, uuid.UUID) (*models.ConnectionRegistration, error) {
	return &models.ConnectionRegistration{ID: uuid.New(), ConnectionID: s.source, Selector: "pg", DisplayName: "pg", SourceKind: "postgres", RegistrationMode: "zero_copy", Metadata: json.RawMessage(`{"supports_zero_copy":true}`)}, nil
}

func (s *routerStore) DeleteRegistration(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}

func (s *routerStore) UpdateConnectionConfig(context.Context, uuid.UUID, json.RawMessage) (*models.Connection, error) {
	return &models.Connection{ID: s.source, Name: "pg", ConnectorType: "postgres", OwnerID: s.owner, Config: json.RawMessage(`{}`)}, nil
}

func (s *routerStore) ListIcebergNamespaces(context.Context) ([]models.Connection, error) {
	return []models.Connection{{ID: s.source, Name: "pg", ConnectorType: "postgres", OwnerID: s.owner, Config: json.RawMessage(`{}`)}}, nil
}

func (s *routerStore) GetIcebergConnection(context.Context, string) (*models.Connection, error) {
	return &models.Connection{ID: s.source, Name: "pg", ConnectorType: "postgres", OwnerID: s.owner, Config: json.RawMessage(`{}`)}, nil
}

func (s *routerStore) ListIcebergTables(context.Context, uuid.UUID) ([]models.ConnectionRegistration, error) {
	return []models.ConnectionRegistration{{ID: uuid.New(), ConnectionID: s.source, Selector: "pg", DisplayName: "pg", SourceKind: "postgres", RegistrationMode: "zero_copy", Metadata: json.RawMessage(`{"supports_zero_copy":true}`)}}, nil
}

func (s *routerStore) GetSourceRetryPolicy(context.Context, uuid.UUID, uuid.UUID) (*models.SourceRetryPolicy, error) {
	return nil, nil
}

func (s *routerStore) UpsertSourceRetryPolicy(context.Context, uuid.UUID, uuid.UUID, *string, models.SourceRetryPolicy) (*models.SourceRetryPolicy, error) {
	return nil, nil
}

func (s *routerStore) ListSyncRunFailuresForSource(context.Context, uuid.UUID, uuid.UUID, int) ([]models.RetryRecoveryRunSummary, error) {
	return nil, nil
}

func (s *routerStore) RecordMediaSetSyncRun(context.Context, uuid.UUID, uuid.UUID, models.MediaSetSyncRun) (*models.MediaSetSyncRun, error) {
	return nil, nil
}

func (s *routerStore) ListMediaSetSyncRuns(context.Context, uuid.UUID, uuid.UUID, int) ([]models.MediaSetSyncRun, error) {
	return nil, nil
}

func (s *routerStore) MediaSetSyncUsageForSource(context.Context, uuid.UUID, uuid.UUID) (map[uuid.UUID]models.MediaSetSyncUsageSummary, error) {
	return map[uuid.UUID]models.MediaSetSyncUsageSummary{}, nil
}

func (s *routerStore) GetDeadLetterSink(context.Context, uuid.UUID, uuid.UUID) (*models.DeadLetterSink, error) {
	return nil, nil
}

func (s *routerStore) UpsertDeadLetterSink(context.Context, uuid.UUID, uuid.UUID, *string, models.UpdateDeadLetterSinkRequest) (*models.DeadLetterSink, error) {
	return nil, nil
}

func (s *routerStore) RecordQuarantinedRecord(context.Context, uuid.UUID, uuid.UUID, models.RecordQuarantineRequest, models.DeadLetterSink, time.Time) (*models.QuarantinedRecord, error) {
	return nil, nil
}

func (s *routerStore) ListQuarantinedRecords(context.Context, uuid.UUID, uuid.UUID, models.QuarantineFailureCategory, int) ([]models.QuarantinedRecord, error) {
	return nil, nil
}

func (s *routerStore) MarkQuarantinedRecordsForReplay(context.Context, uuid.UUID, uuid.UUID, *string, []uuid.UUID, time.Time) (int, error) {
	return 0, nil
}

func stringPtr(v string) *string { return &v }

func testServer(t *testing.T, devAuth bool) (*http.Server, *authmw.JWTConfig, *routerStore) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Service.Name = "connector-management-service"
	cfg.Service.Version = "test"
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0
	cfg.OpenFoundryDevAuth = devAuth
	jwt := authmw.NewJWTConfig("test-secret")
	store := newRouterStore()
	return server.New(cfg, jwt, &handlers.Handlers{Repo: store}, observability.NewMetrics()), jwt, store
}

func bearer(t *testing.T, jwt *authmw.JWTConfig) string {
	t.Helper()
	now := time.Now().Unix()
	tok, err := authmw.EncodeToken(jwt, &authmw.Claims{Sub: uuid.New(), IAT: now, EXP: now + 3600, JTI: uuid.New(), Email: "test@example.com", Name: "Test", Roles: []string{"admin"}})
	require.NoError(t, err)
	return "Bearer " + tok
}

func exercise(ts http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	rec := httptest.NewRecorder()
	ts.ServeHTTP(rec, req)
	return rec
}

func TestRustRouteSurfaceIsMounted(t *testing.T) {
	srv, jwt, store := testServer(t, false)
	token := bearer(t, jwt)
	id := store.source.String()
	agentID := store.agent.String()
	syncID := uuid.NewString()
	regID := uuid.NewString()

	routes := []struct {
		method string
		path   string
		token  string
		body   string
	}{
		{http.MethodGet, "/api/v1/data-connection/catalog", "", ""},
		{http.MethodGet, "/api/v1/data-connection/catalog/contracts", "", ""},
		{http.MethodGet, "/api/v1/data-connection/streaming-sources", "", ""},
		{http.MethodGet, "/api/v1/data-connection/sources", token, ""},
		{http.MethodPost, "/api/v1/data-connection/sources", token, `{"name":"x","connector_type":"postgres"}`},
		{http.MethodGet, "/api/v1/data-connection/sources/" + id, token, ""},
		{http.MethodDelete, "/api/v1/data-connection/sources/" + id, token, ""},
		{http.MethodPost, "/api/v1/data-connection/sources/" + id + "/test-connection", token, "{}"},
		{http.MethodGet, "/api/v1/data-connection/sources/" + id + "/capabilities", "", ""},
		{http.MethodGet, "/api/v1/data-connection/sources/" + id + "/health", token, ""},
		{http.MethodGet, "/api/v1/data-connection/sources/" + id + "/permissions", token, ""},
		{http.MethodPatch, "/api/v1/data-connection/sources/" + id + "/permissions", token, "{}"},
		{http.MethodGet, "/api/v1/data-connection/sources/" + id + "/audit", token, ""},
		{http.MethodGet, "/api/v1/data-connection/sources/" + id + "/credentials", token, ""},
		{http.MethodPost, "/api/v1/data-connection/sources/" + id + "/credentials", token, "{}"},
		{http.MethodGet, "/api/v1/data-connection/agents", token, ""},
		{http.MethodPost, "/api/v1/data-connection/agents", token, `{"name":"edge","agent_url":"https://agent.local"}`},
		{http.MethodPost, "/api/v1/data-connection/agents/" + agentID + "/heartbeat", token, "{}"},
		{http.MethodDelete, "/api/v1/data-connection/agents/" + agentID, token, ""},
		{http.MethodGet, "/api/v1/data-connection/sources/" + id + "/egress-policies", token, ""},
		{http.MethodPost, "/api/v1/data-connection/sources/" + id + "/egress-policies", token, "{}"},
		{http.MethodDelete, "/api/v1/data-connection/sources/" + id + "/egress-policies/" + uuid.NewString(), token, ""},
		{http.MethodGet, "/api/v1/data-connection/sources/" + id + "/syncs", token, ""},
		{http.MethodPost, "/api/v1/data-connection/syncs", token, `{"source_id":"` + id + `","output_dataset_id":"` + uuid.NewString() + `"}`},
		{http.MethodPost, "/api/v1/data-connection/syncs/" + syncID + "/run", token, "{}"},
		{http.MethodGet, "/api/v1/data-connection/syncs/" + syncID + "/runs", token, ""},
		{http.MethodGet, "/api/v1/data-connection/sources/" + id + "/exports", token, ""},
		{http.MethodPost, "/api/v1/data-connection/sources/" + id + "/exports", token, `{"export_type":"table","input_dataset_rid":"ri.foundry.main.dataset.orders","destination_table":"public.orders_export"}`},
		{http.MethodGet, "/api/v1/data-connection/exports/" + uuid.NewString(), token, ""},
		{http.MethodPatch, "/api/v1/data-connection/exports/" + uuid.NewString(), token, `{"schedule_cron":"0 * * * *"}`},
		{http.MethodPost, "/api/v1/data-connection/exports/" + uuid.NewString() + "/run", token, "{}"},
		{http.MethodPost, "/api/v1/data-connection/exports/" + uuid.NewString() + "/start", token, "{}"},
		{http.MethodPost, "/api/v1/data-connection/exports/" + uuid.NewString() + "/stop", token, "{}"},
		{http.MethodGet, "/api/v1/data-connection/sources/" + id + "/media-set-syncs", token, ""},
		{http.MethodPost, "/api/v1/data-connection/sources/" + id + "/media-set-syncs", token, `{"kind":"MEDIA_SET_SYNC","target_media_set_rid":"ri.foundry.main.media_set.x"}`},
		{http.MethodGet, "/api/v1/data-connection/sources/" + id + "/registrations", token, ""},
		{http.MethodPost, "/api/v1/data-connection/sources/" + id + "/registrations/discover", token, "{}"},
		{http.MethodPost, "/api/v1/data-connection/sources/" + id + "/registrations/bulk", token, "{}"},
		{http.MethodPost, "/api/v1/data-connection/sources/" + id + "/registrations/bulk/preview", token, "{}"},
		{http.MethodPost, "/api/v1/data-connection/sources/" + id + "/registrations/auto", token, "{}"},
		{http.MethodGet, "/api/v1/data-connection/sources/" + id + "/registrations/auto/status", token, ""},
		{http.MethodDelete, "/api/v1/data-connection/sources/" + id + "/registrations/" + regID, token, ""},
		{http.MethodPost, "/api/v1/data-connection/sources/" + id + "/registrations/" + regID + "/query", token, "{}"},
		{http.MethodPost, "/api/v1/data-connection/sources/" + id + "/registrations/" + regID + "/query/arrow", token, "{}"},
		{http.MethodGet, "/data-connection/sources/" + id + "/registrations", token, ""},
		{http.MethodPost, "/data-connection/sources/" + id + "/registrations/discover", token, "{}"},
		{http.MethodPost, "/data-connection/sources/" + id + "/registrations/bulk", token, "{}"},
		{http.MethodPost, "/data-connection/sources/" + id + "/registrations/bulk/preview", token, "{}"},
		{http.MethodPost, "/data-connection/sources/" + id + "/registrations/auto", token, "{}"},
		{http.MethodPut, "/data-connection/sources/" + id + "/registrations/auto", token, "{}"},
		{http.MethodGet, "/data-connection/sources/" + id + "/registrations/auto/status", token, ""},
		{http.MethodDelete, "/data-connection/sources/" + id + "/registrations/" + regID, token, ""},
		{http.MethodPost, "/data-connection/sources/" + id + "/registrations/" + regID + "/query", token, "{}"},
		{http.MethodPost, "/data-connection/sources/" + id + "/registrations/" + regID + "/query/arrow", token, "{}"},
		{http.MethodGet, "/api/v1/connections", token, ""},
		{http.MethodPost, "/api/v1/connections", token, `{"name":"x","connector_type":"postgres"}`},
		{http.MethodGet, "/api/v1/connections/" + id, token, ""},
		{http.MethodDelete, "/api/v1/connections/" + id, token, ""},
		{http.MethodPost, "/api/v1/connections/" + id + "/test", token, "{}"},
		{http.MethodPost, "/api/v1/webhooks/" + uuid.NewString() + "/invoke", token, "{}"},
		{http.MethodPost, "/api/v1/sources/ri.source/virtual-tables/enable", token, `{"provider":"BIGQUERY"}`},
		{http.MethodGet, "/api/v1/sources/ri.source/virtual-tables/discover", token, ""},
		{http.MethodPost, "/api/v1/sources/ri.source/virtual-tables/register", token, `{"project_rid":"ri.project","locator":{"kind":"tabular","database":"db","schema":"public","table":"orders"},"table_type":"TABLE"}`},
		{http.MethodPost, "/api/v1/sources/ri.source/virtual-tables/bulk-register", token, `{"project_rid":"ri.project","entries":[{"locator":{"kind":"tabular","database":"db","schema":"public","table":"orders"},"table_type":"TABLE"}]}`},
		{http.MethodPost, "/api/v1/sources/ri.source/auto-registration", token, `{"project_name":"mirror","folder_mirror_kind":"NESTED","poll_interval_seconds":3600}`},
		{http.MethodPost, "/api/v1/sources/ri.source/auto-registration:scan-now", token, "{}"},
		{http.MethodDelete, "/api/v1/sources/ri.source/auto-registration", token, ""},
		{http.MethodGet, "/api/v1/virtual-tables", token, ""},
		{http.MethodGet, "/api/v1/virtual-tables/ri.virtual", token, ""},
		{http.MethodPost, "/api/v1/virtual-tables/ri.virtual/query", token, `{"limit":1}`},
		{http.MethodPatch, "/api/v1/virtual-tables/ri.virtual/update-detection", token, `{"enabled":true,"interval_seconds":3600}`},
		{http.MethodPost, "/api/v1/virtual-tables/ri.virtual/update-detection:poll-now", token, "{}"},
		{http.MethodGet, "/api/v1/virtual-tables/ri.virtual/update-detection/history", token, ""},
		{http.MethodGet, "/api/v1/virtual-tables/ri.virtual/lineage", token, ""},
		{http.MethodGet, "/iceberg/v1/config", token, ""},
		{http.MethodGet, "/iceberg/v1/namespaces", token, ""},
		{http.MethodGet, "/iceberg/v1/namespaces/default", token, ""},
		{http.MethodGet, "/iceberg/v1/namespaces/default/tables", token, ""},
		{http.MethodGet, "/iceberg/v1/namespaces/default/tables/table", token, ""},
		{http.MethodGet, "/health", "", ""},
		{http.MethodGet, "/healthz", "", ""},
		{http.MethodGet, "/readyz", "", ""},
		{http.MethodGet, "/metrics", "", ""},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			rec := exercise(srv.Handler, route.method, route.path, route.token, route.body)
			require.NotEqual(t, http.StatusNotFound, rec.Code, rec.Body.String())
			require.NotEqual(t, http.StatusMethodNotAllowed, rec.Code, rec.Body.String())
		})
	}
}

func TestOptionalAuthKeepsCatalogOpenButHandlersCanRequireClaims(t *testing.T) {
	srv, _, store := testServer(t, false)
	id := store.source.String()

	rec := exercise(srv.Handler, http.MethodGet, "/api/v1/data-connection/catalog", "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "postgresql")

	rec = exercise(srv.Handler, http.MethodGet, "/api/v1/data-connection/sources", "", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = exercise(srv.Handler, http.MethodPost, "/api/v1/data-connection/sources/"+id+"/credentials", "", "{}")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestReadyzUsesReadyChecks(t *testing.T) {
	cfg := &config.Config{}
	cfg.Service.Name = "connector-management-service"
	cfg.Service.Version = "test"
	cfg.Server.Host = "127.0.0.1"
	jwt := authmw.NewJWTConfig("test-secret")
	failing := func(context.Context) error { return errReadyTest }
	srv := server.New(cfg, jwt, &handlers.Handlers{Repo: newRouterStore()}, observability.NewMetrics(), failing)

	rec := exercise(srv.Handler, http.MethodGet, "/readyz", "", "")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "not_ready")
}

func TestDevAuthRoutesMountOnlyWhenEnabled(t *testing.T) {
	srv, _, _ := testServer(t, false)
	rec := exercise(srv.Handler, http.MethodPost, "/api/v1/auth/login", "", "{}")
	require.Equal(t, http.StatusNotFound, rec.Code)

	srv, _, _ = testServer(t, true)
	rec = exercise(srv.Handler, http.MethodPost, "/api/v1/auth/login", "", "{}")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "dev_auth_pending")
}
