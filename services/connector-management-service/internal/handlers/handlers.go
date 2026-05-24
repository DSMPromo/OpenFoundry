// Package handlers wires the HTTP endpoints for connector-management-service.
package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/domain"
	syncdomain "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/domain/sync"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/repo"
	cmruntime "github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/runtime"
)

type Store interface {
	ListConnections(ctx context.Context, ownerID *uuid.UUID) ([]models.Connection, error)
	GetConnection(ctx context.Context, id uuid.UUID) (*models.Connection, error)
	GetConnectionForOwner(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.Connection, error)
	CreateConnection(ctx context.Context, body *models.CreateConnectionRequest, ownerID uuid.UUID) (*models.Connection, error)
	UpdateConnection(ctx context.Context, id uuid.UUID, body *models.UpdateConnectionRequest) (*models.Connection, error)
	DeleteConnection(ctx context.Context, id uuid.UUID) (bool, error)
	MigrateConnectionToFoundryWorker(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.Connection, error)
	CheckSourceRole(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID, role models.SourcePermissionRole) (bool, error)
	GetSourceGovernance(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID) (*models.SourceGovernance, error)
	UpdateSourceGovernance(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID, body *models.UpdateSourceGovernanceRequest) (*models.SourceGovernance, error)
	ListSourceGovernanceAudit(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID, limit int) ([]models.SourceGovernanceAuditEvent, error)
	RecordSourceGovernanceAudit(ctx context.Context, body models.RecordSourceGovernanceAuditRequest) (*models.SourceGovernanceAuditEvent, error)
	ListSyncJobs(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.SyncJob, error)
	GetSyncJob(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.SyncJob, error)
	CreateSyncJob(ctx context.Context, body *models.CreateSyncJobRequest, ownerID uuid.UUID) (*models.SyncJob, error)
	UpdateSyncJob(ctx context.Context, id uuid.UUID, body *models.UpdateSyncJobRequest, ownerID uuid.UUID) (*models.SyncJob, error)
	RunSyncJob(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.SyncRun, error)
	ListSyncRuns(ctx context.Context, syncID uuid.UUID, ownerID uuid.UUID) ([]models.SyncRun, error)
	ListDataExports(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.DataExport, error)
	GetDataExport(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.DataExport, error)
	CreateDataExport(ctx context.Context, body *models.CreateDataExportRequest, ownerID uuid.UUID) (*models.DataExport, error)
	UpdateDataExport(ctx context.Context, id uuid.UUID, body *models.UpdateDataExportRequest, ownerID uuid.UUID) (*models.DataExport, error)
	RunDataExport(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.DataExport, error)
	StartDataExport(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.DataExport, error)
	StopDataExport(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.DataExport, error)
	ListCredentials(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.CredentialResponse, error)
	SetCredential(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, kind string, ciphertext []byte, fingerprint string) (*models.CredentialResponse, error)
	ListConnectorAgents(ctx context.Context, ownerID uuid.UUID) ([]models.ConnectorAgent, error)
	GetConnectorAgent(ctx context.Context, id uuid.UUID) (*models.ConnectorAgent, error)
	RegisterConnectorAgent(ctx context.Context, body *models.RegisterAgentRequest, ownerID uuid.UUID) (*models.ConnectorAgent, error)
	HeartbeatConnectorAgent(ctx context.Context, id uuid.UUID, body *models.AgentHeartbeatRequest, ownerID uuid.UUID) (*models.ConnectorAgent, error)
	DeleteConnectorAgent(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (bool, error)
	ListSourcePolicies(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.SourcePolicyBindingResponse, error)
	AttachPolicy(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, policyID uuid.UUID, kind string) (*models.SourcePolicyBindingResponse, error)
	DetachPolicy(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, policyID uuid.UUID) (bool, error)
	GetSourceCodeImport(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) (*models.SourceCodeImport, error)
	UpdateSourceCodeImport(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, body *models.UpdateSourceCodeImportRequest) (*models.SourceCodeImport, error)
	ResolveSourceCodeImportBuildStart(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, body *models.ResolveSourceCodeImportBuildRequest) (*models.SourceCodeImportBuildResolution, error)
	ListMediaSetSyncs(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) ([]models.MediaSetSync, error)
	GetMediaSetSync(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*models.MediaSetSync, error)
	CreateMediaSetSync(ctx context.Context, sourceID uuid.UUID, body *models.CreateMediaSetSyncRequest, ownerID uuid.UUID) (*models.MediaSetSync, error)
	UpdateMediaSetSync(ctx context.Context, id uuid.UUID, body *models.UpdateMediaSetSyncRequest, ownerID uuid.UUID) (*models.MediaSetSync, error)
	EnableVirtualTableSource(ctx context.Context, sourceRID string, body *models.EnableVirtualTableSourceRequest) (*models.VirtualTableSourceLink, error)
	DiscoverVirtualTableCatalog(ctx context.Context, sourceRID string, path string) ([]models.DiscoveredEntry, error)
	CreateVirtualTable(ctx context.Context, sourceRID string, actorID string, body *models.CreateVirtualTableRequest) (*models.VirtualTable, error)
	BulkRegisterVirtualTables(ctx context.Context, sourceRID string, actorID string, body *models.VirtualTableBulkRegisterRequest) (*models.VirtualTableBulkRegisterResponse, error)
	EnableVirtualTableAutoRegistration(ctx context.Context, sourceRID string, body *models.EnableAutoRegistrationRequest) (*models.VirtualTableSourceLink, error)
	DisableVirtualTableAutoRegistration(ctx context.Context, sourceRID string) error
	ScanVirtualTableAutoRegistrationNow(ctx context.Context, sourceRID string) (*models.AutoRegistrationScanSummary, error)
	ListVirtualTables(ctx context.Context, ownerID string, project, source, name, tableType string, limit int) ([]models.VirtualTable, error)
	GetVirtualTable(ctx context.Context, rid string, ownerID string) (*models.VirtualTable, error)
	SetVirtualTableUpdateDetection(ctx context.Context, rid string, ownerID string, body *models.UpdateDetectionToggle) (*models.VirtualTable, error)
	PollVirtualTableUpdateDetection(ctx context.Context, rid string, ownerID string) (*models.PollResult, error)
	ListVirtualTableUpdateDetectionHistory(ctx context.Context, rid string, ownerID string, limit int) ([]models.PollHistoryRow, error)
	GetVirtualTableLineage(ctx context.Context, rid string, ownerID string) (*models.VirtualTableLineageResponse, error)
	ListRegistrations(ctx context.Context, sourceID uuid.UUID) ([]models.ConnectionRegistration, error)
	UpsertRegistration(ctx context.Context, sourceID uuid.UUID, source models.DiscoveredSource, mode string, autoSync bool, updateDetection bool, targetDatasetID *uuid.UUID, metadata json.RawMessage) (*models.ConnectionRegistration, error)
	GetRegistration(ctx context.Context, sourceID uuid.UUID, registrationID uuid.UUID) (*models.ConnectionRegistration, error)
	DeleteRegistration(ctx context.Context, sourceID uuid.UUID, registrationID uuid.UUID) (bool, error)
	UpdateConnectionConfig(ctx context.Context, id uuid.UUID, config json.RawMessage) (*models.Connection, error)
	ListIcebergNamespaces(ctx context.Context) ([]models.Connection, error)
	GetIcebergConnection(ctx context.Context, namespace string) (*models.Connection, error)
	ListIcebergTables(ctx context.Context, connectionID uuid.UUID) ([]models.ConnectionRegistration, error)
	GetSourceRetryPolicy(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) (*models.SourceRetryPolicy, error)
	UpsertSourceRetryPolicy(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, actorID *string, policy models.SourceRetryPolicy) (*models.SourceRetryPolicy, error)
	ListSyncRunFailuresForSource(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, limit int) ([]models.RetryRecoveryRunSummary, error)
	RecordMediaSetSyncRun(ctx context.Context, syncID uuid.UUID, ownerID uuid.UUID, run models.MediaSetSyncRun) (*models.MediaSetSyncRun, error)
	ListMediaSetSyncRuns(ctx context.Context, syncID uuid.UUID, ownerID uuid.UUID, limit int) ([]models.MediaSetSyncRun, error)
	MediaSetSyncUsageForSource(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID) (map[uuid.UUID]models.MediaSetSyncUsageSummary, error)
	GetDeadLetterSink(ctx context.Context, syncDefID uuid.UUID, ownerID uuid.UUID) (*models.DeadLetterSink, error)
	UpsertDeadLetterSink(ctx context.Context, syncDefID uuid.UUID, ownerID uuid.UUID, actorID *string, req models.UpdateDeadLetterSinkRequest) (*models.DeadLetterSink, error)
	RecordQuarantinedRecord(ctx context.Context, syncDefID uuid.UUID, ownerID uuid.UUID, body models.RecordQuarantineRequest, sink models.DeadLetterSink, recordedAt time.Time) (*models.QuarantinedRecord, error)
	ListQuarantinedRecords(ctx context.Context, syncDefID uuid.UUID, ownerID uuid.UUID, category models.QuarantineFailureCategory, limit int) ([]models.QuarantinedRecord, error)
	MarkQuarantinedRecordsForReplay(ctx context.Context, syncDefID uuid.UUID, ownerID uuid.UUID, actorID *string, recordIDs []uuid.UUID, now time.Time) (int, error)
}

type syncRunCompleter interface {
	CompleteSyncRun(ctx context.Context, runID uuid.UUID, ownerID uuid.UUID, status string, bytesWritten int64, filesWritten int64, errMsg *string, ingestJobID *string, datasetVersionID *uuid.UUID, contentHash *string) (*models.SyncRun, error)
}

type previousDatasetVersionLookup interface {
	PreviousDatasetVersionForHash(ctx context.Context, syncDefID uuid.UUID, contentHash string) (*uuid.UUID, error)
}

type datasetVersionRecorder interface {
	RecordDatasetVersionOnRun(ctx context.Context, runID uuid.UUID, datasetVersionID uuid.UUID, contentHash string) error
}

type RuntimeConfig struct {
	DatasetServiceURL            string
	PipelineServiceURL           string
	OntologyServiceURL           string
	IngestionReplicationGRPCURL  string
	NetworkBoundaryServiceURL    string
	SyncPollIntervalSecs         uint64
	AllowPrivateNetworkEgress    bool
	AllowedEgressHosts           []string
	AgentStaleAfterSecs          uint64
	CredentialEncryptionKey      string
	CredentialKey                [32]byte
	SecretManagerURL             string
	OutboxEnabled                bool
	AutoRegistrationIntervalSecs uint64
	VendedCredentialsTTLSeconds  int64
}

type Handlers struct {
	Repo              Store
	AdapterRegistry   *adapters.Registry
	MediaSetRuntime   MediaSetRuntime
	IngestionRuntime  cmruntime.IngestionPort
	DatasetVersioning cmruntime.DatasetVersioningPort
	Config            RuntimeConfig
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type routeError struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeRoutePending(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, routeError{Error: code, Code: code, Message: message})
}

func routeUUIDParam(r *http.Request, names ...string) (uuid.UUID, string, error) {
	for _, name := range names {
		if raw := chi.URLParam(r, name); raw != "" {
			id, err := uuid.Parse(raw)
			return id, name, err
		}
	}
	return uuid.Nil, names[0], errors.New("missing route parameter")
}

func (h *Handlers) notImplemented(w http.ResponseWriter, r *http.Request, code string, requireAuth bool) {
	if requireAuth {
		if _, ok := requireClaims(w, r); !ok {
			return
		}
	}
	writeRoutePending(w, http.StatusNotFound, code, "development authentication route is disabled; no production capability is exposed")
}

func (h *Handlers) GetConnectorCatalog(w http.ResponseWriter, r *http.Request) {
	catalog := models.BuildGalleryCatalog()
	catalog.CapabilityMatrix = h.connectorCapabilityMatrix()
	writeJSON(w, http.StatusOK, catalog)
}

func (h *Handlers) GetConnectorCapabilityMatrix(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string][]models.ConnectorCapabilityMatrix{"capability_matrix": h.connectorCapabilityMatrix()})
}

