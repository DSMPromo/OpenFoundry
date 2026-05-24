// Package models holds wire types for connector-management-service.
//
// Wire types for connections, sync definitions, exports, and virtual tables.
package models

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// Connection mirrors the `connections` row.
type Connection struct {
	ID            uuid.UUID       `json:"id"`
	Name          string          `json:"name"`
	ConnectorType string          `json:"connector_type"`
	Config        json.RawMessage `json:"config"`
	Status        string          `json:"status"`
	OwnerID       uuid.UUID       `json:"owner_id"`
	LastSyncAt    *time.Time      `json:"last_sync_at"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// CreateConnectionRequest is POST /api/v1/connections.
type CreateConnectionRequest struct {
	Name          string          `json:"name"`
	ConnectorType string          `json:"connector_type"`
	Config        json.RawMessage `json:"config,omitempty"`
}

// UpdateConnectionRequest mirrors PATCH semantics.
type UpdateConnectionRequest struct {
	Name   *string         `json:"name,omitempty"`
	Config json.RawMessage `json:"config,omitempty"`
	Status *string         `json:"status,omitempty"`
}

type SourcePermissionRole string

const (
	SourceRoleView           SourcePermissionRole = "source_view"
	SourceRoleEdit           SourcePermissionRole = "source_edit"
	SourceRoleUse            SourcePermissionRole = "source_use"
	SourceRoleOwner          SourcePermissionRole = "source_owner"
	SourceRoleWebhookExecute SourcePermissionRole = "webhook_execute"
	SourceRoleSyncCreate     SourcePermissionRole = "sync_create"
	SourceRoleExportCreate   SourcePermissionRole = "export_create"
	SourceRoleCodeImport     SourcePermissionRole = "code_import"
)

type SourcePermissionRoleDefinition struct {
	Role         SourcePermissionRole   `json:"role"`
	Label        string                 `json:"label"`
	Description  string                 `json:"description"`
	ImpliedRoles []SourcePermissionRole `json:"implied_roles,omitempty"`
}

type SourcePermissionGrant struct {
	ID            uuid.UUID              `json:"id"`
	SourceID      uuid.UUID              `json:"source_id"`
	PrincipalID   string                 `json:"principal_id"`
	PrincipalType string                 `json:"principal_type"`
	PrincipalName string                 `json:"principal_name,omitempty"`
	Roles         []SourcePermissionRole `json:"roles"`
	GrantedBy     *uuid.UUID             `json:"granted_by,omitempty"`
	Reason        string                 `json:"reason,omitempty"`
	ExpiresAt     *time.Time             `json:"expires_at,omitempty"`
	GrantedAt     time.Time              `json:"granted_at"`
}

type SourceVisibilityPolicy struct {
	SourceVisibilityRoles            []SourcePermissionRole `json:"source_visibility_roles"`
	CredentialVisibilityRoles        []SourcePermissionRole `json:"credential_visibility_roles"`
	ExternalSampleVisibilityRoles    []SourcePermissionRole `json:"external_sample_visibility_roles"`
	OutputDatasetPermissionRoles     []string               `json:"output_dataset_permission_roles"`
	CredentialValuesVisible          bool                   `json:"credential_values_visible"`
	ExternalSamplesPersisted         bool                   `json:"external_samples_persisted"`
	OutputDatasetPermissionsEnforced bool                   `json:"output_dataset_permissions_enforced"`
	OutputDatasetPermissionSystem    string                 `json:"output_dataset_permission_system"`
	SourceVisibilityDistinct         bool                   `json:"source_visibility_distinct"`
	CredentialVisibilityDistinct     bool                   `json:"credential_visibility_distinct"`
	ExternalSampleVisibilityDistinct bool                   `json:"external_sample_visibility_distinct"`
	OutputDatasetPermissionsDistinct bool                   `json:"output_dataset_permissions_distinct"`
}

type SourceOutputDatasetPermission struct {
	DatasetID           *uuid.UUID `json:"dataset_id,omitempty"`
	DatasetRID          string     `json:"dataset_rid,omitempty"`
	RequiredPermissions []string   `json:"required_permissions"`
	ActorPermissions    []string   `json:"actor_permissions"`
	Verified            bool       `json:"verified"`
	Message             string     `json:"message,omitempty"`
}

type SourceGovernanceWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type SourceGovernanceAuditEvent struct {
	ID                    uuid.UUID              `json:"id"`
	SourceID              uuid.UUID              `json:"source_id"`
	ActorID               *uuid.UUID             `json:"actor_id,omitempty"`
	EventType             string                 `json:"event_type"`
	Action                string                 `json:"action"`
	Result                string                 `json:"result"`
	PrincipalID           string                 `json:"principal_id,omitempty"`
	PrincipalType         string                 `json:"principal_type,omitempty"`
	Roles                 []SourcePermissionRole `json:"roles,omitempty"`
	Capability            string                 `json:"capability,omitempty"`
	JobRID                string                 `json:"job_rid,omitempty"`
	DownstreamResourceRID string                 `json:"downstream_resource_rid,omitempty"`
	Message               string                 `json:"message,omitempty"`
	Metadata              map[string]any         `json:"metadata,omitempty"`
	CreatedAt             time.Time              `json:"created_at"`
}

type RecordSourceGovernanceAuditRequest struct {
	SourceID              uuid.UUID              `json:"source_id"`
	ActorID               *uuid.UUID             `json:"actor_id,omitempty"`
	EventType             string                 `json:"event_type"`
	Action                string                 `json:"action"`
	Result                string                 `json:"result,omitempty"`
	PrincipalID           string                 `json:"principal_id,omitempty"`
	PrincipalType         string                 `json:"principal_type,omitempty"`
	Roles                 []SourcePermissionRole `json:"roles,omitempty"`
	Capability            string                 `json:"capability,omitempty"`
	JobRID                string                 `json:"job_rid,omitempty"`
	DownstreamResourceRID string                 `json:"downstream_resource_rid,omitempty"`
	Message               string                 `json:"message,omitempty"`
	Metadata              map[string]any         `json:"metadata,omitempty"`
}

type SourceGovernance struct {
	SourceID                 uuid.UUID                        `json:"source_id"`
	SourceRID                string                           `json:"source_rid"`
	OwnerID                  uuid.UUID                        `json:"owner_id"`
	RoleDefinitions          []SourcePermissionRoleDefinition `json:"role_definitions"`
	EffectiveRoles           []SourcePermissionRole           `json:"effective_roles"`
	PermissionGrants         []SourcePermissionGrant          `json:"permission_grants"`
	Visibility               SourceVisibilityPolicy           `json:"visibility"`
	OutputDatasetPermissions []SourceOutputDatasetPermission  `json:"output_dataset_permissions"`
	AuditEvents              []SourceGovernanceAuditEvent     `json:"audit_events"`
	Warnings                 []SourceGovernanceWarning        `json:"warnings,omitempty"`
}

type DataConnectionHealthState string

const (
	DataConnectionHealthOK       DataConnectionHealthState = "ok"
	DataConnectionHealthWarning  DataConnectionHealthState = "warning"
	DataConnectionHealthCritical DataConnectionHealthState = "critical"
	DataConnectionHealthUnknown  DataConnectionHealthState = "unknown"
)

type DataConnectionHealthSeverity string

const (
	DataConnectionHealthInfoSeverity     DataConnectionHealthSeverity = "info"
	DataConnectionHealthWarningSeverity  DataConnectionHealthSeverity = "warning"
	DataConnectionHealthCriticalSeverity DataConnectionHealthSeverity = "critical"
)

type DataConnectionHealthSurface string

const (
	DataConnectionHealthSurfaceSource        DataConnectionHealthSurface = "source"
	DataConnectionHealthSurfaceAgent         DataConnectionHealthSurface = "agent"
	DataConnectionHealthSurfaceCredential    DataConnectionHealthSurface = "credential"
	DataConnectionHealthSurfaceNetworkPolicy DataConnectionHealthSurface = "network_policy"
	DataConnectionHealthSurfaceSync          DataConnectionHealthSurface = "sync"
	DataConnectionHealthSurfaceStream        DataConnectionHealthSurface = "stream"
	DataConnectionHealthSurfaceExport        DataConnectionHealthSurface = "export"
	DataConnectionHealthSurfaceWebhook       DataConnectionHealthSurface = "webhook"
	DataConnectionHealthSurfaceCDC           DataConnectionHealthSurface = "cdc"
	DataConnectionHealthSurfaceVirtualTable  DataConnectionHealthSurface = "virtual_table"
	DataConnectionHealthSurfaceSchedule      DataConnectionHealthSurface = "schedule"
	DataConnectionHealthSurfaceRetry         DataConnectionHealthSurface = "retry"
)

type DataConnectionHealthCheck struct {
	Code           string                       `json:"code"`
	Label          string                       `json:"label"`
	Surface        DataConnectionHealthSurface  `json:"surface"`
	Severity       DataConnectionHealthSeverity `json:"severity"`
	State          DataConnectionHealthState    `json:"state"`
	Message        string                       `json:"message"`
	ResourceID     string                       `json:"resource_id,omitempty"`
	ResourceRID    string                       `json:"resource_rid,omitempty"`
	ResourceName   string                       `json:"resource_name,omitempty"`
	Recommendation string                       `json:"recommendation,omitempty"`
	LastObservedAt *time.Time                   `json:"last_observed_at,omitempty"`
	Metadata       map[string]any               `json:"metadata,omitempty"`
}

type DataConnectionHealthCounts struct {
	OK       int `json:"ok"`
	Warning  int `json:"warning"`
	Critical int `json:"critical"`
	Unknown  int `json:"unknown"`
}

type DataConnectionHealthSummary struct {
	SourceID  uuid.UUID                     `json:"source_id"`
	SourceRID string                        `json:"source_rid"`
	State     DataConnectionHealthState     `json:"state"`
	CheckedAt time.Time                     `json:"checked_at"`
	Counts    DataConnectionHealthCounts    `json:"counts"`
	Surfaces  []DataConnectionHealthSurface `json:"surfaces"`
	Checks    []DataConnectionHealthCheck   `json:"checks"`
}

type DataConnectionHealthInput struct {
	Source          Connection
	Agents          []ConnectorAgent
	Credentials     []CredentialResponse
	Policies        []SourcePolicyBindingResponse
	Syncs           []SyncJob
	SyncRuns        map[uuid.UUID][]SyncRun
	Exports         []DataExport
	WebhookHistory  []WebhookHistoryEntry
	CodeImport      *SourceCodeImport
	VirtualTables   []VirtualTable
	RetryRecovery   *RetryRecoverySummary
	CheckedAt       time.Time
	AgentStaleAfter time.Duration
	StaleRunAfter   time.Duration
}

func BuildDataConnectionHealthSummary(input DataConnectionHealthInput) DataConnectionHealthSummary {
	now := input.CheckedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	staleRunAfter := input.StaleRunAfter
	if staleRunAfter <= 0 {
		staleRunAfter = 24 * time.Hour
	}
	checks := []DataConnectionHealthCheck{}
	add := func(check DataConnectionHealthCheck) {
		check.Code = strings.TrimSpace(check.Code)
		check.Label = strings.TrimSpace(check.Label)
		check.Message = strings.TrimSpace(check.Message)
		if check.Severity == "" {
			check.Severity = healthSeverityForState(check.State)
		}
		if check.State == "" {
			check.State = healthStateForSeverity(check.Severity)
		}
		if check.ResourceID == "" && check.ResourceRID == "" && check.ResourceName == "" {
			check.ResourceID = input.Source.ID.String()
			check.ResourceRID = SourceRIDForConnection(input.Source.ID)
			check.ResourceName = input.Source.Name
		}
		checks = append(checks, check)
	}

	add(sourceHealthCheck(input.Source, now))
	add(agentHealthCheck(input.Source, input.Agents, input.Policies, now, input.AgentStaleAfter))
	for _, credential := range credentialHealthChecks(input.Credentials, now) {
		add(credential)
	}
	add(networkPolicyHealthCheck(input.Source, input.Policies))
	for _, check := range syncHealthChecks(input.Source, input.Syncs, input.SyncRuns, now, staleRunAfter) {
		add(check)
	}
	for _, check := range exportHealthChecks(input.Exports, now, staleRunAfter) {
		add(check)
	}
	for _, check := range webhookHealthChecks(input.WebhookHistory, now) {
		add(check)
	}
	for _, check := range codeImportHealthChecks(input.CodeImport, now) {
		add(check)
	}
	for _, check := range virtualTableHealthChecks(input.VirtualTables, now) {
		add(check)
	}
	for _, check := range retryRecoveryHealthChecks(input.RetryRecovery, now) {
		add(check)
	}

	summary := DataConnectionHealthSummary{
		SourceID:  input.Source.ID,
		SourceRID: SourceRIDForConnection(input.Source.ID),
		State:     DataConnectionHealthOK,
		CheckedAt: now,
		Checks:    checks,
	}
	surfaceSet := map[DataConnectionHealthSurface]bool{}
	for _, check := range checks {
		switch check.State {
		case DataConnectionHealthCritical:
			summary.Counts.Critical++
			summary.State = DataConnectionHealthCritical
		case DataConnectionHealthWarning:
			summary.Counts.Warning++
			if summary.State != DataConnectionHealthCritical {
				summary.State = DataConnectionHealthWarning
			}
		case DataConnectionHealthUnknown:
			summary.Counts.Unknown++
			if summary.State == DataConnectionHealthOK {
				summary.State = DataConnectionHealthUnknown
			}
		default:
			summary.Counts.OK++
		}
		surfaceSet[check.Surface] = true
	}
	for surface := range surfaceSet {
		summary.Surfaces = append(summary.Surfaces, surface)
	}
	sort.Slice(summary.Surfaces, func(i, j int) bool { return summary.Surfaces[i] < summary.Surfaces[j] })
	sort.SliceStable(summary.Checks, func(i, j int) bool {
		leftRank := healthStateRank(summary.Checks[i].State)
		rightRank := healthStateRank(summary.Checks[j].State)
		if leftRank == rightRank {
			if summary.Checks[i].Surface == summary.Checks[j].Surface {
				return summary.Checks[i].Code < summary.Checks[j].Code
			}
			return summary.Checks[i].Surface < summary.Checks[j].Surface
		}
		return leftRank > rightRank
	})
	return summary
}

func healthStateRank(state DataConnectionHealthState) int {
	switch state {
	case DataConnectionHealthCritical:
		return 3
	case DataConnectionHealthWarning:
		return 2
	case DataConnectionHealthUnknown:
		return 1
	default:
		return 0
	}
}

func healthSeverityForState(state DataConnectionHealthState) DataConnectionHealthSeverity {
	switch state {
	case DataConnectionHealthCritical:
		return DataConnectionHealthCriticalSeverity
	case DataConnectionHealthWarning, DataConnectionHealthUnknown:
		return DataConnectionHealthWarningSeverity
	default:
		return DataConnectionHealthInfoSeverity
	}
}

func healthStateForSeverity(severity DataConnectionHealthSeverity) DataConnectionHealthState {
	switch severity {
	case DataConnectionHealthCriticalSeverity:
		return DataConnectionHealthCritical
	case DataConnectionHealthWarningSeverity:
		return DataConnectionHealthWarning
	default:
		return DataConnectionHealthOK
	}
}

func sourceHealthCheck(source Connection, now time.Time) DataConnectionHealthCheck {
	state := DataConnectionHealthOK
	severity := DataConnectionHealthInfoSeverity
	message := "Source is connected and has no recent source-level failure."
	recommendation := ""
	switch strings.ToLower(strings.TrimSpace(source.Status)) {
	case "", "connected", "healthy", "ok", "ready":
	case "draft", "configuring", "degraded", "warning":
		state = DataConnectionHealthWarning
		severity = DataConnectionHealthWarningSeverity
		message = "Source is not fully healthy: " + source.Status + "."
		recommendation = "Finish configuration or inspect the source test results before scheduling downstream jobs."
	case "error", "failed", "offline":
		state = DataConnectionHealthCritical
		severity = DataConnectionHealthCriticalSeverity
		message = "Source status is " + source.Status + "."
		recommendation = "Run a connection test and inspect credentials, network policy, and agent reachability."
	default:
		state = DataConnectionHealthUnknown
		severity = DataConnectionHealthWarningSeverity
		message = "Source status is " + source.Status + "."
		recommendation = "Refresh source status from the connector runtime."
	}
	return DataConnectionHealthCheck{
		Code:           "source_status",
		Label:          "Source status",
		Surface:        DataConnectionHealthSurfaceSource,
		State:          state,
		Severity:       severity,
		Message:        message,
		ResourceID:     source.ID.String(),
		ResourceRID:    SourceRIDForConnection(source.ID),
		ResourceName:   source.Name,
		Recommendation: recommendation,
		LastObservedAt: &now,
		Metadata: map[string]any{
			"connector_type": source.ConnectorType,
			"status":         source.Status,
		},
	}
}

func agentHealthCheck(source Connection, agents []ConnectorAgent, policies []SourcePolicyBindingResponse, now time.Time, staleAfter time.Duration) DataConnectionHealthCheck {
	policyIDs := map[uuid.UUID]bool{}
	for _, policy := range policies {
		policyIDs[policy.PolicyID] = true
	}
	matching := []ConnectorAgent{}
	for _, agent := range agents {
		if agentMatchesSource(agent, source.ID, policyIDs) {
			matching = append(matching, ConnectorAgentWithHealth(agent, now, staleAfter))
		}
	}
	if len(matching) == 0 {
		state := DataConnectionHealthOK
		severity := DataConnectionHealthInfoSeverity
		message := "No connector agent is required for this source."
		recommendation := ""
		if connectionUsesAgentWorker(source) {
			state = DataConnectionHealthCritical
			severity = DataConnectionHealthCriticalSeverity
			message = "Source is configured for agent-backed connectivity but no reporting agent is connected."
			recommendation = "Register an agent, send a heartbeat, and attach the required proxy policy to the source."
		}
		return DataConnectionHealthCheck{
			Code:           "agent_presence",
			Label:          "Agent heartbeat",
			Surface:        DataConnectionHealthSurfaceAgent,
			State:          state,
			Severity:       severity,
			Message:        message,
			Recommendation: recommendation,
		}
	}
	state := DataConnectionHealthOK
	severity := DataConnectionHealthInfoSeverity
	message := fmt.Sprintf("%d connector agent(s) are reporting for this source.", len(matching))
	recommendation := ""
	failures := 0
	stale := 0
	for _, agent := range matching {
		if agent.Health.State == "error" {
			state = DataConnectionHealthCritical
			severity = DataConnectionHealthCriticalSeverity
			failures += agent.Health.FailureCount
			message = agent.Health.Message
			recommendation = "Inspect the agent connection failure and proxy policy assignment."
		} else if agent.Health.Stale || agent.Health.State == "stale" || agent.Health.State == "warning" {
			stale++
			if state != DataConnectionHealthCritical {
				state = DataConnectionHealthWarning
				severity = DataConnectionHealthWarningSeverity
				message = agent.Health.Message
				recommendation = "Check the agent process and heartbeat cadence."
			}
		}
	}
	if message == "" {
		message = fmt.Sprintf("%d connector agent(s) are reporting for this source.", len(matching))
	}
	return DataConnectionHealthCheck{
		Code:           "agent_health",
		Label:          "Agent heartbeat and failures",
		Surface:        DataConnectionHealthSurfaceAgent,
		State:          state,
		Severity:       severity,
		Message:        message,
		Recommendation: recommendation,
		LastObservedAt: latestAgentHeartbeat(matching),
		Metadata: map[string]any{
			"agent_count":     len(matching),
			"failure_count":   failures,
			"stale_count":     stale,
			"requires_agent":  connectionUsesAgentWorker(source),
			"attached_policy": len(policies),
		},
	}
}

func agentMatchesSource(agent ConnectorAgent, sourceID uuid.UUID, policyIDs map[uuid.UUID]bool) bool {
	for _, connected := range agent.ConnectedSources {
		if connected.SourceID == sourceID {
			return true
		}
	}
	for _, assignment := range agent.AssignedProxyPolicies {
		if assignment.SourceID == sourceID || policyIDs[assignment.PolicyID] {
			return true
		}
	}
	for _, failure := range agent.ConnectionFailures {
		if failure.SourceID == sourceID || policyIDs[failure.PolicyID] {
			return true
		}
	}
	return false
}

func latestAgentHeartbeat(agents []ConnectorAgent) *time.Time {
	var latest *time.Time
	for _, agent := range agents {
		if agent.LastHeartbeatAt != nil && (latest == nil || agent.LastHeartbeatAt.After(*latest)) {
			value := *agent.LastHeartbeatAt
			latest = &value
		}
	}
	return latest
}

func connectionUsesAgentWorker(source Connection) bool {
	if len(source.Config) == 0 || string(source.Config) == "null" {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(source.Config, &payload); err != nil {
		return false
	}
	for _, key := range []string{"worker", "worker_mode", "network_mode", "execution_mode", "runtime"} {
		if value, ok := payload[key]; ok && strings.Contains(strings.ToLower(fmt.Sprint(value)), "agent") {
			return true
		}
	}
	for _, key := range []string{"requires_agent", "use_agent", "requires_private_network_agent", "private_network", "agent_required"} {
		if value, ok := payload[key]; ok {
			switch typed := value.(type) {
			case bool:
				if typed {
					return true
				}
			default:
				if strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "true") || strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "yes") {
					return true
				}
			}
		}
	}
	if value, ok := payload["agent_id"]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
		return true
	}
	return false
}

func credentialHealthChecks(credentials []CredentialResponse, now time.Time) []DataConnectionHealthCheck {
	if len(credentials) == 0 {
		return []DataConnectionHealthCheck{{
			Code:           "credential_missing",
			Label:          "Credential binding",
			Surface:        DataConnectionHealthSurfaceCredential,
			State:          DataConnectionHealthWarning,
			Severity:       DataConnectionHealthWarningSeverity,
			Message:        "No credential metadata is visible for this source.",
			Recommendation: "Attach a credential or verify that the connector uses platform-managed identity.",
		}}
	}
	checks := []DataConnectionHealthCheck{{
		Code:     "credential_present",
		Label:    "Credential binding",
		Surface:  DataConnectionHealthSurfaceCredential,
		State:    DataConnectionHealthOK,
		Severity: DataConnectionHealthInfoSeverity,
		Message:  fmt.Sprintf("%d credential binding(s) are registered.", len(credentials)),
		Metadata: map[string]any{"credential_count": len(credentials)},
	}}
	for _, credential := range credentials {
		if credential.ExpiresAt != nil && credential.ExpiresAt.Before(now) {
			expiresAt := *credential.ExpiresAt
			checks = append(checks, DataConnectionHealthCheck{
				Code:           "credential_expired",
				Label:          "Credential expiration",
				Surface:        DataConnectionHealthSurfaceCredential,
				State:          DataConnectionHealthCritical,
				Severity:       DataConnectionHealthCriticalSeverity,
				Message:        "Credential " + credential.Kind + " has expired.",
				ResourceID:     credential.ID.String(),
				ResourceName:   credential.Kind,
				Recommendation: "Rotate or refresh the credential before running syncs, exports, or source imports.",
				LastObservedAt: &expiresAt,
			})
		} else if credential.ExpiresAt != nil && credential.ExpiresAt.Before(now.Add(7*24*time.Hour)) {
			expiresAt := *credential.ExpiresAt
			checks = append(checks, DataConnectionHealthCheck{
				Code:           "credential_expiring_soon",
				Label:          "Credential expiration",
				Surface:        DataConnectionHealthSurfaceCredential,
				State:          DataConnectionHealthWarning,
				Severity:       DataConnectionHealthWarningSeverity,
				Message:        "Credential " + credential.Kind + " expires within seven days.",
				ResourceID:     credential.ID.String(),
				ResourceName:   credential.Kind,
				Recommendation: "Rotate or extend the credential before its expiry window closes.",
				LastObservedAt: &expiresAt,
			})
		}
		switch strings.ToLower(strings.TrimSpace(credential.ValidationStatus)) {
		case "failed", "error", "invalid":
			checks = append(checks, DataConnectionHealthCheck{
				Code:           "credential_validation_failed",
				Label:          "Credential validation",
				Surface:        DataConnectionHealthSurfaceCredential,
				State:          DataConnectionHealthCritical,
				Severity:       DataConnectionHealthCriticalSeverity,
				Message:        "Credential " + credential.Kind + " failed validation.",
				ResourceID:     credential.ID.String(),
				ResourceName:   credential.Kind,
				Recommendation: "Run credential testing and replace the secret if validation continues to fail.",
				LastObservedAt: credential.LastValidatedAt,
			})
		}
	}
	return checks
}

func networkPolicyHealthCheck(source Connection, policies []SourcePolicyBindingResponse) DataConnectionHealthCheck {
	if !connectionUsesAgentWorker(source) {
		return DataConnectionHealthCheck{
			Code:     "network_policy_not_required",
			Label:    "Network policy",
			Surface:  DataConnectionHealthSurfaceNetworkPolicy,
			State:    DataConnectionHealthOK,
			Severity: DataConnectionHealthInfoSeverity,
			Message:  "No agent network policy is required by the source configuration.",
		}
	}
	if len(policies) == 0 {
		return DataConnectionHealthCheck{
			Code:           "network_policy_missing",
			Label:          "Network policy",
			Surface:        DataConnectionHealthSurfaceNetworkPolicy,
			State:          DataConnectionHealthCritical,
			Severity:       DataConnectionHealthCriticalSeverity,
			Message:        "Agent-backed source has no network egress or proxy policy binding.",
			Recommendation: "Attach a network policy before testing the source or running jobs through an agent.",
		}
	}
	return DataConnectionHealthCheck{
		Code:     "network_policy_attached",
		Label:    "Network policy",
		Surface:  DataConnectionHealthSurfaceNetworkPolicy,
		State:    DataConnectionHealthOK,
		Severity: DataConnectionHealthInfoSeverity,
		Message:  fmt.Sprintf("%d network policy binding(s) are attached.", len(policies)),
		Metadata: map[string]any{"policy_count": len(policies)},
	}
}

func syncHealthChecks(source Connection, syncs []SyncJob, runs map[uuid.UUID][]SyncRun, now time.Time, staleAfter time.Duration) []DataConnectionHealthCheck {
	if len(syncs) == 0 {
		return []DataConnectionHealthCheck{{
			Code:     "syncs_not_configured",
			Label:    "Sync resources",
			Surface:  DataConnectionHealthSurfaceSync,
			State:    DataConnectionHealthOK,
			Severity: DataConnectionHealthInfoSeverity,
			Message:  "No sync definitions are configured for this source.",
		}}
	}
	checks := []DataConnectionHealthCheck{}
	for _, sync := range syncs {
		resourceName := firstNonEmpty(trimPtr(sync.SourceTable), trimPtr(sync.SourceTopic), trimPtr(sync.SourceSelector), sync.ID.String())
		run, ok := latestSyncRun(runs[sync.ID])
		if !ok {
			state := DataConnectionHealthOK
			severity := DataConnectionHealthInfoSeverity
			code := "sync_ready"
			message := "Sync definition has no recorded run yet."
			recommendation := ""
			if trimPtr(sync.ScheduleCron) != "" {
				state = DataConnectionHealthWarning
				severity = DataConnectionHealthWarningSeverity
				code = "schedule_never_triggered"
				message = "Scheduled sync has not produced a run yet."
				recommendation = "Confirm the schedule is active and the build system can trigger this sync."
			}
			checks = append(checks, DataConnectionHealthCheck{
				Code:           code,
				Label:          "Sync run freshness",
				Surface:        DataConnectionHealthSurfaceSync,
				State:          state,
				Severity:       severity,
				Message:        message,
				ResourceID:     sync.ID.String(),
				ResourceName:   resourceName,
				Recommendation: recommendation,
			})
		} else {
			checks = append(checks, syncRunHealthCheck(sync, run, now, staleAfter, resourceName))
		}
		if sync.OutputKind == "stream" || trimPtr(sync.OutputStreamID) != "" {
			checks = append(checks, streamOutputHealthCheck(sync, run, ok, resourceName))
		}
		if sync.CdcSync != nil || sync.CapabilityType == "cdc_sync" {
			checks = append(checks, cdcSyncHealthCheck(source, sync, resourceName))
		}
	}
	return checks
}

func latestSyncRun(runs []SyncRun) (SyncRun, bool) {
	if len(runs) == 0 {
		return SyncRun{}, false
	}
	latest := runs[0]
	for _, run := range runs[1:] {
		if run.StartedAt.After(latest.StartedAt) {
			latest = run
		}
	}
	return latest, true
}

func syncRunHealthCheck(sync SyncJob, run SyncRun, now time.Time, staleAfter time.Duration, resourceName string) DataConnectionHealthCheck {
	state := DataConnectionHealthOK
	severity := DataConnectionHealthInfoSeverity
	code := "sync_recent_success"
	message := "Latest sync run is healthy."
	recommendation := ""
	if strings.EqualFold(run.Status, "failed") || run.Error != nil {
		state = DataConnectionHealthCritical
		severity = DataConnectionHealthCriticalSeverity
		code = "sync_recent_failure"
		message = "Latest sync run failed."
		if run.Error != nil && strings.TrimSpace(*run.Error) != "" {
			message = "Latest sync run failed: " + strings.TrimSpace(*run.Error)
		}
		recommendation = "Open the sync build report, inspect source progress, and rerun after the source issue is fixed."
	} else if strings.EqualFold(run.Status, "running") && now.Sub(run.StartedAt) > staleAfter {
		state = DataConnectionHealthWarning
		severity = DataConnectionHealthWarningSeverity
		code = "stale_sync"
		message = "Sync run has been running longer than the freshness window."
		recommendation = "Inspect worker logs, source progress, and retry state."
	} else if trimPtr(sync.ScheduleCron) != "" && !run.StartedAt.IsZero() && now.Sub(run.StartedAt) > staleAfter {
		state = DataConnectionHealthWarning
		severity = DataConnectionHealthWarningSeverity
		code = "stale_sync"
		message = "Scheduled sync has not completed within the freshness window."
		recommendation = "Check schedule trigger history and source runtime state."
	}
	return DataConnectionHealthCheck{
		Code:           code,
		Label:          "Sync run freshness",
		Surface:        DataConnectionHealthSurfaceSync,
		State:          state,
		Severity:       severity,
		Message:        message,
		ResourceID:     sync.ID.String(),
		ResourceName:   resourceName,
		Recommendation: recommendation,
		LastObservedAt: &run.StartedAt,
		Metadata: map[string]any{
			"status":        run.Status,
			"bytes_written": run.BytesWritten,
			"files_written": run.FilesWritten,
			"ingest_job_id": trimPtr(run.IngestJobID),
		},
	}
}

func streamOutputHealthCheck(sync SyncJob, run SyncRun, hasRun bool, resourceName string) DataConnectionHealthCheck {
	if trimPtr(sync.OutputStreamID) == "" {
		return DataConnectionHealthCheck{
			Code:           "stream_output_missing",
			Label:          "Stream output",
			Surface:        DataConnectionHealthSurfaceStream,
			State:          DataConnectionHealthCritical,
			Severity:       DataConnectionHealthCriticalSeverity,
			Message:        "Streaming sync has no output stream configured.",
			ResourceID:     sync.ID.String(),
			ResourceName:   resourceName,
			Recommendation: "Select an output stream before starting the streaming or CDC sync.",
		}
	}
	if hasRun && (strings.EqualFold(run.Status, "failed") || run.Error != nil) {
		return DataConnectionHealthCheck{
			Code:           "stream_checkpoint_failure",
			Label:          "Stream checkpoint",
			Surface:        DataConnectionHealthSurfaceStream,
			State:          DataConnectionHealthCritical,
			Severity:       DataConnectionHealthCriticalSeverity,
			Message:        "The latest stream-producing sync failed before reaching a healthy checkpoint.",
			ResourceID:     sync.ID.String(),
			ResourceRID:    trimPtr(sync.OutputStreamID),
			ResourceName:   resourceName,
			Recommendation: "Inspect checkpoint logs and resume from the last durable source position.",
			LastObservedAt: &run.StartedAt,
		}
	}
	return DataConnectionHealthCheck{
		Code:         "stream_output_configured",
		Label:        "Stream output",
		Surface:      DataConnectionHealthSurfaceStream,
		State:        DataConnectionHealthOK,
		Severity:     DataConnectionHealthInfoSeverity,
		Message:      "Streaming output is configured.",
		ResourceID:   sync.ID.String(),
		ResourceRID:  trimPtr(sync.OutputStreamID),
		ResourceName: resourceName,
	}
}

func cdcSyncHealthCheck(source Connection, sync SyncJob, resourceName string) DataConnectionHealthCheck {
	errs := ValidateCdcSyncSettings(source.ConnectorType, sync.CdcSync)
	if len(errs) > 0 {
		return DataConnectionHealthCheck{
			Code:           "cdc_metadata_invalid",
			Label:          "CDC metadata",
			Surface:        DataConnectionHealthSurfaceCDC,
			State:          DataConnectionHealthCritical,
			Severity:       DataConnectionHealthCriticalSeverity,
			Message:        strings.Join(errs, "; "),
			ResourceID:     sync.ID.String(),
			ResourceName:   resourceName,
			Recommendation: "Expose changelog data upstream and configure primary key, ordering, deletion, schema, and start position metadata.",
			Metadata:       map[string]any{"validation_errors": errs},
		}
	}
	return DataConnectionHealthCheck{
		Code:         "cdc_metadata_ready",
		Label:        "CDC metadata",
		Surface:      DataConnectionHealthSurfaceCDC,
		State:        DataConnectionHealthOK,
		Severity:     DataConnectionHealthInfoSeverity,
		Message:      "CDC metadata is complete for downstream archive, Data Health, Pipeline Builder, and object indexing consumers.",
		ResourceID:   sync.ID.String(),
		ResourceRID:  trimPtr(sync.OutputStreamID),
		ResourceName: resourceName,
		Metadata: map[string]any{
			"primary_key_columns": sync.CdcSync.PrimaryKeyColumns,
			"ordering_column":     sync.CdcSync.OrderingColumn,
			"deletion_column":     trimPtr(sync.CdcSync.DeletionColumn),
			"start_position":      sync.CdcSync.StartPosition,
		},
	}
}

// SDC.40 — Automatic retries and failure recovery.
//
// The retry surface exposes per-source backoff policies for the four failure
// categories described in the public Data Connection docs (source, network,
// credential, destination), a pure failure classifier, a deterministic
// backoff/decision helper, and an aggregate that escalates exhausted retries
// to the Data Health summary built by SDC.39.

type RetryFailureCategory string

const (
	RetryFailureCategorySource      RetryFailureCategory = "source"
	RetryFailureCategoryNetwork     RetryFailureCategory = "network"
	RetryFailureCategoryCredential  RetryFailureCategory = "credential"
	RetryFailureCategoryDestination RetryFailureCategory = "destination"
	RetryFailureCategoryUnknown     RetryFailureCategory = "unknown"
)

func RetryFailureCategories() []RetryFailureCategory {
	return []RetryFailureCategory{
		RetryFailureCategorySource,
		RetryFailureCategoryNetwork,
		RetryFailureCategoryCredential,
		RetryFailureCategoryDestination,
	}
}

type RetryBackoffPolicy struct {
	MaxAttempts            int      `json:"max_attempts"`
	InitialBackoffSeconds  int      `json:"initial_backoff_seconds"`
	MaxBackoffSeconds      int      `json:"max_backoff_seconds"`
	BackoffMultiplier      float64  `json:"backoff_multiplier"`
	JitterRatio            float64  `json:"jitter_ratio"`
	PreserveCheckpoint     bool     `json:"preserve_checkpoint"`
	EscalateAfterAttempts  int      `json:"escalate_after_attempts"`
	RetryableSubstrings    []string `json:"retryable_substrings,omitempty"`
	NonRetryableSubstrings []string `json:"non_retryable_substrings,omitempty"`
}

type SourceRetryPolicy struct {
	SourceID   uuid.UUID                                   `json:"source_id"`
	SourceRID  string                                      `json:"source_rid"`
	Categories map[RetryFailureCategory]RetryBackoffPolicy `json:"categories"`
	UpdatedBy  *string                                     `json:"updated_by,omitempty"`
	UpdatedAt  time.Time                                   `json:"updated_at"`
}

type UpdateSourceRetryPolicyRequest struct {
	Categories map[RetryFailureCategory]RetryBackoffPolicy `json:"categories"`
}

type RunFailureContext struct {
	Category          RetryFailureCategory `json:"category"`
	ErrorMessage      string               `json:"error_message"`
	Attempt           int                  `json:"attempt"`
	HasCheckpoint     bool                 `json:"has_checkpoint"`
	CheckpointSummary string               `json:"checkpoint_summary,omitempty"`
	LastFailureAt     *time.Time           `json:"last_failure_at,omitempty"`
}

type RetryDecisionAction string

const (
	RetryDecisionRetry     RetryDecisionAction = "retry"
	RetryDecisionExhausted RetryDecisionAction = "exhausted"
	RetryDecisionEscalate  RetryDecisionAction = "escalate"
	RetryDecisionNoRetry   RetryDecisionAction = "no_retry"
)

type RetryDecision struct {
	Action               RetryDecisionAction  `json:"action"`
	NextAttempt          int                  `json:"next_attempt"`
	MaxAttempts          int                  `json:"max_attempts"`
	BackoffSeconds       int                  `json:"backoff_seconds"`
	NextRetryAt          *time.Time           `json:"next_retry_at,omitempty"`
	Category             RetryFailureCategory `json:"category"`
	EscalateToDataHealth bool                 `json:"escalate_to_data_health"`
	PreserveCheckpoint   bool                 `json:"preserve_checkpoint"`
	Reason               string               `json:"reason"`
}

type RetryRecoveryRunSummary struct {
	SyncDefID         uuid.UUID            `json:"sync_def_id"`
	SyncDefName       string               `json:"sync_def_name,omitempty"`
	RunID             uuid.UUID            `json:"run_id"`
	Status            string               `json:"status"`
	Attempt           int                  `json:"attempt"`
	MaxAttempts       int                  `json:"max_attempts"`
	Category          RetryFailureCategory `json:"category"`
	Error             string               `json:"error,omitempty"`
	NextRetryAt       *time.Time           `json:"next_retry_at,omitempty"`
	HasCheckpoint     bool                 `json:"has_checkpoint"`
	CheckpointSummary string               `json:"checkpoint_summary,omitempty"`
	StartedAt         time.Time            `json:"started_at"`
	FinishedAt        *time.Time           `json:"finished_at,omitempty"`
	Escalated         bool                 `json:"escalated"`
	Decision          *RetryDecision       `json:"decision,omitempty"`
}

type RetryRecoverySummary struct {
	SourceID                uuid.UUID                 `json:"source_id"`
	SourceRID               string                    `json:"source_rid"`
	Policy                  SourceRetryPolicy         `json:"policy"`
	RecentRuns              []RetryRecoveryRunSummary `json:"recent_runs"`
	BackoffInProgressCount  int                       `json:"backoff_in_progress_count"`
	ExhaustedCount          int                       `json:"exhausted_count"`
	EscalatedCount          int                       `json:"escalated_count"`
	CheckpointPreservedRuns int                       `json:"checkpoint_preserved_runs"`
	CheckedAt               time.Time                 `json:"checked_at"`
}

// DefaultRetryBackoffPolicy returns sensible defaults for each failure
// category. The values mirror the public Foundry docs guidance that transient
// failures should retry with exponential backoff and escalate when persistent.
func DefaultRetryBackoffPolicy(category RetryFailureCategory) RetryBackoffPolicy {
	switch category {
	case RetryFailureCategoryNetwork:
		return RetryBackoffPolicy{
			MaxAttempts:           6,
			InitialBackoffSeconds: 5,
			MaxBackoffSeconds:     600,
			BackoffMultiplier:     2.0,
			JitterRatio:           0.2,
			PreserveCheckpoint:    true,
			EscalateAfterAttempts: 4,
			RetryableSubstrings: []string{
				"connection reset", "connection refused", "timeout", "tls handshake",
				"i/o timeout", "no route to host", "temporary failure", "503", "504",
			},
		}
	case RetryFailureCategoryCredential:
		return RetryBackoffPolicy{
			MaxAttempts:           3,
			InitialBackoffSeconds: 30,
			MaxBackoffSeconds:     600,
			BackoffMultiplier:     2.0,
			JitterRatio:           0.1,
			PreserveCheckpoint:    true,
			EscalateAfterAttempts: 2,
			RetryableSubstrings: []string{
				"token expired", "expired token", "401", "unauthorized: retry",
				"refresh required",
			},
			NonRetryableSubstrings: []string{
				"invalid credentials", "permission denied", "forbidden",
			},
		}
	case RetryFailureCategoryDestination:
		return RetryBackoffPolicy{
			MaxAttempts:           5,
			InitialBackoffSeconds: 15,
			MaxBackoffSeconds:     1800,
			BackoffMultiplier:     2.0,
			JitterRatio:           0.2,
			PreserveCheckpoint:    true,
			EscalateAfterAttempts: 3,
			RetryableSubstrings: []string{
				"write conflict", "dataset busy", "lock timeout",
				"transient", "throttled", "rate limit",
			},
			NonRetryableSubstrings: []string{
				"schema mismatch", "constraint violation",
			},
		}
	default:
		return RetryBackoffPolicy{
			MaxAttempts:           4,
			InitialBackoffSeconds: 10,
			MaxBackoffSeconds:     900,
			BackoffMultiplier:     2.0,
			JitterRatio:           0.2,
			PreserveCheckpoint:    true,
			EscalateAfterAttempts: 3,
			RetryableSubstrings: []string{
				"temporary", "transient", "retry", "server busy", "throttled",
			},
		}
	}
}

// DefaultSourceRetryPolicy returns a policy with defaults for every supported
// failure category. Sources that have never written a policy fall back to this.
func DefaultSourceRetryPolicy(sourceID uuid.UUID, now time.Time) SourceRetryPolicy {
	categories := make(map[RetryFailureCategory]RetryBackoffPolicy, 4)
	for _, category := range RetryFailureCategories() {
		categories[category] = DefaultRetryBackoffPolicy(category)
	}
	return SourceRetryPolicy{
		SourceID:   sourceID,
		SourceRID:  SourceRIDForConnection(sourceID),
		Categories: categories,
		UpdatedAt:  now,
	}
}

// NormalizeRetryBackoffPolicy clamps any out-of-range values to safe defaults so
// the persisted JSON cannot disable retries entirely or cause runaway backoffs.
func NormalizeRetryBackoffPolicy(policy RetryBackoffPolicy, category RetryFailureCategory) RetryBackoffPolicy {
	defaults := DefaultRetryBackoffPolicy(category)
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = defaults.MaxAttempts
	}
	if policy.MaxAttempts > 50 {
		policy.MaxAttempts = 50
	}
	if policy.InitialBackoffSeconds < 1 {
		policy.InitialBackoffSeconds = defaults.InitialBackoffSeconds
	}
	if policy.MaxBackoffSeconds < policy.InitialBackoffSeconds {
		policy.MaxBackoffSeconds = policy.InitialBackoffSeconds
	}
	if policy.MaxBackoffSeconds > 24*3600 {
		policy.MaxBackoffSeconds = 24 * 3600
	}
	if policy.BackoffMultiplier < 1.0 {
		policy.BackoffMultiplier = defaults.BackoffMultiplier
	}
	if policy.BackoffMultiplier > 10.0 {
		policy.BackoffMultiplier = 10.0
	}
	if policy.JitterRatio < 0 {
		policy.JitterRatio = 0
	}
	if policy.JitterRatio > 1 {
		policy.JitterRatio = 1
	}
	if policy.EscalateAfterAttempts < 1 {
		policy.EscalateAfterAttempts = defaults.EscalateAfterAttempts
	}
	if policy.EscalateAfterAttempts > policy.MaxAttempts {
		policy.EscalateAfterAttempts = policy.MaxAttempts
	}
	policy.RetryableSubstrings = normalizeSubstrings(policy.RetryableSubstrings)
	policy.NonRetryableSubstrings = normalizeSubstrings(policy.NonRetryableSubstrings)
	return policy
}

func normalizeSubstrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		v := strings.ToLower(strings.TrimSpace(value))
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeSourceRetryPolicy fills in any missing categories with defaults and
// clamps the supplied entries.
func NormalizeSourceRetryPolicy(policy SourceRetryPolicy, sourceID uuid.UUID, now time.Time) SourceRetryPolicy {
	if policy.Categories == nil {
		policy.Categories = map[RetryFailureCategory]RetryBackoffPolicy{}
	}
	for _, category := range RetryFailureCategories() {
		existing, ok := policy.Categories[category]
		if !ok {
			policy.Categories[category] = DefaultRetryBackoffPolicy(category)
			continue
		}
		policy.Categories[category] = NormalizeRetryBackoffPolicy(existing, category)
	}
	if policy.SourceID == uuid.Nil {
		policy.SourceID = sourceID
	}
	policy.SourceRID = SourceRIDForConnection(policy.SourceID)
	if policy.UpdatedAt.IsZero() {
		policy.UpdatedAt = now
	}
	return policy
}

// ClassifyRunFailure returns the most likely failure category for a sync run
// based on substring heuristics over the error message. It is intentionally
// conservative: ambiguous errors fall through to RetryFailureCategorySource so
// retry policies for that category apply.
func ClassifyRunFailure(errorMessage string) RetryFailureCategory {
	msg := strings.ToLower(strings.TrimSpace(errorMessage))
	if msg == "" {
		return RetryFailureCategoryUnknown
	}
	credentialMarkers := []string{
		"unauthorized", "401", "403", "forbidden", "credential", "token expired",
		"expired token", "permission denied", "access denied", "invalid signature",
	}
	for _, marker := range credentialMarkers {
		if strings.Contains(msg, marker) {
			return RetryFailureCategoryCredential
		}
	}
	networkMarkers := []string{
		"connection reset", "connection refused", "timeout", "dns",
		"i/o timeout", "tls handshake", "no route to host", "network is unreachable",
		"eof", "broken pipe", "503", "504", "502 bad gateway",
	}
	for _, marker := range networkMarkers {
		if strings.Contains(msg, marker) {
			return RetryFailureCategoryNetwork
		}
	}
	destinationMarkers := []string{
		"dataset", "dataset version", "transaction", "write conflict",
		"schema mismatch", "constraint", "lock timeout", "throttled",
		"rate limit", "destination", "stream archive",
	}
	for _, marker := range destinationMarkers {
		if strings.Contains(msg, marker) {
			return RetryFailureCategoryDestination
		}
	}
	sourceMarkers := []string{
		"source", "query failed", "table not found", "object not found",
		"schema not found", "topic not found", "serialization", "checksum",
	}
	for _, marker := range sourceMarkers {
		if strings.Contains(msg, marker) {
			return RetryFailureCategorySource
		}
	}
	return RetryFailureCategorySource
}

// ComputeRetryBackoffSeconds returns the backoff that should apply before the
// next attempt. It uses deterministic exponential growth capped by
// MaxBackoffSeconds; jitter is supplied separately by the caller when needed
// so this function is deterministic for tests.
func ComputeRetryBackoffSeconds(policy RetryBackoffPolicy, attempt int) int {
	if attempt < 1 {
		attempt = 1
	}
	base := float64(policy.InitialBackoffSeconds)
	if base <= 0 {
		base = 1
	}
	multiplier := policy.BackoffMultiplier
	if multiplier <= 1 {
		multiplier = 1
	}
	value := base
	for i := 1; i < attempt; i++ {
		value *= multiplier
		if policy.MaxBackoffSeconds > 0 && value >= float64(policy.MaxBackoffSeconds) {
			value = float64(policy.MaxBackoffSeconds)
			break
		}
	}
	if policy.MaxBackoffSeconds > 0 && value > float64(policy.MaxBackoffSeconds) {
		value = float64(policy.MaxBackoffSeconds)
	}
	if value < 1 {
		value = 1
	}
	return int(value)
}

// EvaluateRetryDecision applies a policy to a run failure and returns the
// recommended retry action. Callers use the returned decision to schedule the
// next attempt and to drive Data Health escalation.
func EvaluateRetryDecision(policy RetryBackoffPolicy, failure RunFailureContext, now time.Time) RetryDecision {
	policy = NormalizeRetryBackoffPolicy(policy, failure.Category)
	if failure.Attempt < 1 {
		failure.Attempt = 1
	}

	msg := strings.ToLower(strings.TrimSpace(failure.ErrorMessage))
	for _, marker := range policy.NonRetryableSubstrings {
		if marker != "" && strings.Contains(msg, marker) {
			return RetryDecision{
				Action:               RetryDecisionNoRetry,
				NextAttempt:          failure.Attempt,
				MaxAttempts:          policy.MaxAttempts,
				Category:             failure.Category,
				EscalateToDataHealth: true,
				PreserveCheckpoint:   policy.PreserveCheckpoint && failure.HasCheckpoint,
				Reason:               "Failure matched a non-retryable signature; manual remediation required.",
			}
		}
	}

	retryable := true
	if len(policy.RetryableSubstrings) > 0 {
		retryable = false
		for _, marker := range policy.RetryableSubstrings {
			if marker != "" && strings.Contains(msg, marker) {
				retryable = true
				break
			}
		}
	}

	if !retryable {
		return RetryDecision{
			Action:               RetryDecisionNoRetry,
			NextAttempt:          failure.Attempt,
			MaxAttempts:          policy.MaxAttempts,
			Category:             failure.Category,
			EscalateToDataHealth: true,
			PreserveCheckpoint:   policy.PreserveCheckpoint && failure.HasCheckpoint,
			Reason:               "Failure signature is outside the configured retryable patterns for this category.",
		}
	}

	if failure.Attempt >= policy.MaxAttempts {
		return RetryDecision{
			Action:               RetryDecisionExhausted,
			NextAttempt:          failure.Attempt,
			MaxAttempts:          policy.MaxAttempts,
			Category:             failure.Category,
			EscalateToDataHealth: true,
			PreserveCheckpoint:   policy.PreserveCheckpoint && failure.HasCheckpoint,
			Reason:               fmt.Sprintf("Reached the configured max %d attempts for %s failures.", policy.MaxAttempts, failure.Category),
		}
	}

	nextAttempt := failure.Attempt + 1
	backoff := ComputeRetryBackoffSeconds(policy, nextAttempt)
	nextRetryAt := now.Add(time.Duration(backoff) * time.Second)
	escalate := failure.Attempt >= policy.EscalateAfterAttempts

	action := RetryDecisionRetry
	if escalate {
		action = RetryDecisionEscalate
	}

	reason := fmt.Sprintf("Attempt %d/%d scheduled in %ds.", nextAttempt, policy.MaxAttempts, backoff)
	if escalate {
		reason += " Persistent failure escalated to Data Health."
	}

	return RetryDecision{
		Action:               action,
		NextAttempt:          nextAttempt,
		MaxAttempts:          policy.MaxAttempts,
		BackoffSeconds:       backoff,
		NextRetryAt:          &nextRetryAt,
		Category:             failure.Category,
		EscalateToDataHealth: escalate,
		PreserveCheckpoint:   policy.PreserveCheckpoint && failure.HasCheckpoint,
		Reason:               reason,
	}
}

// RetryRecoveryInput drives BuildRetryRecoverySummary so a handler can pass
// the per-source policy together with the recent failed-run snapshots in one
// shot, without the helper having to read from the database.
type RetryRecoveryInput struct {
	SourceID  uuid.UUID
	Policy    SourceRetryPolicy
	Failures  []RetryRecoveryRunSummary
	CheckedAt time.Time
}

// BuildRetryRecoverySummary applies the source's retry policy to each recent
// failed run and produces an aggregate snapshot consumed by the Data Health
// summary and the Source Detail UI.
func BuildRetryRecoverySummary(input RetryRecoveryInput) RetryRecoverySummary {
	now := input.CheckedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	policy := NormalizeSourceRetryPolicy(input.Policy, input.SourceID, now)

	out := RetryRecoverySummary{
		SourceID:  input.SourceID,
		SourceRID: SourceRIDForConnection(input.SourceID),
		Policy:    policy,
		CheckedAt: now,
	}

	runs := make([]RetryRecoveryRunSummary, 0, len(input.Failures))
	for _, run := range input.Failures {
		if run.Category == "" {
			run.Category = ClassifyRunFailure(run.Error)
		}
		categoryPolicy, ok := policy.Categories[run.Category]
		if !ok {
			categoryPolicy = DefaultRetryBackoffPolicy(run.Category)
		}
		decision := EvaluateRetryDecision(categoryPolicy, RunFailureContext{
			Category:          run.Category,
			ErrorMessage:      run.Error,
			Attempt:           run.Attempt,
			HasCheckpoint:     run.HasCheckpoint,
			CheckpointSummary: run.CheckpointSummary,
		}, now)
		copied := decision
		run.Decision = &copied
		if run.MaxAttempts == 0 {
			run.MaxAttempts = decision.MaxAttempts
		}
		if run.NextRetryAt == nil && decision.NextRetryAt != nil && decision.Action == RetryDecisionRetry {
			run.NextRetryAt = decision.NextRetryAt
		}
		runs = append(runs, run)

		switch decision.Action {
		case RetryDecisionRetry, RetryDecisionEscalate:
			if run.NextRetryAt != nil && run.NextRetryAt.After(now) {
				out.BackoffInProgressCount++
			}
		case RetryDecisionExhausted, RetryDecisionNoRetry:
			out.ExhaustedCount++
		}
		if decision.EscalateToDataHealth || run.Escalated {
			out.EscalatedCount++
		}
		if run.HasCheckpoint && decision.PreserveCheckpoint {
			out.CheckpointPreservedRuns++
		}
	}
	out.RecentRuns = runs
	return out
}

func retryRecoveryHealthChecks(summary *RetryRecoverySummary, now time.Time) []DataConnectionHealthCheck {
	if summary == nil {
		return nil
	}
	checks := []DataConnectionHealthCheck{}
	if summary.EscalatedCount > 0 {
		checks = append(checks, DataConnectionHealthCheck{
			Code:           "retry_escalated",
			Label:          "Retry escalation",
			Surface:        DataConnectionHealthSurfaceRetry,
			State:          DataConnectionHealthCritical,
			Severity:       DataConnectionHealthCriticalSeverity,
			Message:        fmt.Sprintf("%d run(s) persistent enough to escalate to Data Health.", summary.EscalatedCount),
			Recommendation: "Open the most recent failed run, inspect logs, and decide whether to relax or override the retry policy.",
			Metadata: map[string]any{
				"escalated_count": summary.EscalatedCount,
			},
			LastObservedAt: &now,
		})
	}
	if summary.ExhaustedCount > 0 {
		checks = append(checks, DataConnectionHealthCheck{
			Code:           "retry_exhausted",
			Label:          "Retry attempts exhausted",
			Surface:        DataConnectionHealthSurfaceRetry,
			State:          DataConnectionHealthCritical,
			Severity:       DataConnectionHealthCriticalSeverity,
			Message:        fmt.Sprintf("%d run(s) reached the configured max attempts without recovery.", summary.ExhaustedCount),
			Recommendation: "Resume from the latest preserved checkpoint after the underlying source, network, credential, or destination issue is fixed.",
			Metadata: map[string]any{
				"exhausted_count": summary.ExhaustedCount,
			},
			LastObservedAt: &now,
		})
	}
	if summary.BackoffInProgressCount > 0 {
		checks = append(checks, DataConnectionHealthCheck{
			Code:           "retry_backoff_in_progress",
			Label:          "Retry backoff in progress",
			Surface:        DataConnectionHealthSurfaceRetry,
			State:          DataConnectionHealthWarning,
			Severity:       DataConnectionHealthWarningSeverity,
			Message:        fmt.Sprintf("%d run(s) are waiting for their next retry window.", summary.BackoffInProgressCount),
			Recommendation: "Wait for the scheduled retry or trigger an explicit run from sync history.",
			Metadata: map[string]any{
				"backoff_in_progress_count": summary.BackoffInProgressCount,
			},
			LastObservedAt: &now,
		})
	}
	if len(checks) == 0 && len(summary.RecentRuns) > 0 {
		checks = append(checks, DataConnectionHealthCheck{
			Code:           "retry_recovery_clean",
			Label:          "Retry posture",
			Surface:        DataConnectionHealthSurfaceRetry,
			State:          DataConnectionHealthOK,
			Severity:       DataConnectionHealthInfoSeverity,
			Message:        "No pending retries, escalations, or exhausted attempts.",
			LastObservedAt: &now,
		})
	}
	return checks
}

func exportHealthChecks(exports []DataExport, now time.Time, staleAfter time.Duration) []DataConnectionHealthCheck {
	if len(exports) == 0 {
		return []DataConnectionHealthCheck{{
			Code:     "exports_not_configured",
			Label:    "Export resources",
			Surface:  DataConnectionHealthSurfaceExport,
			State:    DataConnectionHealthOK,
			Severity: DataConnectionHealthInfoSeverity,
			Message:  "No exports are configured for this source.",
		}}
	}
	checks := []DataConnectionHealthCheck{}
	for _, export := range exports {
		checks = append(checks, dataExportHealthCheck(export, now, staleAfter))
		checks = append(checks, tableExportHealthChecks(export)...)
		checks = append(checks, streamingExportHealthChecks(export)...)
		if trimPtr(export.ScheduleCron) != "" || export.Schedule != nil {
			checks = append(checks, exportScheduleHealthCheck(export))
		}
	}
	return checks
}

func dataExportHealthCheck(export DataExport, now time.Time, staleAfter time.Duration) DataConnectionHealthCheck {
	state := DataConnectionHealthOK
	severity := DataConnectionHealthInfoSeverity
	code := "export_healthy"
	message := "Export health is healthy."
	recommendation := ""
	if export.Status == DataExportStatusFailed || export.Health.State == DataExportHealthError {
		state = DataConnectionHealthCritical
		severity = DataConnectionHealthCriticalSeverity
		code = "export_recent_failure"
		message = "Export is failed."
		if export.Health.Message != nil && strings.TrimSpace(*export.Health.Message) != "" {
			message = strings.TrimSpace(*export.Health.Message)
		}
		recommendation = "Open export history, inspect the build report, and rerun after destination issues are fixed."
	} else if export.Health.State == DataExportHealthWarning {
		state = DataConnectionHealthWarning
		severity = DataConnectionHealthWarningSeverity
		code = "export_warning"
		message = "Export health is warning."
		if export.Health.Message != nil && strings.TrimSpace(*export.Health.Message) != "" {
			message = strings.TrimSpace(*export.Health.Message)
		}
		recommendation = "Inspect export history and destination configuration."
	} else if export.Health.State == DataExportHealthNotRun && (export.ScheduleCron != nil || export.Schedule != nil) {
		state = DataConnectionHealthWarning
		severity = DataConnectionHealthWarningSeverity
		code = "schedule_never_triggered"
		message = "Scheduled export has not run yet."
		recommendation = "Confirm the schedule is active and has permission to trigger the export."
	} else if export.LastRunAt != nil && now.Sub(*export.LastRunAt) > staleAfter && (export.ScheduleCron != nil || export.Schedule != nil) {
		state = DataConnectionHealthWarning
		severity = DataConnectionHealthWarningSeverity
		code = "stale_export"
		message = "Scheduled export has not run within the freshness window."
		recommendation = "Inspect schedule history and retry state."
	}
	if latest, ok := latestExportHistory(export.History); ok && failedHistoryStatus(latest.Status) {
		state = DataConnectionHealthCritical
		severity = DataConnectionHealthCriticalSeverity
		code = "export_recent_failure"
		message = "Latest export job failed."
		if latest.ErrorMessage != nil && strings.TrimSpace(*latest.ErrorMessage) != "" {
			message = strings.TrimSpace(*latest.ErrorMessage)
		}
		recommendation = "Open the export build report and verify destination credentials, schema, and network reachability."
	}
	return DataConnectionHealthCheck{
		Code:           code,
		Label:          "Export health",
		Surface:        DataConnectionHealthSurfaceExport,
		State:          state,
		Severity:       severity,
		Message:        message,
		ResourceID:     export.ID.String(),
		ResourceName:   export.Name,
		Recommendation: recommendation,
		LastObservedAt: export.Health.LastCheckedAt,
		Metadata: map[string]any{
			"export_type": string(export.ExportType),
			"export_mode": string(export.ExportMode),
			"status":      string(export.Status),
		},
	}
}

func latestExportHistory(history []DataExportHistoryEntry) (DataExportHistoryEntry, bool) {
	if len(history) == 0 {
		return DataExportHistoryEntry{}, false
	}
	latest := history[0]
	for _, entry := range history[1:] {
		if entry.CreatedAt.After(latest.CreatedAt) {
			latest = entry
		}
	}
	return latest, true
}

func failedHistoryStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error", "cancelled":
		return true
	default:
		return false
	}
}

func tableExportHealthChecks(export DataExport) []DataConnectionHealthCheck {
	if export.TableExport == nil || len(export.TableExport.ValidationIssues) == 0 {
		return nil
	}
	checks := []DataConnectionHealthCheck{}
	for _, issue := range export.TableExport.ValidationIssues {
		state := DataConnectionHealthWarning
		severity := DataConnectionHealthWarningSeverity
		if strings.EqualFold(issue.Severity, "error") || strings.EqualFold(issue.Severity, "critical") {
			state = DataConnectionHealthCritical
			severity = DataConnectionHealthCriticalSeverity
		}
		checks = append(checks, DataConnectionHealthCheck{
			Code:           "destination_schema_mismatch",
			Label:          "Destination schema",
			Surface:        DataConnectionHealthSurfaceExport,
			State:          state,
			Severity:       severity,
			Message:        issue.Message,
			ResourceID:     export.ID.String(),
			ResourceName:   export.Name,
			Recommendation: "Align destination column names/types, Parquet-backed input, table existence, truncate permission, and nested-type support.",
			Metadata: map[string]any{
				"issue_code": issue.Code,
				"column":     trimPtr(issue.Column),
			},
		})
	}
	return checks
}

func streamingExportHealthChecks(export DataExport) []DataConnectionHealthCheck {
	if export.StreamingExport == nil || len(export.StreamingExport.Warnings) == 0 {
		return nil
	}
	checks := []DataConnectionHealthCheck{}
	for _, warning := range export.StreamingExport.Warnings {
		checks = append(checks, DataConnectionHealthCheck{
			Code:           "streaming_export_replay_warning",
			Label:          "Streaming export replay",
			Surface:        DataConnectionHealthSurfaceExport,
			State:          DataConnectionHealthWarning,
			Severity:       DataConnectionHealthWarningSeverity,
			Message:        warning.Message,
			ResourceID:     export.ID.String(),
			ResourceName:   export.Name,
			Recommendation: "Choose replay behavior deliberately and make downstream consumers tolerate the selected duplicate/drop risk.",
			Metadata:       map[string]any{"warning_code": warning.Code},
		})
	}
	return checks
}

func exportScheduleHealthCheck(export DataExport) DataConnectionHealthCheck {
	if export.Schedule != nil && !export.Schedule.Active {
		return DataConnectionHealthCheck{
			Code:           "schedule_inactive",
			Label:          "Export schedule",
			Surface:        DataConnectionHealthSurfaceSchedule,
			State:          DataConnectionHealthWarning,
			Severity:       DataConnectionHealthWarningSeverity,
			Message:        "Export schedule is inactive.",
			ResourceID:     export.ID.String(),
			ResourceRID:    export.Schedule.RID,
			ResourceName:   export.Schedule.Name,
			Recommendation: "Reactivate or replace the schedule if this export should continue running.",
		}
	}
	if export.Schedule != nil && export.Schedule.LastTriggeredAt == nil && export.LastRunAt == nil {
		return DataConnectionHealthCheck{
			Code:           "schedule_never_triggered",
			Label:          "Export schedule",
			Surface:        DataConnectionHealthSurfaceSchedule,
			State:          DataConnectionHealthWarning,
			Severity:       DataConnectionHealthWarningSeverity,
			Message:        "Export schedule has not triggered a job yet.",
			ResourceID:     export.ID.String(),
			ResourceRID:    export.Schedule.RID,
			ResourceName:   export.Schedule.Name,
			Recommendation: "Confirm schedule permissions and trigger configuration.",
		}
	}
	return DataConnectionHealthCheck{
		Code:         "schedule_active",
		Label:        "Export schedule",
		Surface:      DataConnectionHealthSurfaceSchedule,
		State:        DataConnectionHealthOK,
		Severity:     DataConnectionHealthInfoSeverity,
		Message:      "Export schedule is active.",
		ResourceID:   export.ID.String(),
		ResourceName: export.Name,
	}
}

func webhookHealthChecks(history []WebhookHistoryEntry, now time.Time) []DataConnectionHealthCheck {
	if len(history) == 0 {
		return []DataConnectionHealthCheck{{
			Code:     "webhook_no_recent_failures",
			Label:    "Webhook executions",
			Surface:  DataConnectionHealthSurfaceWebhook,
			State:    DataConnectionHealthOK,
			Severity: DataConnectionHealthInfoSeverity,
			Message:  "No retained webhook failures are present.",
		}}
	}
	failures := 0
	clientErrors := 0
	var latest *WebhookHistoryEntry
	for i := range history {
		entry := history[i]
		if entry.CreatedAt.Before(now.Add(-24 * time.Hour)) {
			continue
		}
		failed := failedHistoryStatus(entry.Status)
		if entry.HTTPStatus != nil && *entry.HTTPStatus >= 500 {
			failed = true
		} else if entry.HTTPStatus != nil && *entry.HTTPStatus >= 400 {
			clientErrors++
		}
		if failed {
			failures++
			if latest == nil || entry.CreatedAt.After(latest.CreatedAt) {
				latest = &entry
			}
		}
	}
	if failures > 0 {
		message := fmt.Sprintf("%d webhook invocation(s) failed in the last 24 hours.", failures)
		if latest != nil && latest.Error != nil && strings.TrimSpace(*latest.Error) != "" {
			message = strings.TrimSpace(*latest.Error)
		}
		return []DataConnectionHealthCheck{{
			Code:           "webhook_recent_failures",
			Label:          "Webhook executions",
			Surface:        DataConnectionHealthSurfaceWebhook,
			State:          DataConnectionHealthCritical,
			Severity:       DataConnectionHealthCriticalSeverity,
			Message:        message,
			Recommendation: "Inspect webhook history, HTTP status, auth reference, timeout, and network policy.",
			LastObservedAt: latestTimeFromWebhook(latest),
			Metadata:       map[string]any{"failure_count": failures},
		}}
	}
	if clientErrors > 0 {
		return []DataConnectionHealthCheck{{
			Code:           "webhook_recent_client_errors",
			Label:          "Webhook executions",
			Surface:        DataConnectionHealthSurfaceWebhook,
			State:          DataConnectionHealthWarning,
			Severity:       DataConnectionHealthWarningSeverity,
			Message:        fmt.Sprintf("%d webhook invocation(s) returned HTTP 4xx in the last 24 hours.", clientErrors),
			Recommendation: "Verify outbound request parameters and credentials.",
			Metadata:       map[string]any{"client_error_count": clientErrors},
		}}
	}
	return []DataConnectionHealthCheck{{
		Code:     "webhook_no_recent_failures",
		Label:    "Webhook executions",
		Surface:  DataConnectionHealthSurfaceWebhook,
		State:    DataConnectionHealthOK,
		Severity: DataConnectionHealthInfoSeverity,
		Message:  "Retained webhook history has no recent failures.",
	}}
}

func latestTimeFromWebhook(entry *WebhookHistoryEntry) *time.Time {
	if entry == nil {
		return nil
	}
	value := entry.CreatedAt
	return &value
}

func codeImportHealthChecks(codeImport *SourceCodeImport, now time.Time) []DataConnectionHealthCheck {
	if codeImport == nil || !codeImport.Enabled {
		return []DataConnectionHealthCheck{{
			Code:     "code_import_disabled",
			Label:    "Source imports",
			Surface:  DataConnectionHealthSurfaceSource,
			State:    DataConnectionHealthOK,
			Severity: DataConnectionHealthInfoSeverity,
			Message:  "Source imports are not enabled for this source.",
		}}
	}
	checks := []DataConnectionHealthCheck{{
		Code:           "code_import_live_configuration",
		Label:          "Source imports",
		Surface:        DataConnectionHealthSurfaceSource,
		State:          DataConnectionHealthOK,
		Severity:       DataConnectionHealthInfoSeverity,
		Message:        "Code imports resolve source credentials, egress, and export controls at build start.",
		ResourceRID:    codeImport.SourceRID,
		ResourceName:   codeImport.FriendlyName,
		LastObservedAt: &now,
	}}
	decision := codeImport.BuildStartResolution.ExportPolicyDecision
	if decision.UsesFoundryInputs && !decision.BuildAllowed {
		message := "Source export policy blocks code import builds that combine Foundry inputs with this external source."
		if len(decision.BlockingReasons) > 0 && strings.TrimSpace(decision.BlockingReasons[0].Message) != "" {
			message = decision.BlockingReasons[0].Message
		}
		checks = append(checks, DataConnectionHealthCheck{
			Code:           "export_policy_violation",
			Label:          "Export policy",
			Surface:        DataConnectionHealthSurfaceExport,
			State:          DataConnectionHealthCritical,
			Severity:       DataConnectionHealthCriticalSeverity,
			Message:        message,
			ResourceRID:    codeImport.SourceRID,
			ResourceName:   codeImport.FriendlyName,
			Recommendation: "Change source export controls or remove Foundry inputs from the build before object indexing or downstream execution.",
			Metadata: map[string]any{
				"status":                decision.Status,
				"foundry_input_count":   len(decision.FoundryInputs),
				"missing_markings":      decision.MissingMarkings,
				"missing_organizations": decision.MissingOrganizations,
			},
		})
	}
	for _, warning := range codeImport.Warnings {
		state := DataConnectionHealthWarning
		severity := DataConnectionHealthWarningSeverity
		if strings.EqualFold(warning.Severity, "error") || strings.EqualFold(warning.Severity, "critical") {
			state = DataConnectionHealthCritical
			severity = DataConnectionHealthCriticalSeverity
		}
		checks = append(checks, DataConnectionHealthCheck{
			Code:           warning.Code,
			Label:          "Source import warning",
			Surface:        DataConnectionHealthSurfaceSource,
			State:          state,
			Severity:       severity,
			Message:        warning.Message,
			ResourceRID:    codeImport.SourceRID,
			ResourceName:   codeImport.FriendlyName,
			Recommendation: "Resolve source import warnings before relying on build-start source resolution.",
		})
	}
	return checks
}

func virtualTableHealthChecks(tables []VirtualTable, now time.Time) []DataConnectionHealthCheck {
	if len(tables) == 0 {
		return []DataConnectionHealthCheck{{
			Code:     "virtual_tables_not_configured",
			Label:    "Virtual tables",
			Surface:  DataConnectionHealthSurfaceVirtualTable,
			State:    DataConnectionHealthOK,
			Severity: DataConnectionHealthInfoSeverity,
			Message:  "No virtual tables are registered for this source.",
		}}
	}
	checks := []DataConnectionHealthCheck{}
	for _, table := range tables {
		state := DataConnectionHealthOK
		severity := DataConnectionHealthInfoSeverity
		code := "virtual_table_update_detection_ready"
		message := "Virtual table update detection is healthy or not required."
		recommendation := ""
		if table.UpdateDetectionConsecutiveFailures > 0 {
			state = DataConnectionHealthWarning
			severity = DataConnectionHealthWarningSeverity
			code = "virtual_table_update_detection_failed"
			message = fmt.Sprintf("Virtual table update detection has %d consecutive failure(s).", table.UpdateDetectionConsecutiveFailures)
			recommendation = "Poll the source table and inspect connector version/update detection support."
			if table.UpdateDetectionConsecutiveFailures >= 3 {
				state = DataConnectionHealthCritical
				severity = DataConnectionHealthCriticalSeverity
			}
		} else if table.UpdateDetectionEnabled && table.LastObservedVersion == nil && table.LastPolledAt == nil {
			state = DataConnectionHealthWarning
			severity = DataConnectionHealthWarningSeverity
			code = "virtual_table_update_detection_unverified"
			message = "Virtual table update detection is enabled but has not observed a source version yet."
			recommendation = "Run an update-detection poll before relying on downstream build skipping."
		} else if table.UpdateDetectionEnabled && table.LastPolledAt != nil && table.UpdateDetectionIntervalSeconds != nil {
			interval := time.Duration(*table.UpdateDetectionIntervalSeconds) * time.Second
			if interval > 0 && now.Sub(*table.LastPolledAt) > 2*interval {
				state = DataConnectionHealthWarning
				severity = DataConnectionHealthWarningSeverity
				code = "virtual_table_update_detection_stale"
				message = "Virtual table update detection poll is stale."
				recommendation = "Check the update-detection scheduler and source-side versioning support."
			}
		}
		checks = append(checks, DataConnectionHealthCheck{
			Code:           code,
			Label:          "Virtual table update detection",
			Surface:        DataConnectionHealthSurfaceVirtualTable,
			State:          state,
			Severity:       severity,
			Message:        message,
			ResourceID:     table.ID.String(),
			ResourceRID:    table.RID,
			ResourceName:   table.Name,
			Recommendation: recommendation,
			LastObservedAt: table.LastPolledAt,
			Metadata: map[string]any{
				"update_detection_enabled":              table.UpdateDetectionEnabled,
				"update_detection_consecutive_failures": table.UpdateDetectionConsecutiveFailures,
				"last_observed_version":                 trimPtr(table.LastObservedVersion),
			},
		})
	}
	return checks
}

type UpdateSourceGovernanceRequest struct {
	PermissionGrants []SourcePermissionGrant `json:"permission_grants,omitempty"`
	Visibility       *SourceVisibilityPolicy `json:"visibility,omitempty"`
	Reason           string                  `json:"reason,omitempty"`
}

func SourcePermissionRoleDefinitions() []SourcePermissionRoleDefinition {
	return []SourcePermissionRoleDefinition{
		{Role: SourceRoleView, Label: "Source view", Description: "See source metadata, worker, network bindings, sync/export/webhook lists, and non-secret configuration."},
		{Role: SourceRoleEdit, Label: "Source edit", Description: "Change source configuration, credentials, network bindings, sync definitions, exports, webhooks, and sharing.", ImpliedRoles: []SourcePermissionRole{SourceRoleView, SourceRoleUse, SourceRoleWebhookExecute, SourceRoleSyncCreate, SourceRoleExportCreate, SourceRoleCodeImport}},
		{Role: SourceRoleUse, Label: "Source use", Description: "Use the source from downstream jobs without granting permission to edit its configuration."},
		{Role: SourceRoleOwner, Label: "Source ownership", Description: "Administer permission grants, exportable markings, and code-import approval.", ImpliedRoles: []SourcePermissionRole{SourceRoleView, SourceRoleEdit, SourceRoleUse, SourceRoleWebhookExecute, SourceRoleSyncCreate, SourceRoleExportCreate, SourceRoleCodeImport}},
		{Role: SourceRoleWebhookExecute, Label: "Webhook execution", Description: "Execute webhooks backed by this source."},
		{Role: SourceRoleSyncCreate, Label: "Sync creation", Description: "Create new sync definitions that read from this source."},
		{Role: SourceRoleExportCreate, Label: "Export creation", Description: "Create export resources that use this source as the external destination."},
		{Role: SourceRoleCodeImport, Label: "Code import", Description: "Import this source into code resources and resolve its credentials and egress at build start."},
	}
}

func AllSourcePermissionRoles() []SourcePermissionRole {
	return []SourcePermissionRole{SourceRoleView, SourceRoleEdit, SourceRoleUse, SourceRoleOwner, SourceRoleWebhookExecute, SourceRoleSyncCreate, SourceRoleExportCreate, SourceRoleCodeImport}
}

func SourceRoleLabel(role SourcePermissionRole) string {
	for _, def := range SourcePermissionRoleDefinitions() {
		if def.Role == role {
			return def.Label
		}
	}
	return string(role)
}

func SourceRoleIsKnown(role SourcePermissionRole) bool {
	for _, known := range AllSourcePermissionRoles() {
		if known == role {
			return true
		}
	}
	return false
}

func NormalizeSourcePermissionRoles(roles []SourcePermissionRole) []SourcePermissionRole {
	out := make([]SourcePermissionRole, 0, len(roles))
	seen := map[SourcePermissionRole]bool{}
	for _, role := range roles {
		clean := SourcePermissionRole(strings.TrimSpace(string(role)))
		if clean == "" || !SourceRoleIsKnown(clean) || seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i]) < string(out[j]) })
	return out
}

func ExpandSourcePermissionRoles(roles []SourcePermissionRole) []SourcePermissionRole {
	expanded := map[SourcePermissionRole]bool{}
	var visit func(SourcePermissionRole)
	defs := map[SourcePermissionRole]SourcePermissionRoleDefinition{}
	for _, def := range SourcePermissionRoleDefinitions() {
		defs[def.Role] = def
	}
	visit = func(role SourcePermissionRole) {
		if !SourceRoleIsKnown(role) || expanded[role] {
			return
		}
		expanded[role] = true
		for _, implied := range defs[role].ImpliedRoles {
			visit(implied)
		}
	}
	for _, role := range NormalizeSourcePermissionRoles(roles) {
		visit(role)
	}
	out := make([]SourcePermissionRole, 0, len(expanded))
	for role := range expanded {
		out = append(out, role)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i]) < string(out[j]) })
	return out
}

func SourceRolesAllow(roles []SourcePermissionRole, required SourcePermissionRole) bool {
	for _, role := range ExpandSourcePermissionRoles(roles) {
		if role == required {
			return true
		}
	}
	return false
}

func SourceRoleListContains(roles []SourcePermissionRole, required SourcePermissionRole) bool {
	for _, role := range NormalizeSourcePermissionRoles(roles) {
		if role == required {
			return true
		}
	}
	return false
}

func RequiredSourceAccessRoles(required SourcePermissionRole) []SourcePermissionRole {
	out := []SourcePermissionRole{required}
	switch required {
	case SourceRoleView:
		out = append(out, SourceRoleEdit, SourceRoleOwner)
	case SourceRoleEdit:
		out = append(out, SourceRoleOwner)
	case SourceRoleUse:
		out = append(out, SourceRoleEdit, SourceRoleOwner)
	case SourceRoleWebhookExecute, SourceRoleSyncCreate, SourceRoleExportCreate, SourceRoleCodeImport:
		out = append(out, SourceRoleEdit, SourceRoleOwner)
	case SourceRoleOwner:
		// Source owner grants are deliberately not implied by edit.
	}
	return NormalizeSourcePermissionRoles(out)
}

func SourcePermissionRoleStrings(roles []SourcePermissionRole) []string {
	normalized := NormalizeSourcePermissionRoles(roles)
	out := make([]string, 0, len(normalized))
	for _, role := range normalized {
		out = append(out, string(role))
	}
	return out
}

func SourcePermissionRolesFromStrings(roles []string) []SourcePermissionRole {
	out := make([]SourcePermissionRole, 0, len(roles))
	for _, role := range roles {
		out = append(out, SourcePermissionRole(strings.TrimSpace(role)))
	}
	return NormalizeSourcePermissionRoles(out)
}

func DefaultSourceVisibilityPolicy() SourceVisibilityPolicy {
	return NormalizeSourceVisibilityPolicy(SourceVisibilityPolicy{})
}

func NormalizeSourceVisibilityPolicy(policy SourceVisibilityPolicy) SourceVisibilityPolicy {
	if len(policy.SourceVisibilityRoles) == 0 {
		policy.SourceVisibilityRoles = []SourcePermissionRole{SourceRoleView, SourceRoleEdit, SourceRoleOwner}
	}
	if len(policy.CredentialVisibilityRoles) == 0 {
		policy.CredentialVisibilityRoles = []SourcePermissionRole{SourceRoleEdit, SourceRoleCodeImport, SourceRoleOwner}
	}
	if len(policy.ExternalSampleVisibilityRoles) == 0 {
		policy.ExternalSampleVisibilityRoles = []SourcePermissionRole{SourceRoleUse, SourceRoleEdit, SourceRoleOwner}
	}
	if len(policy.OutputDatasetPermissionRoles) == 0 {
		policy.OutputDatasetPermissionRoles = []string{"dataset:view", "dataset:edit"}
	}
	if strings.TrimSpace(policy.OutputDatasetPermissionSystem) == "" {
		policy.OutputDatasetPermissionSystem = "dataset-service"
	}
	policy.SourceVisibilityRoles = NormalizeSourcePermissionRoles(policy.SourceVisibilityRoles)
	policy.CredentialVisibilityRoles = NormalizeSourcePermissionRoles(policy.CredentialVisibilityRoles)
	policy.ExternalSampleVisibilityRoles = NormalizeSourcePermissionRoles(policy.ExternalSampleVisibilityRoles)
	policy.OutputDatasetPermissionRoles = normalizeUniqueStrings(policy.OutputDatasetPermissionRoles)
	policy.CredentialValuesVisible = false
	policy.ExternalSamplesPersisted = false
	policy.OutputDatasetPermissionsEnforced = true
	policy.SourceVisibilityDistinct = true
	policy.CredentialVisibilityDistinct = !SourceRolesAllow([]SourcePermissionRole{SourceRoleView}, SourceRoleEdit)
	policy.ExternalSampleVisibilityDistinct = !SourceRolesAllow([]SourcePermissionRole{SourceRoleView}, SourceRoleUse)
	policy.OutputDatasetPermissionsDistinct = true
	return policy
}

func NormalizeSourcePermissionGrant(grant SourcePermissionGrant, sourceID uuid.UUID, actorID uuid.UUID, now time.Time) SourcePermissionGrant {
	grant.SourceID = sourceID
	grant.PrincipalID = strings.TrimSpace(grant.PrincipalID)
	grant.PrincipalType = strings.ToLower(strings.TrimSpace(grant.PrincipalType))
	if grant.PrincipalType == "" {
		grant.PrincipalType = "user"
	}
	grant.PrincipalName = strings.TrimSpace(grant.PrincipalName)
	grant.Reason = strings.TrimSpace(grant.Reason)
	grant.Roles = NormalizeSourcePermissionRoles(grant.Roles)
	if grant.GrantedBy == nil && actorID != uuid.Nil {
		grant.GrantedBy = &actorID
	}
	if grant.GrantedAt.IsZero() {
		grant.GrantedAt = now
	}
	return grant
}

func NormalizeSourcePermissionGrants(grants []SourcePermissionGrant, sourceID uuid.UUID, actorID uuid.UUID, now time.Time) []SourcePermissionGrant {
	out := make([]SourcePermissionGrant, 0, len(grants))
	seen := map[string]bool{}
	for _, grant := range grants {
		grant = NormalizeSourcePermissionGrant(grant, sourceID, actorID, now)
		if grant.PrincipalID == "" || len(grant.Roles) == 0 {
			continue
		}
		key := grant.PrincipalType + "\x00" + grant.PrincipalID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, grant)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PrincipalType == out[j].PrincipalType {
			return out[i].PrincipalID < out[j].PrincipalID
		}
		return out[i].PrincipalType < out[j].PrincipalType
	})
	return out
}

func SourceGovernanceWarnings(policy SourceVisibilityPolicy) []SourceGovernanceWarning {
	policy = NormalizeSourceVisibilityPolicy(policy)
	warnings := []SourceGovernanceWarning{}
	if SourceRoleListContains(policy.SourceVisibilityRoles, SourceRoleView) && SourceRoleListContains(policy.CredentialVisibilityRoles, SourceRoleView) {
		warnings = append(warnings, SourceGovernanceWarning{Code: "source-view-implies-credential-visibility", Severity: "error", Message: "Source visibility must not imply credential metadata visibility."})
	}
	if SourceRoleListContains(policy.SourceVisibilityRoles, SourceRoleView) && SourceRoleListContains(policy.ExternalSampleVisibilityRoles, SourceRoleView) {
		warnings = append(warnings, SourceGovernanceWarning{Code: "source-view-implies-sample-visibility", Severity: "error", Message: "Source visibility must not imply external sample visibility."})
	}
	if policy.CredentialValuesVisible {
		warnings = append(warnings, SourceGovernanceWarning{Code: "credential-values-visible", Severity: "error", Message: "Credential secret values must remain write-only and cannot be exposed by source permissions."})
	}
	if policy.ExternalSamplesPersisted {
		warnings = append(warnings, SourceGovernanceWarning{Code: "external-samples-persisted", Severity: "warning", Message: "External samples should be redacted or withheld and not persisted by default."})
	}
	if !policy.OutputDatasetPermissionsEnforced {
		warnings = append(warnings, SourceGovernanceWarning{Code: "output-dataset-permissions-not-enforced", Severity: "error", Message: "Output dataset permissions must be checked separately from source permissions."})
	}
	return warnings
}

func NormalizeSourceGovernanceAuditRequest(body RecordSourceGovernanceAuditRequest) RecordSourceGovernanceAuditRequest {
	body.EventType = strings.TrimSpace(body.EventType)
	if body.EventType == "" {
		body.EventType = "source_use"
	}
	body.Action = strings.TrimSpace(body.Action)
	body.Result = strings.TrimSpace(body.Result)
	if body.Result == "" {
		body.Result = "succeeded"
	}
	body.PrincipalID = strings.TrimSpace(body.PrincipalID)
	body.PrincipalType = strings.TrimSpace(body.PrincipalType)
	body.Roles = NormalizeSourcePermissionRoles(body.Roles)
	body.Capability = strings.TrimSpace(body.Capability)
	body.JobRID = strings.TrimSpace(body.JobRID)
	body.DownstreamResourceRID = strings.TrimSpace(body.DownstreamResourceRID)
	body.Message = strings.TrimSpace(body.Message)
	if body.Metadata == nil {
		body.Metadata = map[string]any{}
	}
	return body
}

// SyncJob mirrors the current Rust data-connection sync definition surface
// backed by batch_sync_defs after sync runtime state moved out of this service.
type SyncJob struct {
	ID                     uuid.UUID        `json:"id"`
	SourceID               uuid.UUID        `json:"source_id"`
	CapabilityType         string           `json:"capability_type,omitempty"`
	OutputKind             string           `json:"output_kind,omitempty"`
	OutputDatasetID        *uuid.UUID       `json:"output_dataset_id,omitempty"`
	OutputStreamID         *string          `json:"output_stream_id,omitempty"`
	OutputMediaSetID       *string          `json:"output_media_set_id,omitempty"`
	SourceSelector         *string          `json:"source_selector,omitempty"`
	SourcePath             *string          `json:"source_path,omitempty"`
	SourceTable            *string          `json:"source_table,omitempty"`
	SourceTopic            *string          `json:"source_topic,omitempty"`
	Schema                 json.RawMessage  `json:"schema,omitempty"`
	WriteMode              *string          `json:"write_mode,omitempty"`
	TransactionMode        *string          `json:"transaction_mode,omitempty"`
	BuildIntegration       *string          `json:"build_integration,omitempty"`
	DatasetTransactionType *string          `json:"dataset_transaction_type,omitempty"`
	FileSync               json.RawMessage  `json:"file_sync,omitempty"`
	TableSync              json.RawMessage  `json:"table_sync,omitempty"`
	CdcSync                *CdcSyncSettings `json:"cdc_sync,omitempty"`
	FileGlob               *string          `json:"file_glob"`
	ScheduleCron           *string          `json:"schedule_cron"`
	CreatedAt              time.Time        `json:"created_at"`
}

type DataExportType string

const (
	DataExportTypeFile      DataExportType = "file"
	DataExportTypeTable     DataExportType = "table"
	DataExportTypeStreaming DataExportType = "streaming"
)

type DataExportMode string

const (
	DataExportModeFileSnapshot           DataExportMode = "snapshot"
	DataExportModeFileIncremental        DataExportMode = "incremental"
	DataExportModeTableMirror            DataExportMode = "mirror"
	DataExportModeTableFullSnapshot      DataExportMode = "full_snapshot"
	DataExportModeTableFullSnapshotTrunc DataExportMode = "full_snapshot_truncate"
	DataExportModeTableIncremental       DataExportMode = "incremental"
	DataExportModeTableIncrementalTrunc  DataExportMode = "incremental_truncate"
	DataExportModeTableIncrementalAppend DataExportMode = "incremental_append_only"
	DataExportModeStreamingContinuous    DataExportMode = "continuous"
)

type DataExportStatus string

const (
	DataExportStatusDraft     DataExportStatus = "draft"
	DataExportStatusScheduled DataExportStatus = "scheduled"
	DataExportStatusRunning   DataExportStatus = "running"
	DataExportStatusSucceeded DataExportStatus = "succeeded"
	DataExportStatusFailed    DataExportStatus = "failed"
	DataExportStatusStopped   DataExportStatus = "stopped"
)

type DataExportHealthState string

const (
	DataExportHealthNotRun  DataExportHealthState = "not_run"
	DataExportHealthHealthy DataExportHealthState = "healthy"
	DataExportHealthWarning DataExportHealthState = "warning"
	DataExportHealthError   DataExportHealthState = "error"
	DataExportHealthRunning DataExportHealthState = "running"
)

type DataExportHealth struct {
	State         DataExportHealthState `json:"state"`
	Message       *string               `json:"message,omitempty"`
	LastCheckedAt *time.Time            `json:"last_checked_at,omitempty"`
}

type DataExportSchedule struct {
	RID               string     `json:"rid"`
	Name              string     `json:"name"`
	BuildSystem       string     `json:"build_system"`
	TriggerKind       string     `json:"trigger_kind"`
	Cron              string     `json:"cron"`
	TimeZone          string     `json:"time_zone"`
	TargetKind        string     `json:"target_kind"`
	TargetRID         string     `json:"target_rid"`
	TargetDisplayName string     `json:"target_display_name"`
	ScheduleURL       string     `json:"schedule_url,omitempty"`
	Active            bool       `json:"active"`
	LastTriggeredAt   *time.Time `json:"last_triggered_at,omitempty"`
}

type DataExportHistoryEntry struct {
	ID                         uuid.UUID      `json:"id"`
	Action                     string         `json:"action"`
	Status                     string         `json:"status"`
	Message                    *string        `json:"message,omitempty"`
	BuildID                    *string        `json:"build_id,omitempty"`
	BuildReportURL             *string        `json:"build_report_url,omitempty"`
	FilesWritten               int64          `json:"files_written,omitempty"`
	FilesSkipped               int64          `json:"files_skipped,omitempty"`
	BytesWritten               int64          `json:"bytes_written,omitempty"`
	RowsWritten                int64          `json:"rows_written,omitempty"`
	TruncatePerformed          bool           `json:"truncate_performed,omitempty"`
	RecordsExported            int64          `json:"records_exported,omitempty"`
	RecordsSkipped             int64          `json:"records_skipped,omitempty"`
	LastExportedOffset         *string        `json:"last_exported_offset,omitempty"`
	ReplayBehavior             string         `json:"replay_behavior,omitempty"`
	ScheduleTriggered          bool           `json:"schedule_triggered,omitempty"`
	RetryAttempts              int64          `json:"retry_attempts,omitempty"`
	ErrorMessage               *string        `json:"error_message,omitempty"`
	HighWatermarkTransactionID *string        `json:"high_watermark_transaction_id,omitempty"`
	FullReexport               bool           `json:"full_reexport,omitempty"`
	Metadata                   map[string]any `json:"metadata,omitempty"`
	StartedAt                  *time.Time     `json:"started_at,omitempty"`
	FinishedAt                 *time.Time     `json:"finished_at,omitempty"`
	CreatedAt                  time.Time      `json:"created_at"`
}

type FileExportSourceFile struct {
	Path          string     `json:"path"`
	SizeBytes     int64      `json:"size_bytes"`
	ModifiedAt    *time.Time `json:"modified_at,omitempty"`
	TransactionID *string    `json:"transaction_id,omitempty"`
	ContentHash   *string    `json:"content_hash,omitempty"`
}

type FileExportSettings struct {
	IncrementalPolicy            string                 `json:"incremental_policy"`
	OverwriteBehavior            string                 `json:"overwrite_behavior"`
	DestinationSubfolder         *string                `json:"destination_subfolder,omitempty"`
	PreserveDirectoryStructure   bool                   `json:"preserve_directory_structure"`
	FullReexportRequested        bool                   `json:"full_reexport_requested"`
	FullReexportStrategy         string                 `json:"full_reexport_strategy"`
	SourceFiles                  []FileExportSourceFile `json:"source_files,omitempty"`
	LastSuccessfulTransactionID  *string                `json:"last_successful_transaction_id,omitempty"`
	LastSuccessfulAt             *time.Time             `json:"last_successful_at,omitempty"`
	DestinationSubfolderGuidance []string               `json:"destination_subfolder_guidance,omitempty"`
}

type FileExportRunPlan struct {
	IncrementalPolicy          string                 `json:"incremental_policy"`
	OverwriteBehavior          string                 `json:"overwrite_behavior"`
	DestinationPath            string                 `json:"destination_path"`
	DestinationSubfolder       *string                `json:"destination_subfolder,omitempty"`
	FilesConsidered            int64                  `json:"files_considered"`
	FilesWritten               int64                  `json:"files_written"`
	FilesSkipped               int64                  `json:"files_skipped"`
	BytesWritten               int64                  `json:"bytes_written"`
	FullReexport               bool                   `json:"full_reexport"`
	LastSuccessfulAt           *time.Time             `json:"last_successful_at,omitempty"`
	LastExportedTransactionID  *string                `json:"last_exported_transaction_id,omitempty"`
	ExportedFiles              []FileExportSourceFile `json:"exported_files,omitempty"`
	SkippedFiles               []FileExportSourceFile `json:"skipped_files,omitempty"`
	DestinationSubfolderAdvice []string               `json:"destination_subfolder_advice,omitempty"`
}

type TableExportColumn struct {
	Name         string `json:"name"`
	FoundryType  string `json:"foundry_type"`
	ExternalType string `json:"external_type"`
	Nullable     bool   `json:"nullable"`
}

type TableExportValidationIssue struct {
	Code     string  `json:"code"`
	Severity string  `json:"severity"`
	Message  string  `json:"message"`
	Column   *string `json:"column,omitempty"`
}

type TableExportSettings struct {
	DatasetSchema               []TableExportColumn          `json:"dataset_schema,omitempty"`
	DestinationSchema           []TableExportColumn          `json:"destination_schema,omitempty"`
	InputParquetBacked          bool                         `json:"input_parquet_backed"`
	DestinationTableExists      bool                         `json:"destination_table_exists"`
	TruncatePermission          bool                         `json:"truncate_permission"`
	ExactColumnMatch            bool                         `json:"exact_column_match"`
	RowCountEstimate            *int64                       `json:"row_count_estimate,omitempty"`
	LastSuccessfulTransactionID *string                      `json:"last_successful_transaction_id,omitempty"`
	LastSuccessfulAt            *time.Time                   `json:"last_successful_at,omitempty"`
	ValidationIssues            []TableExportValidationIssue `json:"validation_issues,omitempty"`
}

type TableExportRunPlan struct {
	ExportMode             DataExportMode               `json:"export_mode"`
	ResolutionStrategy     string                       `json:"resolution_strategy"`
	RowsWritten            int64                        `json:"rows_written"`
	TruncateRequired       bool                         `json:"truncate_required"`
	TruncatePerformed      bool                         `json:"truncate_performed"`
	InputParquetBacked     bool                         `json:"input_parquet_backed"`
	DestinationTableExists bool                         `json:"destination_table_exists"`
	ExactColumnMatch       bool                         `json:"exact_column_match"`
	LastSuccessfulAt       *time.Time                   `json:"last_successful_at,omitempty"`
	ValidationIssues       []TableExportValidationIssue `json:"validation_issues,omitempty"`
}

type StreamingExportWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type StreamingExportSettings struct {
	ReplayBehavior            string                   `json:"replay_behavior"`
	StartOffset               string                   `json:"start_offset"`
	StartOffsetValue          *string                  `json:"start_offset_value,omitempty"`
	LastExportedOffset        *string                  `json:"last_exported_offset,omitempty"`
	LastCheckpointID          *string                  `json:"last_checkpoint_id,omitempty"`
	ScheduleRestartEnabled    bool                     `json:"schedule_restart_enabled"`
	RestartFromPreviousOffset bool                     `json:"restart_from_previous_offset"`
	RecordsExportedEstimate   *int64                   `json:"records_exported_estimate,omitempty"`
	ReplayedRecordsDetected   bool                     `json:"replayed_records_detected"`
	LastStartedAt             *time.Time               `json:"last_started_at,omitempty"`
	LastStoppedAt             *time.Time               `json:"last_stopped_at,omitempty"`
	Warnings                  []StreamingExportWarning `json:"warnings,omitempty"`
}

type StreamingExportStartPlan struct {
	ReplayBehavior            string                   `json:"replay_behavior"`
	StartOffset               string                   `json:"start_offset"`
	EffectiveStartOffset      *string                  `json:"effective_start_offset,omitempty"`
	RestartFromPreviousOffset bool                     `json:"restart_from_previous_offset"`
	ScheduleRestartEnabled    bool                     `json:"schedule_restart_enabled"`
	ScheduleTriggered         bool                     `json:"schedule_triggered"`
	RecordsToExport           int64                    `json:"records_to_export"`
	DuplicateRisk             bool                     `json:"duplicate_risk"`
	DropRisk                  bool                     `json:"drop_risk"`
	Warnings                  []StreamingExportWarning `json:"warnings,omitempty"`
}

type DataExport struct {
	ID               uuid.UUID                `json:"id"`
	SourceID         uuid.UUID                `json:"source_id"`
	Name             string                   `json:"name"`
	ExportType       DataExportType           `json:"export_type"`
	ExportMode       DataExportMode           `json:"export_mode"`
	InputDatasetID   *uuid.UUID               `json:"input_dataset_id,omitempty"`
	InputDatasetRID  *string                  `json:"input_dataset_rid,omitempty"`
	InputStreamID    *string                  `json:"input_stream_id,omitempty"`
	DestinationPath  *string                  `json:"destination_path,omitempty"`
	DestinationTable *string                  `json:"destination_table,omitempty"`
	DestinationTopic *string                  `json:"destination_topic,omitempty"`
	ScheduleCron     *string                  `json:"schedule_cron,omitempty"`
	StartBehavior    string                   `json:"start_behavior"`
	StopBehavior     string                   `json:"stop_behavior"`
	ExportControls   ExportControls           `json:"export_controls"`
	Config           json.RawMessage          `json:"config"`
	FileExport       *FileExportSettings      `json:"file_export,omitempty"`
	TableExport      *TableExportSettings     `json:"table_export,omitempty"`
	StreamingExport  *StreamingExportSettings `json:"streaming_export,omitempty"`
	Schedule         *DataExportSchedule      `json:"schedule,omitempty"`
	Status           DataExportStatus         `json:"status"`
	Health           DataExportHealth         `json:"health"`
	History          []DataExportHistoryEntry `json:"history"`
	LastRunAt        *time.Time               `json:"last_run_at,omitempty"`
	CreatedBy        *uuid.UUID               `json:"created_by,omitempty"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
}

type CreateDataExportRequest struct {
	SourceID         uuid.UUID                `json:"source_id"`
	Name             string                   `json:"name"`
	ExportType       DataExportType           `json:"export_type"`
	ExportMode       DataExportMode           `json:"export_mode,omitempty"`
	InputDatasetID   *uuid.UUID               `json:"input_dataset_id,omitempty"`
	InputDatasetRID  *string                  `json:"input_dataset_rid,omitempty"`
	InputStreamID    *string                  `json:"input_stream_id,omitempty"`
	DestinationPath  *string                  `json:"destination_path,omitempty"`
	DestinationTable *string                  `json:"destination_table,omitempty"`
	DestinationTopic *string                  `json:"destination_topic,omitempty"`
	ScheduleCron     *string                  `json:"schedule_cron,omitempty"`
	StartBehavior    string                   `json:"start_behavior,omitempty"`
	StopBehavior     string                   `json:"stop_behavior,omitempty"`
	ExportControls   ExportControls           `json:"export_controls,omitempty"`
	Config           json.RawMessage          `json:"config,omitempty"`
	FileExport       *FileExportSettings      `json:"file_export,omitempty"`
	TableExport      *TableExportSettings     `json:"table_export,omitempty"`
	StreamingExport  *StreamingExportSettings `json:"streaming_export,omitempty"`
}