func (h *Handlers) connectorCapabilityMatrix() []models.ConnectorCapabilityMatrix {
	profiles := models.ConnectorProfiles()
	connectorTypes := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		connectorTypes = append(connectorTypes, profile.ConnectorType)
	}
	registry := h.AdapterRegistry
	if registry == nil {
		registry = adapters.NewRegistry()
	}
	return registry.CapabilityMatrix(connectorTypes)
}

func (h *Handlers) GetSourceHealth(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleView) {
		return
	}
	source, err := h.Repo.GetConnectionForOwner(r.Context(), sourceID, claims.Sub)
	if err != nil {
		slog.Error("load source for health", slog.String("source_id", sourceID.String()), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load source")
		return
	}
	if source == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	ownerID := source.OwnerID
	credentials, err := h.Repo.ListCredentials(r.Context(), sourceID, ownerID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list credential health")
		return
	}
	policies, err := h.Repo.ListSourcePolicies(r.Context(), sourceID, ownerID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list network policy health")
		return
	}
	agents, err := h.Repo.ListConnectorAgents(r.Context(), ownerID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list agent health")
		return
	}
	for i := range agents {
		agents[i] = h.enrichConnectorAgent(agents[i])
	}
	syncs, err := h.Repo.ListSyncJobs(r.Context(), sourceID, ownerID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list sync health")
		return
	}
	syncRuns := map[uuid.UUID][]models.SyncRun{}
	for _, sync := range syncs {
		runs, err := h.Repo.ListSyncRuns(r.Context(), sync.ID, ownerID)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, "failed to list sync run health")
			return
		}
		syncRuns[sync.ID] = runs
	}
	exports, err := h.Repo.ListDataExports(r.Context(), sourceID, ownerID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list export health")
		return
	}
	codeImport, err := h.Repo.GetSourceCodeImport(r.Context(), sourceID, ownerID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list source import health")
		return
	}
	sourceRID := models.SourceRIDForConnection(sourceID)
	virtualTables, err := h.Repo.ListVirtualTables(r.Context(), ownerID.String(), "", sourceRID, "", "", 500)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list virtual table health")
		return
	}
	webhookHistory := []models.WebhookHistoryEntry{}
	if lister, ok := h.Repo.(webhookHistoryLister); ok {
		webhookHistory, err = lister.ListWebhookHistory(r.Context(), sourceID, 100)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, "failed to list webhook health")
			return
		}
	}
	staleAfter := time.Duration(h.Config.AgentStaleAfterSecs) * time.Second
	now := time.Now().UTC()
	retryRecovery, err := h.loadRetryRecoverySummary(r.Context(), sourceID, ownerID, now)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to compute retry recovery summary")
		return
	}
	summary := models.BuildDataConnectionHealthSummary(models.DataConnectionHealthInput{
		Source:          *source,
		Agents:          agents,
		Credentials:     credentials,
		Policies:        policies,
		Syncs:           syncs,
		SyncRuns:        syncRuns,
		Exports:         exports,
		WebhookHistory:  webhookHistory,
		CodeImport:      codeImport,
		VirtualTables:   virtualTables,
		RetryRecovery:   retryRecovery,
		CheckedAt:       now,
		AgentStaleAfter: staleAfter,
	})
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handlers) GetConnectorContracts(w http.ResponseWriter, r *http.Request) {
	catalog := models.BuildConnectorContractCatalog()
	catalog.CapabilityMatrix = h.connectorCapabilityMatrix()
	writeJSON(w, http.StatusOK, catalog)
}

func (h *Handlers) ListStreamingSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string][]models.StreamingSourceContract{"data": models.StreamingSourceContracts()})
}

func (h *Handlers) GetConnectionCapabilities(w http.ResponseWriter, r *http.Request) {
	id, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if h.Repo == nil {
		writeRoutePending(w, http.StatusServiceUnavailable, "repository_unavailable", "connection repository is not configured")
		return
	}
	connection, err := h.Repo.GetConnection(r.Context(), id)
	if err != nil {
		slog.Error("connection capability lookup failed", "error", err)
		writeJSONErr(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if connection == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	contract, ok := models.ConnectorProfile(connection.ConnectorType)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no connector contract available for " + connection.ConnectorType})
		return
	}
	capabilities := h.connectionEffectiveCapabilities(*connection, contract)
	writeJSON(w, http.StatusOK, models.ConnectionCapabilityResponse{ConnectionID: connection.ID, ConnectorType: connection.ConnectorType, Status: connection.Status, Contract: contract, Capabilities: capabilities})
}

func (h *Handlers) connectionEffectiveCapabilities(connection models.Connection, contract models.ConnectorContractProfile) models.ConnectionEffectiveCapabilities {
	configKeys, inferred := models.ConfigKeys(connection.Config)
	workers := []string{"foundry"}
	if contract.Auth.SupportsPrivateNetworkAgent {
		workers = append(workers, "agent")
	}
	warnings := []string{}
	privateAllowed := h.Config.AllowPrivateNetworkEgress
	if len(h.Config.AllowedEgressHosts) > 0 {
		privateAllowed = true
	}
	requiresAgent := contract.Auth.SupportsPrivateNetworkAgent && !privateAllowed
	if requiresAgent {
		warnings = append(warnings, "private network egress is disabled; route through a connector agent or allow egress for this source")
	}
	exportTypes := models.SupportedDataExportTypes(connection.ConnectorType)
	return models.ConnectionEffectiveCapabilities{
		ConnectionID:                connection.ID,
		ConnectorType:               connection.ConnectorType,
		Status:                      connection.Status,
		Modes:                       append([]string(nil), contract.Sync.Modes...),
		Workers:                     workers,
		SupportsConnectionTesting:   contract.Testing.SupportsConnectionTesting,
		SupportsDiscovery:           contract.Testing.SupportsDiscovery,
		SupportsSchemaIntrospection: contract.Testing.SupportsSchemaIntrospection,
		SupportsIncremental:         contract.Sync.SupportsIncremental || inferred.HasIncrementalCursor,
		SupportsCDC:                 contract.Sync.SupportsCDC && (inferred.HasCDCSelector || connection.ConnectorType == "kafka" || connection.ConnectorType == "kinesis" || connection.ConnectorType == "iot"),
		SupportsZeroCopy:            contract.Sync.SupportsZeroCopy || inferred.RequestsZeroCopy,
		SupportsFileExport:          models.ConnectorSupportsDataExport(connection.ConnectorType, models.DataExportTypeFile),
		SupportsTableExport:         models.ConnectorSupportsDataExport(connection.ConnectorType, models.DataExportTypeTable),
		SupportsStreamingExport:     models.ConnectorSupportsDataExport(connection.ConnectorType, models.DataExportTypeStreaming),
		SupportedExportTypes:        exportTypes,
		SupportsPrivateNetworkAgent: contract.Auth.SupportsPrivateNetworkAgent,
		RequiresPrivateNetworkAgent: requiresAgent,
		PrivateNetworkEgressAllowed: privateAllowed,
		AllowedEgressHosts:          append([]string(nil), h.Config.AllowedEgressHosts...),
		ConfigKeys:                  configKeys,
		ConfigInferred:              inferred,
		PolicyWarnings:              warnings,
		FoundryCompute:              models.FoundryCompute{PythonSingleNode: true, PythonSpark: true, PipelineBuilderSingleNode: true, PipelineBuilderSpark: true},
	}
}

var validCredentialKinds = map[string]bool{
	"password": true, "api_key": true, "oauth_token": true, "aws_keys": true, "service_account_json": true,
}

func (h *Handlers) ListCredentials(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleCodeImport) {
		return
	}
	items, err := h.Repo.ListCredentials(r.Context(), sourceID, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list credentials")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handlers) SetCredential(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	var body models.SetCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.Kind = strings.TrimSpace(body.Kind)
	if !validCredentialKinds[body.Kind] {
		writeJSONErr(w, http.StatusBadRequest, fmt.Sprintf("unsupported credential kind: %s", body.Kind))
		return
	}
	if body.Value == "" {
		writeJSONErr(w, http.StatusBadRequest, "credential value required")
		return
	}
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleEdit) {
		return
	}
	digest := sha256.Sum256([]byte(body.Value))
	fingerprint := fmt.Sprintf("%x", digest[:])
	ciphertext, err := domain.EncryptCredential(h.Config.CredentialKey, []byte(body.Value))
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "credential encryption failed")
		return
	}
	v, err := h.Repo.SetCredential(r.Context(), sourceID, claims.Sub, body.Kind, ciphertext, fingerprint)
	if errors.Is(err, pgx.ErrNoRows) || v == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to store credential")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) ListConnectorAgents(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	items, err := h.Repo.ListConnectorAgents(r.Context(), claims.Sub)
	if err != nil {
		slog.Error("list connector agents", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	for i := range items {
		items[i] = h.enrichConnectorAgent(items[i])
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.ConnectorAgent]{Items: items})
}

func (h *Handlers) RegisterConnectorAgent(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	var body models.RegisterAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.AgentURL = strings.TrimSpace(body.AgentURL)
	if body.Name == "" || body.AgentURL == "" {
		writeJSONErr(w, http.StatusBadRequest, "name and agent_url required")
		return
	}
	parsed, err := url.ParseRequestURI(body.AgentURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		writeJSONErr(w, http.StatusBadRequest, "agent_url must be an http(s) URL")
		return
	}
	if !normalizeJSONObject(&body.Capabilities, "capabilities", w) {
		return
	}
	if !normalizeJSONObject(&body.Metadata, "metadata", w) {
		return
	}
	models.NormalizeRegisterAgentRequest(&body, parsed.Hostname())
	v, err := h.Repo.RegisterConnectorAgent(r.Context(), &body, claims.Sub)
	if err != nil {
		slog.Error("register connector agent", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to register agent")
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusConflict, "agent_url already registered")
		return
	}
	enriched := h.enrichConnectorAgent(*v)
	writeJSON(w, http.StatusCreated, enriched)
}

func (h *Handlers) HeartbeatConnectorAgent(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, _, err := routeUUIDParam(r, "id", "agent_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid agent_id")
		return
	}
	var body models.AgentHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !normalizeJSONObject(&body.Capabilities, "capabilities", w) {
		return
	}
	if !normalizeJSONObject(&body.Metadata, "metadata", w) {
		return
	}
	models.NormalizeAgentHeartbeatRequest(&body)
	v, err := h.Repo.HeartbeatConnectorAgent(r.Context(), id, &body, claims.Sub)
	if err != nil {
		slog.Error("connector agent heartbeat", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to update heartbeat")
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "agent not found")
		return
	}
	enriched := h.enrichConnectorAgent(*v)
	writeJSON(w, http.StatusOK, enriched)
}