type UpdateDataExportRequest struct {
	Name             *string                  `json:"name,omitempty"`
	ExportMode       *DataExportMode          `json:"export_mode,omitempty"`
	InputDatasetID   *uuid.UUID               `json:"input_dataset_id,omitempty"`
	InputDatasetRID  *string                  `json:"input_dataset_rid,omitempty"`
	InputStreamID    *string                  `json:"input_stream_id,omitempty"`
	DestinationPath  *string                  `json:"destination_path,omitempty"`
	DestinationTable *string                  `json:"destination_table,omitempty"`
	DestinationTopic *string                  `json:"destination_topic,omitempty"`
	ScheduleCron     *string                  `json:"schedule_cron,omitempty"`
	StartBehavior    *string                  `json:"start_behavior,omitempty"`
	StopBehavior     *string                  `json:"stop_behavior,omitempty"`
	ExportControls   *ExportControls          `json:"export_controls,omitempty"`
	Config           json.RawMessage          `json:"config,omitempty"`
	FileExport       *FileExportSettings      `json:"file_export,omitempty"`
	TableExport      *TableExportSettings     `json:"table_export,omitempty"`
	StreamingExport  *StreamingExportSettings `json:"streaming_export,omitempty"`
}

var fileExportConnectors = map[string]bool{
	"s3": true, "amazon_s3": true, "azure_blob": true, "adls": true, "abfs": true,
	"onelake": true, "gcs": true, "google_cloud_storage": true, "hdfs": true,
	"sftp": true, "sharepoint": true, "sharepoint_online": true, "agent_filesystem": true,
	"parquet": true, "csv": true, "json": true, "excel": true,
}

var tableExportConnectors = map[string]bool{
	"postgresql": true, "postgres": true, "mssql": true, "sqlserver": true,
	"microsoft_sql_server": true, "oracle": true, "oracle_database": true,
	"db2": true, "ibm_db2": true, "jdbc": true, "odbc": true, "redshift": true,
	"aws_redshift": true, "snowflake": true, "bigquery": true, "databricks": true,
	"salesforce": true,
}

var streamingExportConnectors = map[string]bool{
	"kafka": true, "streaming_kafka": true, "kinesis": true, "streaming_kinesis": true,
	"pubsub": true, "streaming_pubsub": true, "google_pubsub": true, "streaming_sqs": true,
	"sqs": true, "solace": true, "postgresql": true, "postgres": true, "iot": true,
	"streaming_external": true,
}

func SupportedDataExportTypes(connectorType string) []DataExportType {
	normalized := strings.ToLower(strings.TrimSpace(connectorType))
	out := []DataExportType{}
	if fileExportConnectors[normalized] {
		out = append(out, DataExportTypeFile)
	}
	if tableExportConnectors[normalized] {
		out = append(out, DataExportTypeTable)
	}
	if streamingExportConnectors[normalized] {
		out = append(out, DataExportTypeStreaming)
	}
	return out
}

func ConnectorSupportsDataExport(connectorType string, exportType DataExportType) bool {
	for _, supported := range SupportedDataExportTypes(connectorType) {
		if supported == exportType {
			return true
		}
	}
	return false
}

func DataExportCapability(exportType DataExportType) string {
	switch exportType {
	case DataExportTypeFile:
		return "file_export"
	case DataExportTypeTable:
		return "table_export"
	case DataExportTypeStreaming:
		return "streaming_export"
	default:
		return ""
	}
}

func DefaultDataExportMode(exportType DataExportType) DataExportMode {
	switch exportType {
	case DataExportTypeFile:
		return DataExportModeFileIncremental
	case DataExportTypeTable:
		return DataExportModeTableMirror
	case DataExportTypeStreaming:
		return DataExportModeStreamingContinuous
	default:
		return ""
	}
}

func DefaultDataExportHealth() DataExportHealth {
	msg := "Export has not run yet"
	return DataExportHealth{State: DataExportHealthNotRun, Message: &msg}
}

func DataExportScheduleFor(exportID uuid.UUID, name string, exportType DataExportType, scheduleCron *string, lastTriggeredAt *time.Time) *DataExportSchedule {
	cron := trimPtr(scheduleCron)
	if cron == "" || (exportType != DataExportTypeFile && exportType != DataExportTypeTable) || exportID == uuid.Nil {
		return nil
	}
	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = DataExportScheduleTargetKind(exportType)
	}
	rid := "ri.foundry.main.schedule." + exportID.String()
	return &DataExportSchedule{
		RID:               rid,
		Name:              displayName + " export schedule",
		BuildSystem:       "data-integration-build-schedules",
		TriggerKind:       "time",
		Cron:              cron,
		TimeZone:          "UTC",
		TargetKind:        DataExportScheduleTargetKind(exportType),
		TargetRID:         "ri.foundry.main.export." + exportID.String(),
		TargetDisplayName: displayName,
		ScheduleURL:       "/schedules/" + rid,
		Active:            true,
		LastTriggeredAt:   lastTriggeredAt,
	}
}

func DataExportScheduleTargetKind(exportType DataExportType) string {
	switch exportType {
	case DataExportTypeFile:
		return "file_export"
	case DataExportTypeTable:
		return "table_export"
	default:
		return "data_export"
	}
}

func NewDataExportBuildID() string {
	return "ri.foundry.main.build." + uuid.New().String()
}