func (h *Handlers) DeleteConnectorAgent(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, _, err := routeUUIDParam(r, "id", "agent_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid agent_id")
		return
	}
	deleted, err := h.Repo.DeleteConnectorAgent(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to delete agent")
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "agent not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListSourceAgents returns the subset of agents currently associated with a
// source via their `connected_sources` list. Used by the Select agents panel
// on the source detail page.
func (h *Handlers) ListSourceAgents(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source id")
		return
	}
	agents, err := h.Repo.ListConnectorAgents(r.Context(), claims.Sub)
	if err != nil {
		slog.Error("list source agents", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	filtered := make([]models.ConnectorAgent, 0, len(agents))
	for i := range agents {
		for _, cs := range agents[i].ConnectedSources {
			if cs.SourceID == sourceID {
				filtered = append(filtered, h.enrichConnectorAgent(agents[i]))
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.ConnectorAgent]{Items: filtered})
}

// AssignSourceToAgent appends the source to an agent's `connected_sources`
// list, preserving the agent's other heartbeat fields.
func (h *Handlers) AssignSourceToAgent(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source id")
		return
	}
	var body struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	agentID, err := uuid.Parse(strings.TrimSpace(body.AgentID))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid agent_id")
		return
	}
	conn, err := h.Repo.GetConnection(r.Context(), sourceID)
	if err != nil {
		slog.Error("assign source to agent: get connection", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load source")
		return
	}
	if conn == nil || conn.OwnerID != claims.Sub {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	agent, err := h.Repo.GetConnectorAgent(r.Context(), agentID)
	if err != nil {
		slog.Error("assign source to agent: get agent", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load agent")
		return
	}
	if agent == nil || agent.OwnerID != claims.Sub {
		writeJSONErr(w, http.StatusNotFound, "agent not found")
		return
	}
	sources := append([]models.AgentConnectedSource{}, agent.ConnectedSources...)
	for _, cs := range sources {
		if cs.SourceID == sourceID {
			// Already assigned — return the existing agent unchanged.
			writeJSON(w, http.StatusOK, h.enrichConnectorAgent(*agent))
			return
		}
	}
	sources = append(sources, models.AgentConnectedSource{
		SourceID:      sourceID,
		SourceName:    conn.Name,
		ConnectorType: conn.ConnectorType,
		Status:        "assigned",
	})
	heartbeat := &models.AgentHeartbeatRequest{
		Capabilities:                   agent.Capabilities,
		Metadata:                       agent.Metadata,
		ConnectedSources:               sources,
		SupportedConnectorCapabilities: agent.SupportedConnectorCapabilities,
		AssignedProxyPolicies:          agent.AssignedProxyPolicies,
		ConnectionFailures:             agent.ConnectionFailures,
	}
	models.NormalizeAgentHeartbeatRequest(heartbeat)
	updated, err := h.Repo.HeartbeatConnectorAgent(r.Context(), agentID, heartbeat, claims.Sub)
	if err != nil {
		slog.Error("assign source to agent: heartbeat", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to assign agent")
		return
	}
	if updated == nil {
		writeJSONErr(w, http.StatusNotFound, "agent not found")
		return
	}
	writeJSON(w, http.StatusOK, h.enrichConnectorAgent(*updated))
}

// UnassignSourceFromAgent removes a source from an agent's `connected_sources`
// list, preserving the agent's other heartbeat fields.
func (h *Handlers) UnassignSourceFromAgent(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "source_id")))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source id")
		return
	}
	agentID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "agent_id")))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid agent_id")
		return
	}
	agent, err := h.Repo.GetConnectorAgent(r.Context(), agentID)
	if err != nil {
		slog.Error("unassign source from agent: get agent", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load agent")
		return
	}
	if agent == nil || agent.OwnerID != claims.Sub {
		writeJSONErr(w, http.StatusNotFound, "agent not found")
		return
	}
	filtered := make([]models.AgentConnectedSource, 0, len(agent.ConnectedSources))
	removed := false
	for _, cs := range agent.ConnectedSources {
		if cs.SourceID == sourceID {
			removed = true
			continue
		}
		filtered = append(filtered, cs)
	}
	if !removed {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	heartbeat := &models.AgentHeartbeatRequest{
		Capabilities:                   agent.Capabilities,
		Metadata:                       agent.Metadata,
		ConnectedSources:               filtered,
		SupportedConnectorCapabilities: agent.SupportedConnectorCapabilities,
		AssignedProxyPolicies:          agent.AssignedProxyPolicies,
		ConnectionFailures:             agent.ConnectionFailures,
	}
	models.NormalizeAgentHeartbeatRequest(heartbeat)
	if _, err := h.Repo.HeartbeatConnectorAgent(r.Context(), agentID, heartbeat, claims.Sub); err != nil {
		slog.Error("unassign source from agent: heartbeat", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to unassign agent")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MigrateToFoundryWorkerRequest carries the wizard's optional context so the
// audit trail can record what the user picked. None of the fields are
// required: the migration itself only needs the source id and the caller's
// claims; the wizard data is forwarded to slog for traceability.
type MigrateToFoundryWorkerRequest struct {
	RepresentativeAgentID string   `json:"representative_agent_id,omitempty"`
	Certificates          []string `json:"certificates,omitempty"`
	DriverID              string   `json:"driver_id,omitempty"`
	EgressPolicyIDs       []string `json:"egress_policy_ids,omitempty"`
	Acknowledged          bool     `json:"acknowledged,omitempty"`
}

// MigrateToFoundryWorker snapshots the source's current config and flips its
// `worker` key to "foundry". The previous snapshot lives in
// `previous_config_snapshot` for 30 days to support revert.
func (h *Handlers) MigrateToFoundryWorker(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source id")
		return
	}
	var body MigrateToFoundryWorkerRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid body")
			return
		}
	}
	if !body.Acknowledged {
		writeJSONErr(w, http.StatusBadRequest, "acknowledged must be true")
		return
	}
	conn, err := h.Repo.GetConnection(r.Context(), sourceID)
	if err != nil {
		slog.Error("migrate to foundry worker: load source", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load source")
		return
	}
	if conn == nil || conn.OwnerID != claims.Sub {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	updated, err := h.Repo.MigrateConnectionToFoundryWorker(r.Context(), sourceID, claims.Sub)
	if err != nil {
		slog.Error("migrate to foundry worker", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to migrate source")
		return
	}
	if updated == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	slog.Info(
		"migrated source to foundry worker",
		slog.String("source_id", sourceID.String()),
		slog.String("representative_agent_id", body.RepresentativeAgentID),
		slog.Int("certificate_count", len(body.Certificates)),
		slog.String("driver_id", body.DriverID),
		slog.Int("egress_policy_count", len(body.EgressPolicyIDs)),
	)
	writeJSON(w, http.StatusOK, updated)
}

func normalizeJSONObject(raw *json.RawMessage, field string, w http.ResponseWriter) bool {
	trimmed := strings.TrimSpace(string(*raw))
	if trimmed == "" || trimmed == "null" {
		*raw = json.RawMessage(`{}`)
		return true
	}
	if !json.Valid(*raw) {
		writeJSONErr(w, http.StatusBadRequest, field+" must be valid JSON")
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal(*raw, &obj); err != nil {
		writeJSONErr(w, http.StatusBadRequest, field+" must be a JSON object")
		return false
	}
	return true
}

func (h *Handlers) enrichConnectorAgent(agent models.ConnectorAgent) models.ConnectorAgent {
	staleAfter := time.Duration(h.Config.AgentStaleAfterSecs) * time.Second
	if staleAfter <= 0 {
		staleAfter = 120 * time.Second
	}
	return models.ConnectorAgentWithHealth(agent, time.Now().UTC(), staleAfter)
}

func (h *Handlers) ListSourcePolicies(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleView) {
		return
	}
	items, err := h.Repo.ListSourcePolicies(r.Context(), sourceID, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list source policies")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handlers) AttachPolicy(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	var body models.AttachPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.PolicyID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "policy_id required")
		return
	}
	kind := strings.TrimSpace(body.Kind)
	if kind == "" {
		kind = "direct"
	}
	if kind != "direct" && kind != "agent_proxy" && kind != "same_region_bucket" {
		writeJSONErr(w, http.StatusBadRequest, "kind must be 'direct', 'agent_proxy', or 'same_region_bucket'")
		return
	}
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleEdit) {
		return
	}
	v, err := h.Repo.AttachPolicy(r.Context(), sourceID, claims.Sub, body.PolicyID, kind)
	if errors.Is(err, pgx.ErrNoRows) || v == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to attach policy")
		return
	}
	h.recordSourceUseAudit(r.Context(), sourceID, claims.Sub, "egress_policy_attached", "network_egress", body.PolicyID.String(), models.SourceRIDForConnection(sourceID), "Attached egress policy to source", map[string]any{
		"policy_id":             body.PolicyID.String(),
		"kind":                  kind,
		"potential_data_export": true,
		"audit_categories":      []string{"networkEgress", "dataExport", "managementPermissions"},
	})
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) DetachPolicy(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "source_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	policyID, _, err := routeUUIDParam(r, "policy_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid policy_id")
		return
	}
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleEdit) {
		return
	}
	deleted, err := h.Repo.DetachPolicy(r.Context(), sourceID, claims.Sub, policyID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to detach policy")
		return
	}
	if !deleted {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.recordSourceUseAudit(r.Context(), sourceID, claims.Sub, "egress_policy_detached", "network_egress", policyID.String(), models.SourceRIDForConnection(sourceID), "Detached egress policy from source", map[string]any{
		"policy_id":             policyID.String(),
		"potential_data_export": true,
		"audit_categories":      []string{"networkEgress", "dataExport", "managementPermissions"},
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) GetSourceGovernance(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "source_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	v, err := h.Repo.GetSourceGovernance(r.Context(), sourceID, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load source governance")
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) UpdateSourceGovernance(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "source_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	var body models.UpdateSourceGovernanceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	v, err := h.Repo.UpdateSourceGovernance(r.Context(), sourceID, claims.Sub, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to update source governance")
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusForbidden, "missing source role: "+string(models.SourceRoleOwner))
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) ListSourceGovernanceAudit(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "source_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSONErr(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}
	items, err := h.Repo.ListSourceGovernanceAudit(r.Context(), sourceID, claims.Sub, limit)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list source audit")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.SourceGovernanceAuditEvent]{Items: items})
}

func (h *Handlers) GetSourceCodeImport(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "source_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleView) {
		return
	}
	v, err := h.Repo.GetSourceCodeImport(r.Context(), sourceID, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) UpdateSourceCodeImport(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "source_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleCodeImport) {
		return
	}
	var body models.UpdateSourceCodeImportRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	v, err := h.Repo.UpdateSourceCodeImport(r.Context(), sourceID, claims.Sub, &body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) ResolveSourceCodeImportBuildStart(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "source_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleCodeImport) {
		return
	}
	var body models.ResolveSourceCodeImportBuildRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeJSONErr(w, http.StatusBadRequest, "invalid body")
			return
		}
	}
	v, err := h.Repo.ResolveSourceCodeImportBuildStart(r.Context(), sourceID, claims.Sub, &body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	h.recordSourceUseAudit(r.Context(), sourceID, claims.Sub, "code_import_build_start", "code_import", stringPtrValue(v.BuildRID), stringPtrValue(v.RepositoryRID), "Resolved source import for code build start", map[string]any{
		"uses_foundry_inputs": v.ExportPolicyDecision.UsesFoundryInputs,
		"build_allowed":       v.ExportPolicyDecision.BuildAllowed,
	})
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) ListRuns(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	syncID, _, err := routeUUIDParam(r, "id", "sync_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid sync id")
		return
	}
	items, err := h.Repo.ListSyncRuns(r.Context(), syncID, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list sync runs")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handlers) DevAuthLogin(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "dev_auth_pending", false)
}

func (h *Handlers) DevAuthRefresh(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "dev_auth_pending", false)
}

func (h *Handlers) DevAuthBootstrapStatus(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "dev_auth_pending", false)
}

func (h *Handlers) DevAuthMe(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "dev_auth_me_pending", true)
}

func (h *Handlers) ListConnections(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ownerID := &claims.Sub
	if raw := r.URL.Query().Get("owner_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid owner_id")
			return
		}
		if id == claims.Sub || claims.HasRole("admin") {
			ownerID = &id
		}
	}
	items, err := h.Repo.ListConnections(r.Context(), ownerID)
	if err != nil {
		slog.Error("list connections", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list connections")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.Connection]{Items: items})
}

func (h *Handlers) GetConnection(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !h.requireSourceRole(w, r, id, claims.Sub, models.SourceRoleView) {
		return
	}
	v, err := h.Repo.GetConnection(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "connection not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) CreateConnection(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body models.CreateConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Name == "" || body.ConnectorType == "" {
		writeJSONErr(w, http.StatusBadRequest, "name and connector_type required")
		return
	}
	if len(body.Config) > 0 && !json.Valid(body.Config) {
		writeJSONErr(w, http.StatusBadRequest, "config must be valid JSON")
		return
	}
	normalized, err := normalizeConnectionConfig(body.ConnectorType, body.Config)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	body.Config = normalized
	v, err := h.Repo.CreateConnection(r.Context(), &body, caller.Sub)
	if err != nil {
		slog.Error("create connection", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (h *Handlers) UpdateConnection(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !h.requireSourceRole(w, r, id, claims.Sub, models.SourceRoleEdit) {
		return
	}
	var body models.UpdateConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(body.Config) > 0 && !json.Valid(body.Config) {
		writeJSONErr(w, http.StatusBadRequest, "config must be valid JSON")
		return
	}
	if len(body.Config) > 0 {
		current, err := h.Repo.GetConnection(r.Context(), id)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if current == nil {
			writeJSONErr(w, http.StatusNotFound, "connection not found")
			return
		}
		normalized, err := normalizeConnectionConfig(current.ConnectorType, body.Config)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, err.Error())
			return
		}
		body.Config = normalized
	}
	v, err := h.Repo.UpdateConnection(r.Context(), id, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "connection not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func normalizeConnectionConfig(connectorType string, raw json.RawMessage) (json.RawMessage, error) {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(connectorType), "-", "_")) {
	case "rest_api":
		return models.NormalizeRESTAPISourceConfig(raw)
	default:
		if len(raw) == 0 {
			return raw, nil
		}
		return raw, nil
	}
}

func (h *Handlers) DeleteConnection(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !h.requireSourceRole(w, r, id, claims.Sub, models.SourceRoleOwner) {
		return
	}
	deleted, err := h.Repo.DeleteConnection(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "connection not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func requireClaims(w http.ResponseWriter, r *http.Request) (*authmw.Claims, bool) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	return claims, true
}

func (h *Handlers) requireSourceRole(w http.ResponseWriter, r *http.Request, sourceID uuid.UUID, actorID uuid.UUID, role models.SourcePermissionRole) bool {
	if h.Repo == nil {
		writeRoutePending(w, http.StatusServiceUnavailable, "repository_unavailable", "connection repository is not configured")
		return false
	}
	allowed, err := h.Repo.CheckSourceRole(r.Context(), sourceID, actorID, role)
	if err != nil {
		slog.Error("source permission check failed", slog.String("source_id", sourceID.String()), slog.String("role", string(role)), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "source permission check failed")
		return false
	}
	if !allowed {
		writeJSONErr(w, http.StatusForbidden, "missing source role: "+string(role))
		return false
	}
	return true
}

func (h *Handlers) recordSourceUseAudit(ctx context.Context, sourceID uuid.UUID, actorID uuid.UUID, action string, capability string, jobRID string, downstreamRID string, message string, metadata map[string]any) {
	if h.Repo == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	_, err := h.Repo.RecordSourceGovernanceAudit(ctx, models.RecordSourceGovernanceAuditRequest{
		SourceID:              sourceID,
		ActorID:               &actorID,
		EventType:             "source_use",
		Action:                action,
		Result:                "succeeded",
		Capability:            capability,
		JobRID:                jobRID,
		DownstreamResourceRID: downstreamRID,
		Message:               message,
		Metadata:              metadata,
	})
	if err != nil {
		slog.Warn("record source use audit failed", slog.String("source_id", sourceID.String()), slog.String("action", action), slog.String("error", err.Error()))
	}
}

func stringPtrValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return strings.TrimSpace(*ptr)
}

func dataExportDestinationRID(v *models.DataExport) string {
	if v == nil {
		return ""
	}
	for _, candidate := range []*string{v.DestinationTable, v.DestinationTopic, v.DestinationPath, v.InputDatasetRID, v.InputStreamID} {
		if value := stringPtrValue(candidate); value != "" {
			return value
		}
	}
	if v.InputDatasetID != nil && *v.InputDatasetID != uuid.Nil {
		return v.InputDatasetID.String()
	}
	return ""
}

func syncString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func syncOutputRID(v *models.SyncJob) string {
	if v == nil {
		return ""
	}
	for _, candidate := range []*string{v.OutputStreamID, v.OutputMediaSetID} {
		if value := stringPtrValue(candidate); value != "" {
			return value
		}
	}
	if v.OutputDatasetID != nil && *v.OutputDatasetID != uuid.Nil {
		return v.OutputDatasetID.String()
	}
	return ""
}

func (h *Handlers) ListSyncJobs(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	items, err := h.Repo.ListSyncJobs(r.Context(), sourceID, claims.Sub)
	if err != nil {
		slog.Error("list sync jobs", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list sync jobs")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handlers) GetSyncJob(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, param, err := routeUUIDParam(r, "sync_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+param)
		return
	}
	v, err := h.Repo.GetSyncJob(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "sync job not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) ListDataExports(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "source_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	items, err := h.Repo.ListDataExports(r.Context(), sourceID, claims.Sub)
	if err != nil {
		slog.Error("list data exports", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list exports")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handlers) GetDataExport(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, param, err := routeUUIDParam(r, "export_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+param)
		return
	}
	v, err := h.Repo.GetDataExport(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "export not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) CreateDataExport(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "source_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	var body models.CreateDataExportRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.SourceID != uuid.Nil && body.SourceID != sourceID {
		writeJSONErr(w, http.StatusBadRequest, "source_id does not match route")
		return
	}
	body.SourceID = sourceID
	models.NormalizeCreateDataExportRequest(&body)
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleExportCreate) {
		return
	}
	conn, err := h.Repo.GetConnection(r.Context(), sourceID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if conn == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	if errs := body.ValidateForConnector(conn.ConnectorType); len(errs) > 0 {
		writeJSONErr(w, http.StatusBadRequest, strings.Join(errs, "; "))
		return
	}
	v, err := h.Repo.CreateDataExport(r.Context(), &body, claims.Sub)
	if err != nil {
		slog.Error("create data export", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to create export")
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	h.recordSourceUseAudit(r.Context(), sourceID, claims.Sub, "export_created", models.DataExportCapability(v.ExportType), v.ID.String(), dataExportDestinationRID(v), "Created data export from source", map[string]any{"export_type": v.ExportType, "export_mode": v.ExportMode})
	writeJSON(w, http.StatusCreated, v)
}

func (h *Handlers) UpdateDataExport(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, param, err := routeUUIDParam(r, "export_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+param)
		return
	}
	var body models.UpdateDataExportRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	current, err := h.Repo.GetDataExport(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if current == nil {
		writeJSONErr(w, http.StatusNotFound, "export not found")
		return
	}
	if body.ExportMode != nil && !models.ValidDataExportMode(current.ExportType, *body.ExportMode) {
		writeJSONErr(w, http.StatusBadRequest, "export_mode is not supported for "+string(current.ExportType)+" exports")
		return
	}
	if body.FileExport != nil {
		if current.ExportType != models.DataExportTypeFile {
			writeJSONErr(w, http.StatusBadRequest, "file_export settings can only be applied to file exports")
			return
		}
		mode := current.ExportMode
		if body.ExportMode != nil {
			mode = *body.ExportMode
		}
		destination := current.DestinationPath
		if body.DestinationPath != nil {
			destination = body.DestinationPath
		}
		destinationPath := ""
		if destination != nil {
			destinationPath = strings.TrimSpace(*destination)
		}
		models.NormalizeFileExportSettings(body.FileExport, destinationPath, mode)
		if errs := models.ValidateFileExportSettings(body.FileExport); len(errs) > 0 {
			writeJSONErr(w, http.StatusBadRequest, strings.Join(errs, "; "))
			return
		}
	}
	if body.TableExport != nil || (current.ExportType == models.DataExportTypeTable && body.ExportMode != nil && current.TableExport != nil) {
		if current.ExportType != models.DataExportTypeTable {
			writeJSONErr(w, http.StatusBadRequest, "table_export settings can only be applied to table exports")
			return
		}
		mode := current.ExportMode
		if body.ExportMode != nil {
			mode = *body.ExportMode
		}
		settings := current.TableExport
		if body.TableExport != nil {
			settings = body.TableExport
		}
		if settings != nil {
			models.NormalizeTableExportSettings(settings, mode)
			if errs := models.ValidateTableExportSettings(settings, mode); len(errs) > 0 {
				writeJSONErr(w, http.StatusBadRequest, strings.Join(errs, "; "))
				return
			}
		}
	}
	if body.StreamingExport != nil {
		if current.ExportType != models.DataExportTypeStreaming {
			writeJSONErr(w, http.StatusBadRequest, "streaming_export settings can only be applied to streaming exports")
			return
		}
		scheduleConfigured := current.ScheduleCron != nil
		if body.ScheduleCron != nil {
			scheduleConfigured = strings.TrimSpace(*body.ScheduleCron) != ""
		}
		models.NormalizeStreamingExportSettings(body.StreamingExport, scheduleConfigured)
		if errs := models.ValidateStreamingExportSettings(body.StreamingExport); len(errs) > 0 {
			writeJSONErr(w, http.StatusBadRequest, strings.Join(errs, "; "))
			return
		}
	}
	if len(body.Config) > 0 && !json.Valid(body.Config) {
		writeJSONErr(w, http.StatusBadRequest, "config must be valid JSON")
		return
	}
	v, err := h.Repo.UpdateDataExport(r.Context(), id, &body, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "export not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) RunDataExport(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, param, err := routeUUIDParam(r, "export_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+param)
		return
	}
	current, err := h.Repo.GetDataExport(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if current == nil {
		writeJSONErr(w, http.StatusNotFound, "export not found")
		return
	}
	if current.ExportType == models.DataExportTypeStreaming {
		writeJSONErr(w, http.StatusBadRequest, "streaming exports use start/stop instead of one-shot runs")
		return
	}
	if !h.requireSourceRole(w, r, current.SourceID, claims.Sub, models.SourceRoleUse) {
		return
	}
	v, err := h.Repo.RunDataExport(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v != nil {
		h.recordSourceUseAudit(r.Context(), v.SourceID, claims.Sub, "export_run", models.DataExportCapability(v.ExportType), v.ID.String(), dataExportDestinationRID(v), "Ran data export", map[string]any{"status": v.Status})
	}
	writeJSON(w, http.StatusAccepted, v)
}

func (h *Handlers) StartDataExport(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, param, err := routeUUIDParam(r, "export_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+param)
		return
	}
	current, err := h.Repo.GetDataExport(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if current == nil {
		writeJSONErr(w, http.StatusNotFound, "export not found")
		return
	}
	if current.ExportType != models.DataExportTypeStreaming {
		writeJSONErr(w, http.StatusBadRequest, "file and table exports use run instead of start")
		return
	}
	if !h.requireSourceRole(w, r, current.SourceID, claims.Sub, models.SourceRoleUse) {
		return
	}
	v, err := h.Repo.StartDataExport(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v != nil {
		h.recordSourceUseAudit(r.Context(), v.SourceID, claims.Sub, "streaming_export_started", models.DataExportCapability(v.ExportType), v.ID.String(), dataExportDestinationRID(v), "Started streaming export", map[string]any{"status": v.Status})
	}
	writeJSON(w, http.StatusAccepted, v)
}

func (h *Handlers) StopDataExport(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, param, err := routeUUIDParam(r, "export_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+param)
		return
	}
	current, err := h.Repo.GetDataExport(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if current == nil {
		writeJSONErr(w, http.StatusNotFound, "export not found")
		return
	}
	if current.ExportType != models.DataExportTypeStreaming {
		writeJSONErr(w, http.StatusBadRequest, "file and table exports use run instead of stop")
		return
	}
	if !h.requireSourceRole(w, r, current.SourceID, claims.Sub, models.SourceRoleUse) {
		return
	}
	v, err := h.Repo.StopDataExport(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v != nil {
		h.recordSourceUseAudit(r.Context(), v.SourceID, claims.Sub, "streaming_export_stopped", models.DataExportCapability(v.ExportType), v.ID.String(), dataExportDestinationRID(v), "Stopped streaming export", map[string]any{"status": v.Status})
	}
	writeJSON(w, http.StatusAccepted, v)
}

func (h *Handlers) CreateSyncJob(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	var body models.CreateSyncJobRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.SourceID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "source_id required")
		return
	}
	if !h.requireSourceRole(w, r, body.SourceID, claims.Sub, models.SourceRoleSyncCreate) {
		return
	}
	conn, err := h.Repo.GetConnection(r.Context(), body.SourceID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if conn == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	if errs := body.ValidateForConnector(conn.ConnectorType); len(errs) > 0 {
		writeJSONErr(w, http.StatusBadRequest, strings.Join(errs, "; "))
		return
	}
	v, err := h.Repo.CreateSyncJob(r.Context(), &body, claims.Sub)
	if err != nil {
		slog.Error("create sync job", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to create sync job")
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	h.recordSourceUseAudit(r.Context(), body.SourceID, claims.Sub, "sync_created", syncString(v.CapabilityType, "batch_sync"), v.ID.String(), syncOutputRID(v), "Created source sync", map[string]any{"output_kind": syncString(v.OutputKind, "dataset")})
	writeJSON(w, http.StatusCreated, v)
}

func (h *Handlers) UpdateSyncJob(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, param, err := routeUUIDParam(r, "sync_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+param)
		return
	}
	var body models.UpdateSyncJobRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	v, err := h.Repo.UpdateSyncJob(r.Context(), id, &body, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "sync job not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) RunSyncJob(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, param, err := routeUUIDParam(r, "sync_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+param)
		return
	}
	job, err := h.Repo.GetSyncJob(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if job == nil {
		writeJSONErr(w, http.StatusNotFound, "sync job not found")
		return
	}
	if job.OutputKind != "" && job.OutputKind != "dataset" {
		writeJSONErr(w, http.StatusBadRequest, "stream and media syncs are controlled by their long-running runtime, not one-shot sync runs")
		return
	}
	if job.OutputDatasetID == nil || *job.OutputDatasetID == uuid.Nil {
		writeJSONErr(w, http.StatusBadRequest, "output_dataset_id required for one-shot sync runs")
		return
	}
	if !h.requireSourceRole(w, r, job.SourceID, claims.Sub, models.SourceRoleUse) {
		return
	}
	conn, err := h.Repo.GetConnection(r.Context(), job.SourceID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if conn == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	run, err := h.Repo.RunSyncJob(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if run == nil {
		writeJSONErr(w, http.StatusNotFound, "sync job not found")
		return
	}
	ingestion := h.ingestionPort()
	if ingestion == nil {
		h.recordSourceUseAudit(r.Context(), job.SourceID, claims.Sub, "sync_run", syncString(job.CapabilityType, "batch_sync"), run.ID.String(), syncOutputRID(job), "Started source sync run", map[string]any{"status": run.Status})
		writeJSON(w, http.StatusAccepted, run)
		return
	}
	completed := h.dispatchSyncRun(r.Context(), claims.Sub, run, job, conn, ingestion)
	h.recordSourceUseAudit(r.Context(), job.SourceID, claims.Sub, "sync_run", syncString(job.CapabilityType, "batch_sync"), run.ID.String(), syncOutputRID(job), "Dispatched source sync run", map[string]any{"status": completed.Status})
	writeJSON(w, http.StatusAccepted, completed)
}

func (h *Handlers) ingestionPort() cmruntime.IngestionPort {
	if h.IngestionRuntime != nil {
		return h.IngestionRuntime
	}
	if strings.TrimSpace(h.Config.IngestionReplicationGRPCURL) == "" {
		return nil
	}
	return cmruntime.HTTPIngestionClient{BaseURL: h.Config.IngestionReplicationGRPCURL}
}

func (h *Handlers) datasetVersioningPort() cmruntime.DatasetVersioningPort {
	if h.DatasetVersioning != nil {
		return h.DatasetVersioning
	}
	if strings.TrimSpace(h.Config.DatasetServiceURL) == "" {
		return nil
	}
	return cmruntime.HTTPDatasetVersioningClient{BaseURL: h.Config.DatasetServiceURL}
}

func (h *Handlers) dispatchSyncRun(ctx context.Context, ownerID uuid.UUID, run *models.SyncRun, job *models.SyncJob, conn *models.Connection, ingestion cmruntime.IngestionPort) *models.SyncRun {
	materialized, err := cmruntime.Materialize(run.ID, job, conn)
	if err != nil {
		return h.completeRun(ctx, ownerID, run, syncdomain.RunStatusFailed, 0, 0, err, nil, nil, nil)
	}
	result, err := ingestion.Dispatch(ctx, materialized)
	if err != nil {
		return h.completeRun(ctx, ownerID, run, syncdomain.RunStatusFailed, 0, 0, err, nil, nil, &materialized.ContentHash)
	}
	contentHash := materialized.ContentHash
	if len(result.Payload) > 0 {
		digest := sha256.Sum256(result.Payload)
		contentHash = fmt.Sprintf("%x", digest[:])
	}
	datasetVersionID := h.registerDatasetVersion(ctx, run.ID, job, conn, result, contentHash)
	ingestJobID := strings.TrimSpace(result.IngestJobID)
	var ingestJobIDPtr *string
	if ingestJobID != "" {
		ingestJobIDPtr = &ingestJobID
	}
	return h.completeRun(ctx, ownerID, run, syncdomain.RunStatusSucceeded, result.BytesWritten, result.FilesWritten, nil, ingestJobIDPtr, datasetVersionID, &contentHash)
}

func (h *Handlers) registerDatasetVersion(ctx context.Context, runID uuid.UUID, job *models.SyncJob, conn *models.Connection, result *cmruntime.IngestionResult, contentHash string) *uuid.UUID {
	if lookup, ok := h.Repo.(previousDatasetVersionLookup); ok {
		previous, err := lookup.PreviousDatasetVersionForHash(ctx, job.ID, contentHash)
		if err == nil && previous != nil {
			if recorder, ok := h.Repo.(datasetVersionRecorder); ok {
				_ = recorder.RecordDatasetVersionOnRun(ctx, runID, *previous, contentHash)
			}
			return previous
		}
	}
	port := h.datasetVersioningPort()
	if port == nil || job.OutputDatasetID == nil {
		return nil
	}
	version, err := port.Register(ctx, cmruntime.DatasetVersionRequest{SyncDefID: job.ID, RunID: runID, SourceID: conn.ID, OutputDatasetID: *job.OutputDatasetID, ContentHash: contentHash, RowCount: result.RowsWritten, SizeBytes: result.BytesWritten, Schema: json.RawMessage(`{}`), Message: "connector sync " + job.ID.String()})
	if err != nil || version == nil || version.DatasetVersionID == uuid.Nil {
		if err != nil {
			slog.Warn("dataset version registration failed", slog.String("error", err.Error()), slog.String("sync_run_id", runID.String()))
		}
		return nil
	}
	return &version.DatasetVersionID
}

func (h *Handlers) completeRun(ctx context.Context, ownerID uuid.UUID, run *models.SyncRun, status string, bytesWritten int64, filesWritten int64, runErr error, ingestJobID *string, datasetVersionID *uuid.UUID, contentHash *string) *models.SyncRun {
	var errMsg *string
	if runErr != nil {
		msg := runErr.Error()
		errMsg = &msg
	}
	if completer, ok := h.Repo.(syncRunCompleter); ok {
		updated, err := completer.CompleteSyncRun(ctx, run.ID, ownerID, status, bytesWritten, filesWritten, errMsg, ingestJobID, datasetVersionID, contentHash)
		if err == nil && updated != nil {
			return updated
		}
		if err != nil {
			slog.Error("complete sync run", slog.String("error", err.Error()), slog.String("sync_run_id", run.ID.String()))
		}
	}
	now := time.Now().UTC()
	copy := *run
	copy.Status = status
	copy.FinishedAt = &now
	copy.BytesWritten = bytesWritten
	copy.FilesWritten = filesWritten
	copy.Error = errMsg
	copy.IngestJobID = ingestJobID
	copy.DatasetVersionID = datasetVersionID
	copy.ContentHash = contentHash
	return &copy
}

func (h *Handlers) EnableVirtualTableSource(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireClaims(w, r); !ok {
		return
	}
	sourceRID := strings.TrimSpace(chi.URLParam(r, "source_rid"))
	if sourceRID == "" {
		writeJSONErr(w, http.StatusBadRequest, "source_rid required")
		return
	}
	var body models.EnableVirtualTableSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	v, err := h.Repo.EnableVirtualTableSource(r.Context(), sourceRID, &body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) DiscoverVirtualTableCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireClaims(w, r); !ok {
		return
	}
	sourceRID := strings.TrimSpace(chi.URLParam(r, "source_rid"))
	if sourceRID == "" {
		writeJSONErr(w, http.StatusBadRequest, "source_rid required")
		return
	}
	items, err := h.Repo.DiscoverVirtualTableCatalog(r.Context(), sourceRID, r.URL.Query().Get("path"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if items == nil {
		writeJSONErr(w, http.StatusNotFound, "source not enabled")
		return
	}
	writeJSON(w, http.StatusOK, map[string][]models.DiscoveredEntry{"data": items})
}

func (h *Handlers) CreateVirtualTable(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceRID := strings.TrimSpace(chi.URLParam(r, "source_rid"))
	if sourceRID == "" {
		writeJSONErr(w, http.StatusBadRequest, "source_rid required")
		return
	}
	var body models.CreateVirtualTableRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(body.ProjectRID) == "" || strings.TrimSpace(body.TableType) == "" {
		writeJSONErr(w, http.StatusBadRequest, "project_rid and table_type required")
		return
	}
	v, err := h.Repo.CreateVirtualTable(r.Context(), sourceRID, claims.Sub.String(), &body)
	if errors.Is(err, repo.ErrConflict) {
		writeJSONErr(w, http.StatusConflict, "virtual table already registered")
		return
	}
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "source not enabled")
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (h *Handlers) BulkRegisterVirtualTables(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceRID := strings.TrimSpace(chi.URLParam(r, "source_rid"))
	if sourceRID == "" {
		writeJSONErr(w, http.StatusBadRequest, "source_rid required")
		return
	}
	var body models.VirtualTableBulkRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(body.ProjectRID) == "" || len(body.Entries) == 0 {
		writeJSONErr(w, http.StatusBadRequest, "project_rid and entries required")
		return
	}
	response, err := h.Repo.BulkRegisterVirtualTables(r.Context(), sourceRID, claims.Sub.String(), &body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if response == nil {
		writeJSONErr(w, http.StatusNotFound, "source not enabled")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handlers) EnableVirtualTableAutoRegistration(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireClaims(w, r); !ok {
		return
	}
	sourceRID := strings.TrimSpace(chi.URLParam(r, "source_rid"))
	if sourceRID == "" {
		writeJSONErr(w, http.StatusBadRequest, "source_rid required")
		return
	}
	var body models.EnableAutoRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	link, err := h.Repo.EnableVirtualTableAutoRegistration(r.Context(), sourceRID, &body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if link == nil {
		writeJSONErr(w, http.StatusNotFound, "source not enabled")
		return
	}
	writeJSON(w, http.StatusOK, link)
}

func (h *Handlers) DisableVirtualTableAutoRegistration(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireClaims(w, r); !ok {
		return
	}
	sourceRID := strings.TrimSpace(chi.URLParam(r, "source_rid"))
	if sourceRID == "" {
		writeJSONErr(w, http.StatusBadRequest, "source_rid required")
		return
	}
	if err := h.Repo.DisableVirtualTableAutoRegistration(r.Context(), sourceRID); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) ScanVirtualTableAutoRegistrationNow(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireClaims(w, r); !ok {
		return
	}
	sourceRID := strings.TrimSpace(chi.URLParam(r, "source_rid"))
	if sourceRID == "" {
		writeJSONErr(w, http.StatusBadRequest, "source_rid required")
		return
	}
	summary, err := h.Repo.ScanVirtualTableAutoRegistrationNow(r.Context(), sourceRID)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if summary == nil {
		writeJSONErr(w, http.StatusNotFound, "auto-registration not enabled")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handlers) ListVirtualTables(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	items, err := h.Repo.ListVirtualTables(r.Context(), claims.Sub.String(), r.URL.Query().Get("project"), r.URL.Query().Get("source"), r.URL.Query().Get("name"), r.URL.Query().Get("type"), limit)
	if err != nil {
		slog.Error("list virtual tables", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list virtual tables")
		return
	}
	writeJSON(w, http.StatusOK, models.ListVirtualTablesResponse{Items: items})
}

func (h *Handlers) GetVirtualTable(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	v, err := h.Repo.GetVirtualTable(r.Context(), rid, claims.Sub.String())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "virtual table not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) QueryVirtualTable(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	table, err := h.Repo.GetVirtualTable(r.Context(), rid, claims.Sub.String())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if table == nil {
		writeJSONErr(w, http.StatusNotFound, "virtual table not found")
		return
	}
	var query models.VirtualTableQueryRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&query); err != nil && !errors.Is(err, io.EOF) {
			writeJSONErr(w, http.StatusBadRequest, "invalid body")
			return
		}
	}
	if strings.TrimSpace(query.Selector) == "" {
		query.Selector = virtualTableSelector(table)
	}
	limit := clampVirtualTableQueryLimit(query.Limit)
	query.Limit = &limit

	var connection *models.Connection
	if sourceID, err := uuid.Parse(table.SourceRID); err == nil {
		connection, err = h.Repo.GetConnectionForOwner(r.Context(), sourceID, claims.Sub)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, "connection lookup failed")
			return
		}
	}

	var adapterLimitations []models.VirtualTableLimitation
	response := (*models.VirtualTableQueryResponse)(nil)
	if connection != nil {
		if h.AdapterRegistry == nil {
			writeJSONErr(w, http.StatusServiceUnavailable, "virtual table adapter registry unavailable for real preview")
			return
		}
		adapter, err := h.AdapterRegistry.Lookup(connection.ConnectorType)
		if err == nil {
			response, err = adapter.QueryVirtualTable(r.Context(), connection, &query, "")
			if err != nil && !errors.Is(err, adapters.ErrNotImplemented) {
				writeJSONErr(w, http.StatusBadGateway, err.Error())
				return
			}
		}
		if errors.Is(err, adapters.ErrAdapterNotFound) || errors.Is(err, adapters.ErrNotImplemented) {
			writeJSONErr(w, http.StatusServiceUnavailable, "virtual table adapter unavailable for real preview")
			return
		}
	}
	if response == nil {
		adapterLimitations = append(adapterLimitations, models.VirtualTableLimitation{
			Code:        "virtual_table_metadata_preview",
			Severity:    "warning",
			Message:     "No connection is linked; returning degraded metadata-derived sample rows. No remote source data was read.",
			Remediation: "Link a connector connection to run a real adapter preview against source data.",
		})
		response = virtualTableQueryFromMetadata(table, query)
	}
	decorateVirtualTableQueryResponse(table, connection, &query, response, adapterLimitations)
	writeJSON(w, http.StatusOK, response)
}

func (h *Handlers) SetVirtualTableUpdateDetection(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	var body models.UpdateDetectionToggle
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Enabled && body.IntervalSeconds < 60 {
		writeJSONErr(w, http.StatusBadRequest, "interval_seconds must be at least 60")
		return
	}
	table, err := h.Repo.SetVirtualTableUpdateDetection(r.Context(), rid, claims.Sub.String(), &body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if table == nil {
		writeJSONErr(w, http.StatusNotFound, "virtual table not found")
		return
	}
	writeJSON(w, http.StatusOK, table)
}

func (h *Handlers) PollVirtualTableUpdateDetectionNow(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	result, err := h.Repo.PollVirtualTableUpdateDetection(r.Context(), rid, claims.Sub.String())
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if result == nil {
		writeJSONErr(w, http.StatusNotFound, "virtual table not found")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) ListVirtualTableUpdateDetectionHistory(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	items, err := h.Repo.ListVirtualTableUpdateDetectionHistory(r.Context(), rid, claims.Sub.String(), limit)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if items == nil {
		writeJSONErr(w, http.StatusNotFound, "virtual table not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string][]models.PollHistoryRow{"data": items})
}

func (h *Handlers) GetVirtualTableLineage(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	lineage, err := h.Repo.GetVirtualTableLineage(r.Context(), rid, claims.Sub.String())
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if lineage == nil {
		writeJSONErr(w, http.StatusNotFound, "virtual table not found")
		return
	}
	writeJSON(w, http.StatusOK, lineage)
}

func clampVirtualTableQueryLimit(limit *int) int {
	if limit == nil {
		return 50
	}
	if *limit < 1 {
		return 1
	}
	if *limit > 500 {
		return 500
	}
	return *limit
}

func virtualTableSelector(table *models.VirtualTable) string {
	var locator map[string]string
	if err := json.Unmarshal(table.Locator, &locator); err != nil {
		return table.Name
	}
	switch locator["kind"] {
	case "tabular":
		parts := []string{locator["database"], locator["schema"], locator["table"]}
		return strings.Join(nonEmptyStrings(parts...), ".")
	case "iceberg":
		parts := []string{locator["catalog"], locator["namespace"], locator["table"]}
		return strings.Join(nonEmptyStrings(parts...), ".")
	case "file":
		parts := []string{locator["bucket"], locator["prefix"]}
		return strings.Join(nonEmptyStrings(parts...), "/")
	default:
		return table.Name
	}
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func virtualTableQueryFromMetadata(table *models.VirtualTable, query models.VirtualTableQueryRequest) *models.VirtualTableQueryResponse {
	limit := clampVirtualTableQueryLimit(query.Limit)
	columns := virtualTableQueryColumns(table, query.Columns)
	if len(columns) == 0 {
		columns = []string{"selector", "row_number"}
	}
	rows := make([]json.RawMessage, 0, limit)
	for i := 0; i < limit; i++ {
		row := map[string]any{}
		for _, column := range columns {
			row[column] = virtualTableSampleValue(column, i+1)
		}
		if _, ok := row["selector"]; ok {
			row["selector"] = query.Selector
		}
		if _, ok := row["row_number"]; ok {
			row["row_number"] = i + 1
		}
		buf, _ := json.Marshal(row)
		rows = append(rows, buf)
	}
	sigBytes, _ := json.Marshal(map[string]any{
		"rid":      table.RID,
		"selector": query.Selector,
		"locator":  json.RawMessage(table.Locator),
		"schema":   json.RawMessage(table.SchemaInferred),
	})
	digest := sha256.Sum256(sigBytes)
	signature := fmt.Sprintf("sha256:%x", digest[:])
	metadata, _ := json.Marshal(map[string]any{
		"adapter":             "openfoundry_virtual_table_preview",
		"virtual_table_rid":   table.RID,
		"source_rid":          table.SourceRID,
		"degraded":            true,
		"source":              "metadata",
		"direct_query":        false,
		"uses_copied_dataset": false,
		"preview_notice":      "Rows are metadata-derived samples; no remote source data was read.",
	})
	return &models.VirtualTableQueryResponse{
		Selector:        query.Selector,
		Mode:            "virtual_table_direct",
		Columns:         columns,
		RowCount:        len(rows),
		Rows:            rows,
		SourceSignature: &signature,
		Metadata:        metadata,
		Degraded:        true,
		Source:          "metadata",
	}
}

func virtualTableQueryColumns(table *models.VirtualTable, requested []string) []string {
	if len(requested) > 0 {
		return append([]string(nil), requested...)
	}
	var schema []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(table.SchemaInferred, &schema); err == nil {
		columns := make([]string, 0, len(schema))
		for _, col := range schema {
			if strings.TrimSpace(col.Name) != "" {
				columns = append(columns, col.Name)
			}
		}
		if len(columns) > 0 {
			return columns
		}
	}
	return nil
}

func virtualTableSampleValue(column string, rowNumber int) any {
	name := strings.ToLower(column)
	switch {
	case strings.Contains(name, "id") || strings.Contains(name, "count") || strings.Contains(name, "number"):
		return rowNumber
	case strings.Contains(name, "amount") || strings.Contains(name, "price") || strings.Contains(name, "total"):
		return float64(rowNumber) * 10.5
	case strings.Contains(name, "date") || strings.Contains(name, "time"):
		return time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC).Add(time.Duration(rowNumber) * time.Hour).Format(time.RFC3339)
	default:
		return fmt.Sprintf("%s_%d", column, rowNumber)
	}
}

func decorateVirtualTableQueryResponse(table *models.VirtualTable, connection *models.Connection, query *models.VirtualTableQueryRequest, response *models.VirtualTableQueryResponse, extra []models.VirtualTableLimitation) {
	if response.Selector == "" {
		response.Selector = query.Selector
	}
	if response.Mode == "" {
		response.Mode = "virtual_table_direct"
	}
	if response.Rows == nil {
		response.Rows = []json.RawMessage{}
	}
	if response.Columns == nil {
		response.Columns = columnsForRows(response.Rows)
	}
	response.RowCount = len(response.Rows)
	plan := virtualTablePushdownPlan(table, query)
	limitations := virtualTableQueryLimitations(table, connection, query, plan)
	limitations = append(limitations, extra...)
	response.ComputeLocation = plan.ComputeLocation
	response.Pushdown = plan
	response.Limitations = limitations
	if response.Degraded && response.Source == "" {
		response.Source = "metadata"
	}
	response.Metadata = mergeVirtualTableQueryMetadata(response.Metadata, table, plan, limitations)
}

func virtualTablePushdownPlan(table *models.VirtualTable, query *models.VirtualTableQueryRequest) *models.VirtualTablePushdownPlan {
	caps := virtualTableCapabilities(table)
	pushdownEngine := ""
	if caps.ComputePushdown != nil {
		pushdownEngine = string(*caps.ComputePushdown)
	}
	operations := virtualTableQueryOperations(query)
	location := "openfoundry"
	pushed := []string{}
	foundry := append([]string(nil), operations...)
	if pushdownEngine != "" && !query.RequiresFoundryCompute {
		location = "source_system"
		pushed = append([]string(nil), operations...)
		foundry = []string{}
	} else if pushdownEngine != "" && query.RequiresFoundryCompute {
		location = "hybrid"
		pushed = []string{"scan", "projection", "filter"}
		foundry = []string{"custom_expression"}
	}
	var pushdown *string
	if pushdownEngine != "" {
		pushdown = &pushdownEngine
	}
	foundryEngine := "openfoundry_spark"
	return &models.VirtualTablePushdownPlan{
		ComputeLocation:    location,
		PushdownEngine:     pushdown,
		FoundryEngine:      &foundryEngine,
		PushedOperations:   pushed,
		FoundryOperations:  foundry,
		DirectQuery:        true,
		UsesCopiedDataset:  false,
		InteractivePreview: true,
	}
}

func virtualTableQueryOperations(query *models.VirtualTableQueryRequest) []string {
	ops := []string{"scan"}
	if len(query.Columns) > 0 {
		ops = append(ops, "projection")
	}
	if len(query.Filters) > 0 {
		ops = append(ops, "filter")
	}
	if len(query.Aggregations) > 0 {
		ops = append(ops, "aggregation")
	}
	if len(query.OrderBy) > 0 {
		ops = append(ops, "order_by")
	}
	ops = append(ops, "limit")
	return ops
}

func virtualTableQueryLimitations(table *models.VirtualTable, connection *models.Connection, query *models.VirtualTableQueryRequest, plan *models.VirtualTablePushdownPlan) []models.VirtualTableLimitation {
	limitations := []models.VirtualTableLimitation{
		{
			Code:        "interactive_performance",
			Severity:    "info",
			Message:     "Interactive reads query the external table directly, so latency depends on the source system and network path.",
			Remediation: "Use a synchronized Foundry dataset for latency-sensitive or heavily reused interactive analysis.",
		},
	}
	switch plan.ComputeLocation {
	case "source_system":
		limitations = append(limitations, models.VirtualTableLimitation{
			Code:     "source_compute_usage",
			Severity: "info",
			Message:  "This query can run in the source system and may consume source warehouse or database compute.",
		})
	case "hybrid":
		limitations = append(limitations, models.VirtualTableLimitation{
			Code:        "hybrid_compute_usage",
			Severity:    "warning",
			Message:     "This query can push scan/projection work to the source but still needs OpenFoundry compute for unsupported operations.",
			Remediation: "Remove custom expressions or unsupported operations to maximize source pushdown.",
		})
	default:
		limitations = append(limitations, models.VirtualTableLimitation{
			Code:        "openfoundry_compute_usage",
			Severity:    "warning",
			Message:     "No native source pushdown engine is available for this source/table type; OpenFoundry compute handles the preview after reading source rows.",
			Remediation: "Use BigQuery, Databricks, or Snowflake table types with a supported pushdown engine when source-side compute is required.",
		})
	}
	if query.RequiresFoundryCompute {
		limitations = append(limitations, models.VirtualTableLimitation{
			Code:        "unsupported_feature_partial_pushdown",
			Severity:    "warning",
			Message:     "The requested operation requires OpenFoundry-side execution and cannot be fully pushed down.",
			Remediation: "Use source-native filters, projections, aggregations, and ordering when possible.",
		})
	}
	limitations = append(limitations, virtualTableNetworkLimitations(connection)...)
	return limitations
}

func virtualTableNetworkLimitations(connection *models.Connection) []models.VirtualTableLimitation {
	if connection == nil {
		return []models.VirtualTableLimitation{{
			Code:        "worker_network_unverified",
			Severity:    "warning",
			Message:     "The backing source connection could not be resolved, so worker and egress compatibility could not be verified.",
			Remediation: "Ensure the virtual table source RID points to a Foundry-worker source with direct egress.",
		}}
	}
	raw := strings.ToLower(string(connection.Config))
	findings := []models.VirtualTableLimitation{}
	if strings.Contains(raw, "agent_worker") || strings.Contains(raw, `"worker_mode":"agent"`) || strings.Contains(raw, `"worker":"agent"`) {
		findings = append(findings, models.VirtualTableLimitation{
			Code:        "agent_worker_not_supported",
			Severity:    "error",
			Message:     "Virtual table queries do not support sources configured for an agent worker.",
			Remediation: "Move the source to a Foundry worker with direct egress, or sync the data to a Foundry dataset.",
		})
	}
	for token, code := range map[string]string{
		"agent_proxy":          "agent_proxy_not_supported",
		"self_service_private": "self_service_private_link_not_supported",
		"bucket_endpoint":      "bucket_endpoint_not_supported",
	} {
		if strings.Contains(raw, token) {
			findings = append(findings, models.VirtualTableLimitation{
				Code:        code,
				Severity:    "error",
				Message:     "This source uses an egress mode that virtual tables do not support for direct queries.",
				Remediation: "Use direct egress from a Foundry worker or a Palantir-managed private link.",
			})
		}
	}
	return findings
}

func virtualTableCapabilities(table *models.VirtualTable) models.Capabilities {
	var caps models.Capabilities
	_ = json.Unmarshal(table.Capabilities, &caps)
	return caps
}

func mergeVirtualTableQueryMetadata(raw json.RawMessage, table *models.VirtualTable, plan *models.VirtualTablePushdownPlan, limitations []models.VirtualTableLimitation) json.RawMessage {
	meta := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		_ = json.Unmarshal(raw, &meta)
	}
	meta["virtual_table_rid"] = table.RID
	meta["source_rid"] = table.SourceRID
	if _, ok := meta["direct_query"]; !ok {
		meta["direct_query"] = true
	}
	meta["uses_copied_dataset"] = false
	meta["compute_location"] = plan.ComputeLocation
	meta["limitation_count"] = len(limitations)
	out, _ := json.Marshal(meta)
	return out
}

func (h *Handlers) ListMediaSetSyncs(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	items, err := h.Repo.ListMediaSetSyncs(r.Context(), sourceID, claims.Sub)
	if err != nil {
		slog.Error("list media set syncs", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list media set syncs")
		return
	}
	usage, err := h.Repo.MediaSetSyncUsageForSource(r.Context(), sourceID, claims.Sub)
	if err != nil {
		slog.Error("media set sync usage", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list media set sync usage")
		return
	}
	out := make([]models.MediaSetSyncWithUsage, 0, len(items))
	for _, item := range items {
		entry := models.MediaSetSyncWithUsage{MediaSetSync: item}
		if summary, ok := usage[item.ID]; ok {
			copied := summary
			copied.SyncDefID = item.ID
			entry.Usage = &copied
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) GetMediaSetSync(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, param, err := routeUUIDParam(r, "sync_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+param)
		return
	}
	v, err := h.Repo.GetMediaSetSync(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "media set sync not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) CreateMediaSetSync(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body models.CreateMediaSetSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if errs := body.Validate(); len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string][]string{"errors": errs})
		return
	}
	source, err := h.Repo.GetConnectionForOwner(r.Context(), sourceID, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load source")
		return
	}
	if source == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	if !models.ConnectorSupportsMediaSync(source.ConnectorType) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":                "connector does not support media set sync handoff",
			"connector_type":       source.ConnectorType,
			"supported_connectors": models.MediaSyncSupportedConnectors,
		})
		return
	}
	v, err := h.Repo.CreateMediaSetSync(r.Context(), sourceID, &body, claims.Sub)
	if err != nil {
		slog.Error("create media set sync", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to create media set sync")
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (h *Handlers) UpdateMediaSetSync(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, param, err := routeUUIDParam(r, "sync_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+param)
		return
	}
	var body models.UpdateMediaSetSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Kind != nil && !body.Kind.Valid() {
		writeJSON(w, http.StatusBadRequest, map[string][]string{"errors": {"kind must be MEDIA_SET_SYNC or VIRTUAL_MEDIA_SET_SYNC"}})
		return
	}
	if body.TargetMediaSetRID != nil && !strings.HasPrefix(strings.TrimSpace(*body.TargetMediaSetRID), "ri.foundry.main.media_set.") {
		writeJSON(w, http.StatusBadRequest, map[string][]string{"errors": {"target_media_set_rid must start with ri.foundry.main.media_set."}})
		return
	}
	v, err := h.Repo.UpdateMediaSetSync(r.Context(), id, &body, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if v == nil {
		writeJSONErr(w, http.StatusNotFound, "media set sync not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) RunMediaSetSync(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, param, err := routeUUIDParam(r, "sync_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+param)
		return
	}
	var body models.RunMediaSetSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	sync, err := h.Repo.GetMediaSetSync(r.Context(), id, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sync == nil {
		writeJSONErr(w, http.StatusNotFound, "media set sync not found")
		return
	}
	if h.MediaSetRuntime == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "media set runtime not configured")
		return
	}
	startedAt := time.Now().UTC()
	report, runtimeErr := h.MediaSetRuntime.ExecuteMediaSetSync(r.Context(), sync, &body, r.Header.Get("Authorization"))
	finishedAt := time.Now().UTC()

	run := models.MediaSetSyncRun{
		SyncDefID:        sync.ID,
		Status:           models.ClassifyMediaSetSyncRunStatus(report, runtimeErr),
		StartedAt:        startedAt,
		FinishedAt:       &finishedAt,
		SelectedPaths:    models.CollectSelectedPaths(&body),
		SchemaMismatches: []string{},
	}
	actor := claims.Sub.String()
	run.TriggeredBy = &actor
	if report != nil {
		run.AcceptedFiles = report.Stats.Accepted
		run.SkippedFiles = report.Stats.Skipped
		run.SchemaMismatched = report.Stats.SchemaMismatched
		run.DispatchedFiles = report.Dispatched
		run.DispatchErrors = report.DispatchErrors
		run.BytesAccepted = models.ComputeMediaSetSyncBytesAccepted(report, &body, sync.Filters)
		if len(report.SchemaMismatches) > 0 {
			run.SchemaMismatches = report.SchemaMismatches
		}
	}
	if runtimeErr != nil {
		msg := runtimeErr.Error()
		run.ErrorMessage = &msg
	}
	if _, persistErr := h.Repo.RecordMediaSetSyncRun(r.Context(), sync.ID, claims.Sub, run); persistErr != nil {
		slog.Warn("persist media set sync run", slog.String("error", persistErr.Error()))
	}

	if runtimeErr != nil {
		writeJSONErr(w, runtimeHTTPStatus(runtimeErr), runtimeErr.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, report)
}

func (h *Handlers) ListMediaSetSyncRuns(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	id, param, err := routeUUIDParam(r, "sync_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid "+param)
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSONErr(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}
	runs, err := h.Repo.ListMediaSetSyncRuns(r.Context(), id, claims.Sub, limit)
	if err != nil {
		slog.Error("list media set sync runs", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list media set sync runs")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.MediaSetSyncRun]{Items: runs})
}

func (h *Handlers) GetMediaSetSyncHandoffDelegation(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, models.DefaultMediaSetSyncHandoffDelegation())
}

// SDC.47 — Dead-letter sink + quarantine handling. The sink is per-sync and
// drives where quarantined records are persisted plus how long they're
// retained; the quarantine endpoints record runtime failures, list the
// stored records for the operator, and mark records for replay after a
// schema or connector fix.
func (h *Handlers) GetDeadLetterSink(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	syncDefID, _, err := routeUUIDParam(r, "sync_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid sync_id")
		return
	}
	sink, err := h.Repo.GetDeadLetterSink(r.Context(), syncDefID, claims.Sub)
	if err != nil {
		slog.Error("load dead letter sink", slog.String("sync_id", syncDefID.String()), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load dead letter sink")
		return
	}
	if sink == nil {
		defaultSink := models.DefaultDeadLetterSink(syncDefID, time.Now().UTC())
		writeJSON(w, http.StatusOK, defaultSink)
		return
	}
	writeJSON(w, http.StatusOK, sink)
}

func (h *Handlers) UpdateDeadLetterSink(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	syncDefID, _, err := routeUUIDParam(r, "sync_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid sync_id")
		return
	}
	var body models.UpdateDeadLetterSinkRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if errs := models.ValidateDeadLetterSink(body); len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string][]string{"errors": errs})
		return
	}
	actor := claims.Sub.String()
	stored, err := h.Repo.UpsertDeadLetterSink(r.Context(), syncDefID, claims.Sub, &actor, body)
	if err != nil {
		slog.Error("upsert dead letter sink", slog.String("sync_id", syncDefID.String()), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to update dead letter sink")
		return
	}
	if stored == nil {
		writeJSONErr(w, http.StatusForbidden, "missing source role: "+string(models.SourceRoleEdit))
		return
	}
	writeJSON(w, http.StatusOK, stored)
}

func (h *Handlers) RecordQuarantinedRecord(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	syncDefID, _, err := routeUUIDParam(r, "sync_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid sync_id")
		return
	}
	var body models.RecordQuarantineRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.FailureCategory == "" {
		body.FailureCategory = models.ClassifyQuarantineFailure(body.ErrorMessage)
	}
	if strings.TrimSpace(body.ErrorMessage) == "" {
		writeJSONErr(w, http.StatusBadRequest, "error_message is required")
		return
	}
	sink, err := h.Repo.GetDeadLetterSink(r.Context(), syncDefID, claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load dead letter sink")
		return
	}
	effective := models.DefaultDeadLetterSink(syncDefID, time.Now().UTC())
	if sink != nil {
		effective = *sink
	}
	record, err := h.Repo.RecordQuarantinedRecord(r.Context(), syncDefID, claims.Sub, body, effective, time.Now().UTC())
	if err != nil {
		slog.Error("record quarantine", slog.String("sync_id", syncDefID.String()), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to record quarantine")
		return
	}
	if record == nil {
		writeJSONErr(w, http.StatusForbidden, "missing source role: "+string(models.SourceRoleUse))
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (h *Handlers) ListQuarantinedRecords(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	syncDefID, _, err := routeUUIDParam(r, "sync_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid sync_id")
		return
	}
	category := models.QuarantineFailureCategory(strings.TrimSpace(r.URL.Query().Get("category")))
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSONErr(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}
	records, err := h.Repo.ListQuarantinedRecords(r.Context(), syncDefID, claims.Sub, category, limit)
	if err != nil {
		slog.Error("list quarantine", slog.String("sync_id", syncDefID.String()), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to list quarantine")
		return
	}
	summary := models.BuildQuarantineSummary(syncDefID, records)
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handlers) ReplayQuarantinedRecords(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	syncDefID, _, err := routeUUIDParam(r, "sync_id", "id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid sync_id")
		return
	}
	var body models.QuarantineReplayRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	records, err := h.Repo.ListQuarantinedRecords(r.Context(), syncDefID, claims.Sub, "", 500)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list quarantine")
		return
	}
	now := time.Now().UTC()
	plan := models.BuildQuarantineReplayPlan(syncDefID, records, body.RecordIDs, now)
	if plan.RecordsMatched == 0 {
		writeJSON(w, http.StatusOK, plan)
		return
	}
	actor := claims.Sub.String()
	if _, err := h.Repo.MarkQuarantinedRecordsForReplay(r.Context(), syncDefID, claims.Sub, &actor, plan.RecordIDs, now); err != nil {
		slog.Error("mark quarantine replay", slog.String("sync_id", syncDefID.String()), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to mark quarantine for replay")
		return
	}
	writeJSON(w, http.StatusAccepted, plan)
}

// SDC.46 — Stream replay controls. Stateless pure-function planner: callers
// POST the desired replay window plus the downstream inventory (active
// streaming exports, CDC archive views, object indexing pipelines,
// duplicate-tolerant consumers) and any operator acknowledgements they want
// to attach, and the handler returns a structured `StreamReplayPlan` with
// per-impact severity, the acknowledgement IDs required, and the aggregate
// status (`ready`/`requires_confirmation`/`blocked`). Active streaming
// exports always raise a `block` impact until explicit acknowledgement.
func (h *Handlers) ComputeStreamReplayPlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireClaims(w, r); !ok {
		return
	}
	var req models.StreamReplayPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.StreamID) == "" {
		writeJSONErr(w, http.StatusBadRequest, "stream_id is required")
		return
	}
	plan := models.BuildStreamReplayPlan(req)
	writeJSON(w, http.StatusOK, plan)
}

// SDC.45 — Stream lag and throughput metrics. The endpoint is a stateless
// pure-function calculator: callers POST the raw counters they have collected
// (ingestion/consumption totals, hot buffer state, checkpoint samples, retry
// counters, plus per-sync/export/partition/consumer breakdowns) and receive
// back a normalized `StreamMetricsSnapshot` with rates, lag, and warnings.
// Keeping it stateless lets the same builder run server-side from
// connector-management telemetry today and again client-side against the
// existing DataConnectionStreamResource without diverging.
func (h *Handlers) ComputeStreamMetricsSnapshot(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireClaims(w, r); !ok {
		return
	}
	var input models.StreamMetricsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(input.StreamID) == "" {
		writeJSONErr(w, http.StatusBadRequest, "stream_id is required")
		return
	}
	snapshot := models.BuildStreamMetricsSnapshot(input)
	writeJSON(w, http.StatusOK, snapshot)
}

// SDC.44 — Connector-specific capability packs. The list endpoint returns
// every pack; the single endpoint returns one by connector type. Both are
// read-only and identical regardless of authentication context so the catalog
// can be rendered from the source-detail "Capabilities" tab and the new-source
// wizard without leaking other data.
func (h *Handlers) ListConnectorCapabilityPacks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, models.ListResponse[models.ConnectorCapabilityPack]{
		Items: models.BuildConnectorCapabilityPacks(),
	})
}

func (h *Handlers) GetConnectorCapabilityPack(w http.ResponseWriter, r *http.Request) {
	connectorType := strings.TrimSpace(chi.URLParam(r, "connector_type"))
	if connectorType == "" {
		writeJSONErr(w, http.StatusBadRequest, "connector_type is required")
		return
	}
	pack := models.ConnectorCapabilityPackFor(connectorType)
	if pack == nil {
		writeJSONErr(w, http.StatusNotFound, "capability pack not defined for connector type")
		return
	}
	writeJSON(w, http.StatusOK, pack)
}

// SDC.43 — Listener-style inbound ingestion (blocked descriptor). Returns the
// per-source descriptor documenting which inbound listener facets (schema
// mapping, auth strategy, replay/idempotency, dead-letter) are available,
// partial, or blocked, alongside the existing HTTPS receiver surfaces so users
// can pick the right ingestion path today.
func (h *Handlers) GetListenerInboundDescriptor(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	source, err := h.Repo.GetConnectionForOwner(r.Context(), sourceID, claims.Sub)
	if err != nil {
		slog.Error("load source for listener descriptor", slog.String("source_id", sourceID.String()), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load source")
		return
	}
	if source == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	descriptor := models.BuildListenerInboundDescriptor(sourceID, models.SourceRIDForConnection(sourceID), source.ConnectorType)
	writeJSON(w, http.StatusOK, descriptor)
}

// SDC.42 — Virtual media handoff (blocked). The descriptor is read-only; it
// returns the registration modes available for the source's connector along
// with the blockers and contracts still missing before the product can ship.
func (h *Handlers) GetVirtualMediaHandoff(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	source, err := h.Repo.GetConnectionForOwner(r.Context(), sourceID, claims.Sub)
	if err != nil {
		slog.Error("load source for virtual media handoff", slog.String("source_id", sourceID.String()), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load source")
		return
	}
	if source == nil {
		writeJSONErr(w, http.StatusNotFound, "source not found")
		return
	}
	descriptor := models.BuildVirtualMediaHandoffDescriptor(sourceID, models.SourceRIDForConnection(sourceID), source.ConnectorType)
	writeJSON(w, http.StatusOK, descriptor)
}

// SDC.40 — retry/recovery handlers. Sources expose three endpoints: read the
// effective retry policy, update it, and read the aggregated recovery summary
// (recent failed runs + decisions). The recovery summary also feeds the
// existing GetSourceHealth aggregator so retry posture surfaces in Data Health.

func (h *Handlers) loadRetryRecoverySummary(ctx context.Context, sourceID uuid.UUID, ownerID uuid.UUID, now time.Time) (*models.RetryRecoverySummary, error) {
	policy, err := h.Repo.GetSourceRetryPolicy(ctx, sourceID, ownerID)
	if err != nil {
		return nil, err
	}
	effective := models.DefaultSourceRetryPolicy(sourceID, now)
	if policy != nil {
		effective = models.NormalizeSourceRetryPolicy(*policy, sourceID, now)
	}
	failures, err := h.Repo.ListSyncRunFailuresForSource(ctx, sourceID, ownerID, 100)
	if err != nil {
		return nil, err
	}
	summary := models.BuildRetryRecoverySummary(models.RetryRecoveryInput{
		SourceID:  sourceID,
		Policy:    effective,
		Failures:  failures,
		CheckedAt: now,
	})
	return &summary, nil
}

func (h *Handlers) GetSourceRetryPolicy(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleView) {
		return
	}
	policy, err := h.Repo.GetSourceRetryPolicy(r.Context(), sourceID, claims.Sub)
	if err != nil {
		slog.Error("load retry policy", slog.String("source_id", sourceID.String()), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load retry policy")
		return
	}
	now := time.Now().UTC()
	effective := models.DefaultSourceRetryPolicy(sourceID, now)
	if policy != nil {
		effective = models.NormalizeSourceRetryPolicy(*policy, sourceID, now)
	}
	writeJSON(w, http.StatusOK, effective)
}

func (h *Handlers) UpdateSourceRetryPolicy(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleEdit) {
		return
	}
	var body models.UpdateSourceRetryPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	now := time.Now().UTC()
	actor := claims.Sub.String()
	policy := models.SourceRetryPolicy{
		SourceID:   sourceID,
		Categories: body.Categories,
		UpdatedBy:  &actor,
		UpdatedAt:  now,
	}
	stored, err := h.Repo.UpsertSourceRetryPolicy(r.Context(), sourceID, claims.Sub, &actor, policy)
	if err != nil {
		slog.Error("upsert retry policy", slog.String("source_id", sourceID.String()), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to update retry policy")
		return
	}
	if stored == nil {
		writeJSONErr(w, http.StatusForbidden, "missing source role: "+string(models.SourceRoleEdit))
		return
	}
	effective := models.NormalizeSourceRetryPolicy(*stored, sourceID, now)
	writeJSON(w, http.StatusOK, effective)
}

func (h *Handlers) GetSourceRetryRecovery(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	sourceID, _, err := routeUUIDParam(r, "id", "source_id")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid source_id")
		return
	}
	if !h.requireSourceRole(w, r, sourceID, claims.Sub, models.SourceRoleView) {
		return
	}
	now := time.Now().UTC()
	summary, err := h.loadRetryRecoverySummary(r.Context(), sourceID, claims.Sub, now)
	if err != nil {
		slog.Error("retry recovery summary", slog.String("source_id", sourceID.String()), slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load retry recovery summary")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