func DataExportBuildReportURL(buildID string) string {
	buildID = strings.TrimSpace(buildID)
	if buildID == "" {
		return ""
	}
	return "/builds/" + buildID
}

func DataExportRetryAttempts(config json.RawMessage) int64 {
	if len(config) == 0 || !json.Valid(config) {
		return 0
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(config, &fields); err != nil {
		return 0
	}
	for _, key := range []string{"retry_attempts", "retry_count"} {
		if value, ok := fields[key]; ok {
			if retries := parseDataExportRetryAttempts(value); retries > 0 {
				return retries
			}
		}
	}
	return 0
}

func parseDataExportRetryAttempts(value json.RawMessage) int64 {
	var integer int64
	if err := json.Unmarshal(value, &integer); err == nil {
		if integer > 0 {
			return integer
		}
		return 0
	}
	var decimal float64
	if err := json.Unmarshal(value, &decimal); err == nil {
		if decimal > 0 {
			return int64(decimal)
		}
		return 0
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		parsed, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func DefaultFileExportSettings(destinationPath string, mode DataExportMode) FileExportSettings {
	settings := FileExportSettings{}
	NormalizeFileExportSettings(&settings, destinationPath, mode)
	return settings
}

func NormalizeFileExportSettings(settings *FileExportSettings, destinationPath string, mode DataExportMode) {
	if settings == nil {
		return
	}
	settings.IncrementalPolicy = strings.TrimSpace(settings.IncrementalPolicy)
	if settings.IncrementalPolicy == "" {
		if mode == DataExportModeFileSnapshot {
			settings.IncrementalPolicy = "full_snapshot"
		} else {
			settings.IncrementalPolicy = "modified_since_last_success"
		}
	}
	settings.OverwriteBehavior = strings.TrimSpace(settings.OverwriteBehavior)
	if settings.OverwriteBehavior == "" {
		settings.OverwriteBehavior = "overwrite_existing"
	}
	settings.FullReexportStrategy = strings.TrimSpace(settings.FullReexportStrategy)
	if settings.FullReexportStrategy == "" {
		settings.FullReexportStrategy = "create_new_export_or_overwrite_upstream"
	}
	if settings.DestinationSubfolder != nil {
		clean := strings.Trim(strings.TrimSpace(*settings.DestinationSubfolder), "/")
		if clean == "" {
			settings.DestinationSubfolder = nil
		} else {
			settings.DestinationSubfolder = &clean
		}
	}
	for i := range settings.SourceFiles {
		settings.SourceFiles[i].Path = strings.TrimSpace(settings.SourceFiles[i].Path)
		if settings.SourceFiles[i].TransactionID != nil {
			clean := strings.TrimSpace(*settings.SourceFiles[i].TransactionID)
			if clean == "" {
				settings.SourceFiles[i].TransactionID = nil
			} else {
				settings.SourceFiles[i].TransactionID = &clean
			}
		}
	}
	settings.DestinationSubfolderGuidance = FileExportDestinationGuidance(destinationPath, settings)
}

func FileExportDestinationGuidance(destinationPath string, settings *FileExportSettings) []string {
	guidance := []string{
		"File exports copy raw dataset files and default to files modified since the last successful export transaction.",
	}
	if settings != nil && settings.FullReexportRequested {
		guidance = append(guidance, "For a full re-export, create a new export or overwrite all files upstream; this run is marked to include the whole file manifest once.")
	}
	if settings != nil && settings.OverwriteBehavior == "overwrite_existing" && !fileExportHasDedicatedSubfolder(destinationPath, settings.DestinationSubfolder) {
		guidance = append(guidance, "Use a dedicated destination subfolder to avoid overwriting files owned by other systems.")
	}
	return guidance
}

func fileExportHasDedicatedSubfolder(destinationPath string, subfolder *string) bool {
	if subfolder != nil && strings.TrimSpace(*subfolder) != "" {
		return true
	}
	clean := strings.TrimSpace(destinationPath)
	if clean == "" {
		return false
	}
	if idx := strings.Index(clean, "://"); idx >= 0 {
		clean = clean[idx+3:]
		if slash := strings.Index(clean, "/"); slash >= 0 {
			clean = clean[slash+1:]
		} else {
			clean = ""
		}
	}
	clean = strings.Trim(clean, "/")
	if clean == "" {
		return false
	}
	return strings.Contains(clean, "/")
}

func ValidateFileExportSettings(settings *FileExportSettings) []string {
	if settings == nil {
		return nil
	}
	errs := []string{}
	switch settings.IncrementalPolicy {
	case "", "modified_since_last_success", "full_snapshot":
	default:
		errs = append(errs, "file_export.incremental_policy must be modified_since_last_success or full_snapshot")
	}
	switch settings.OverwriteBehavior {
	case "", "overwrite_existing", "fail_if_exists", "skip_existing", "connector_default":
	default:
		errs = append(errs, "file_export.overwrite_behavior must be overwrite_existing, fail_if_exists, skip_existing, or connector_default")
	}
	switch settings.FullReexportStrategy {
	case "", "create_new_export_or_overwrite_upstream", "include_all_files_once":
	default:
		errs = append(errs, "file_export.full_reexport_strategy must be create_new_export_or_overwrite_upstream or include_all_files_once")
	}
	if settings.DestinationSubfolder != nil {
		clean := filepath.Clean(strings.TrimSpace(*settings.DestinationSubfolder))
		if strings.HasPrefix(clean, "..") || strings.Contains(clean, "/../") {
			errs = append(errs, "file_export.destination_subfolder cannot traverse parent directories")
		}
	}
	seen := map[string]bool{}
	for _, file := range settings.SourceFiles {
		if strings.TrimSpace(file.Path) == "" {
			errs = append(errs, "file_export.source_files cannot contain blank paths")
		}
		if file.SizeBytes < 0 {
			errs = append(errs, "file_export.source_files size_bytes cannot be negative")
		}
		key := strings.ToLower(strings.TrimSpace(file.Path))
		if key != "" && seen[key] {
			errs = append(errs, "file_export.source_files cannot contain duplicate paths")
		}
		seen[key] = true
	}
	return errs
}

func BuildFileExportRunPlan(settings FileExportSettings, destinationPath string, now time.Time) FileExportRunPlan {
	NormalizeFileExportSettings(&settings, destinationPath, DataExportModeFileIncremental)
	fullReexport := settings.FullReexportRequested || settings.IncrementalPolicy == "full_snapshot"
	files := append([]FileExportSourceFile(nil), settings.SourceFiles...)
	sortFileExportFiles(files)
	plan := FileExportRunPlan{
		IncrementalPolicy:          settings.IncrementalPolicy,
		OverwriteBehavior:          settings.OverwriteBehavior,
		DestinationPath:            strings.TrimSpace(destinationPath),
		DestinationSubfolder:       settings.DestinationSubfolder,
		FilesConsidered:            int64(len(files)),
		FullReexport:               fullReexport,
		LastSuccessfulAt:           settings.LastSuccessfulAt,
		DestinationSubfolderAdvice: FileExportDestinationGuidance(destinationPath, &settings),
	}
	if len(files) == 0 {
		plan.DestinationSubfolderAdvice = append(plan.DestinationSubfolderAdvice, "No file manifest was supplied; the export runtime will enumerate dataset files during execution.")
		return plan
	}
	for _, file := range files {
		shouldWrite := fullReexport || settings.LastSuccessfulAt == nil || file.ModifiedAt == nil || file.ModifiedAt.After(*settings.LastSuccessfulAt)
		if shouldWrite {
			plan.ExportedFiles = append(plan.ExportedFiles, file)
			plan.FilesWritten++
			plan.BytesWritten += file.SizeBytes
			if file.TransactionID != nil {
				tx := *file.TransactionID
				plan.LastExportedTransactionID = &tx
			}
			continue
		}
		plan.SkippedFiles = append(plan.SkippedFiles, file)
		plan.FilesSkipped++
	}
	if plan.LastExportedTransactionID == nil && settings.LastSuccessfulTransactionID != nil {
		tx := *settings.LastSuccessfulTransactionID
		plan.LastExportedTransactionID = &tx
	}
	_ = now
	return plan
}

func sortFileExportFiles(files []FileExportSourceFile) {
	sort.SliceStable(files, func(i, j int) bool {
		left, right := files[i], files[j]
		if left.ModifiedAt != nil && right.ModifiedAt != nil && !left.ModifiedAt.Equal(*right.ModifiedAt) {
			return left.ModifiedAt.Before(*right.ModifiedAt)
		}
		if left.ModifiedAt == nil && right.ModifiedAt != nil {
			return true
		}
		if left.ModifiedAt != nil && right.ModifiedAt == nil {
			return false
		}
		return left.Path < right.Path
	})
}

func DefaultTableExportSettings(mode DataExportMode) TableExportSettings {
	settings := TableExportSettings{}
	NormalizeTableExportSettings(&settings, mode)
	return settings
}

func NormalizeTableExportSettings(settings *TableExportSettings, mode DataExportMode) {
	if settings == nil {
		return
	}
	for i := range settings.DatasetSchema {
		normalizeTableExportColumn(&settings.DatasetSchema[i])
	}
	for i := range settings.DestinationSchema {
		normalizeTableExportColumn(&settings.DestinationSchema[i])
	}
	if settings.RowCountEstimate != nil && *settings.RowCountEstimate < 0 {
		zero := int64(0)
		settings.RowCountEstimate = &zero
	}
	if settings.LastSuccessfulTransactionID != nil {
		clean := strings.TrimSpace(*settings.LastSuccessfulTransactionID)
		if clean == "" {
			settings.LastSuccessfulTransactionID = nil
		} else {
			settings.LastSuccessfulTransactionID = &clean
		}
	}
	settings.ExactColumnMatch = tableExportColumnsExactlyMatch(settings.DatasetSchema, settings.DestinationSchema)
	settings.ValidationIssues = TableExportValidationIssues(*settings, mode)
}

func normalizeTableExportColumn(col *TableExportColumn) {
	col.Name = strings.TrimSpace(col.Name)
	col.FoundryType = strings.TrimSpace(col.FoundryType)
	col.ExternalType = strings.TrimSpace(col.ExternalType)
}

func ValidateTableExportSettings(settings *TableExportSettings, mode DataExportMode) []string {
	if settings == nil {
		return []string{"table_export settings required for table exports"}
	}
	normalized := *settings
	NormalizeTableExportSettings(&normalized, mode)
	errs := []string{}
	for _, issue := range normalized.ValidationIssues {
		if issue.Severity == "error" {
			errs = append(errs, issue.Message)
		}
	}
	return errs
}

func TableExportValidationIssues(settings TableExportSettings, mode DataExportMode) []TableExportValidationIssue {
	issues := []TableExportValidationIssue{}
	if !settings.InputParquetBacked {
		issues = append(issues, tableExportIssue("input_not_parquet", "table_export.input_parquet_backed must be true because table exports require Parquet-backed dataset files", nil))
	}
	if !settings.DestinationTableExists {
		issues = append(issues, tableExportIssue("destination_table_missing", "table_export.destination_table_exists must be true because OpenFoundry does not create external destination tables", nil))
	}
	if TableExportModeRequiresTruncate(mode) && !settings.TruncatePermission {
		issues = append(issues, tableExportIssue("truncate_permission_missing", "table_export.truncate_permission must be true for mirror or truncating table export modes", nil))
	}
	if len(settings.DatasetSchema) == 0 {
		issues = append(issues, tableExportIssue("dataset_schema_missing", "table_export.dataset_schema required for table exports", nil))
	}
	if len(settings.DestinationSchema) == 0 {
		issues = append(issues, tableExportIssue("destination_schema_missing", "table_export.destination_schema required for table exports", nil))
	}
	issues = append(issues, tableExportColumnIssues(settings.DatasetSchema, "dataset_schema")...)
	issues = append(issues, tableExportColumnIssues(settings.DestinationSchema, "destination_schema")...)
	issues = append(issues, tableExportSchemaMatchIssues(settings.DatasetSchema, settings.DestinationSchema)...)
	return issues
}

func tableExportIssue(code, message string, column *string) TableExportValidationIssue {
	return TableExportValidationIssue{Code: code, Severity: "error", Message: message, Column: column}
}

func tableExportColumnIssues(columns []TableExportColumn, field string) []TableExportValidationIssue {
	issues := []TableExportValidationIssue{}
	seen := map[string]bool{}
	for i, col := range columns {
		columnName := col.Name
		if columnName == "" {
			issues = append(issues, tableExportIssue(field+"_blank_column", fmt.Sprintf("table_export.%s[%d].name cannot be blank", field, i), nil))
			continue
		}
		if seen[columnName] {
			name := columnName
			issues = append(issues, tableExportIssue(field+"_duplicate_column", "table_export."+field+" cannot contain duplicate column "+columnName, &name))
		}
		seen[columnName] = true
		if tableExportColumnHasNestedType(col) {
			name := columnName
			issues = append(issues, tableExportIssue(field+"_unsupported_nested_type", "table exports do not support nested ARRAY, MAP, STRUCT, JSON, or object column types for "+columnName, &name))
		}
		if tableExportEffectiveType(col, field == "dataset_schema") == "" {
			name := columnName
			issues = append(issues, tableExportIssue(field+"_missing_type", "table_export."+field+" column "+columnName+" must define a type", &name))
		}
	}
	return issues
}

func tableExportSchemaMatchIssues(dataset, destination []TableExportColumn) []TableExportValidationIssue {
	issues := []TableExportValidationIssue{}
	if len(dataset) == 0 || len(destination) == 0 {
		return issues
	}
	if len(dataset) != len(destination) {
		issues = append(issues, tableExportIssue("schema_column_count_mismatch", "table_export dataset_schema and destination_schema must contain the same number of columns", nil))
	}
	limit := len(dataset)
	if len(destination) < limit {
		limit = len(destination)
	}
	for i := 0; i < limit; i++ {
		left := dataset[i]
		right := destination[i]
		if left.Name != right.Name {
			name := left.Name
			message := fmt.Sprintf("table_export column %d name mismatch: dataset %q must exactly match destination %q", i, left.Name, right.Name)
			if strings.EqualFold(left.Name, right.Name) {
				message += " including case"
			}
			issues = append(issues, tableExportIssue("schema_column_name_mismatch", message, &name))
		}
		if !tableExportTypesCompatible(tableExportEffectiveType(left, true), tableExportEffectiveType(right, false)) {
			name := left.Name
			issues = append(issues, tableExportIssue(
				"schema_column_type_mismatch",
				fmt.Sprintf("table_export column %s type mismatch: dataset %q must be compatible with destination %q", left.Name, tableExportEffectiveType(left, true), tableExportEffectiveType(right, false)),
				&name,
			))
		}
	}
	return issues
}

func tableExportColumnsExactlyMatch(dataset, destination []TableExportColumn) bool {
	if len(dataset) == 0 || len(dataset) != len(destination) {
		return false
	}
	for i := range dataset {
		if dataset[i].Name == "" || dataset[i].Name != destination[i].Name {
			return false
		}
		if !tableExportTypesCompatible(tableExportEffectiveType(dataset[i], true), tableExportEffectiveType(destination[i], false)) {
			return false
		}
	}
	return true
}

func tableExportColumnHasNestedType(col TableExportColumn) bool {
	return tableExportTypeIsNested(col.FoundryType) || tableExportTypeIsNested(col.ExternalType)
}

func tableExportTypeIsNested(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "array") ||
		strings.Contains(normalized, "map") ||
		strings.Contains(normalized, "struct") ||
		strings.Contains(normalized, "list") ||
		strings.Contains(normalized, "record") ||
		strings.Contains(normalized, "object") ||
		strings.Contains(normalized, "json") ||
		strings.Contains(normalized, "variant")
}

func tableExportEffectiveType(col TableExportColumn, dataset bool) string {
	if dataset {
		if strings.TrimSpace(col.FoundryType) != "" {
			return strings.TrimSpace(col.FoundryType)
		}
		return strings.TrimSpace(col.ExternalType)
	}
	if strings.TrimSpace(col.ExternalType) != "" {
		return strings.TrimSpace(col.ExternalType)
	}
	return strings.TrimSpace(col.FoundryType)
}

func tableExportTypesCompatible(datasetType, destinationType string) bool {
	left := tableExportTypeFamily(datasetType)
	right := tableExportTypeFamily(destinationType)
	if left == "" || right == "" {
		return false
	}
	return left == right
}

func tableExportTypeFamily(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || tableExportTypeIsNested(normalized) {
		return ""
	}
	if idx := strings.Index(normalized, "("); idx >= 0 {
		normalized = normalized[:idx]
	}
	normalized = strings.TrimSpace(strings.ReplaceAll(normalized, "_", " "))
	normalized = strings.Join(strings.Fields(normalized), " ")
	switch normalized {
	case "string", "varchar", "varchar2", "char", "character", "text", "nvarchar", "nchar", "ntext":
		return "string"
	case "boolean", "bool", "bit":
		return "boolean"
	case "byte", "tinyint":
		return "tinyint"
	case "short", "smallint", "int2":
		return "smallint"
	case "integer", "int", "int4":
		return "integer"
	case "long", "bigint", "int8":
		return "bigint"
	case "float", "float32", "real":
		return "float"
	case "double", "float64", "double precision":
		return "double"
	case "decimal", "numeric", "number":
		return "decimal"
	case "date":
		return "date"
	case "timestamp", "timestamp without time zone", "timestamp with time zone", "timestamptz", "datetime":
		return "timestamp"
	case "binary", "bytes", "bytea", "varbinary":
		return "binary"
	default:
		return normalized
	}
}

func TableExportModeRequiresTruncate(mode DataExportMode) bool {
	return mode == DataExportModeTableMirror ||
		mode == DataExportModeTableFullSnapshotTrunc ||
		mode == DataExportModeTableIncrementalTrunc
}

func BuildTableExportRunPlan(settings TableExportSettings, mode DataExportMode, now time.Time) TableExportRunPlan {
	NormalizeTableExportSettings(&settings, mode)
	_ = now
	rows := int64(0)
	if settings.RowCountEstimate != nil && *settings.RowCountEstimate > 0 {
		rows = *settings.RowCountEstimate
	}
	truncateRequired := TableExportModeRequiresTruncate(mode)
	return TableExportRunPlan{
		ExportMode:             mode,
		ResolutionStrategy:     tableExportResolutionStrategy(mode),
		RowsWritten:            rows,
		TruncateRequired:       truncateRequired,
		TruncatePerformed:      truncateRequired,
		InputParquetBacked:     settings.InputParquetBacked,
		DestinationTableExists: settings.DestinationTableExists,
		ExactColumnMatch:       settings.ExactColumnMatch,
		LastSuccessfulAt:       settings.LastSuccessfulAt,
		ValidationIssues:       settings.ValidationIssues,
	}
}

func tableExportResolutionStrategy(mode DataExportMode) string {
	switch mode {
	case DataExportModeTableMirror:
		return "efficient_mirror"
	case DataExportModeTableFullSnapshot:
		return "full_dataset_without_truncation"
	case DataExportModeTableFullSnapshotTrunc:
		return "full_dataset_with_truncation"
	case DataExportModeTableIncremental:
		return "incremental"
	case DataExportModeTableIncrementalTrunc:
		return "incremental_with_truncation"
	case DataExportModeTableIncrementalAppend:
		return "incremental_append_only"
	default:
		return "table_export"
	}
}

func DefaultStreamingExportSettings(scheduleConfigured bool) StreamingExportSettings {
	settings := StreamingExportSettings{}
	if scheduleConfigured {
		settings.ScheduleRestartEnabled = true
	}
	NormalizeStreamingExportSettings(&settings, scheduleConfigured)
	return settings
}

func NormalizeStreamingExportSettings(settings *StreamingExportSettings, scheduleConfigured bool) {
	if settings == nil {
		return
	}
	settings.ReplayBehavior = strings.TrimSpace(settings.ReplayBehavior)
	if settings.ReplayBehavior == "" {
		settings.ReplayBehavior = "export_replayed_records"
	}
	settings.StartOffset = strings.TrimSpace(settings.StartOffset)
	if settings.StartOffset == "" {
		settings.StartOffset = "previous_export_offset"
	}
	settings.RestartFromPreviousOffset = settings.StartOffset == "previous_export_offset"
	if scheduleConfigured && !settings.ScheduleRestartEnabled {
		settings.ScheduleRestartEnabled = true
	}
	settings.StartOffsetValue = cleanOptionalString(settings.StartOffsetValue)
	settings.LastExportedOffset = cleanOptionalString(settings.LastExportedOffset)
	settings.LastCheckpointID = cleanOptionalString(settings.LastCheckpointID)
	if settings.RecordsExportedEstimate != nil && *settings.RecordsExportedEstimate < 0 {
		zero := int64(0)
		settings.RecordsExportedEstimate = &zero
	}
	settings.Warnings = StreamingExportWarnings(*settings)
}

func ValidateStreamingExportSettings(settings *StreamingExportSettings) []string {
	if settings == nil {
		return []string{"streaming_export settings required for streaming exports"}
	}
	normalized := *settings
	NormalizeStreamingExportSettings(&normalized, false)
	errs := []string{}
	switch normalized.ReplayBehavior {
	case "export_replayed_records", "skip_replayed_records":
	default:
		errs = append(errs, "streaming_export.replay_behavior must be export_replayed_records or skip_replayed_records")
	}
	switch normalized.StartOffset {
	case "previous_export_offset", "latest", "earliest", "explicit":
	default:
		errs = append(errs, "streaming_export.start_offset must be previous_export_offset, latest, earliest, or explicit")
	}
	if normalized.StartOffset == "explicit" && trimPtr(normalized.StartOffsetValue) == "" {
		errs = append(errs, "streaming_export.start_offset_value required when start_offset is explicit")
	}
	return errs
}

func StreamingExportWarnings(settings StreamingExportSettings) []StreamingExportWarning {
	warnings := []StreamingExportWarning{}
	switch settings.ReplayBehavior {
	case "export_replayed_records":
		warnings = append(warnings, StreamingExportWarning{
			Code:     "replay_duplicate_risk",
			Severity: "warning",
			Message:  "Exporting replayed stream records can duplicate records in the external destination; configure downstream consumers to tolerate duplicates.",
		})
	case "skip_replayed_records":
		warnings = append(warnings, StreamingExportWarning{
			Code:     "replay_drop_risk",
			Severity: "warning",
			Message:  "Skipping replayed stream records can drop records because offsets are not guaranteed to match across replayed streams.",
		})
	}
	return warnings
}

func BuildStreamingExportStartPlan(settings StreamingExportSettings, scheduleTriggered bool, now time.Time) StreamingExportStartPlan {
	NormalizeStreamingExportSettings(&settings, scheduleTriggered)
	_ = now
	records := int64(0)
	if settings.RecordsExportedEstimate != nil && *settings.RecordsExportedEstimate > 0 {
		records = *settings.RecordsExportedEstimate
	}
	effectiveOffset := streamingExportEffectiveStartOffset(settings)
	return StreamingExportStartPlan{
		ReplayBehavior:            settings.ReplayBehavior,
		StartOffset:               settings.StartOffset,
		EffectiveStartOffset:      effectiveOffset,
		RestartFromPreviousOffset: settings.RestartFromPreviousOffset,
		ScheduleRestartEnabled:    settings.ScheduleRestartEnabled,
		ScheduleTriggered:         scheduleTriggered,
		RecordsToExport:           records,
		DuplicateRisk:             settings.ReplayBehavior == "export_replayed_records",
		DropRisk:                  settings.ReplayBehavior == "skip_replayed_records",
		Warnings:                  settings.Warnings,
	}
}

func AdvanceStreamingExportOffset(settings StreamingExportSettings) *string {
	if settings.RecordsExportedEstimate == nil || *settings.RecordsExportedEstimate <= 0 {
		return settings.LastExportedOffset
	}
	if settings.LastExportedOffset == nil {
		next := fmt.Sprintf("%d", *settings.RecordsExportedEstimate)
		return &next
	}
	current := strings.TrimSpace(*settings.LastExportedOffset)
	if current == "" {
		next := fmt.Sprintf("%d", *settings.RecordsExportedEstimate)
		return &next
	}
	if parsed, err := strconv.ParseInt(current, 10, 64); err == nil {
		next := fmt.Sprintf("%d", parsed+*settings.RecordsExportedEstimate)
		return &next
	}
	next := current
	return &next
}

func streamingExportEffectiveStartOffset(settings StreamingExportSettings) *string {
	switch settings.StartOffset {
	case "previous_export_offset":
		if settings.LastExportedOffset != nil {
			offset := *settings.LastExportedOffset
			return &offset
		}
		offset := "latest"
		return &offset
	case "explicit":
		if settings.StartOffsetValue != nil {
			offset := *settings.StartOffsetValue
			return &offset
		}
	case "earliest", "latest":
		offset := settings.StartOffset
		return &offset
	}
	return nil
}

func cleanOptionalString(ptr *string) *string {
	if ptr == nil {
		return nil
	}
	clean := strings.TrimSpace(*ptr)
	if clean == "" {
		return nil
	}
	return &clean
}

func NormalizeCreateDataExportRequest(body *CreateDataExportRequest) {
	body.Name = strings.TrimSpace(body.Name)
	body.StartBehavior = strings.TrimSpace(body.StartBehavior)
	body.StopBehavior = strings.TrimSpace(body.StopBehavior)
	if body.StartBehavior == "" {
		body.StartBehavior = "manual"
	}
	if body.ExportType != DataExportTypeStreaming && trimPtr(body.ScheduleCron) != "" && body.StartBehavior == "manual" {
		body.StartBehavior = "scheduled"
	}
	if body.StopBehavior == "" {
		if body.ExportType == DataExportTypeStreaming {
			body.StopBehavior = "manual"
		} else {
			body.StopBehavior = "after_run"
		}
	}
	if body.ExportMode == "" {
		body.ExportMode = DefaultDataExportMode(body.ExportType)
	}
	if len(body.Config) == 0 || string(body.Config) == "null" {
		body.Config = []byte(`{}`)
	}
	if body.ExportType == DataExportTypeFile {
		if body.FileExport == nil {
			settings := DefaultFileExportSettings(trimPtr(body.DestinationPath), body.ExportMode)
			body.FileExport = &settings
		}
		NormalizeFileExportSettings(body.FileExport, trimPtr(body.DestinationPath), body.ExportMode)
	}
	if body.ExportType == DataExportTypeTable {
		if body.TableExport == nil {
			settings := DefaultTableExportSettings(body.ExportMode)
			body.TableExport = &settings
		}
		NormalizeTableExportSettings(body.TableExport, body.ExportMode)
	}
	if body.ExportType == DataExportTypeStreaming {
		if body.StreamingExport == nil {
			settings := DefaultStreamingExportSettings(body.ScheduleCron != nil)
			body.StreamingExport = &settings
		}
		NormalizeStreamingExportSettings(body.StreamingExport, body.ScheduleCron != nil)
	}
}

func (r CreateDataExportRequest) ValidateForConnector(connectorType string) []string {
	errs := []string{}
	if r.SourceID == uuid.Nil {
		errs = append(errs, "source_id required")
	}
	switch r.ExportType {
	case DataExportTypeFile, DataExportTypeTable, DataExportTypeStreaming:
	default:
		errs = append(errs, "export_type must be file, table, or streaming")
	}
	if r.ExportType != "" && !ConnectorSupportsDataExport(connectorType, r.ExportType) {
		errs = append(errs, "connector does not support "+DataExportCapability(r.ExportType))
	}
	if r.ExportMode == "" {
		errs = append(errs, "export_mode required")
	} else if !ValidDataExportMode(r.ExportType, r.ExportMode) {
		errs = append(errs, "export_mode is not supported for "+string(r.ExportType)+" exports")
	}
	if r.ExportType == DataExportTypeStreaming {
		if trimPtr(r.InputStreamID) == "" {
			errs = append(errs, "input_stream_id required for streaming exports")
		}
		if trimPtr(r.DestinationTopic) == "" {
			errs = append(errs, "destination_topic required for streaming exports")
		}
		errs = append(errs, ValidateStreamingExportSettings(r.StreamingExport)...)
	} else {
		if (r.InputDatasetID == nil || *r.InputDatasetID == uuid.Nil) && trimPtr(r.InputDatasetRID) == "" {
			errs = append(errs, "input_dataset_id or input_dataset_rid required for file and table exports")
		}
		if r.ExportType == DataExportTypeFile && trimPtr(r.DestinationPath) == "" {
			errs = append(errs, "destination_path required for file exports")
		}
		if r.ExportType == DataExportTypeFile {
			errs = append(errs, ValidateFileExportSettings(r.FileExport)...)
		}
		if r.ExportType == DataExportTypeTable && trimPtr(r.DestinationTable) == "" {
			errs = append(errs, "destination_table required for table exports")
		}
		if r.ExportType == DataExportTypeTable {
			errs = append(errs, ValidateTableExportSettings(r.TableExport, r.ExportMode)...)
		}
	}
	switch r.StartBehavior {
	case "", "manual", "scheduled", "start_immediately":
	default:
		errs = append(errs, "start_behavior must be manual, scheduled, or start_immediately")
	}
	switch r.StopBehavior {
	case "", "after_run", "manual", "continuous":
	default:
		errs = append(errs, "stop_behavior must be after_run, manual, or continuous")
	}
	if r.ExportType == DataExportTypeStreaming && r.StopBehavior == "after_run" {
		errs = append(errs, "streaming exports must use manual or continuous stop behavior")
	}
	if r.ExportType != DataExportTypeStreaming && r.StopBehavior == "continuous" {
		errs = append(errs, "file and table exports cannot use continuous stop behavior")
	}
	if r.ScheduleCron != nil && strings.TrimSpace(*r.ScheduleCron) == "" {
		errs = append(errs, "schedule_cron cannot be blank when provided")
	}
	if len(r.Config) > 0 && !json.Valid(r.Config) {
		errs = append(errs, "config must be valid JSON")
	}
	return errs
}

func ValidDataExportMode(exportType DataExportType, mode DataExportMode) bool {
	switch exportType {
	case DataExportTypeFile:
		return mode == DataExportModeFileSnapshot || mode == DataExportModeFileIncremental
	case DataExportTypeTable:
		return mode == DataExportModeTableMirror ||
			mode == DataExportModeTableFullSnapshot ||
			mode == DataExportModeTableFullSnapshotTrunc ||
			mode == DataExportModeTableIncremental ||
			mode == DataExportModeTableIncrementalTrunc ||
			mode == DataExportModeTableIncrementalAppend
	case DataExportTypeStreaming:
		return mode == DataExportModeStreamingContinuous
	default:
		return false
	}
}

type CreateSyncJobRequest struct {
	SourceID               uuid.UUID        `json:"source_id"`
	CapabilityType         *string          `json:"capability_type,omitempty"`
	OutputKind             *string          `json:"output_kind,omitempty"`
	OutputDatasetID        *uuid.UUID       `json:"output_dataset_id,omitempty"`
	OutputStreamID         *string          `json:"output_stream_id,omitempty"`
	OutputMediaSetID       *string          `json:"output_media_set_id,omitempty"`
	SourceSelector         *string          `json:"source_selector,omitempty"`
	SourcePath             *string          `json:"source_path,omitempty"`
	SourceTable            *string          `json:"source_table,omitempty"`
	SourceTopic            *string          `json:"source_topic,omitempty"`
	Schema                 json.RawMessage  `json:"schema,omitempty"`
	WriteMode              *string          `json:"write_mode,omitempty"`
	TransactionMode        *string          `json:"transaction_mode,omitempty"`
	BuildIntegration       *string          `json:"build_integration,omitempty"`
	DatasetTransactionType *string          `json:"dataset_transaction_type,omitempty"`
	FileSync               json.RawMessage  `json:"file_sync,omitempty"`
	TableSync              json.RawMessage  `json:"table_sync,omitempty"`
	CdcSync                *CdcSyncSettings `json:"cdc_sync,omitempty"`
	FileGlob               *string          `json:"file_glob,omitempty"`
	ScheduleCron           *string          `json:"schedule_cron,omitempty"`
}

type UpdateSyncJobRequest struct {
	OutputDatasetID        *uuid.UUID       `json:"output_dataset_id,omitempty"`
	OutputStreamID         *string          `json:"output_stream_id,omitempty"`
	OutputMediaSetID       *string          `json:"output_media_set_id,omitempty"`
	SourceSelector         *string          `json:"source_selector,omitempty"`
	SourcePath             *string          `json:"source_path,omitempty"`
	SourceTable            *string          `json:"source_table,omitempty"`
	SourceTopic            *string          `json:"source_topic,omitempty"`
	Schema                 json.RawMessage  `json:"schema,omitempty"`
	WriteMode              *string          `json:"write_mode,omitempty"`
	TransactionMode        *string          `json:"transaction_mode,omitempty"`
	BuildIntegration       *string          `json:"build_integration,omitempty"`
	DatasetTransactionType *string          `json:"dataset_transaction_type,omitempty"`
	FileSync               json.RawMessage  `json:"file_sync,omitempty"`
	TableSync              json.RawMessage  `json:"table_sync,omitempty"`
	CdcSync                *CdcSyncSettings `json:"cdc_sync,omitempty"`
	FileGlob               *string          `json:"file_glob,omitempty"`
	ScheduleCron           *string          `json:"schedule_cron,omitempty"`
}

type SyncSchemaField struct {
	Name        string `json:"name"`
	SourceType  string `json:"source_type"`
	FoundryType string `json:"foundry_type"`
	Nullable    bool   `json:"nullable"`
}

type CdcConnectorMetadata struct {
	ConnectorType         string         `json:"connector_type"`
	SourceDatabase        *string        `json:"source_database,omitempty"`
	SourceSchema          *string        `json:"source_schema,omitempty"`
	SourceTable           *string        `json:"source_table,omitempty"`
	UpstreamTopic         *string        `json:"upstream_topic,omitempty"`
	OutputStreamID        *string        `json:"output_stream_id,omitempty"`
	DebeziumConnector     *string        `json:"debezium_connector,omitempty"`
	SnapshotMode          *string        `json:"snapshot_mode,omitempty"`
	PublicationName       *string        `json:"publication_name,omitempty"`
	ReplicationSlot       *string        `json:"replication_slot,omitempty"`
	StartPositionMetadata map[string]any `json:"start_position_metadata,omitempty"`
	Properties            map[string]any `json:"properties,omitempty"`
	DerivedAt             *string        `json:"derived_at,omitempty"`
}

type CdcSyncSettings struct {
	InputKind                string               `json:"input_kind"`
	SourceDatabase           *string              `json:"source_database,omitempty"`
	SourceSchema             *string              `json:"source_schema,omitempty"`
	SourceTable              string               `json:"source_table"`
	SourceTopic              *string              `json:"source_topic,omitempty"`
	PrimaryKeyColumns        []string             `json:"primary_key_columns"`
	OrderingColumn           string               `json:"ordering_column"`
	DeletionColumn           *string              `json:"deletion_column,omitempty"`
	OutputStreamID           *string              `json:"output_stream_id,omitempty"`
	OutputStreamLocation     string               `json:"output_stream_location"`
	Schema                   []SyncSchemaField    `json:"schema"`
	StartPosition            string               `json:"start_position"`
	StartPositionValue       any                  `json:"start_position_value,omitempty"`
	SourceDatabaseCDCEnabled bool                 `json:"source_database_cdc_enabled"`
	SourceTableCDCEnabled    bool                 `json:"source_table_cdc_enabled"`
	ChangelogInputValidated  bool                 `json:"changelog_input_validated"`
	ConnectorMetadata        CdcConnectorMetadata `json:"connector_metadata"`
}

var relationalCdcConnectors = map[string]bool{
	"postgresql": true, "postgres": true, "mssql": true, "sqlserver": true,
	"microsoft_sql_server": true, "oracle": true, "oracle_database": true,
	"db2": true, "ibm_db2": true,
}

var streamingChangelogCdcConnectors = map[string]bool{
	"kafka": true, "streaming_kafka": true, "kinesis": true, "streaming_kinesis": true,
	"pubsub": true, "streaming_pubsub": true, "google_pubsub": true, "iot": true,
	"streaming_external": true,
}

func trimPtr(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func stringValue(ptr *string, fallback string) string {
	if ptr == nil || strings.TrimSpace(*ptr) == "" {
		return fallback
	}
	return strings.TrimSpace(*ptr)
}

func CdcInputKindForConnector(connectorType string) string {
	normalized := strings.ToLower(strings.TrimSpace(connectorType))
	if relationalCdcConnectors[normalized] {
		return "relational_connector"
	}
	if streamingChangelogCdcConnectors[normalized] {
		return "streaming_middleware_changelog"
	}
	return ""
}

func ConnectorSupportsCdcSync(connectorType string) bool {
	return CdcInputKindForConnector(connectorType) != ""
}

func ValidateCdcSyncSettings(connectorType string, settings *CdcSyncSettings) []string {
	errs := []string{}
	if settings == nil {
		return []string{"cdc_sync settings are required"}
	}
	expectedKind := CdcInputKindForConnector(connectorType)
	if expectedKind == "" {
		errs = append(errs, "connector does not support CDC sync setup")
	}
	if settings.InputKind == "" {
		settings.InputKind = expectedKind
	}
	if expectedKind != "" && settings.InputKind != expectedKind {
		errs = append(errs, "cdc input kind does not match connector type")
	}
	if settings.InputKind == "relational_connector" {
		if strings.TrimSpace(settings.SourceTable) == "" {
			errs = append(errs, "source_table is required for relational CDC syncs")
		}
		if !settings.SourceDatabaseCDCEnabled {
			errs = append(errs, "source database must expose changelog data before creating a CDC sync")
		}
		if !settings.SourceTableCDCEnabled {
			errs = append(errs, "source table must expose changelog data before creating a CDC sync")
		}
	}
	if settings.InputKind == "streaming_middleware_changelog" {
		if trimPtr(settings.SourceTopic) == "" {
			errs = append(errs, "source_topic is required for changelog-shaped streaming middleware inputs")
		}
		if !settings.ChangelogInputValidated {
			errs = append(errs, "streaming middleware input must be validated as changelog-shaped")
		}
	}
	if len(settings.PrimaryKeyColumns) == 0 {
		errs = append(errs, "primary_key_columns are required")
	}
	seenPK := map[string]bool{}
	for _, column := range settings.PrimaryKeyColumns {
		clean := strings.TrimSpace(column)
		if clean == "" {
			errs = append(errs, "primary_key_columns cannot contain blanks")
			continue
		}
		lower := strings.ToLower(clean)
		if seenPK[lower] {
			errs = append(errs, "primary_key_columns cannot contain duplicates")
		}
		seenPK[lower] = true
	}
	if strings.TrimSpace(settings.OrderingColumn) == "" {
		errs = append(errs, "ordering_column is required")
	}
	if strings.TrimSpace(settings.OutputStreamLocation) == "" && trimPtr(settings.OutputStreamID) == "" {
		errs = append(errs, "output stream is required")
	}
	switch settings.StartPosition {
	case "", "initial_snapshot", "latest":
	case "timestamp", "lsn", "offset":
		if settings.StartPositionValue == nil || strings.TrimSpace(fmt.Sprint(settings.StartPositionValue)) == "" {
			errs = append(errs, "start_position_value is required for timestamp, lsn, and offset starts")
		}
	default:
		errs = append(errs, "start_position must be initial_snapshot, latest, timestamp, lsn, or offset")
	}
	if trimPtr(settings.DeletionColumn) != "" && strings.EqualFold(trimPtr(settings.DeletionColumn), strings.TrimSpace(settings.OrderingColumn)) {
		errs = append(errs, "deletion_column and ordering_column must be distinct")
	}
	schemaFields := map[string]bool{}
	for _, field := range settings.Schema {
		if strings.TrimSpace(field.Name) != "" {
			schemaFields[strings.ToLower(strings.TrimSpace(field.Name))] = true
		}
	}
	if len(schemaFields) > 0 {
		for _, column := range settings.PrimaryKeyColumns {
			if !schemaFields[strings.ToLower(strings.TrimSpace(column))] {
				errs = append(errs, "primary key column is not present in schema: "+column)
			}
		}
		if settings.OrderingColumn != "" && !schemaFields[strings.ToLower(strings.TrimSpace(settings.OrderingColumn))] {
			errs = append(errs, "ordering column is not present in schema: "+settings.OrderingColumn)
		}
		if trimPtr(settings.DeletionColumn) != "" && !schemaFields[strings.ToLower(trimPtr(settings.DeletionColumn))] {
			errs = append(errs, "deletion column is not present in schema: "+trimPtr(settings.DeletionColumn))
		}
	}
	if strings.TrimSpace(settings.ConnectorMetadata.ConnectorType) == "" {
		settings.ConnectorMetadata.ConnectorType = connectorType
	}
	return errs
}

func (r CreateSyncJobRequest) ValidateForConnector(connectorType string) []string {
	errs := []string{}
	capability := stringValue(r.CapabilityType, "batch_sync")
	outputKind := stringValue(r.OutputKind, "dataset")
	if r.SourceID == uuid.Nil {
		errs = append(errs, "source_id required")
	}
	switch outputKind {
	case "dataset":
		if r.OutputDatasetID == nil || *r.OutputDatasetID == uuid.Nil {
			errs = append(errs, "output_dataset_id required for dataset syncs")
		}
	case "stream":
		if trimPtr(r.OutputStreamID) == "" {
			errs = append(errs, "output_stream_id required for stream syncs")
		}
	case "media_set":
		if trimPtr(r.OutputMediaSetID) == "" {
			errs = append(errs, "output_media_set_id required for media set syncs")
		}
	default:
		errs = append(errs, "output_kind must be dataset, stream, or media_set")
	}
	if capability == "cdc_sync" {
		errs = append(errs, ValidateCdcSyncSettings(connectorType, r.CdcSync)...)
	}
	return errs
}

// MediaSetSyncKind identifies the Foundry media-set sync flavour.
type MediaSetSyncKind string

const (
	MediaSetSyncKindCopy    MediaSetSyncKind = "MEDIA_SET_SYNC"
	MediaSetSyncKindVirtual MediaSetSyncKind = "VIRTUAL_MEDIA_SET_SYNC"
)

type MediaSetSyncFilters struct {
	ExcludeAlreadySynced  bool    `json:"exclude_already_synced"`
	PathGlob              *string `json:"path_glob,omitempty"`
	FileSizeLimit         *uint64 `json:"file_size_limit,omitempty"`
	IgnoreUnmatchedSchema bool    `json:"ignore_unmatched_schema"`
}

type MediaSetSync struct {
	ID                uuid.UUID           `json:"id"`
	SourceID          uuid.UUID           `json:"source_id"`
	Kind              MediaSetSyncKind    `json:"kind"`
	TargetMediaSetRID string              `json:"target_media_set_rid"`
	Subfolder         string              `json:"subfolder"`
	Filters           MediaSetSyncFilters `json:"filters"`
	ScheduleCron      *string             `json:"schedule_cron,omitempty"`
	CreatedAt         time.Time           `json:"created_at"`
}

type CreateMediaSetSyncRequest struct {
	Kind              MediaSetSyncKind    `json:"kind"`
	TargetMediaSetRID string              `json:"target_media_set_rid"`
	Subfolder         string              `json:"subfolder,omitempty"`
	Filters           MediaSetSyncFilters `json:"filters,omitempty"`
	ScheduleCron      *string             `json:"schedule_cron,omitempty"`
}

type UpdateMediaSetSyncRequest struct {
	Kind              *MediaSetSyncKind    `json:"kind,omitempty"`
	TargetMediaSetRID *string              `json:"target_media_set_rid,omitempty"`
	Subfolder         *string              `json:"subfolder,omitempty"`
	Filters           *MediaSetSyncFilters `json:"filters,omitempty"`
	ScheduleCron      *string              `json:"schedule_cron,omitempty"`
}

type SourceFile struct {
	Path      string `json:"path"`
	SizeBytes uint64 `json:"size_bytes"`
	MimeType  string `json:"mime_type"`
}

type RunMediaSetSyncRequest struct {
	SourceFiles      []SourceFile `json:"source_files,omitempty"`
	AlreadySynced    []string     `json:"already_synced,omitempty"`
	AllowedMIMETypes []string     `json:"allowed_mime_types,omitempty"`
}

type SyncStats struct {
	Accepted         uint32 `json:"accepted"`
	Skipped          uint32 `json:"skipped"`
	SchemaMismatched uint32 `json:"schema_mismatched"`
}

type MediaSetSyncExecutionReport struct {
	Stats            SyncStats `json:"stats"`
	Dispatched       uint32    `json:"dispatched"`
	DispatchErrors   uint32    `json:"dispatch_errors"`
	SchemaMismatches []string  `json:"schema_mismatches"`
}

func (k MediaSetSyncKind) Valid() bool {
	return k == MediaSetSyncKindCopy || k == MediaSetSyncKindVirtual
}

func ValidateMediaSetSyncConfig(kind MediaSetSyncKind, targetRID string, filters MediaSetSyncFilters, schedule *string) []string {
	errs := []string{}
	if !kind.Valid() {
		errs = append(errs, "kind must be MEDIA_SET_SYNC or VIRTUAL_MEDIA_SET_SYNC")
	}
	if !strings.HasPrefix(strings.TrimSpace(targetRID), "ri.foundry.main.media_set.") {
		errs = append(errs, "target_media_set_rid must start with ri.foundry.main.media_set.")
	}
	if filters.PathGlob != nil {
		if _, err := filepath.Match(*filters.PathGlob, ""); err != nil {
			errs = append(errs, "invalid path_glob: "+err.Error())
		}
	}
	if filters.FileSizeLimit != nil && *filters.FileSizeLimit == 0 {
		errs = append(errs, "file_size_limit must be > 0")
	}
	if schedule != nil {
		fields := strings.Fields(strings.TrimSpace(*schedule))
		if len(fields) != 5 && len(fields) != 6 {
			errs = append(errs, "schedule_cron must have 5 or 6 fields")
		}
	}
	return errs
}

func (m MediaSetSync) Validate() []string {
	return ValidateMediaSetSyncConfig(m.Kind, m.TargetMediaSetRID, m.Filters, m.ScheduleCron)
}

// SDC.41 — Media sync handoff: history, errors, usage, and connector gating.
//
// The execution dispatch itself stays inside the media-sets-service; this
// file only persists the *handoff* metadata so a Data Connection user can
// audit which paths were selected, which media-set-service accepted, and
// which failed. Media schema, conversion, and reference behavior remain
// delegated to the Media Sets checklist.

type MediaSetSyncRunStatus string

const (
	MediaSetSyncRunStatusRunning            MediaSetSyncRunStatus = "running"
	MediaSetSyncRunStatusSucceeded          MediaSetSyncRunStatus = "succeeded"
	MediaSetSyncRunStatusFailed             MediaSetSyncRunStatus = "failed"
	MediaSetSyncRunStatusPartiallySucceeded MediaSetSyncRunStatus = "partially_succeeded"
)

type MediaSetSyncRun struct {
	ID               uuid.UUID             `json:"id"`
	SyncDefID        uuid.UUID             `json:"sync_def_id"`
	Status           MediaSetSyncRunStatus `json:"status"`
	StartedAt        time.Time             `json:"started_at"`
	FinishedAt       *time.Time            `json:"finished_at,omitempty"`
	AcceptedFiles    uint32                `json:"accepted_files"`
	SkippedFiles     uint32                `json:"skipped_files"`
	SchemaMismatched uint32                `json:"schema_mismatched"`
	DispatchedFiles  uint32                `json:"dispatched_files"`
	DispatchErrors   uint32                `json:"dispatch_errors"`
	BytesAccepted    uint64                `json:"bytes_accepted"`
	SelectedPaths    []string              `json:"selected_paths"`
	SchemaMismatches []string              `json:"schema_mismatches"`
	ErrorMessage     *string               `json:"error_message,omitempty"`
	TriggeredBy      *string               `json:"triggered_by,omitempty"`
}

type MediaSetSyncUsageSummary struct {
	SyncDefID           uuid.UUID              `json:"sync_def_id"`
	RunCount            uint32                 `json:"run_count"`
	LastRunAt           *time.Time             `json:"last_run_at,omitempty"`
	LastStatus          *MediaSetSyncRunStatus `json:"last_status,omitempty"`
	LastErrorMessage    *string                `json:"last_error_message,omitempty"`
	TotalAcceptedFiles  uint64                 `json:"total_accepted_files"`
	TotalBytesAccepted  uint64                 `json:"total_bytes_accepted"`
	TotalDispatchErrors uint64                 `json:"total_dispatch_errors"`
	TotalSchemaMismatch uint64                 `json:"total_schema_mismatch"`
}

type MediaSetSyncWithUsage struct {
	MediaSetSync
	Usage *MediaSetSyncUsageSummary `json:"usage,omitempty"`
}

// ClassifyMediaSetSyncRunStatus turns an execution report into a persisted
// run status. The runtime is allowed to surface dispatch_errors on an
// otherwise-completing run; that becomes partially_succeeded rather than
// failed so audit history stays useful.
func ClassifyMediaSetSyncRunStatus(report *MediaSetSyncExecutionReport, runtimeErr error) MediaSetSyncRunStatus {
	if runtimeErr != nil {
		return MediaSetSyncRunStatusFailed
	}
	if report == nil {
		return MediaSetSyncRunStatusFailed
	}
	if report.DispatchErrors > 0 || report.Stats.SchemaMismatched > 0 {
		return MediaSetSyncRunStatusPartiallySucceeded
	}
	return MediaSetSyncRunStatusSucceeded
}

// ComputeMediaSetSyncBytesAccepted sums the byte sizes of the source files
// that classified as accepted. The runtime emits the raw stats; this helper
// turns the request payload into the bytes-accepted counter persisted on the
// run row so the usage summary can roll it up across history.
func ComputeMediaSetSyncBytesAccepted(report *MediaSetSyncExecutionReport, request *RunMediaSetSyncRequest, filters MediaSetSyncFilters) uint64 {
	if report == nil || request == nil {
		return 0
	}
	already := map[string]bool{}
	for _, path := range request.AlreadySynced {
		already[strings.TrimSpace(path)] = true
	}
	mimeAllowed := map[string]bool{}
	for _, mime := range request.AllowedMIMETypes {
		mimeAllowed[strings.ToLower(strings.TrimSpace(mime))] = true
	}
	var total uint64
	for _, file := range request.SourceFiles {
		if filters.ExcludeAlreadySynced && already[strings.TrimSpace(file.Path)] {
			continue
		}
		if filters.FileSizeLimit != nil && file.SizeBytes > *filters.FileSizeLimit {
			continue
		}
		if len(mimeAllowed) > 0 {
			if !mimeAllowed[strings.ToLower(strings.TrimSpace(file.MimeType))] {
				continue
			}
		}
		total += file.SizeBytes
	}
	return total
}

// CollectSelectedPaths returns the unique trimmed paths from a run request,
// preserving insertion order. It is used as the audit trail of which source
// files were submitted to the handoff for a given run.
func CollectSelectedPaths(request *RunMediaSetSyncRequest) []string {
	if request == nil {
		return []string{}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(request.SourceFiles))
	for _, file := range request.SourceFiles {
		p := strings.TrimSpace(file.Path)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// MediaSyncSupportedConnectors lists the connector types where Data Connection
// can hand off raw media files to the Media Sets surface. The frontend mirrors
// this list; the backend uses it to reject media sync setup on unsupported
// sources before they reach the media-sets-service.
var MediaSyncSupportedConnectors = []string{"s3", "onelake", "abfs"}

func ConnectorSupportsMediaSync(connectorType string) bool {
	target := strings.ToLower(strings.TrimSpace(connectorType))
	for _, supported := range MediaSyncSupportedConnectors {
		if supported == target {
			return true
		}
	}
	return false
}

// MediaSetSyncHandoffDelegation captures the items SDC.41 explicitly delegates
// to the Media Sets checklist. Returned by the API so frontends can render
// the delegated responsibilities without hard-coding them.
type MediaSetSyncHandoffDelegation struct {
	Schema            string `json:"schema"`
	Conversion        string `json:"conversion"`
	Transformations   string `json:"transformations"`
	TransactionPolicy string `json:"transaction_policy"`
	MediaReference    string `json:"media_reference"`
}

func DefaultMediaSetSyncHandoffDelegation() MediaSetSyncHandoffDelegation {
	return MediaSetSyncHandoffDelegation{
		Schema:            "Owned by Media Sets; sync filters carry MIME hints only.",
		Conversion:        "Performed by media-sets-service upload pipeline.",
		Transformations:   "Configured on the target media set, not on the sync.",
		TransactionPolicy: "Determined by the target media set definition.",
		MediaReference:    "Resolved via the media-sets-service item registry.",
	}
}

// SDC.42 — Virtual media handoff (blocked).
//
// Records the intended OpenFoundry surface for registering external media
// files as virtual media items without copying bytes into OpenFoundry
// storage. The product status stays "blocked" until the Media Sets checklist
// defines the virtual media item lifecycle (MS.18–MS.20) and the platform
// agrees on an object-storage authorization contract (presigned URLs, SAS
// tokens, or access grants). The descriptor is exposed so users see what's
// blocked, what contracts are required, and what a registration payload
// would look like once unblocked.

type VirtualMediaHandoffMode string

const (
	VirtualMediaHandoffModeMediaSetSync      VirtualMediaHandoffMode = "media_set_sync_virtual"
	VirtualMediaHandoffModeExternalTransform VirtualMediaHandoffMode = "external_transform"
	VirtualMediaHandoffModeRestAPI           VirtualMediaHandoffMode = "rest_api"
)

type VirtualMediaHandoff struct {
	ID                    string                  `json:"id"`
	Title                 string                  `json:"title"`
	Summary               string                  `json:"summary"`
	HandoffMode           VirtualMediaHandoffMode `json:"handoff_mode"`
	ConnectorType         string                  `json:"connector_type"`
	Status                string                  `json:"status"`
	Blockers              []string                `json:"blockers"`
	ReadinessChecks       []string                `json:"readiness_checks"`
	RequiredContracts     []string                `json:"required_contracts"`
	SourceRID             string                  `json:"source_rid,omitempty"`
	MediaSetContract      string                  `json:"media_set_contract"`
	ObjectStorageContract string                  `json:"object_storage_contract"`
	AuthorizationContract string                  `json:"authorization_contract"`
	RegistrationSketch    string                  `json:"registration_sketch"`
	DocsURL               string                  `json:"docs_url"`
}

type VirtualMediaHandoffDescriptor struct {
	SourceID            uuid.UUID                     `json:"source_id,omitempty"`
	SourceRID           string                        `json:"source_rid,omitempty"`
	ConnectorType       string                        `json:"connector_type"`
	Status              string                        `json:"status"`
	BlockedReason       string                        `json:"blocked_reason,omitempty"`
	SupportedConnectors []string                      `json:"supported_connectors"`
	Handoffs            []VirtualMediaHandoff         `json:"handoffs"`
	Delegation          MediaSetSyncHandoffDelegation `json:"delegation"`
}

// virtualMediaHandoffBaseBlockers lists every blocker present on every handoff
// regardless of connector or registration mode. They map to specific Media Sets
// and platform checklist items that must complete before SDC.42 ships.
var virtualMediaHandoffBaseBlockers = []string{
	"media_sets_virtual_item_semantics", // MS.18, MS.19, MS.20 still todo
	"object_storage_authorization",      // no presigned URL / SAS / access-grant primitive
	"external_credential_routing",       // source creds aren't forwarded to media-sets-service
	"virtual_item_update_detection",     // MS.20: external mutation detection
}

func VirtualMediaHandoffBaseBlockers() []string {
	out := make([]string, len(virtualMediaHandoffBaseBlockers))
	copy(out, virtualMediaHandoffBaseBlockers)
	return out
}

// VirtualMediaHandoffSupportedConnectors mirrors MediaSyncSupportedConnectors;
// only those connectors can address a physical storage path that the media
// sets service could resolve as a virtual item. Returned as a fresh slice so
// callers can sort or extend it without mutating package state.
func VirtualMediaHandoffSupportedConnectors() []string {
	out := make([]string, len(MediaSyncSupportedConnectors))
	copy(out, MediaSyncSupportedConnectors)
	return out
}

// BuildVirtualMediaHandoffsForSource emits one handoff descriptor per
// registration mode for a supported connector source. When the source's
// connector is not part of the supported list (e.g., a REST API), the result
// is empty — there is no virtual media surface to expose.
func BuildVirtualMediaHandoffsForSource(sourceID uuid.UUID, sourceRID, connectorType string) []VirtualMediaHandoff {
	if !ConnectorSupportsMediaSync(connectorType) {
		return []VirtualMediaHandoff{}
	}
	if sourceRID == "" {
		sourceRID = SourceRIDForConnection(sourceID)
	}
	connectorLabel := strings.ToLower(strings.TrimSpace(connectorType))

	makeReadiness := func(extra ...string) []string {
		readiness := []string{
			"media-sets-service exposes /virtual-items lifecycle and refresh endpoints",
			"object storage authorization primitive is published as a shared library",
			"source credentials can be vended to media-sets-service through a scoped grant",
			"virtual items participate in lineage and Data Health like copied items",
		}
		return append(readiness, extra...)
	}

	makeContracts := func(extra ...string) []string {
		contracts := []string{
			"VirtualMediaItem.physical_path is a stable URI reachable by media-sets-service",
			"Access grants carry the source RID and the actor on whose behalf they are vended",
			"Media-sets-service rejects writes to virtual items unless the source policy allows it",
		}
		return append(contracts, extra...)
	}

	return []VirtualMediaHandoff{
		{
			ID:                    "virtual-media-set-sync-" + connectorLabel,
			Title:                 "Virtual media set sync handoff",
			Summary:               "Register media files referenced by a VIRTUAL_MEDIA_SET_SYNC run as virtual items instead of copying bytes.",
			HandoffMode:           VirtualMediaHandoffModeMediaSetSync,
			ConnectorType:         connectorLabel,
			Status:                "blocked",
			Blockers:              append(VirtualMediaHandoffBaseBlockers(), "media_set_sync_virtual_runtime_contract"),
			ReadinessChecks:       makeReadiness("VIRTUAL_MEDIA_SET_SYNC runtime path persists per-item failures into sync_run_failures"),
			RequiredContracts:     makeContracts("Sync execution carries the source signature so re-runs preserve already-registered items"),
			SourceRID:             sourceRID,
			MediaSetContract:      "media-sets-service POST /media-sets/{rid}/virtual-items accepts {physical_path, item_path, mime_type, size_bytes}",
			ObjectStorageContract: "Source credentials are exchanged for a media-sets-service access grant scoped to the source bucket/container",
			AuthorizationContract: "Cedar policy: source_role(use) + media_set_role(write) required before /virtual-items accepts the registration",
			RegistrationSketch: strings.Join([]string{
				"# Virtual media set sync — VIRTUAL_MEDIA_SET_SYNC dispatch (blocked)",
				"POST /api/v1/data-connection/media-set-syncs/{id}/run",
				"{",
				"  \"source_files\": [{\"path\": \"archive/2026/scan.png\", \"mime_type\": \"image/png\", \"size_bytes\": 81920}],",
				"  \"allowed_mime_types\": [\"image/png\"]",
				"}",
				"# Runtime forwards to:",
				"POST {media-sets}/media-sets/{rid}/virtual-items",
				"{",
				"  \"physical_path\": \"" + connectorLabel + "://bucket/archive/2026/scan.png\",",
				"  \"item_path\": \"archive/2026/scan.png\",",
				"  \"mime_type\": \"image/png\",",
				"  \"size_bytes\": 81920,",
				"  \"source_rid\": \"" + sourceRID + "\"",
				"}",
			}, "\n"),
			DocsURL: "https://www.palantir.com/docs/foundry/data-connection/core-concepts/",
		},
		{
			ID:                    "virtual-media-external-transform-" + connectorLabel,
			Title:                 "Virtual media item from external transform",
			Summary:               "Register a single virtual media item from a Python transform using the source-bound generated binding.",
			HandoffMode:           VirtualMediaHandoffModeExternalTransform,
			ConnectorType:         connectorLabel,
			Status:                "blocked",
			Blockers:              append(VirtualMediaHandoffBaseBlockers(), "external_transform_virtual_item_sdk"),
			ReadinessChecks:       makeReadiness("Python source binding exposes a register_virtual_media_item helper backed by media-sets-service"),
			RequiredContracts:     makeContracts("Build-start resolves source credentials into a short-lived access grant the transform passes through"),
			SourceRID:             sourceRID,
			MediaSetContract:      "media-sets-service POST /media-sets/{rid}/virtual-items idempotent on (source_rid, item_path)",
			ObjectStorageContract: "External-systems binding produces a presigned object URL with read scope only when the source allows export",
			AuthorizationContract: "Source export policy must include the target media set's marking/org before the binding emits a grant",
			RegistrationSketch: strings.Join([]string{
				"@transform_external_systems(source=Sources." + strings.ReplaceAll(connectorLabel, "-", "_") + ")",
				"def register(source, media_set):",
				"    grant = source.access_grant(scope='read-only')",
				"    media_set.register_virtual_media_item(",
				"        physical_path=f'" + connectorLabel + "://bucket/{key}',",
				"        item_path=key,",
				"        mime_type='image/png',",
				"        size_bytes=size,",
				"        access_grant=grant,",
				"    )",
			}, "\n"),
			DocsURL: "https://www.palantir.com/docs/foundry/data-connection/external-transforms",
		},
		{
			ID:                    "virtual-media-rest-api-" + connectorLabel,
			Title:                 "Virtual media item registration via REST",
			Summary:               "Register external media as virtual items from outside Foundry via an authenticated REST endpoint.",
			HandoffMode:           VirtualMediaHandoffModeRestAPI,
			ConnectorType:         connectorLabel,
			Status:                "blocked",
			Blockers:              append(VirtualMediaHandoffBaseBlockers(), "external_caller_authentication"),
			ReadinessChecks:       makeReadiness("Tokens issued for the source can be exchanged for a media-sets-service grant"),
			RequiredContracts:     makeContracts("Replay-safe registration: re-submission with the same (source_rid, item_path) is idempotent"),
			SourceRID:             sourceRID,
			MediaSetContract:      "media-sets-service exposes a public POST /media-sets/{rid}/virtual-items endpoint scoped by source token",
			ObjectStorageContract: "External callers attach a checksum (sha256) so media-sets-service can detect drift without reading the bytes",
			AuthorizationContract: "Source token + media set marking are both validated before the registration is accepted",
			RegistrationSketch: strings.Join([]string{
				"POST /api/v1/media-sets/{media_set_rid}/virtual-items",
				"Authorization: Bearer <source-issued token>",
				"{",
				"  \"physical_path\": \"" + connectorLabel + "://bucket/archive/2026/scan.png\",",
				"  \"item_path\": \"archive/2026/scan.png\",",
				"  \"mime_type\": \"image/png\",",
				"  \"size_bytes\": 81920,",
				"  \"checksum\": \"sha256:<digest>\",",
				"  \"source_rid\": \"" + sourceRID + "\"",
				"}",
			}, "\n"),
			DocsURL: "https://www.palantir.com/docs/foundry/data-connection/core-concepts/",
		},
	}
}

// VirtualMediaHandoffsAreBlocked returns true when every handoff in the list is
// in a "blocked" status. An empty list returns false so the UI can distinguish
// "no virtual handoff is supported" from "blocked while contracts are pending".
func VirtualMediaHandoffsAreBlocked(handoffs []VirtualMediaHandoff) bool {
	if len(handoffs) == 0 {
		return false
	}
	for _, h := range handoffs {
		if h.Status != "blocked" {
			return false
		}
	}
	return true
}

func VirtualMediaHandoffBlockers(handoffs []VirtualMediaHandoff) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, h := range handoffs {
		for _, blocker := range h.Blockers {
			if blocker == "" || seen[blocker] {
				continue
			}
			seen[blocker] = true
			out = append(out, blocker)
		}
	}
	sort.Strings(out)
	return out
}

func VirtualMediaHandoffCoverage(handoffs []VirtualMediaHandoff) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, h := range handoffs {
		mode := string(h.HandoffMode)
		if mode == "" || seen[mode] {
			continue
		}
		seen[mode] = true
		out = append(out, mode)
	}
	sort.Strings(out)
	return out
}

// BuildVirtualMediaHandoffDescriptor wraps the list with the source context, a
// canonical blocked_reason, and the delegation block from SDC.41 so the UI can
// render the panel in one round trip.
func BuildVirtualMediaHandoffDescriptor(sourceID uuid.UUID, sourceRID, connectorType string) VirtualMediaHandoffDescriptor {
	handoffs := BuildVirtualMediaHandoffsForSource(sourceID, sourceRID, connectorType)
	descriptor := VirtualMediaHandoffDescriptor{
		SourceID:            sourceID,
		SourceRID:           sourceRID,
		ConnectorType:       strings.ToLower(strings.TrimSpace(connectorType)),
		SupportedConnectors: VirtualMediaHandoffSupportedConnectors(),
		Handoffs:            handoffs,
		Delegation:          DefaultMediaSetSyncHandoffDelegation(),
	}
	if !ConnectorSupportsMediaSync(connectorType) {
		descriptor.Status = "not_supported"
		descriptor.BlockedReason = "Connector does not expose physical media files; virtual media handoff is only available for object stores."
		return descriptor
	}
	if VirtualMediaHandoffsAreBlocked(handoffs) {
		descriptor.Status = "blocked"
		descriptor.BlockedReason = "Blocked until Media Sets virtual media item semantics (MS.18–MS.20) and the object storage authorization contract are defined locally."
	} else {
		descriptor.Status = "available"
	}
	return descriptor
}

// SDC.43 — Listener-style inbound ingestion (blocked).
//
// OpenFoundry already ships a minimal HTTPS inbound listener
// (`POST /api/v1/listeners/{id}/events` with HMAC-SHA256 or shared-secret auth,
// header sanitization, and idempotency key extraction). SDC.43 captures the
// four product-policy concerns from the public Foundry docs — schema mapping,
// auth strategy, replay/idempotency controls, and dead-letter handling — and
// keeps the aggregate status `blocked` until either Palantir publishes the
// exact semantics or OpenFoundry product policy fills the gap. The descriptor
// surfaces what already works ("available_surfaces") so users can choose the
// HMAC webhook today while the higher-fidelity flows are designed.

type ListenerInboundFacet string

const (
	ListenerInboundFacetSchemaMapping     ListenerInboundFacet = "schema_mapping"
	ListenerInboundFacetAuthStrategy      ListenerInboundFacet = "auth_strategy"
	ListenerInboundFacetReplayIdempotency ListenerInboundFacet = "replay_idempotency"
	ListenerInboundFacetDeadLetter        ListenerInboundFacet = "dead_letter"
)

type ListenerInboundCapability struct {
	ID                  string               `json:"id"`
	Title               string               `json:"title"`
	Summary             string               `json:"summary"`
	Facet               ListenerInboundFacet `json:"facet"`
	Status              string               `json:"status"`
	ExistingSurface     string               `json:"existing_surface"`
	Blockers            []string             `json:"blockers"`
	ReadinessChecks     []string             `json:"readiness_checks"`
	RequiredContracts   []string             `json:"required_contracts"`
	ConfigurationSketch string               `json:"configuration_sketch"`
	DocsURL             string               `json:"docs_url"`
}

type ListenerInboundDescriptor struct {
	SourceID              uuid.UUID                     `json:"source_id,omitempty"`
	SourceRID             string                        `json:"source_rid,omitempty"`
	ConnectorType         string                        `json:"connector_type"`
	Status                string                        `json:"status"`
	BlockedReason         string                        `json:"blocked_reason,omitempty"`
	AvailableSurfaces     []string                      `json:"available_surfaces"`
	SupportedAuthModes    []string                      `json:"supported_auth_modes"`
	BlockedAuthModes      []string                      `json:"blocked_auth_modes"`
	IdempotencyKeyHeaders []string                      `json:"idempotency_key_headers"`
	MaxPayloadBytes       uint64                        `json:"max_payload_bytes"`
	Capabilities          []ListenerInboundCapability   `json:"capabilities"`
	Recommendation        StreamIngestionRecommendation `json:"recommendation"`
}

type StreamIngestionRecommendation struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// listenerInboundBaseBlockers apply to every facet because the product as a
// whole is blocked on either published listener documentation or local policy.
var listenerInboundBaseBlockers = []string{
	"listener_public_documentation", // Palantir docs do not yet pin exact semantics
	"listener_product_policy",       // OpenFoundry policy on listener lifecycle/auth issuance/retention
}

// listenerInboundAvailableSurfaces reflects the actual wired endpoints today.
// Kept in sync with [services/connector-management-service/internal/server/server.go].
var listenerInboundAvailableSurfaces = []string{
	"POST /api/v1/listeners/{id}/events",
	"POST /api/v1/data-connection/sources/{source_id}/listeners/{listener_id}/events",
	"GET  /api/v1/listeners/{id}/events",
	"GET  /api/v1/data-connection/sources/{id}/listener-events",
}

// listenerInboundIdempotencyKeyHeaders mirrors inboundListenerExternalEventID.
var listenerInboundIdempotencyKeyHeaders = []string{
	"X-OpenFoundry-Event-Id",
	"X-Event-Id",
	"Idempotency-Key",
}

// listenerInboundSupportedAuthModes mirrors verifyInboundListenerAuth.
var listenerInboundSupportedAuthModes = []string{"none", "shared_secret", "hmac_sha256"}

// listenerInboundBlockedAuthModes documents the auth modes the product policy
// must define before they can ship.
var listenerInboundBlockedAuthModes = []string{"oauth2_client_credentials", "jwt_bearer", "mutual_tls"}

func ListenerInboundBaseBlockers() []string {
	out := make([]string, len(listenerInboundBaseBlockers))
	copy(out, listenerInboundBaseBlockers)
	return out
}

func ListenerInboundAvailableSurfaces() []string {
	out := make([]string, len(listenerInboundAvailableSurfaces))
	copy(out, listenerInboundAvailableSurfaces)
	return out
}

// BuildListenerInboundCapabilities returns one capability per SDC.43 facet.
// Each entry classifies its status (`available`/`partial`/`blocked`) based on
// what's already wired in the inbound listener pipeline, with explicit blockers
// for the missing pieces.
func BuildListenerInboundCapabilities() []ListenerInboundCapability {
	base := ListenerInboundBaseBlockers()

	merge := func(extra ...string) []string {
		out := make([]string, 0, len(base)+len(extra))
		out = append(out, base...)
		out = append(out, extra...)
		return out
	}

	return []ListenerInboundCapability{
		{
			ID:              "listener-schema-mapping",
			Title:           "Schema mapping",
			Summary:         "Translate the inbound payload into the target stream/dataset schema when the producer cannot conform to it directly.",
			Facet:           ListenerInboundFacetSchemaMapping,
			Status:          "blocked",
			ExistingSurface: "Payload is stored verbatim in inbound_listener_events.payload (JSONB). No mapping pipeline is wired.",
			Blockers: merge(
				"schema_mapping_pipeline",
				"target_resource_resolution_contract",
				"mapping_versioning_policy",
			),
			ReadinessChecks: []string{
				"Listener definition declares target_schema_ref and mapping_expression",
				"Mapper rejects events whose required fields are missing",
				"Mapping evaluator is versioned and updatable without dropping events",
			},
			RequiredContracts: []string{
				"InboundListenerDestinationConfig carries a mapping_expression field with declared input/output schemas",
				"Mapping evaluator is shared across listener and webhook outbound transforms",
			},
			ConfigurationSketch: strings.Join([]string{
				"listener:",
				"  type: https",
				"  destination:",
				"    mode: stream",
				"    target_stream_rid: ri.streams.main.event-firehose",
				"    mapping_expression: |",
				"      {",
				"        \"event_id\": payload.id,",
				"        \"occurred_at\": payload.created_at,",
				"        \"actor\": payload.actor.email,",
				"        \"payload\": payload.body",
				"      }",
				"    schema_ref: ri.streams.main.event-firehose#schema/v3",
			}, "\n"),
			DocsURL: "https://www.palantir.com/docs/foundry/data-connection/push-based-ingestion/",
		},
		{
			ID:              "listener-auth-strategy",
			Title:           "Auth strategy",
			Summary:         "Authenticate inbound calls when external producers cannot use the OpenFoundry push API token model.",
			Facet:           ListenerInboundFacetAuthStrategy,
			Status:          "partial",
			ExistingSurface: "Auth modes wired today: " + strings.Join(listenerInboundSupportedAuthModes, ", ") + " (see verifyInboundListenerAuth).",
			Blockers: merge(
				"oauth2_listener_token_exchange",
				"mtls_inbound_trust_anchor",
				"jwt_inbound_validation_policy",
			),
			ReadinessChecks: []string{
				"Listener definition supports an auth.type discriminator covering OAuth2/JWT/mTLS",
				"Tokens are scoped to (source_id, listener_id) and revocable",
				"Auth failures are surfaced in inbound_listener_events with status=rejected and a reason",
			},
			RequiredContracts: []string{
				"InboundListenerAuthConfig accepts oauth2_client_credentials / jwt_bearer / mutual_tls",
				"Listener auth tokens roll up to the source's audit trail (SDC.50)",
			},
			ConfigurationSketch: strings.Join([]string{
				"auth:",
				"  type: oauth2_client_credentials",
				"  token_issuer: https://idp.example.com/oauth2/token",
				"  audience: ri.connection.main.source.42",
				"  client_id_ref: secret://connection/42/client_id",
				"  client_secret_ref: secret://connection/42/client_secret",
			}, "\n"),
			DocsURL: "https://www.palantir.com/docs/foundry/data-connection/push-based-ingestion/",
		},
		{
			ID:              "listener-replay-idempotency",
			Title:           "Replay and idempotency controls",
			Summary:         "Detect and collapse duplicate inbound events, support bounded replay windows, and preserve ordering where the source guarantees it.",
			Facet:           ListenerInboundFacetReplayIdempotency,
			Status:          "partial",
			ExistingSurface: "Idempotency key is extracted from " + strings.Join(listenerInboundIdempotencyKeyHeaders, ", ") + " or payload.event_id, but no dedupe store enforces it.",
			Blockers: merge(
				"listener_dedupe_window",
				"listener_replay_endpoint",
				"event_ordering_semantics",
			),
			ReadinessChecks: []string{
				"Dedupe store rejects a second event with the same (source_id, listener_id, event_id) within the configured window",
				"Operators can replay a window of events through the listener pipeline without producer involvement",
				"Per-listener ordering guarantee is documented (none, per-key, or global)",
			},
			RequiredContracts: []string{
				"InboundListenerLimits carries a dedupe_window_seconds field",
				"Replay endpoint accepts (from, to) bounded by retention policy",
			},
			ConfigurationSketch: strings.Join([]string{
				"limits:",
				"  max_payload_bytes: 1048576",
				"  dedupe_window_seconds: 600",
				"  ordering: per_key",
				"  retention_days: 7",
				"replay:",
				"  endpoint: POST /api/v1/data-connection/sources/{id}/listeners/{listener_id}/replay",
				"  request: { from: <iso-8601>, to: <iso-8601>, max_events: 10000 }",
			}, "\n"),
			DocsURL: "https://www.palantir.com/docs/foundry/data-connection/push-based-ingestion/",
		},
		{
			ID:              "listener-dead-letter",
			Title:           "Dead-letter handling",
			Summary:         "Capture rejected events (validation, auth, schema, destination) into a separate sink so operators can inspect, redact, or requeue them.",
			Facet:           ListenerInboundFacetDeadLetter,
			Status:          "blocked",
			ExistingSurface: "inbound_listener_events.status='rejected' rows persist with redacted headers, but there is no dead-letter dataset/stream or requeue API.",
			Blockers: merge(
				"dead_letter_sink_definition",
				"dead_letter_retention_policy",
				"requeue_authorization",
			),
			ReadinessChecks: []string{
				"Rejected events are routed to a dead-letter resource declared on the listener",
				"Requeue is gated by source_role(use) + dead_letter_role(write)",
				"Dead-letter resource respects markings/export policy from the originating source",
			},
			RequiredContracts: []string{
				"InboundListenerDestinationConfig carries dead_letter_resource_rid and dead_letter_retention_days",
				"Requeue API verifies the actor before re-emitting events",
			},
			ConfigurationSketch: strings.Join([]string{
				"destination:",
				"  mode: stream",
				"  target_stream_rid: ri.streams.main.event-firehose",
				"  dead_letter_resource_rid: ri.streams.main.event-firehose-dlq",
				"  dead_letter_retention_days: 14",
				"requeue:",
				"  endpoint: POST /api/v1/data-connection/sources/{id}/listeners/{listener_id}/dead-letter:requeue",
				"  body: { event_ids: [<uuid>...] }",
			}, "\n"),
			DocsURL: "https://www.palantir.com/docs/foundry/data-connection/push-based-ingestion/",
		},
	}
}

func ListenerInboundCapabilitiesAreBlocked(caps []ListenerInboundCapability) bool {
	if len(caps) == 0 {
		return false
	}
	for _, c := range caps {
		if c.Status != "blocked" {
			return false
		}
	}
	return true
}

func ListenerInboundBlockers(caps []ListenerInboundCapability) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, c := range caps {
		for _, b := range c.Blockers {
			if b == "" || seen[b] {
				continue
			}
			seen[b] = true
			out = append(out, b)
		}
	}
	sort.Strings(out)
	return out
}

func ListenerInboundCoverage(caps []ListenerInboundCapability) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, c := range caps {
		facet := string(c.Facet)
		if facet == "" || seen[facet] {
			continue
		}
		seen[facet] = true
		out = append(out, facet)
	}
	sort.Strings(out)
	return out
}

// AggregateListenerInboundStatus collapses the per-facet statuses into a
// single value used by the descriptor. "blocked" wins over "partial" wins over
// "available". An empty list defaults to "blocked".
func AggregateListenerInboundStatus(caps []ListenerInboundCapability) string {
	if len(caps) == 0 {
		return "blocked"
	}
	hasBlocked := false
	hasPartial := false
	for _, c := range caps {
		switch c.Status {
		case "blocked":
			hasBlocked = true
		case "partial":
			hasPartial = true
		}
	}
	if hasBlocked {
		return "blocked"
	}
	if hasPartial {
		return "partial"
	}
	return "available"
}

// BuildListenerInboundDescriptor wraps the capabilities with the per-source
// context, the available surfaces, and the SDC.17 ingestion recommendation.
func BuildListenerInboundDescriptor(sourceID uuid.UUID, sourceRID, connectorType string) ListenerInboundDescriptor {
	caps := BuildListenerInboundCapabilities()
	descriptor := ListenerInboundDescriptor{
		SourceID:              sourceID,
		SourceRID:             sourceRID,
		ConnectorType:         strings.ToLower(strings.TrimSpace(connectorType)),
		AvailableSurfaces:     ListenerInboundAvailableSurfaces(),
		SupportedAuthModes:    append([]string{}, listenerInboundSupportedAuthModes...),
		BlockedAuthModes:      append([]string{}, listenerInboundBlockedAuthModes...),
		IdempotencyKeyHeaders: append([]string{}, listenerInboundIdempotencyKeyHeaders...),
		MaxPayloadBytes:       DefaultInboundListenerMaxPayloadBytes,
		Capabilities:          caps,
		Recommendation: StreamIngestionRecommendation{
			Kind:    "listener",
			Message: "Use listeners when inbound systems cannot authenticate to the push API or conform to the target stream schema.",
		},
	}
	descriptor.Status = AggregateListenerInboundStatus(caps)
	switch descriptor.Status {
	case "blocked":
		descriptor.BlockedReason = "Blocked until public listener documentation or local product policy pins the schema mapping, auth strategy, replay/idempotency, and dead-letter semantics."
	case "partial":
		descriptor.BlockedReason = "HMAC/shared-secret webhook listeners are available today; the remaining facets are blocked until product policy defines them."
	}
	return descriptor
}

func (r CreateMediaSetSyncRequest) Validate() []string {
	return ValidateMediaSetSyncConfig(r.Kind, r.TargetMediaSetRID, r.Filters, r.ScheduleCron)
}

type SyncRun struct {
	ID               uuid.UUID  `json:"id"`
	SyncDefID        uuid.UUID  `json:"sync_def_id"`
	Status           string     `json:"status"`
	StartedAt        time.Time  `json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at"`
	BytesWritten     int64      `json:"bytes_written"`
	FilesWritten     int64      `json:"files_written"`
	Error            *string    `json:"error"`
	IngestJobID      *string    `json:"ingest_job_id"`
	DatasetVersionID *uuid.UUID `json:"dataset_version_id"`
	ContentHash      *string    `json:"content_hash"`
}

type VirtualTableSourceLink struct {
	SourceRID                    string          `json:"source_rid"`
	Provider                     string          `json:"provider"`
	VirtualTablesEnabled         bool            `json:"virtual_tables_enabled"`
	CodeImportsEnabled           bool            `json:"code_imports_enabled"`
	ExportControls               json.RawMessage `json:"export_controls"`
	AutoRegisterProjectRID       *string         `json:"auto_register_project_rid"`
	AutoRegisterEnabled          bool            `json:"auto_register_enabled"`
	AutoRegisterIntervalSeconds  *int32          `json:"auto_register_interval_seconds"`
	AutoRegisterTagFilters       json.RawMessage `json:"auto_register_tag_filters"`
	AutoRegisterFolderMirrorKind string          `json:"auto_register_folder_mirror_kind"`
	AutoRegisterTableTagFilters  []string        `json:"auto_register_table_tag_filters"`
	IcebergCatalogKind           *string         `json:"iceberg_catalog_kind"`
	IcebergCatalogConfig         json.RawMessage `json:"iceberg_catalog_config"`
	CreatedAt                    time.Time       `json:"created_at"`
	UpdatedAt                    time.Time       `json:"updated_at"`
}

type EnableVirtualTableSourceRequest struct {
	Provider             string          `json:"provider"`
	IcebergCatalogKind   *string         `json:"iceberg_catalog_kind,omitempty"`
	IcebergCatalogConfig json.RawMessage `json:"iceberg_catalog_config,omitempty"`
}

type VirtualTable struct {
	ID                                 uuid.UUID       `json:"id"`
	RID                                string          `json:"rid"`
	SourceRID                          string          `json:"source_rid"`
	ProjectRID                         string          `json:"project_rid"`
	Name                               string          `json:"name"`
	ParentFolderRID                    *string         `json:"parent_folder_rid"`
	Locator                            json.RawMessage `json:"locator"`
	TableType                          string          `json:"table_type"`
	SchemaInferred                     json.RawMessage `json:"schema_inferred"`
	Capabilities                       json.RawMessage `json:"capabilities"`
	UpdateDetectionEnabled             bool            `json:"update_detection_enabled"`
	UpdateDetectionIntervalSeconds     *int32          `json:"update_detection_interval_seconds"`
	LastObservedVersion                *string         `json:"last_observed_version"`
	LastPolledAt                       *time.Time      `json:"last_polled_at"`
	UpdateDetectionConsecutiveFailures int32           `json:"update_detection_consecutive_failures"`
	UpdateDetectionNextPollAt          *time.Time      `json:"update_detection_next_poll_at"`
	Markings                           []string        `json:"markings"`
	Properties                         json.RawMessage `json:"properties"`
	CreatedBy                          *string         `json:"created_by"`
	CreatedAt                          time.Time       `json:"created_at"`
	UpdatedAt                          time.Time       `json:"updated_at"`
}

type Locator struct {
	Kind      string `json:"kind"`
	Database  string `json:"database,omitempty"`
	Schema    string `json:"schema,omitempty"`
	Table     string `json:"table,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	Format    string `json:"format,omitempty"`
	Catalog   string `json:"catalog,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type CreateVirtualTableRequest struct {
	ProjectRID      string          `json:"project_rid"`
	Name            *string         `json:"name,omitempty"`
	ParentFolderRID *string         `json:"parent_folder_rid,omitempty"`
	Locator         Locator         `json:"locator"`
	TableType       string          `json:"table_type"`
	SchemaInferred  json.RawMessage `json:"schema_inferred,omitempty"`
	Capabilities    json.RawMessage `json:"capabilities,omitempty"`
	Properties      json.RawMessage `json:"properties,omitempty"`
	Owner           *string         `json:"owner,omitempty"`
	Permissions     json.RawMessage `json:"permissions,omitempty"`
	Markings        []string        `json:"markings,omitempty"`
}

type ListVirtualTablesResponse struct {
	Items      []VirtualTable `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}

func (l Locator) CanonicalJSON() (json.RawMessage, error) {
	switch l.Kind {
	case "tabular":
		return json.Marshal(map[string]string{"kind": "tabular", "database": strings.TrimSpace(l.Database), "schema": strings.TrimSpace(l.Schema), "table": strings.TrimSpace(l.Table)})
	case "file":
		return json.Marshal(map[string]string{"kind": "file", "bucket": strings.TrimSpace(l.Bucket), "prefix": strings.TrimSpace(l.Prefix), "format": strings.ToLower(strings.TrimSpace(l.Format))})
	case "iceberg":
		return json.Marshal(map[string]string{"kind": "iceberg", "catalog": strings.TrimSpace(l.Catalog), "namespace": strings.TrimSpace(l.Namespace), "table": strings.TrimSpace(l.Table)})
	default:
		return nil, fmt.Errorf("invalid locator kind: %s", l.Kind)
	}
}

func (l Locator) DefaultDisplayName() string {
	switch l.Kind {
	case "tabular", "iceberg":
		return strings.TrimSpace(l.Table)
	case "file":
		bucket := strings.TrimSpace(l.Bucket)
		prefix := strings.TrimSpace(l.Prefix)
		if prefix == "" {
			return bucket
		}
		return bucket + "/" + prefix
	default:
		return ""
	}
}

// ConnectorAgent mirrors models/agent.rs.
type ConnectorAgent struct {
	ID                             uuid.UUID                         `json:"id"`
	Name                           string                            `json:"name"`
	AgentURL                       string                            `json:"agent_url"`
	Version                        string                            `json:"version"`
	Environment                    string                            `json:"environment"`
	Host                           string                            `json:"host"`
	OwnerID                        uuid.UUID                         `json:"owner_id"`
	Status                         string                            `json:"status"`
	Capabilities                   json.RawMessage                   `json:"capabilities"`
	Metadata                       json.RawMessage                   `json:"metadata"`
	ConnectedSources               []AgentConnectedSource            `json:"connected_sources"`
	SupportedConnectorCapabilities []AgentConnectorCapabilitySummary `json:"supported_connector_capabilities"`
	AssignedProxyPolicies          []AgentProxyPolicyAssignment      `json:"assigned_proxy_policies"`
	ConnectionFailures             []AgentConnectionFailure          `json:"connection_failures"`
	Health                         AgentHealthSummary                `json:"health"`
	LastHeartbeatAt                *time.Time                        `json:"last_heartbeat_at"`
	CreatedAt                      time.Time                         `json:"created_at"`
	UpdatedAt                      time.Time                         `json:"updated_at"`
}

type RegisterAgentRequest struct {
	Name                           string                            `json:"name"`
	AgentURL                       string                            `json:"agent_url"`
	Version                        string                            `json:"version,omitempty"`
	Environment                    string                            `json:"environment,omitempty"`
	Host                           string                            `json:"host,omitempty"`
	Capabilities                   json.RawMessage                   `json:"capabilities"`
	Metadata                       json.RawMessage                   `json:"metadata"`
	ConnectedSources               []AgentConnectedSource            `json:"connected_sources,omitempty"`
	SupportedConnectorCapabilities []AgentConnectorCapabilitySummary `json:"supported_connector_capabilities,omitempty"`
	AssignedProxyPolicies          []AgentProxyPolicyAssignment      `json:"assigned_proxy_policies,omitempty"`
	ConnectionFailures             []AgentConnectionFailure          `json:"connection_failures,omitempty"`
}

type AgentHeartbeatRequest struct {
	Version                        string                            `json:"version,omitempty"`
	Environment                    string                            `json:"environment,omitempty"`
	Host                           string                            `json:"host,omitempty"`
	Capabilities                   json.RawMessage                   `json:"capabilities"`
	Metadata                       json.RawMessage                   `json:"metadata"`
	ConnectedSources               []AgentConnectedSource            `json:"connected_sources,omitempty"`
	SupportedConnectorCapabilities []AgentConnectorCapabilitySummary `json:"supported_connector_capabilities,omitempty"`
	AssignedProxyPolicies          []AgentProxyPolicyAssignment      `json:"assigned_proxy_policies,omitempty"`
	ConnectionFailures             []AgentConnectionFailure          `json:"connection_failures,omitempty"`
}

type AgentConnectedSource struct {
	SourceID        uuid.UUID  `json:"source_id"`
	SourceName      string     `json:"source_name"`
	ConnectorType   string     `json:"connector_type"`
	Status          string     `json:"status"`
	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
}

type AgentConnectorCapabilitySummary struct {
	ConnectorType string   `json:"connector_type"`
	Capabilities  []string `json:"capabilities"`
}

type AgentProxyPolicyAssignment struct {
	PolicyID   uuid.UUID  `json:"policy_id"`
	PolicyName string     `json:"policy_name,omitempty"`
	SourceID   uuid.UUID  `json:"source_id,omitempty"`
	SourceName string     `json:"source_name,omitempty"`
	ProxyMode  string     `json:"proxy_mode,omitempty"`
	Status     string     `json:"status,omitempty"`
	AssignedAt *time.Time `json:"assigned_at,omitempty"`
}

type AgentConnectionFailure struct {
	SourceID   uuid.UUID  `json:"source_id,omitempty"`
	SourceName string     `json:"source_name,omitempty"`
	PolicyID   uuid.UUID  `json:"policy_id,omitempty"`
	Code       string     `json:"code"`
	Message    string     `json:"message"`
	Retryable  bool       `json:"retryable"`
	OccurredAt *time.Time `json:"occurred_at,omitempty"`
}

type AgentHealthSummary struct {
	State                    string `json:"state"`
	Message                  string `json:"message,omitempty"`
	Stale                    bool   `json:"stale"`
	LastHeartbeatAgeSeconds  *int64 `json:"last_heartbeat_age_seconds,omitempty"`
	ConnectedSourceCount     int    `json:"connected_source_count"`
	AssignedProxyPolicyCount int    `json:"assigned_proxy_policy_count"`
	FailureCount             int    `json:"failure_count"`
}

func NormalizeConnectorAgent(agent ConnectorAgent) ConnectorAgent {
	agent.Name = strings.TrimSpace(agent.Name)
	agent.AgentURL = strings.TrimSpace(agent.AgentURL)
	agent.Version = strings.TrimSpace(agent.Version)
	agent.Environment = strings.TrimSpace(agent.Environment)
	agent.Host = strings.TrimSpace(agent.Host)
	if len(agent.Capabilities) == 0 || string(agent.Capabilities) == "null" {
		agent.Capabilities = json.RawMessage(`{}`)
	}
	if len(agent.Metadata) == 0 || string(agent.Metadata) == "null" {
		agent.Metadata = json.RawMessage(`{}`)
	}
	agent.ConnectedSources = NormalizeAgentConnectedSources(agent.ConnectedSources)
	agent.SupportedConnectorCapabilities = NormalizeAgentConnectorCapabilities(agent.SupportedConnectorCapabilities)
	agent.AssignedProxyPolicies = NormalizeAgentProxyPolicyAssignments(agent.AssignedProxyPolicies)
	agent.ConnectionFailures = NormalizeAgentConnectionFailures(agent.ConnectionFailures)
	return agent
}

func NormalizeRegisterAgentRequest(body *RegisterAgentRequest, fallbackHost string) {
	body.Name = strings.TrimSpace(body.Name)
	body.AgentURL = strings.TrimSpace(body.AgentURL)
	body.Version = firstNonEmpty(body.Version, metadataString(body.Metadata, "version"), metadataString(body.Capabilities, "version"))
	body.Environment = firstNonEmpty(body.Environment, metadataString(body.Metadata, "environment"), metadataString(body.Metadata, "region"))
	body.Host = firstNonEmpty(body.Host, metadataString(body.Metadata, "host"), fallbackHost)
	body.ConnectedSources = NormalizeAgentConnectedSources(body.ConnectedSources)
	body.SupportedConnectorCapabilities = NormalizeAgentConnectorCapabilities(body.SupportedConnectorCapabilities)
	if len(body.SupportedConnectorCapabilities) == 0 {
		body.SupportedConnectorCapabilities = ConnectorCapabilitiesFromRaw(body.Capabilities)
	}
	body.AssignedProxyPolicies = NormalizeAgentProxyPolicyAssignments(body.AssignedProxyPolicies)
	body.ConnectionFailures = NormalizeAgentConnectionFailures(body.ConnectionFailures)
}

func NormalizeAgentHeartbeatRequest(body *AgentHeartbeatRequest) {
	body.Version = strings.TrimSpace(body.Version)
	body.Environment = strings.TrimSpace(body.Environment)
	body.Host = strings.TrimSpace(body.Host)
	body.ConnectedSources = NormalizeAgentConnectedSources(body.ConnectedSources)
	body.SupportedConnectorCapabilities = NormalizeAgentConnectorCapabilities(body.SupportedConnectorCapabilities)
	if len(body.SupportedConnectorCapabilities) == 0 {
		body.SupportedConnectorCapabilities = ConnectorCapabilitiesFromRaw(body.Capabilities)
	}
	body.AssignedProxyPolicies = NormalizeAgentProxyPolicyAssignments(body.AssignedProxyPolicies)
	body.ConnectionFailures = NormalizeAgentConnectionFailures(body.ConnectionFailures)
}

func NormalizeAgentConnectedSources(values []AgentConnectedSource) []AgentConnectedSource {
	out := make([]AgentConnectedSource, 0, len(values))
	seen := map[uuid.UUID]bool{}
	for _, value := range values {
		if value.SourceID == uuid.Nil || seen[value.SourceID] {
			continue
		}
		value.SourceName = strings.TrimSpace(value.SourceName)
		value.ConnectorType = strings.TrimSpace(value.ConnectorType)
		value.Status = strings.TrimSpace(value.Status)
		if value.Status == "" {
			value.Status = "connected"
		}
		seen[value.SourceID] = true
		out = append(out, value)
	}
	return out
}

func NormalizeAgentConnectorCapabilities(values []AgentConnectorCapabilitySummary) []AgentConnectorCapabilitySummary {
	out := make([]AgentConnectorCapabilitySummary, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value.ConnectorType = strings.TrimSpace(value.ConnectorType)
		if value.ConnectorType == "" {
			continue
		}
		key := strings.ToLower(value.ConnectorType)
		if seen[key] {
			continue
		}
		seen[key] = true
		value.Capabilities = normalizeUniqueStrings(value.Capabilities)
		out = append(out, value)
	}
	return out
}

func NormalizeAgentProxyPolicyAssignments(values []AgentProxyPolicyAssignment) []AgentProxyPolicyAssignment {
	out := make([]AgentProxyPolicyAssignment, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if value.PolicyID == uuid.Nil {
			continue
		}
		key := value.PolicyID.String() + ":" + value.SourceID.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		value.PolicyName = strings.TrimSpace(value.PolicyName)
		value.SourceName = strings.TrimSpace(value.SourceName)
		value.ProxyMode = strings.TrimSpace(value.ProxyMode)
		value.Status = strings.TrimSpace(value.Status)
		if value.Status == "" {
			value.Status = "assigned"
		}
		out = append(out, value)
	}
	return out
}

func NormalizeAgentConnectionFailures(values []AgentConnectionFailure) []AgentConnectionFailure {
	out := make([]AgentConnectionFailure, 0, len(values))
	for _, value := range values {
		value.SourceName = strings.TrimSpace(value.SourceName)
		value.Code = strings.TrimSpace(value.Code)
		value.Message = strings.TrimSpace(value.Message)
		if value.Code == "" && value.Message == "" {
			continue
		}
		if value.Code == "" {
			value.Code = "agent_connection_failure"
		}
		out = append(out, value)
	}
	return out
}

func ConnectorAgentWithHealth(agent ConnectorAgent, now time.Time, staleAfter time.Duration) ConnectorAgent {
	agent = NormalizeConnectorAgent(agent)
	agent.Health = ConnectorAgentHealth(agent, now, staleAfter)
	return agent
}

func ConnectorAgentHealth(agent ConnectorAgent, now time.Time, staleAfter time.Duration) AgentHealthSummary {
	if staleAfter <= 0 {
		staleAfter = 120 * time.Second
	}
	health := AgentHealthSummary{
		State:                    "healthy",
		ConnectedSourceCount:     len(agent.ConnectedSources),
		AssignedProxyPolicyCount: len(agent.AssignedProxyPolicies),
		FailureCount:             len(agent.ConnectionFailures),
	}
	if agent.LastHeartbeatAt == nil {
		health.State = "stale"
		health.Stale = true
		health.Message = "Agent has not sent a heartbeat."
		return health
	}
	age := int64(now.Sub(*agent.LastHeartbeatAt).Seconds())
	if age < 0 {
		age = 0
	}
	health.LastHeartbeatAgeSeconds = &age
	if now.Sub(*agent.LastHeartbeatAt) > staleAfter {
		health.State = "stale"
		health.Stale = true
		health.Message = "Agent heartbeat is stale."
	}
	if agent.Status != "" && agent.Status != "online" {
		health.State = "warning"
		health.Message = "Agent status is " + agent.Status + "."
	}
	if len(agent.ConnectionFailures) > 0 {
		health.State = "error"
		latest := agent.ConnectionFailures[0]
		for _, failure := range agent.ConnectionFailures[1:] {
			if failure.OccurredAt != nil && (latest.OccurredAt == nil || failure.OccurredAt.After(*latest.OccurredAt)) {
				latest = failure
			}
		}
		health.Message = latest.Message
		if health.Message == "" {
			health.Message = latest.Code
		}
	}
	return health
}

func ConnectorCapabilitiesFromRaw(raw json.RawMessage) []AgentConnectorCapabilitySummary {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	connectors := stringsFromAny(payload["connectors"])
	capabilityMap := map[string][]string{}
	if rawMap, ok := payload["connector_capabilities"].(map[string]any); ok {
		for connector, capabilities := range rawMap {
			capabilityMap[connector] = stringsFromAny(capabilities)
			if !containsStringFold(connectors, connector) {
				connectors = append(connectors, connector)
			}
		}
	}
	out := make([]AgentConnectorCapabilitySummary, 0, len(connectors))
	for _, connector := range connectors {
		capabilities := capabilityMap[connector]
		if len(capabilities) == 0 {
			capabilities = stringsFromAny(payload["capabilities"])
		}
		out = append(out, AgentConnectorCapabilitySummary{ConnectorType: connector, Capabilities: normalizeUniqueStrings(capabilities)})
	}
	return NormalizeAgentConnectorCapabilities(out)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}

func metadataString(raw json.RawMessage, key string) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func stringsFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	case string:
		if typed == "" {
			return nil
		}
		return strings.Split(typed, ",")
	default:
		return nil
	}
}

func containsStringFold(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(value, needle) {
			return true
		}
	}
	return false
}

type ListConnectionsQuery struct {
	Page    *int64 `json:"page,omitempty"`
	PerPage *int64 `json:"per_page,omitempty"`
}

type ConnectorContractCatalog struct {
	Connectors           []ConnectorContractProfile    `json:"connectors"`
	CertificationSummary ConnectorCertificationSummary `json:"certification_summary"`
	CapabilityMatrix     []ConnectorCapabilityMatrix   `json:"capability_matrix,omitempty"`
}

type ConnectorContractProfile struct {
	ConnectorType  string                        `json:"connector_type"`
	DisplayName    string                        `json:"display_name"`
	TemplateFamily string                        `json:"template_family"`
	Auth           ConnectorAuthProfile          `json:"auth"`
	Testing        ConnectorTestingProfile       `json:"testing"`
	Sync           ConnectorSyncProfile          `json:"sync"`
	Observability  ConnectorObservabilityProfile `json:"observability"`
	Builder        ConnectorBuilderProfile       `json:"builder"`
	Certification  ConnectorCertificationProfile `json:"certification"`
	Notes          []string                      `json:"notes"`
}

type ConnectorAuthProfile struct {
	Strategy                    string   `json:"strategy"`
	SecretFields                []string `json:"secret_fields"`
	SupportsOAuth               bool     `json:"supports_oauth"`
	SupportsPrivateNetworkAgent bool     `json:"supports_private_network_agent"`
}

type ConnectorTestingProfile struct {
	SupportsConnectionTesting   bool `json:"supports_connection_testing"`
	SupportsDiscovery           bool `json:"supports_discovery"`
	SupportsSchemaIntrospection bool `json:"supports_schema_introspection"`
}

type ConnectorSyncProfile struct {
	Modes               []string `json:"modes"`
	SupportsIncremental bool     `json:"supports_incremental"`
	SupportsCDC         bool     `json:"supports_cdc"`
	SupportsZeroCopy    bool     `json:"supports_zero_copy"`
}

type ConnectorObservabilityProfile struct {
	Retries          bool `json:"retries"`
	StatusTracking   bool `json:"status_tracking"`
	SourceSignatures bool `json:"source_signatures"`
}

type ConnectorBuilderProfile struct {
	ScaffoldKind       string   `json:"scaffold_kind"`
	ReusableComponents []string `json:"reusable_components"`
	ExampleTargets     []string `json:"example_targets"`
}

type ConnectorCertificationProfile struct {
	Level              string `json:"level"`
	RuntimeDepth       string `json:"runtime_depth"`
	Auth               string `json:"auth"`
	Observability      string `json:"observability"`
	SchemaEvolution    string `json:"schema_evolution"`
	PerformancePosture string `json:"performance_posture"`
	FailureHandling    string `json:"failure_handling"`
}

type ConnectorCertificationSummary struct {
	CertifiedConnectors        int      `json:"certified_connectors"`
	AdvancedConnectors         int      `json:"advanced_connectors"`
	ConnectorsNeedingHardening int      `json:"connectors_needing_hardening"`
	TemplateFamilies           []string `json:"template_families"`
}

type ConnectionCapabilityResponse struct {
	ConnectionID  uuid.UUID                       `json:"connection_id"`
	ConnectorType string                          `json:"connector_type"`
	Status        string                          `json:"status"`
	Contract      ConnectorContractProfile        `json:"contract"`
	Capabilities  ConnectionEffectiveCapabilities `json:"capabilities"`
}

type CredentialResponse struct {
	ID               uuid.UUID  `json:"id"`
	SourceID         uuid.UUID  `json:"source_id"`
	Kind             string     `json:"kind"`
	Fingerprint      string     `json:"fingerprint"`
	ValidationStatus string     `json:"validation_status,omitempty"`
	LastValidatedAt  *time.Time `json:"last_validated_at,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type SetCredentialRequest struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type SourcePolicyBindingResponse struct {
	SourceID uuid.UUID `json:"source_id"`
	PolicyID uuid.UUID `json:"policy_id"`
	Kind     string    `json:"kind"`
}

type AttachPolicyRequest struct {
	PolicyID uuid.UUID `json:"policy_id"`
	Kind     string    `json:"kind"`
}

type SyncStatus string

const (
	SyncStatusPending   SyncStatus = "pending"
	SyncStatusRunning   SyncStatus = "running"
	SyncStatusRetrying  SyncStatus = "retrying"
	SyncStatusCompleted SyncStatus = "completed"
	SyncStatusFailed    SyncStatus = "failed"
)

type LegacySyncJob struct {
	ID                   uuid.UUID       `json:"id"`
	ConnectionID         uuid.UUID       `json:"connection_id"`
	TargetDatasetID      *uuid.UUID      `json:"target_dataset_id"`
	TableName            string          `json:"table_name"`
	Status               string          `json:"status"`
	RowsSynced           int64           `json:"rows_synced"`
	Error                *string         `json:"error"`
	Attempts             int32           `json:"attempts"`
	MaxAttempts          int32           `json:"max_attempts"`
	ScheduledAt          time.Time       `json:"scheduled_at"`
	NextRetryAt          *time.Time      `json:"next_retry_at"`
	ResultDatasetVersion *int32          `json:"result_dataset_version"`
	SyncMetadata         json.RawMessage `json:"sync_metadata"`
	StartedAt            *time.Time      `json:"started_at"`
	CompletedAt          *time.Time      `json:"completed_at"`
	CreatedAt            time.Time       `json:"created_at"`
}

type SyncRequest struct {
	TableName       string     `json:"table_name"`
	TargetDatasetID *uuid.UUID `json:"target_dataset_id"`
	ScheduleAt      *time.Time `json:"schedule_at"`
	MaxAttempts     *int32     `json:"max_attempts"`
}

type ConnectionRegistration struct {
	ID                  uuid.UUID       `json:"id"`
	ConnectionID        uuid.UUID       `json:"connection_id"`
	Selector            string          `json:"selector"`
	DisplayName         string          `json:"display_name"`
	SourceKind          string          `json:"source_kind"`
	RegistrationMode    string          `json:"registration_mode"`
	AutoSync            bool            `json:"auto_sync"`
	UpdateDetection     bool            `json:"update_detection"`
	TargetDatasetID     *uuid.UUID      `json:"target_dataset_id"`
	LastSourceSignature *string         `json:"last_source_signature"`
	LastDatasetVersion  *int32          `json:"last_dataset_version"`
	Metadata            json.RawMessage `json:"metadata"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type DiscoveredSource struct {
	Selector         string          `json:"selector"`
	DisplayName      string          `json:"display_name"`
	SourceKind       string          `json:"source_kind"`
	SupportsSync     bool            `json:"supports_sync"`
	SupportsZeroCopy bool            `json:"supports_zero_copy"`
	SourceSignature  *string         `json:"source_signature,omitempty"`
	Metadata         json.RawMessage `json:"metadata"`
}

type AutoRegisterRequest struct {
	Selectors              []string   `json:"selectors"`
	RegistrationMode       *string    `json:"registration_mode"`
	AutoSync               *bool      `json:"auto_sync"`
	UpdateDetection        *bool      `json:"update_detection"`
	DefaultTargetDatasetID *uuid.UUID `json:"default_target_dataset_id"`
}

type RegistrationBulkRegisterRequest struct {
	Registrations []BulkRegistrationItem `json:"registrations"`
}

type BulkRegistrationItem struct {
	Selector         string          `json:"selector"`
	DisplayName      *string         `json:"display_name"`
	SourceKind       *string         `json:"source_kind"`
	RegistrationMode *string         `json:"registration_mode"`
	AutoSync         *bool           `json:"auto_sync"`
	UpdateDetection  *bool           `json:"update_detection"`
	TargetDatasetID  *uuid.UUID      `json:"target_dataset_id"`
	Metadata         json.RawMessage `json:"metadata"`
}

type VirtualTableQueryRequest struct {
	Selector                 string   `json:"selector"`
	Limit                    *int     `json:"limit"`
	Columns                  []string `json:"columns,omitempty"`
	Filters                  []string `json:"filters,omitempty"`
	OrderBy                  []string `json:"order_by,omitempty"`
	Aggregations             []string `json:"aggregations,omitempty"`
	RequiresFoundryCompute   bool     `json:"requires_foundry_compute,omitempty"`
	PreferredComputeLocation *string  `json:"preferred_compute_location,omitempty"`
}

type VirtualTableQueryResponse struct {
	Selector        string                    `json:"selector"`
	Mode            string                    `json:"mode"`
	Columns         []string                  `json:"columns"`
	RowCount        int                       `json:"row_count"`
	Rows            []json.RawMessage         `json:"rows"`
	SourceSignature *string                   `json:"source_signature,omitempty"`
	Metadata        json.RawMessage           `json:"metadata"`
	ComputeLocation string                    `json:"compute_location,omitempty"`
	Pushdown        *VirtualTablePushdownPlan `json:"pushdown,omitempty"`
	Limitations     []VirtualTableLimitation  `json:"limitations,omitempty"`
	Degraded        bool                      `json:"degraded,omitempty"`
	Source          string                    `json:"source,omitempty"`
}

type QueryRegistrationBody struct {
	Limit *int `json:"limit"`
}

type VirtualTablePushdownPlan struct {
	ComputeLocation    string   `json:"compute_location"`
	PushdownEngine     *string  `json:"pushdown_engine,omitempty"`
	FoundryEngine      *string  `json:"foundry_engine,omitempty"`
	PushedOperations   []string `json:"pushed_operations"`
	FoundryOperations  []string `json:"foundry_operations"`
	DirectQuery        bool     `json:"direct_query"`
	UsesCopiedDataset  bool     `json:"uses_copied_dataset"`
	InteractivePreview bool     `json:"interactive_preview"`
}

type VirtualTableLimitation struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

type UpdateAutoRegistrationBody struct {
	Enabled          *bool    `json:"enabled,omitempty"`
	RegistrationMode *string  `json:"registration_mode,omitempty"`
	AutoSync         *bool    `json:"auto_sync,omitempty"`
	UpdateDetection  *bool    `json:"update_detection,omitempty"`
	Selectors        []string `json:"selectors,omitempty"`
	IntervalSeconds  *int32   `json:"interval_seconds,omitempty"`
	TagFilters       []string `json:"tag_filters,omitempty"`
}

type HyperAutoErpRequest struct {
	Selectors        []string `json:"selectors"`
	MaxEntities      *int     `json:"max_entities"`
	SampleLimit      *int     `json:"sample_limit"`
	ScheduleCron     *string  `json:"schedule_cron"`
	PipelineStatus   *string  `json:"pipeline_status"`
	QueueInitialSync *bool    `json:"queue_initial_sync"`
	CreateLinkTypes  *bool    `json:"create_link_types"`
}

type HyperAutoErpFieldPlan struct {
	SourceName       string `json:"source_name"`
	PropertyName     string `json:"property_name"`
	PropertyType     string `json:"property_type"`
	Nullable         bool   `json:"nullable"`
	UniqueConstraint bool   `json:"unique_constraint"`
	SemanticRole     string `json:"semantic_role"`
}

type HyperAutoErpEntityPlan struct {
	Selector           string                  `json:"selector"`
	DisplayName        string                  `json:"display_name"`
	SourceKind         string                  `json:"source_kind"`
	Module             string                  `json:"module"`
	SampleRowCount     int                     `json:"sample_row_count"`
	RawDatasetName     string                  `json:"raw_dataset_name"`
	CuratedDatasetName string                  `json:"curated_dataset_name"`
	PipelineName       string                  `json:"pipeline_name"`
	ObjectTypeName     string                  `json:"object_type_name"`
	ObjectDisplayName  string                  `json:"object_display_name"`
	PrimaryKeyProperty *string                 `json:"primary_key_property"`
	NormalizationSQL   string                  `json:"normalization_sql"`
	Fields             []HyperAutoErpFieldPlan `json:"fields"`
}

type HyperAutoErpLinkPlan struct {
	Name                     string  `json:"name"`
	DisplayName              string  `json:"display_name"`
	SourceObjectTypeName     string  `json:"source_object_type_name"`
	TargetObjectTypeName     string  `json:"target_object_type_name"`
	SourcePropertyName       string  `json:"source_property_name"`
	TargetPrimaryKeyProperty *string `json:"target_primary_key_property"`
	Cardinality              string  `json:"cardinality"`
	Rationale                string  `json:"rationale"`
}

type HyperAutoErpPreviewResponse struct {
	ConnectionID   uuid.UUID                `json:"connection_id"`
	ConnectionName string                   `json:"connection_name"`
	ConnectorType  string                   `json:"connector_type"`
	ErpSystem      string                   `json:"erp_system"`
	GeneratedAt    time.Time                `json:"generated_at"`
	EntityCount    int                      `json:"entity_count"`
	PipelineStatus string                   `json:"pipeline_status"`
	ScheduleCron   *string                  `json:"schedule_cron"`
	Entities       []HyperAutoErpEntityPlan `json:"entities"`
	Links          []HyperAutoErpLinkPlan   `json:"links"`
	Warnings       []string                 `json:"warnings"`
}

type HyperAutoGeneratedDataset struct {
	Selector    string    `json:"selector"`
	DatasetID   uuid.UUID `json:"dataset_id"`
	DatasetName string    `json:"dataset_name"`
	Stage       string    `json:"stage"`
	Reused      bool      `json:"reused"`
}

type HyperAutoGeneratedRegistration struct {
	Selector        string    `json:"selector"`
	RegistrationID  uuid.UUID `json:"registration_id"`
	TargetDatasetID uuid.UUID `json:"target_dataset_id"`
}

type HyperAutoGeneratedPipeline struct {
	Selector     string    `json:"selector"`
	PipelineID   uuid.UUID `json:"pipeline_id"`
	PipelineName string    `json:"pipeline_name"`
	Reused       bool      `json:"reused"`
}

type HyperAutoGeneratedObjectType struct {
	Selector          string    `json:"selector"`
	ObjectTypeID      uuid.UUID `json:"object_type_id"`
	ObjectTypeName    string    `json:"object_type_name"`
	Reused            bool      `json:"reused"`
	PropertiesCreated int       `json:"properties_created"`
}

type HyperAutoGeneratedLinkType struct {
	Name       string    `json:"name"`
	LinkTypeID uuid.UUID `json:"link_type_id"`
	Reused     bool      `json:"reused"`
}

type HyperAutoQueuedIngestJob struct {
	Selector        string    `json:"selector"`
	JobID           uuid.UUID `json:"job_id"`
	TargetDatasetID uuid.UUID `json:"target_dataset_id"`
	ScheduledAt     time.Time `json:"scheduled_at"`
}

type HyperAutoErpGenerateResponse struct {
	Preview         HyperAutoErpPreviewResponse      `json:"preview"`
	RawDatasets     []HyperAutoGeneratedDataset      `json:"raw_datasets"`
	CuratedDatasets []HyperAutoGeneratedDataset      `json:"curated_datasets"`
	Registrations   []HyperAutoGeneratedRegistration `json:"registrations"`
	Pipelines       []HyperAutoGeneratedPipeline     `json:"pipelines"`
	ObjectTypes     []HyperAutoGeneratedObjectType   `json:"object_types"`
	LinkTypes       []HyperAutoGeneratedLinkType     `json:"link_types"`
	IngestJobs      []HyperAutoQueuedIngestJob       `json:"ingest_jobs"`
}

type SourceProvider string

const (
	SourceProviderAmazonS3       SourceProvider = "AMAZON_S3"
	SourceProviderAzureABFS      SourceProvider = "AZURE_ABFS"
	SourceProviderBigQuery       SourceProvider = "BIGQUERY"
	SourceProviderDatabricks     SourceProvider = "DATABRICKS"
	SourceProviderFoundryIceberg SourceProvider = "FOUNDRY_ICEBERG"
	SourceProviderGCS            SourceProvider = "GCS"
	SourceProviderSnowflake      SourceProvider = "SNOWFLAKE"
)

type TableType string

const (
	TableTypeTable            TableType = "TABLE"
	TableTypeView             TableType = "VIEW"
	TableTypeMaterializedView TableType = "MATERIALIZED_VIEW"
	TableTypeExternalDelta    TableType = "EXTERNAL_DELTA"
	TableTypeManagedDelta     TableType = "MANAGED_DELTA"
	TableTypeManagedIceberg   TableType = "MANAGED_ICEBERG"
	TableTypeParquetFiles     TableType = "PARQUET_FILES"
	TableTypeAvroFiles        TableType = "AVRO_FILES"
	TableTypeCSVFiles         TableType = "CSV_FILES"
	TableTypeOther            TableType = "OTHER"
)

type ComputePushdownEngine string

const (
	ComputePushdownEngineIbis     ComputePushdownEngine = "ibis"
	ComputePushdownEnginePySpark  ComputePushdownEngine = "pyspark"
	ComputePushdownEngineSnowpark ComputePushdownEngine = "snowpark"
)

type FoundryCompute struct {
	PythonSingleNode          bool `json:"python_single_node"`
	PythonSpark               bool `json:"python_spark"`
	PipelineBuilderSingleNode bool `json:"pipeline_builder_single_node"`
	PipelineBuilderSpark      bool `json:"pipeline_builder_spark"`
}

type Capabilities struct {
	Read                bool                   `json:"read"`
	Write               bool                   `json:"write"`
	Incremental         bool                   `json:"incremental"`
	Versioning          bool                   `json:"versioning"`
	ComputePushdown     *ComputePushdownEngine `json:"compute_pushdown"`
	SnapshotSupported   bool                   `json:"snapshot_supported"`
	AppendOnlySupported bool                   `json:"append_only_supported"`
	FoundryCompute      FoundryCompute         `json:"foundry_compute"`
}

type VirtualTableBulkRegisterRequest struct {
	ProjectRID string                      `json:"project_rid"`
	Entries    []CreateVirtualTableRequest `json:"entries"`
}

type VirtualTableBulkRegisterResponse struct {
	Registered []VirtualTable          `json:"registered"`
	Errors     []VirtualTableBulkError `json:"errors"`
}

type VirtualTableBulkError struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

type AutoRegistrationScanSummary struct {
	Added    int32 `json:"added"`
	Updated  int32 `json:"updated"`
	Orphaned int32 `json:"orphaned"`
}

type UpdateMarkingsRequest struct {
	Markings []string `json:"markings"`
}

type DiscoverQuery struct {
	Path *string `json:"path"`
}

type DiscoveredEntry struct {
	DisplayName       string  `json:"display_name"`
	Path              string  `json:"path"`
	Kind              string  `json:"kind"`
	Registrable       bool    `json:"registrable"`
	InferredTableType *string `json:"inferred_table_type"`
}

type ListVirtualTablesQuery struct {
	Project   *string `json:"project"`
	Source    *string `json:"source"`
	Name      *string `json:"name"`
	TableType *string `json:"type"`
	Limit     *int64  `json:"limit"`
	Cursor    *string `json:"cursor"`
}

type FolderMirrorKind string

const (
	FolderMirrorKindFlat   FolderMirrorKind = "FLAT"
	FolderMirrorKindNested FolderMirrorKind = "NESTED"
)

type RemoteTable struct {
	Database        string    `json:"database"`
	Schema          string    `json:"schema"`
	Table           string    `json:"table"`
	TableType       TableType `json:"table_type"`
	SchemaSignature string    `json:"schema_signature"`
	Tags            []string  `json:"tags"`
}

type ExistingTable struct {
	RID             string `json:"rid"`
	Database        string `json:"database"`
	Schema          string `json:"schema"`
	Table           string `json:"table"`
	SchemaSignature string `json:"schema_signature"`
}

type DiffResult struct {
	Added    []RemoteTable   `json:"added"`
	Updated  []UpdatedTable  `json:"updated"`
	Orphaned []ExistingTable `json:"orphaned"`
}

type UpdatedTable struct {
	RID    string      `json:"rid"`
	Remote RemoteTable `json:"remote"`
}

type SourceAutoRegisterConfig struct {
	SourceRID           string           `json:"source_rid"`
	Provider            SourceProvider   `json:"provider"`
	ProjectRID          string           `json:"project_rid"`
	Layout              FolderMirrorKind `json:"layout"`
	TagFilters          []string         `json:"tag_filters"`
	PollIntervalSeconds uint64           `json:"poll_interval_seconds"`
}

type EnableAutoRegistrationRequest struct {
	ProjectName         string   `json:"project_name"`
	FolderMirrorKind    string   `json:"folder_mirror_kind"`
	TableTagFilters     []string `json:"table_tag_filters"`
	PollIntervalSeconds uint64   `json:"poll_interval_seconds"`
}

type AutoRegisterRun struct {
	ID         uuid.UUID       `json:"id"`
	SourceRID  string          `json:"source_rid"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	Status     string          `json:"status"`
	Added      int32           `json:"added"`
	Updated    int32           `json:"updated"`
	Orphaned   int32           `json:"orphaned"`
	Errors     json.RawMessage `json:"errors"`
}

type AutoRegistrationSettingsView struct {
	Enabled          bool     `json:"enabled"`
	RegistrationMode string   `json:"registration_mode"`
	AutoSync         bool     `json:"auto_sync"`
	UpdateDetection  bool     `json:"update_detection"`
	Selectors        []string `json:"selectors"`
}

type Version struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

type PollOutcome string

const (
	PollOutcomeInitial         PollOutcome = "initial"
	PollOutcomeChanged         PollOutcome = "changed"
	PollOutcomeUnchanged       PollOutcome = "unchanged"
	PollOutcomePotentialUpdate PollOutcome = "potential_update"
	PollOutcomeFailed          PollOutcome = "failed"
)

type UpdateDetectionToggle struct {
	Enabled         bool   `json:"enabled"`
	IntervalSeconds uint64 `json:"interval_seconds"`
}

type PollResult struct {
	VirtualTableRID  string                            `json:"virtual_table_rid"`
	Outcome          PollOutcome                       `json:"outcome"`
	ObservedVersion  *string                           `json:"observed_version"`
	PreviousVersion  *string                           `json:"previous_version"`
	LatencyMS        int32                             `json:"latency_ms"`
	ChangeDetected   bool                              `json:"change_detected"`
	EventEmitted     bool                              `json:"event_emitted"`
	DownstreamBuilds []VirtualTableDownstreamBuildPlan `json:"downstream_builds,omitempty"`
}

type PollHistoryRow struct {
	ID              uuid.UUID `json:"id"`
	VirtualTableID  uuid.UUID `json:"virtual_table_id"`
	PolledAt        time.Time `json:"polled_at"`
	ObservedVersion *string   `json:"observed_version"`
	ChangeDetected  bool      `json:"change_detected"`
	LatencyMS       int32     `json:"latency_ms"`
	ErrorMessage    *string   `json:"error_message"`
}

type VirtualTableLineageNode struct {
	RID         string          `json:"rid"`
	Kind        string          `json:"kind"`
	DisplayName string          `json:"display_name"`
	Status      string          `json:"status"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

type VirtualTableLineageEdge struct {
	FromRID string `json:"from_rid"`
	ToRID   string `json:"to_rid"`
	Kind    string `json:"kind"`
}

type VirtualTableDownstreamBuildPlan struct {
	TargetRID   string `json:"target_rid"`
	TargetKind  string `json:"target_kind"`
	DisplayName string `json:"display_name"`
	Action      string `json:"action"`
	Reason      string `json:"reason"`
}

type VirtualTableLineageResponse struct {
	VirtualTableRID        string                            `json:"virtual_table_rid"`
	SourceRID              string                            `json:"source_rid"`
	UpdateDetectionEnabled bool                              `json:"update_detection_enabled"`
	LastObservedVersion    *string                           `json:"last_observed_version"`
	Nodes                  []VirtualTableLineageNode         `json:"nodes"`
	Edges                  []VirtualTableLineageEdge         `json:"edges"`
	DownstreamBuilds       []VirtualTableDownstreamBuildPlan `json:"downstream_builds"`
}

type ExportControls struct {
	AllowFoundryInputs   bool     `json:"allow_foundry_inputs"`
	AllowedMarkings      []string `json:"allowed_markings"`
	AllowedOrganizations []string `json:"allowed_organizations"`
}

type CodeRepositorySourceImport struct {
	RepositoryRID   string     `json:"repository_rid"`
	RepositoryName  string     `json:"repository_name"`
	FilePath        *string    `json:"file_path,omitempty"`
	URL             *string    `json:"url,omitempty"`
	ImportedName    string     `json:"imported_name"`
	LastImportedAt  *time.Time `json:"last_imported_at,omitempty"`
	RenderedLink    string     `json:"rendered_link,omitempty"`
	RenderedDisplay string     `json:"rendered_display,omitempty"`
}

type SourceGeneratedBinding struct {
	Library        string `json:"library"`
	ImportLine     string `json:"import_line"`
	Decorator      string `json:"decorator"`
	SourceRID      string `json:"source_rid"`
	ParameterName  string `json:"parameter_name"`
	FriendlyName   string `json:"friendly_name"`
	CodeSnippet    string `json:"code_snippet"`
	SourcePanelURL string `json:"source_panel_url"`
}

type ExternalTransformPattern struct {
	ID                     string   `json:"id"`
	Title                  string   `json:"title"`
	Summary                string   `json:"summary"`
	AlternativeFor         []string `json:"alternative_for"`
	ExampleKind            string   `json:"example_kind"`
	Runtime                string   `json:"runtime"`
	RequiresSourceImport   bool     `json:"requires_source_import"`
	RequiresFoundryInput   bool     `json:"requires_foundry_input"`
	RequiresExportControls bool     `json:"requires_export_controls"`
	RequiresAgentProxy     bool     `json:"requires_agent_proxy"`
	SourceRequirements     []string `json:"source_requirements"`
	RecommendedWhen        []string `json:"recommended_when"`
	Limitations            []string `json:"limitations"`
	CodeSnippet            string   `json:"code_snippet"`
	DocsURL                string   `json:"docs_url"`
}

type ComputeModuleAlternative struct {
	ID                   string   `json:"id"`
	Title                string   `json:"title"`
	Summary              string   `json:"summary"`
	AlternativeFor       string   `json:"alternative_for"`
	RuntimeKind          string   `json:"runtime_kind"`
	Status               string   `json:"status"`
	SupportedLanguages   []string `json:"supported_languages"`
	RequiredContracts    []string `json:"required_contracts"`
	Blockers             []string `json:"blockers"`
	ReadinessChecks      []string `json:"readiness_checks"`
	SourceRID            string   `json:"source_rid"`
	SourceImportContract string   `json:"source_import_contract"`
	DeploymentContract   string   `json:"deployment_contract"`
	ExecutionContract    string   `json:"execution_contract"`
	CodeSketch           string   `json:"code_sketch"`
	DocsURL              string   `json:"docs_url"`
}

type SourceCodeImportWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type SourceCodeImportFoundryInput struct {
	RID           string   `json:"rid"`
	DisplayName   string   `json:"display_name,omitempty"`
	ResourceType  string   `json:"resource_type,omitempty"`
	Markings      []string `json:"markings"`
	Organizations []string `json:"organizations"`
}

type SourceCodeImportExportPolicyDecision struct {
	Status                string                         `json:"status"`
	BuildAllowed          bool                           `json:"build_allowed"`
	UsesFoundryInputs     bool                           `json:"uses_foundry_inputs"`
	AllowFoundryInputs    bool                           `json:"allow_foundry_inputs"`
	FoundryInputs         []SourceCodeImportFoundryInput `json:"foundry_inputs"`
	MatchedMarkings       []string                       `json:"matched_markings,omitempty"`
	MissingMarkings       []string                       `json:"missing_markings,omitempty"`
	MatchedOrganizations  []string                       `json:"matched_organizations,omitempty"`
	MissingOrganizations  []string                       `json:"missing_organizations,omitempty"`
	BlockingReasons       []SourceCodeImportWarning      `json:"blocking_reasons,omitempty"`
	OwnerApprovalRequired bool                           `json:"owner_approval_required"`
}

type SourceCredentialBinding struct {
	CredentialID uuid.UUID `json:"credential_id"`
	Kind         string    `json:"kind"`
	Fingerprint  string    `json:"fingerprint"`
	CreatedAt    time.Time `json:"created_at"`
}

type SourceEgressPolicyBinding struct {
	PolicyID uuid.UUID `json:"policy_id"`
	Kind     string    `json:"kind"`
}

type SourceCodeImportBuildResolution struct {
	SourceID              uuid.UUID                            `json:"source_id"`
	SourceRID             string                               `json:"source_rid"`
	SourceName            string                               `json:"source_name"`
	ConnectorType         string                               `json:"connector_type"`
	PythonIdentifier      string                               `json:"python_identifier"`
	FriendlyName          string                               `json:"friendly_name"`
	BuildRID              *string                              `json:"build_rid,omitempty"`
	RepositoryRID         *string                              `json:"repository_rid,omitempty"`
	Branch                *string                              `json:"branch,omitempty"`
	ResolvedAt            time.Time                            `json:"resolved_at"`
	SourceUpdatedAt       time.Time                            `json:"source_updated_at"`
	ConfigHash            string                               `json:"config_hash"`
	CredentialBindings    []SourceCredentialBinding            `json:"credential_bindings"`
	EgressPolicyBindings  []SourceEgressPolicyBinding          `json:"egress_policy_bindings"`
	ExportControls        ExportControls                       `json:"export_controls"`
	ExportPolicyDecision  SourceCodeImportExportPolicyDecision `json:"export_policy_decision"`
	UsesLiveConfiguration bool                                 `json:"uses_live_configuration"`
	NoCodeChangeRequired  bool                                 `json:"no_code_change_required"`
	GeneratedBinding      SourceGeneratedBinding               `json:"generated_binding"`
	Warnings              []SourceCodeImportWarning            `json:"warnings,omitempty"`
}

type SourceCodeImport struct {
	SourceID                  uuid.UUID                       `json:"source_id"`
	SourceRID                 string                          `json:"source_rid"`
	SourceName                string                          `json:"source_name"`
	ConnectorType             string                          `json:"connector_type"`
	Enabled                   bool                            `json:"enabled"`
	FriendlyName              string                          `json:"friendly_name"`
	PythonIdentifier          string                          `json:"python_identifier"`
	GeneratedBinding          SourceGeneratedBinding          `json:"generated_binding"`
	CodeRepositories          []CodeRepositorySourceImport    `json:"code_repositories"`
	ExportControls            ExportControls                  `json:"export_controls"`
	ExternalTransformPatterns []ExternalTransformPattern      `json:"external_transform_patterns"`
	ComputeModuleAlternatives []ComputeModuleAlternative      `json:"compute_module_alternatives"`
	BuildStartResolution      SourceCodeImportBuildResolution `json:"build_start_resolution"`
	Warnings                  []SourceCodeImportWarning       `json:"warnings,omitempty"`
	CreatedAt                 time.Time                       `json:"created_at"`
	UpdatedAt                 time.Time                       `json:"updated_at"`
}

type UpdateSourceCodeImportRequest struct {
	Enabled          *bool                        `json:"enabled,omitempty"`
	FriendlyName     *string                      `json:"friendly_name,omitempty"`
	PythonIdentifier *string                      `json:"python_identifier,omitempty"`
	CodeRepositories []CodeRepositorySourceImport `json:"code_repositories,omitempty"`
	ExportControls   *ExportControls              `json:"export_controls,omitempty"`
}

type ResolveSourceCodeImportBuildRequest struct {
	RepositoryRID     *string                        `json:"repository_rid,omitempty"`
	BuildRID          *string                        `json:"build_rid,omitempty"`
	Branch            *string                        `json:"branch,omitempty"`
	UsesFoundryInputs *bool                          `json:"uses_foundry_inputs,omitempty"`
	FoundryInputs     []SourceCodeImportFoundryInput `json:"foundry_inputs,omitempty"`
}

type ToggleCodeImportsRequest struct {
	Enabled        bool           `json:"enabled"`
	ExportControls ExportControls `json:"export_controls"`
}

func SourceRIDForConnection(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return "ri.foundry.main.source." + id.String()
}

func NormalizeExportControls(controls ExportControls) ExportControls {
	return ExportControls{
		AllowFoundryInputs:   controls.AllowFoundryInputs,
		AllowedMarkings:      normalizeUniqueStrings(controls.AllowedMarkings),
		AllowedOrganizations: normalizeUniqueStrings(controls.AllowedOrganizations),
	}
}

func NormalizeSourceCodeImportFoundryInputs(inputs []SourceCodeImportFoundryInput) []SourceCodeImportFoundryInput {
	out := make([]SourceCodeImportFoundryInput, 0, len(inputs))
	seen := map[string]bool{}
	for _, input := range inputs {
		input.RID = strings.TrimSpace(input.RID)
		input.DisplayName = strings.TrimSpace(input.DisplayName)
		input.ResourceType = strings.TrimSpace(input.ResourceType)
		input.Markings = normalizeUniqueStrings(input.Markings)
		input.Organizations = normalizeUniqueStrings(input.Organizations)
		if input.RID == "" && input.DisplayName == "" {
			continue
		}
		key := strings.ToLower(input.RID + "\x00" + input.DisplayName)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, input)
	}
	return out
}

func ResolveSourceCodeImportExportPolicy(controls ExportControls, usesFoundryInputs bool, foundryInputs []SourceCodeImportFoundryInput) SourceCodeImportExportPolicyDecision {
	controls = NormalizeExportControls(controls)
	foundryInputs = NormalizeSourceCodeImportFoundryInputs(foundryInputs)
	if len(foundryInputs) > 0 {
		usesFoundryInputs = true
	}
	decision := SourceCodeImportExportPolicyDecision{
		Status:             "not_applicable",
		BuildAllowed:       true,
		UsesFoundryInputs:  usesFoundryInputs,
		AllowFoundryInputs: controls.AllowFoundryInputs,
		FoundryInputs:      foundryInputs,
	}
	if !usesFoundryInputs {
		return decision
	}
	decision.Status = "allowed"
	if !controls.AllowFoundryInputs {
		decision.OwnerApprovalRequired = true
		decision.BlockingReasons = append(decision.BlockingReasons, SourceCodeImportWarning{
			Code:     "source-export-controls-disabled",
			Severity: "error",
			Message:  "Source owner has not enabled Foundry inputs for jobs with access to this external system.",
		})
	}
	if len(controls.AllowedMarkings) == 0 && len(controls.AllowedOrganizations) == 0 {
		decision.OwnerApprovalRequired = true
		decision.BlockingReasons = append(decision.BlockingReasons, SourceCodeImportWarning{
			Code:     "source-export-controls-empty-policy",
			Severity: "error",
			Message:  "Source export policy has no exportable markings or organizations configured.",
		})
	}
	allowedMarkings := lowerSet(controls.AllowedMarkings)
	allowedOrganizations := lowerSet(controls.AllowedOrganizations)
	matchedMarkings := []string{}
	missingMarkings := []string{}
	matchedOrganizations := []string{}
	missingOrganizations := []string{}
	for _, input := range foundryInputs {
		for _, marking := range input.Markings {
			if allowedMarkings[strings.ToLower(marking)] {
				matchedMarkings = append(matchedMarkings, marking)
			} else {
				missingMarkings = append(missingMarkings, scopedInputToken(input, marking))
			}
		}
		if len(allowedOrganizations) > 0 {
			if len(input.Organizations) == 0 {
				missingOrganizations = append(missingOrganizations, scopedInputToken(input, "<no organization>"))
			}
			for _, organization := range input.Organizations {
				if allowedOrganizations[strings.ToLower(organization)] {
					matchedOrganizations = append(matchedOrganizations, organization)
				} else {
					missingOrganizations = append(missingOrganizations, scopedInputToken(input, organization))
				}
			}
		} else {
			for _, organization := range input.Organizations {
				missingOrganizations = append(missingOrganizations, scopedInputToken(input, organization))
			}
		}
	}
	decision.MatchedMarkings = normalizeUniqueStrings(matchedMarkings)
	decision.MissingMarkings = normalizeUniqueStrings(missingMarkings)
	decision.MatchedOrganizations = normalizeUniqueStrings(matchedOrganizations)
	decision.MissingOrganizations = normalizeUniqueStrings(missingOrganizations)
	if len(decision.MissingMarkings) > 0 {
		decision.BlockingReasons = append(decision.BlockingReasons, SourceCodeImportWarning{
			Code:     "source-export-controls-marking-denied",
			Severity: "error",
			Message:  "One or more Foundry input markings are not exportable to this source.",
		})
	}
	if len(decision.MissingOrganizations) > 0 {
		decision.BlockingReasons = append(decision.BlockingReasons, SourceCodeImportWarning{
			Code:     "source-export-controls-organization-denied",
			Severity: "error",
			Message:  "One or more Foundry input organizations are not exportable to this source.",
		})
	}
	if len(decision.BlockingReasons) > 0 {
		decision.Status = "blocked"
		decision.BuildAllowed = false
	}
	return decision
}

func lowerSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		out[strings.ToLower(clean)] = true
	}
	return out
}

func scopedInputToken(input SourceCodeImportFoundryInput, value string) string {
	label := strings.TrimSpace(input.DisplayName)
	if label == "" {
		label = strings.TrimSpace(input.RID)
	}
	if label == "" {
		return value
	}
	return label + ":" + value
}

func NormalizeCodeRepositories(repos []CodeRepositorySourceImport, pythonIdentifier string) []CodeRepositorySourceImport {
	out := make([]CodeRepositorySourceImport, 0, len(repos))
	seen := map[string]bool{}
	for _, repo := range repos {
		repo.RepositoryRID = strings.TrimSpace(repo.RepositoryRID)
		repo.RepositoryName = strings.TrimSpace(repo.RepositoryName)
		repo.ImportedName = PythonIdentifier(repo.ImportedName, pythonIdentifier)
		if repo.FilePath != nil {
			clean := strings.TrimSpace(*repo.FilePath)
			if clean == "" {
				repo.FilePath = nil
			} else {
				repo.FilePath = &clean
			}
		}
		if repo.URL != nil {
			clean := strings.TrimSpace(*repo.URL)
			if clean == "" {
				repo.URL = nil
			} else {
				repo.URL = &clean
			}
		}
		if repo.RepositoryName == "" && repo.RepositoryRID != "" {
			repo.RepositoryName = repo.RepositoryRID
		}
		if repo.RepositoryRID == "" && repo.RepositoryName == "" {
			continue
		}
		key := strings.ToLower(repo.RepositoryRID + "\x00" + repo.RepositoryName + "\x00" + trimStringPtr(repo.FilePath))
		if seen[key] {
			continue
		}
		seen[key] = true
		if repo.URL != nil {
			repo.RenderedLink = *repo.URL
		} else if repo.RepositoryRID != "" {
			repo.RenderedLink = "/code-repos/" + repo.RepositoryRID
		}
		repo.RenderedDisplay = repo.RepositoryName
		if repo.FilePath != nil {
			repo.RenderedDisplay += " · " + *repo.FilePath
		}
		out = append(out, repo)
	}
	return out
}

func PythonIdentifier(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	if value == "" {
		value = "source"
	}
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if b.Len() == 0 {
				b.WriteString("source_")
			}
			b.WriteRune(r)
		case r == '_':
			if b.Len() == 0 {
				b.WriteString("source")
			}
			b.WriteRune(r)
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "_") {
				b.WriteRune('_')
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "source"
	}
	if pythonKeywords[out] {
		out += "_source"
	}
	return out
}

func SourceBindingSnippet(sourceRID string, friendlyName string, pythonIdentifier string) SourceGeneratedBinding {
	pythonIdentifier = PythonIdentifier(pythonIdentifier, friendlyName)
	sourceRID = strings.TrimSpace(sourceRID)
	friendlyName = strings.TrimSpace(friendlyName)
	if friendlyName == "" {
		friendlyName = pythonIdentifier
	}
	decorator := fmt.Sprintf("@external_systems(\n    %s=Source(%q)\n)", pythonIdentifier, sourceRID)
	snippet := strings.Join([]string{
		"from transforms.api import Output, transform_pandas",
		"from transforms.external.systems import Source, ResolvedSource, external_systems",
		"",
		decorator,
		"@transform_pandas(Output(\"<output_dataset_rid>\"))",
		fmt.Sprintf("def compute(%s: ResolvedSource):", pythonIdentifier),
		fmt.Sprintf("    client = %s.get_https_connection().get_client()", pythonIdentifier),
		fmt.Sprintf("    base_url = %s.get_https_connection().url", pythonIdentifier),
		"    ...",
	}, "\n")
	return SourceGeneratedBinding{
		Library:        "transforms-external-systems",
		ImportLine:     "from transforms.external.systems import Source, ResolvedSource, external_systems",
		Decorator:      decorator,
		SourceRID:      sourceRID,
		ParameterName:  pythonIdentifier,
		FriendlyName:   friendlyName,
		CodeSnippet:    snippet,
		SourcePanelURL: "/data-connection/sources/" + strings.TrimPrefix(sourceRID, "ri.foundry.main.source."),
	}
}

func ExternalTransformPatternsForSource(sourceRID string, friendlyName string, pythonIdentifier string, controls ExportControls) []ExternalTransformPattern {
	binding := SourceBindingSnippet(sourceRID, friendlyName, pythonIdentifier)
	alias := binding.ParameterName
	sourceExpr := fmt.Sprintf("%s=Source(%q)", alias, binding.SourceRID)
	exportControlStatus := "source owner must enable Foundry inputs before combining Foundry inputs with this source"
	controls = NormalizeExportControls(controls)
	if controls.AllowFoundryInputs && (len(controls.AllowedMarkings) > 0 || len(controls.AllowedOrganizations) > 0) {
		exportControlStatus = fmt.Sprintf("export controls configured for markings [%s] and organizations [%s]", strings.Join(controls.AllowedMarkings, ", "), strings.Join(controls.AllowedOrganizations, ", "))
	} else if controls.AllowFoundryInputs {
		exportControlStatus = "Foundry inputs are enabled but exportable markings and organizations still need review"
	}
	return []ExternalTransformPattern{
		{
			ID:                     "rest-api-batch-sync",
			Title:                  "REST API batch sync",
			Summary:                "Read pages from an imported REST API source and write a schema-bearing dataset.",
			AlternativeFor:         []string{"batch_sync"},
			ExampleKind:            "rest_api",
			Runtime:                "python_transform",
			RequiresSourceImport:   true,
			RequiresFoundryInput:   false,
			RequiresExportControls: false,
			SourceRequirements:     []string{"Foundry worker source", "single HTTPS connection on the source", "transforms-external-systems library"},
			RecommendedWhen:        []string{"The connector UI lacks a dedicated endpoint option", "custom pagination or response shaping is required"},
			Limitations:            []string{"HTTP retries and pagination are authored in code", "large responses should be chunked before dataframe creation"},
			DocsURL:                "https://www.palantir.com/docs/foundry/data-connection/external-transforms",
			CodeSnippet: strings.Join([]string{
				"from transforms.api import Output, transform_pandas",
				"from transforms.external.systems import Source, external_systems",
				"import pandas as pd",
				"",
				"@external_systems(" + sourceExpr + ")",
				"@transform_pandas(Output(\"<output_dataset_rid>\"))",
				fmt.Sprintf("def compute(%s):", alias),
				fmt.Sprintf("    connection = %s.get_https_connection()", alias),
				"    client = connection.get_client()",
				"    response = client.get(connection.url + \"/records\", timeout=30)",
				"    response.raise_for_status()",
				"    return pd.DataFrame(response.json().get(\"items\", []))",
			}, "\n"),
		},
		{
			ID:                     "database-table-batch-sync",
			Title:                  "Database table batch sync",
			Summary:                "Use a database client when the source needs custom SQL, joins, predicates, or driver behavior.",
			AlternativeFor:         []string{"batch_sync", "table_batch_sync"},
			ExampleKind:            "database",
			Runtime:                "python_transform",
			RequiresSourceImport:   true,
			RequiresFoundryInput:   false,
			RequiresExportControls: false,
			SourceRequirements:     []string{"Database credentials stored as source secrets", "network path through direct egress or agent proxy"},
			RecommendedWhen:        []string{"The table sync UI cannot express the desired query", "the source connector does not support table syncs yet"},
			Limitations:            []string{"Non-secret connection attributes may need repository configuration", "transactions and incremental watermarks are authored by the transform"},
			DocsURL:                "https://www.palantir.com/docs/foundry/data-connection/external-transforms",
			CodeSnippet: strings.Join([]string{
				"from transforms.api import Output, transform_pandas",
				"from transforms.external.systems import Source, external_systems",
				"import pandas as pd",
				"from sqlalchemy import create_engine, text",
				"",
				"@external_systems(" + sourceExpr + ")",
				"@transform_pandas(Output(\"<output_dataset_rid>\"))",
				fmt.Sprintf("def compute(%s):", alias),
				fmt.Sprintf("    username = %s.get_secret(\"username\")", alias),
				fmt.Sprintf("    password = %s.get_secret(\"password\")", alias),
				"    engine = create_engine(f\"postgresql://{username}:{password}@<host>:5432/<db>\")",
				"    return pd.read_sql_query(text(\"select * from public.orders where updated_at >= :watermark\"), engine, params={\"watermark\": \"2026-01-01\"})",
			}, "\n"),
		},
		{
			ID:                     "buffered-parquet-batch-sync",
			Title:                  "Buffered Parquet batch sync",
			Summary:                "Buffer API or file pages into Parquet before committing a dataset output.",
			AlternativeFor:         []string{"batch_sync"},
			ExampleKind:            "buffered_parquet",
			Runtime:                "python_transform",
			RequiresSourceImport:   true,
			RequiresFoundryInput:   false,
			RequiresExportControls: false,
			SourceRequirements:     []string{"Source client can page or stream records", "transform has enough local disk for temporary Parquet parts"},
			RecommendedWhen:        []string{"The source returns many small pages", "records need normalization before dataset commit"},
			Limitations:            []string{"Temporary files count against transform worker storage", "schema drift should be handled before writing Parquet"},
			DocsURL:                "https://www.palantir.com/docs/foundry/data-connection/external-transforms",
			CodeSnippet: strings.Join([]string{
				"from transforms.api import Output, transform",
				"from transforms.external.systems import Source, external_systems",
				"import pandas as pd",
				"import tempfile",
				"",
				"@external_systems(" + sourceExpr + ")",
				"@transform(out=Output(\"<output_dataset_rid>\"))",
				fmt.Sprintf("def compute(ctx, out, %s):", alias),
				fmt.Sprintf("    connection = %s.get_https_connection()", alias),
				"    client = connection.get_client()",
				"    with tempfile.TemporaryDirectory() as tmp:",
				"        for page in range(0, 10):",
				"            records = client.get(f\"{connection.url}/records?page={page}\", timeout=30).json().get(\"items\", [])",
				"            pd.DataFrame(records).to_parquet(f\"{tmp}/part-{page:05d}.parquet\", index=False)",
				"        out.write_dataframe(ctx.spark.read.parquet(tmp))",
			}, "\n"),
		},
		{
			ID:                     "csv-file-export",
			Title:                  "CSV file export",
			Summary:                "Read a Foundry dataset, render CSV, and write it to an external filesystem or API.",
			AlternativeFor:         []string{"file_export"},
			ExampleKind:            "csv_export",
			Runtime:                "python_transform",
			RequiresSourceImport:   true,
			RequiresFoundryInput:   true,
			RequiresExportControls: true,
			SourceRequirements:     []string{exportControlStatus, "external destination accepts file upload or object writes"},
			RecommendedWhen:        []string{"The file export UI cannot produce the required CSV format", "custom partitioning, names, headers, or encoding are needed"},
			Limitations:            []string{"Large datasets should stream or partition output instead of collecting to one pandas frame", "export governance is enforced through source export controls"},
			DocsURL:                "https://www.palantir.com/docs/foundry/data-connection/external-transforms",
			CodeSnippet: strings.Join([]string{
				"from transforms.api import Input, Output, transform",
				"from transforms.external.systems import Source, external_systems",
				"",
				"@external_systems(" + sourceExpr + ")",
				"@transform(audit=Output(\"<audit_dataset_rid>\"), rows=Input(\"<input_dataset_rid>\"))",
				fmt.Sprintf("def compute(ctx, audit, rows, %s):", alias),
				"    csv_body = rows.dataframe().toPandas().to_csv(index=False)",
				fmt.Sprintf("    connection = %s.get_https_connection()", alias),
				"    response = connection.get_client().put(connection.url + \"/exports/orders.csv\", data=csv_body.encode(\"utf-8\"), timeout=60)",
				"    response.raise_for_status()",
				"    audit.write_dataframe(ctx.spark.createDataFrame([{\"path\": \"orders.csv\", \"status\": \"uploaded\"}]))",
			}, "\n"),
		},
		{
			ID:                     "database-table-export",
			Title:                  "Database table export",
			Summary:                "Write schema-bearing Foundry rows into an external database table with custom SQL behavior.",
			AlternativeFor:         []string{"table_export"},
			ExampleKind:            "database",
			Runtime:                "python_transform",
			RequiresSourceImport:   true,
			RequiresFoundryInput:   true,
			RequiresExportControls: true,
			SourceRequirements:     []string{exportControlStatus, "database write credentials available as source secrets"},
			RecommendedWhen:        []string{"The table export UI cannot express merge/upsert logic", "database-specific staging or stored procedures are required"},
			Limitations:            []string{"The transform owns idempotency and rollback strategy in the external database", "nested types should be flattened before export"},
			DocsURL:                "https://www.palantir.com/docs/foundry/data-connection/external-transforms",
			CodeSnippet: strings.Join([]string{
				"from transforms.api import Input, Output, transform",
				"from transforms.external.systems import Source, external_systems",
				"from sqlalchemy import create_engine",
				"",
				"@external_systems(" + sourceExpr + ")",
				"@transform(audit=Output(\"<audit_dataset_rid>\"), rows=Input(\"<input_dataset_rid>\"))",
				fmt.Sprintf("def compute(ctx, audit, rows, %s):", alias),
				fmt.Sprintf("    username = %s.get_secret(\"username\")", alias),
				fmt.Sprintf("    password = %s.get_secret(\"password\")", alias),
				"    engine = create_engine(f\"postgresql://{username}:{password}@<host>:5432/<db>\")",
				"    rows.dataframe().toPandas().to_sql(\"orders_export\", engine, schema=\"public\", if_exists=\"append\", index=False)",
				"    audit.write_dataframe(ctx.spark.createDataFrame([{\"table\": \"public.orders_export\", \"status\": \"written\"}]))",
			}, "\n"),
		},
		{
			ID:                     "media-sync-handoff",
			Title:                  "Media sync handoff",
			Summary:                "Create a manifest or dispatch files for downstream media-set ingestion when a native media sync is not available.",
			AlternativeFor:         []string{"media_sync_handoff"},
			ExampleKind:            "media_sync",
			Runtime:                "python_transform",
			RequiresSourceImport:   true,
			RequiresFoundryInput:   false,
			RequiresExportControls: false,
			SourceRequirements:     []string{"external source can list media objects", "target media set RID or downstream ingestion handoff is configured"},
			RecommendedWhen:        []string{"A connector can enumerate media but does not support point-and-click media sync", "files require custom filtering or metadata enrichment"},
			Limitations:            []string{"Binary transfer should use a media-set runtime where available", "the manifest must preserve URI, checksum, size, and MIME metadata"},
			DocsURL:                "https://www.palantir.com/docs/foundry/data-connection/core-concepts",
			CodeSnippet: strings.Join([]string{
				"from transforms.api import Output, transform_pandas",
				"from transforms.external.systems import Source, external_systems",
				"import pandas as pd",
				"",
				"@external_systems(" + sourceExpr + ")",
				"@transform_pandas(Output(\"<media_handoff_manifest_dataset_rid>\"))",
				fmt.Sprintf("def compute(%s):", alias),
				fmt.Sprintf("    client = %s.get_https_connection().get_client()", alias),
				"    files = client.get(\"/media/files\", timeout=30).json().get(\"items\", [])",
				"    return pd.DataFrame([{\"external_uri\": item[\"uri\"], \"mime_type\": item.get(\"mime_type\"), \"checksum\": item.get(\"sha256\")} for item in files])",
			}, "\n"),
		},
		{
			ID:                     "virtual-table-registration",
			Title:                  "Virtual table registration",
			Summary:                "Generate registration requests for external tables that should remain stored in the source system.",
			AlternativeFor:         []string{"virtual_table_registration"},
			ExampleKind:            "virtual_table_registration",
			Runtime:                "python_transform",
			RequiresSourceImport:   true,
			RequiresFoundryInput:   false,
			RequiresExportControls: false,
			SourceRequirements:     []string{"source supports virtual tables", "external database, schema, and table references are known"},
			RecommendedWhen:        []string{"registration needs custom discovery or filtering", "bulk registration policy must be generated from source metadata"},
			Limitations:            []string{"Registration should be reviewed before exposing broad schemas", "unsupported source systems still need a connector-level virtual table provider"},
			DocsURL:                "https://www.palantir.com/docs/foundry/data-connection/core-concepts",
			CodeSnippet: strings.Join([]string{
				"import requests",
				"",
				"registration = {",
				"    \"project_rid\": \"<save_project_rid>\",",
				"    \"display_name\": \"orders_live\",",
				"    \"locator\": {\"kind\": \"tabular\", \"database\": \"WAREHOUSE\", \"schema\": \"PUBLIC\", \"table\": \"ORDERS\"},",
				"    \"table_type\": \"TABLE\",",
				"}",
				fmt.Sprintf("requests.post(\"<openfoundry_api>/api/v1/sources/%s/virtual-tables/register\", json=registration, timeout=30).raise_for_status()", binding.SourceRID),
			}, "\n"),
		},
		{
			ID:                     "virtual-media-registration",
			Title:                  "Virtual media registration",
			Summary:                "Emit a virtual-media registration payload that keeps media bytes in the external system.",
			AlternativeFor:         []string{"virtual_media_registration"},
			ExampleKind:            "virtual_media_registration",
			Runtime:                "python_transform",
			RequiresSourceImport:   true,
			RequiresFoundryInput:   false,
			RequiresExportControls: false,
			SourceRequirements:     []string{"external media URLs are stable", "metadata includes MIME type, size, checksum, and object path"},
			RecommendedWhen:        []string{"media should be indexed without copying bytes", "the source has a custom object hierarchy"},
			Limitations:            []string{"Virtual media runtime availability is product-policy dependent", "access checks must be preserved when dereferencing external media"},
			DocsURL:                "https://www.palantir.com/docs/foundry/data-connection/core-concepts",
			CodeSnippet: strings.Join([]string{
				"virtual_media_item = {",
				fmt.Sprintf("    \"source_rid\": %q,", binding.SourceRID),
				"    \"external_uri\": \"s3://bucket/path/image.png\",",
				"    \"mime_type\": \"image/png\",",
				"    \"size_bytes\": 1048576,",
				"    \"checksum\": \"sha256:<digest>\",",
				"}",
				"# Submit this payload to the OpenFoundry virtual-media registration handoff when enabled.",
			}, "\n"),
		},
		{
			ID:                     "lightweight-rest-transform",
			Title:                  "Lightweight transform lookup",
			Summary:                "Use a small Python transform for lookups or enrichment when a full connector resource would be heavier than the workflow needs.",
			AlternativeFor:         []string{"batch_sync"},
			ExampleKind:            "lightweight_transform",
			Runtime:                "python_transform",
			RequiresSourceImport:   true,
			RequiresFoundryInput:   true,
			RequiresExportControls: true,
			SourceRequirements:     []string{exportControlStatus, "external request volume is bounded and suitable for transform execution"},
			RecommendedWhen:        []string{"A small Foundry input needs request-per-row enrichment", "the result can be recomputed during normal dataset builds"},
			Limitations:            []string{"Avoid unbounded request fan-out from large datasets", "cache or batch remote calls where possible"},
			DocsURL:                "https://www.palantir.com/docs/foundry/data-connection/external-transforms",
			CodeSnippet: strings.Join([]string{
				"from transforms.api import Input, Output, transform_pandas",
				"from transforms.external.systems import Source, external_systems",
				"import pandas as pd",
				"",
				"@external_systems(" + sourceExpr + ")",
				"@transform_pandas(Output(\"<output_dataset_rid>\"), rows=Input(\"<input_dataset_rid>\"))",
				fmt.Sprintf("def compute(rows: pd.DataFrame, %s):", alias),
				fmt.Sprintf("    client = %s.get_https_connection().get_client()", alias),
				"    enriched = []",
				"    for row in rows.head(1000).to_dict(\"records\"):",
				"        enriched.append({**row, \"lookup\": client.get(f\"/lookup/{row['id']}\", timeout=10).json()})",
				"    return pd.DataFrame(enriched)",
			}, "\n"),
		},
		{
			ID:                     "agent-proxy-private-network",
			Title:                  "Private network via agent proxy",
			Summary:                "Reach private hosts through an attached agent-proxy egress policy from source-imported code.",
			AlternativeFor:         []string{"batch_sync", "file_export", "table_export"},
			ExampleKind:            "agent_proxy",
			Runtime:                "python_transform",
			RequiresSourceImport:   true,
			RequiresFoundryInput:   false,
			RequiresExportControls: false,
			RequiresAgentProxy:     true,
			SourceRequirements:     []string{"agent-proxy egress policy attached to the source", "private host and port allowed by policy"},
			RecommendedWhen:        []string{"the external system is on-premises or private", "a source needs socket-based access instead of public Internet access"},
			Limitations:            []string{"Use the source-provided client or socket so proxy configuration is applied", "agent worker sources configure networking on the agent host instead"},
			DocsURL:                "https://www.palantir.com/docs/foundry/data-connection/external-transforms",
			CodeSnippet: strings.Join([]string{
				"from transforms.api import Output, transform_pandas",
				"from transforms.external.systems import Source, external_systems",
				"import pandas as pd",
				"",
				"@external_systems(" + sourceExpr + ")",
				"@transform_pandas(Output(\"<output_dataset_rid>\"))",
				fmt.Sprintf("def compute(%s):", alias),
				fmt.Sprintf("    proxy_socket = %s.create_socket(\"db.internal.example\", 5432)", alias),
				"    # Pass proxy_socket to a database, SFTP, or protocol client that accepts a preconnected socket.",
				"    return pd.DataFrame([{\"private_network\": \"reachable\"}])",
			}, "\n"),
		},
	}
}

func ComputeModuleAlternativesForSource(sourceRID string, friendlyName string, pythonIdentifier string) []ComputeModuleAlternative {
	binding := SourceBindingSnippet(sourceRID, friendlyName, pythonIdentifier)
	alias := binding.ParameterName
	blockers := []string{
		"compute_module_runtime",
		"compute_module_deployment_contract",
		"compute_module_source_import_contract",
	}
	requiredContracts := []string{
		"long_running_arbitrary_language_runtime",
		"deployment_and_rollout_contract",
		"source_import_binding_contract",
		"checkpoint_health_and_logs_contract",
	}
	readinessChecks := []string{
		"Long-running modules can start, stop, restart, and expose health.",
		"Deployments can roll versions and attach source-import bindings.",
		"Source imports resolve credentials, egress, and export controls inside the module.",
		"Checkpoint state is durable across restarts and scheduled resumes.",
	}
	docsURL := "https://www.palantir.com/docs/foundry/data-connection/core-concepts/"
	base := ComputeModuleAlternative{
		RuntimeKind:          "long_running_compute_module",
		Status:               "blocked",
		SupportedLanguages:   []string{"python", "typescript", "java", "go"},
		RequiredContracts:    requiredContracts,
		Blockers:             blockers,
		ReadinessChecks:      readinessChecks,
		SourceRID:            binding.SourceRID,
		SourceImportContract: "source imports must be injectable into long-running modules with the same credential, egress, and export-control semantics as Python transforms",
		DeploymentContract:   "modules need versioned deployment, rollout, start/stop, schedule restart, logs, and health APIs",
		ExecutionContract:    "modules need durable checkpoints, retry policy, backpressure, and dead-letter/error reporting contracts",
		DocsURL:              docsURL,
	}
	return []ComputeModuleAlternative{
		mergeComputeModuleAlternative(base, ComputeModuleAlternative{
			ID:             "compute-module-streaming-sync",
			Title:          "Streaming sync compute module",
			Summary:        "Run a long-lived connector loop that reads external records and writes them into an OpenFoundry stream.",
			AlternativeFor: "streaming_sync",
			CodeSketch: strings.Join([]string{
				"module:",
				"  runtime: long_running_compute_module",
				"  source_imports:",
				fmt.Sprintf("    %s: %s", alias, binding.SourceRID),
				"  checkpoints:",
				"    stream_offset: durable",
				"loop:",
				fmt.Sprintf("  client = source_import(%q).open_stream_client()", binding.SourceRID),
				"  for record in client.read_from(checkpoint(\"stream_offset\")):",
				"    openfoundry.stream(\"<output_stream_rid>\").write(record)",
				"    checkpoint(\"stream_offset\", record.offset)",
			}, "\n"),
		}),
		mergeComputeModuleAlternative(base, ComputeModuleAlternative{
			ID:             "compute-module-streaming-export",
			Title:          "Streaming export compute module",
			Summary:        "Continuously read an OpenFoundry stream and export records to an external queue or topic.",
			AlternativeFor: "streaming_export",
			CodeSketch: strings.Join([]string{
				"module:",
				"  runtime: long_running_compute_module",
				"  source_imports:",
				fmt.Sprintf("    %s: %s", alias, binding.SourceRID),
				"  replay_behavior: duplicate_or_skip_with_warning",
				"loop:",
				"  stream = openfoundry.stream(\"<input_stream_rid>\")",
				fmt.Sprintf("  producer = source_import(%q).open_topic_producer(\"<destination_topic>\")", binding.SourceRID),
				"  for event in stream.read_from(checkpoint(\"export_offset\")):",
				"    producer.send(event.key, event.value)",
				"    checkpoint(\"export_offset\", event.offset)",
			}, "\n"),
		}),
		mergeComputeModuleAlternative(base, ComputeModuleAlternative{
			ID:             "compute-module-cdc-sync",
			Title:          "CDC sync compute module",
			Summary:        "Read source-side changelog records and publish ordered CDC entries to an OpenFoundry changelog stream.",
			AlternativeFor: "cdc_sync",
			CodeSketch: strings.Join([]string{
				"module:",
				"  runtime: long_running_compute_module",
				"  source_imports:",
				fmt.Sprintf("    %s: %s", alias, binding.SourceRID),
				"  cdc:",
				"    primary_key_columns: [id]",
				"    ordering_column: updated_at",
				"    deletion_column: is_deleted",
				"loop:",
				fmt.Sprintf("  cdc = source_import(%q).open_changelog_cursor(\"<source_table>\")", binding.SourceRID),
				"  for change in cdc.read_from(checkpoint(\"cdc_position\")):",
				"    openfoundry.stream(\"<output_changelog_stream_rid>\").write(change)",
				"    checkpoint(\"cdc_position\", change.position)",
			}, "\n"),
		}),
		mergeComputeModuleAlternative(base, ComputeModuleAlternative{
			ID:             "compute-module-webhook",
			Title:          "Webhook compute module",
			Summary:        "Host a long-running arbitrary-language endpoint that validates webhook requests and performs source-backed work.",
			AlternativeFor: "webhook",
			CodeSketch: strings.Join([]string{
				"module:",
				"  runtime: long_running_compute_module",
				"  source_imports:",
				fmt.Sprintf("    %s: %s", alias, binding.SourceRID),
				"  endpoint: POST /webhooks/<listener>",
				"handler:",
				"  verify_signature(request.headers, request.body)",
				fmt.Sprintf("  client = source_import(%q).get_client()", binding.SourceRID),
				"  result = client.post(\"/events\", json=request.json())",
				"  openfoundry.webhook_ack(status=202, body={\"external_id\": result.id})",
			}, "\n"),
		}),
	}
}

func mergeComputeModuleAlternative(base ComputeModuleAlternative, override ComputeModuleAlternative) ComputeModuleAlternative {
	base.ID = override.ID
	base.Title = override.Title
	base.Summary = override.Summary
	base.AlternativeFor = override.AlternativeFor
	base.CodeSketch = override.CodeSketch
	return base
}

func SourceCodeImportWarnings(enabled bool, credentials []SourceCredentialBinding, egress []SourceEgressPolicyBinding, controls ExportControls, decisions ...SourceCodeImportExportPolicyDecision) []SourceCodeImportWarning {
	warnings := []SourceCodeImportWarning{}
	controls = NormalizeExportControls(controls)
	if !enabled {
		warnings = append(warnings, SourceCodeImportWarning{Code: "source-code-import-disabled", Severity: "warning", Message: "Source must be approved for code imports before Python transforms can import it."})
	}
	if len(egress) == 0 {
		warnings = append(warnings, SourceCodeImportWarning{Code: "source-code-import-no-egress-policy", Severity: "warning", Message: "No egress policy is attached; build-start resolution will not be able to reach private external systems."})
	}
	if len(credentials) == 0 {
		warnings = append(warnings, SourceCodeImportWarning{Code: "source-code-import-no-credentials", Severity: "warning", Message: "No credential reference is configured; only unauthenticated source clients can be initialized."})
	}
	blockedByDecision := false
	for _, decision := range decisions {
		if decision.Status == "blocked" {
			blockedByDecision = true
			break
		}
	}
	if !controls.AllowFoundryInputs && !blockedByDecision {
		warnings = append(warnings, SourceCodeImportWarning{Code: "source-export-controls-disabled", Severity: "warning", Message: "Foundry inputs are not enabled for jobs that import this source."})
	} else if controls.AllowFoundryInputs && len(controls.AllowedMarkings) == 0 && len(controls.AllowedOrganizations) == 0 && !blockedByDecision {
		warnings = append(warnings, SourceCodeImportWarning{Code: "source-export-controls-empty-policy", Severity: "warning", Message: "Foundry inputs are enabled but no exportable markings or organizations are configured."})
	}
	for _, decision := range decisions {
		if decision.Status == "blocked" {
			warnings = append(warnings, decision.BlockingReasons...)
		}
	}
	return warnings
}

func normalizeUniqueStrings(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, clean)
	}
	return out
}

func trimStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

var pythonKeywords = map[string]bool{
	"and": true, "as": true, "assert": true, "async": true, "await": true,
	"break": true, "class": true, "continue": true, "def": true, "del": true,
	"elif": true, "else": true, "except": true, "false": true, "finally": true,
	"for": true, "from": true, "global": true, "if": true, "import": true,
	"in": true, "is": true, "lambda": true, "none": true, "nonlocal": true,
	"not": true, "or": true, "pass": true, "raise": true, "return": true,
	"true": true, "try": true, "while": true, "with": true, "yield": true,
}

type ArrowType string

const (
	ArrowTypeBoolean   ArrowType = "boolean"
	ArrowTypeInt32     ArrowType = "int32"
	ArrowTypeInt64     ArrowType = "int64"
	ArrowTypeFloat32   ArrowType = "float32"
	ArrowTypeFloat64   ArrowType = "float64"
	ArrowTypeDecimal   ArrowType = "decimal"
	ArrowTypeUtf8      ArrowType = "utf8"
	ArrowTypeBinary    ArrowType = "binary"
	ArrowTypeDate32    ArrowType = "date32"
	ArrowTypeTimestamp ArrowType = "timestamp"
	ArrowTypeList      ArrowType = "list"
	ArrowTypeStruct    ArrowType = "struct"
)

type Mapping struct {
	Arrow   ArrowType `json:"arrow"`
	Warning *string   `json:"warning"`
}

type InferredColumn struct {
	Name         string `json:"name"`
	SourceType   string `json:"source_type"`
	InferredType string `json:"inferred_type"`
	Nullable     bool   `json:"nullable"`
}

type IcebergConfigResponse struct {
	Defaults  IcebergConfigValues `json:"defaults"`
	Overrides IcebergConfigValues `json:"overrides"`
}

type IcebergConfigValues struct {
	Warehouse string `json:"warehouse,omitempty"`
}

type IcebergListNamespacesResponse struct {
	Namespaces [][]string `json:"namespaces"`
}

type IcebergNamespaceResponse struct {
	Namespace  []string          `json:"namespace"`
	Properties map[string]string `json:"properties"`
}

type IcebergTableIdentifier struct {
	Namespace []string `json:"namespace"`
	Name      string   `json:"name"`
}

type IcebergListTablesResponse struct {
	Identifiers []IcebergTableIdentifier `json:"identifiers"`
}

type IcebergLoadTableResponse struct {
	MetadataLocation string          `json:"metadata-location"`
	Metadata         json.RawMessage `json:"metadata"`
	Config           json.RawMessage `json:"config"`
}

type WebhookDefinition struct {
	Name             string                   `json:"name,omitempty"`
	Description      string                   `json:"description,omitempty"`
	URL              string                   `json:"url,omitempty"`
	Method           string                   `json:"method,omitempty"`
	Path             string                   `json:"path,omitempty"`
	QueryParams      map[string]string        `json:"query_params,omitempty"`
	Headers          map[string]string        `json:"headers,omitempty"`
	Body             json.RawMessage          `json:"body,omitempty"`
	BodyTemplate     string                   `json:"body_template,omitempty"`
	InputSchema      json.RawMessage          `json:"input_schema,omitempty"`
	OutputSchema     json.RawMessage          `json:"output_schema,omitempty"`
	AuthRef          *string                  `json:"auth_ref,omitempty"`
	Inputs           []WebhookParameter       `json:"inputs,omitempty"`
	Calls            []WebhookCall            `json:"calls,omitempty"`
	Outputs          []WebhookOutputParameter `json:"outputs,omitempty"`
	TimeoutMS        int                      `json:"timeout_ms,omitempty"`
	ConcurrencyLimit int                      `json:"concurrency_limit,omitempty"`
	RateLimit        *WebhookRateLimit        `json:"rate_limit,omitempty"`
	Limits           WebhookInvocationLimits  `json:"limits,omitempty"`
	History          WebhookHistorySettings   `json:"history,omitempty"`
}

type InvokeWebhookRequest struct {
	Inputs json.RawMessage `json:"inputs"`
}

type InvokeWebhookResponse struct {
	Status           uint16          `json:"status"`
	Response         json.RawMessage `json:"response"`
	OutputParameters json.RawMessage `json:"output_parameters"`
	History          json.RawMessage `json:"history,omitempty"`
}

type WebhookHistoryInputPolicy struct {
	StoreInputs  bool   `json:"store_inputs"`
	StoreOutputs bool   `json:"store_outputs"`
	Visibility   string `json:"visibility"`
}

type WebhookHistoryEntry struct {
	ID                 uuid.UUID                 `json:"id"`
	SourceID           uuid.UUID                 `json:"source_id"`
	UserID             uuid.UUID                 `json:"user_id"`
	Status             string                    `json:"status"`
	HTTPStatus         *uint16                   `json:"http_status,omitempty"`
	InputPolicy        WebhookHistoryInputPolicy `json:"input_policy"`
	Inputs             json.RawMessage           `json:"inputs,omitempty"`
	OutputParameters   json.RawMessage           `json:"output_parameters,omitempty"`
	Error              *string                   `json:"error,omitempty"`
	CallCount          int                       `json:"call_count"`
	StartedAt          time.Time                 `json:"started_at"`
	FinishedAt         time.Time                 `json:"finished_at"`
	DurationMS         int64                     `json:"duration_ms"`
	RetentionExpiresAt time.Time                 `json:"retention_expires_at"`
	CreatedAt          time.Time                 `json:"created_at"`
}

type CreateWebhookHistoryEntry struct {
	SourceID           uuid.UUID                 `json:"source_id"`
	UserID             uuid.UUID                 `json:"user_id"`
	Status             string                    `json:"status"`
	HTTPStatus         *uint16                   `json:"http_status,omitempty"`
	InputPolicy        WebhookHistoryInputPolicy `json:"input_policy"`
	Inputs             json.RawMessage           `json:"inputs,omitempty"`
	OutputParameters   json.RawMessage           `json:"output_parameters,omitempty"`
	Error              *string                   `json:"error,omitempty"`
	CallCount          int                       `json:"call_count"`
	StartedAt          time.Time                 `json:"started_at"`
	FinishedAt         time.Time                 `json:"finished_at"`
	RetentionExpiresAt time.Time                 `json:"retention_expires_at"`
}

const DefaultInboundListenerMaxPayloadBytes = 1024 * 1024

type InboundListenerDefinition struct {
	ID          string                           `json:"id,omitempty"`
	Name        string                           `json:"name,omitempty"`
	Description string                           `json:"description,omitempty"`
	Type        string                           `json:"type,omitempty"`
	Enabled     bool                             `json:"enabled"`
	Auth        InboundListenerAuthConfig        `json:"auth,omitempty"`
	Destination InboundListenerDestinationConfig `json:"destination,omitempty"`
	Limits      InboundListenerLimits            `json:"limits,omitempty"`
	Metadata    map[string]json.RawMessage       `json:"metadata,omitempty"`
}

type InboundListenerAuthConfig struct {
	Type            string `json:"type,omitempty"`
	Header          string `json:"header,omitempty"`
	Secret          string `json:"secret,omitempty"`
	SecretRef       string `json:"secret_ref,omitempty"`
	TimestampHeader string `json:"timestamp_header,omitempty"`
}

type InboundListenerDestinationConfig struct {
	Mode         string     `json:"mode,omitempty"`
	DatasetID    *uuid.UUID `json:"dataset_id,omitempty"`
	ObjectTypeID *uuid.UUID `json:"object_type_id,omitempty"`
}

type InboundListenerLimits struct {
	MaxPayloadBytes int `json:"max_payload_bytes,omitempty"`
}

type InboundListenerEvent struct {
	ID                uuid.UUID                        `json:"id"`
	SourceID          uuid.UUID                        `json:"source_id"`
	ListenerID        string                           `json:"listener_id"`
	EventID           string                           `json:"event_id,omitempty"`
	Status            string                           `json:"status"`
	SignatureVerified bool                             `json:"signature_verified"`
	Payload           json.RawMessage                  `json:"payload,omitempty"`
	Headers           json.RawMessage                  `json:"headers,omitempty"`
	Destination       InboundListenerDestinationConfig `json:"destination,omitempty"`
	CreatedAt         time.Time                        `json:"created_at"`
}

type CreateInboundListenerEvent struct {
	SourceID          uuid.UUID                        `json:"source_id"`
	ListenerID        string                           `json:"listener_id"`
	EventID           string                           `json:"event_id,omitempty"`
	Status            string                           `json:"status"`
	SignatureVerified bool                             `json:"signature_verified"`
	Payload           json.RawMessage                  `json:"payload,omitempty"`
	Headers           json.RawMessage                  `json:"headers,omitempty"`
	Destination       InboundListenerDestinationConfig `json:"destination,omitempty"`
}

type ReceiveInboundListenerResponse struct {
	EventID           uuid.UUID                        `json:"event_id"`
	SourceID          uuid.UUID                        `json:"source_id"`
	ListenerID        string                           `json:"listener_id"`
	Status            string                           `json:"status"`
	SignatureVerified bool                             `json:"signature_verified"`
	Destination       InboundListenerDestinationConfig `json:"destination,omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthenticatedResponse struct {
	Status       string `json:"status"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

type UserProfile struct {
	ID             uuid.UUID       `json:"id"`
	Email          string          `json:"email"`
	Name           string          `json:"name"`
	IsActive       bool            `json:"is_active"`
	Roles          []string        `json:"roles"`
	Groups         []string        `json:"groups"`
	Permissions    []string        `json:"permissions"`
	OrganizationID *uuid.UUID      `json:"organization_id"`
	Attributes     json.RawMessage `json:"attributes"`
	MFAEnabled     bool            `json:"mfa_enabled"`
	MFAEnforced    bool            `json:"mfa_enforced"`
	AuthSource     string          `json:"auth_source"`
	CreatedAt      string          `json:"created_at"`
}

type BootstrapStatusResponse struct {
	RequiresInitialAdmin bool `json:"requires_initial_admin"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

type StreamingSyncFieldDescriptor struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type StreamingSourceContract struct {
	Kind          string                         `json:"kind"`
	DisplayName   string                         `json:"display_name"`
	Description   string                         `json:"description"`
	RequiresAgent bool                           `json:"requires_agent"`
	ConfigFields  []StreamingSyncFieldDescriptor `json:"config_fields"`
}

type ConnectionChangedEvent struct {
	EventType     string          `json:"event_type"`
	Aggregate     string          `json:"aggregate"`
	AggregateID   string          `json:"aggregate_id"`
	Version       string          `json:"version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Name          string          `json:"name"`
	ConnectorType string          `json:"connector_type"`
	Status        string          `json:"status"`
	Payload       json.RawMessage `json:"payload"`
}

type OutboxEvent struct {
	ID              uuid.UUID       `json:"id"`
	Aggregate       string          `json:"aggregate"`
	AggregateID     string          `json:"aggregate_id"`
	Topic           string          `json:"topic"`
	Payload         json.RawMessage `json:"payload"`
	OccurredAt      time.Time       `json:"occurred_at"`
	PublishedAt     *time.Time      `json:"published_at"`
	PublishAttempts int32           `json:"publish_attempts"`
	LastError       *string         `json:"last_error"`
}
