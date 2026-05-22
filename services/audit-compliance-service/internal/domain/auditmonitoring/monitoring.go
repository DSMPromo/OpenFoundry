package auditmonitoring

import (
	"strings"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/domain/alerting"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

func StarterPack(events []models.AuditEvent) models.AuditMonitoringStarterPack {
	queries := starterQueries()
	counts := map[string]int{
		"admin_changes":      countMatching(events, hasAnyCategory("managementPermissions", "managementUsers", "managementGroups", "managementMarkings")),
		"permission_grants":  countMatching(events, hasAnyCategory("managementPermissions")),
		"marking_changes":    countMatching(events, hasAnyCategory("managementMarkings")),
		"failed_access":      countMatching(events, failedAccess),
		"egress_use":         countMatching(events, hasAnyCategory("networkEgress")),
		"export_events":      countMatching(events, hasAnyCategory("dataExport")),
		"token_creation":     countMatching(events, hasAnyCategory("tokenGeneration", "managementTokens")),
		"anomalous_activity": len(alerting.DetectAnomalies(events)),
	}
	return models.AuditMonitoringStarterPack{
		AccessTier:              "security_sensitive",
		RestrictedTo:            []string{"admin", "security-auditor", "auditor", "audit-logs:view"},
		ExternalSIEMSupported:   true,
		FoundryDatasetSupported: true,
		Queries:                 queries,
		Dashboards:              starterDashboards(),
		Monitors: []models.AuditMonitorDefinition{
			monitor("admin_changes", "Administrative and permission changes", "high", []string{"managementPermissions", "managementUsers", "managementGroups", "managementMarkings"}, counts["admin_changes"], "Review approval trail and verify change ticket."),
			monitor("failed_access", "Failed or denied access", "medium", []string{"authenticationCheck", "dataLoad"}, counts["failed_access"], "Investigate repeated denials by actor, origin, and resource."),
			monitor("data_export", "Data export and egress", "critical", []string{"dataExport", "networkEgress"}, counts["export_events"]+counts["egress_use"], "Confirm business purpose, destination, markings, and egress policy approval."),
			monitor("token_creation", "Token generation and token management", "high", []string{"tokenGeneration", "managementTokens"}, counts["token_creation"], "Review token owner, scopes, service account, and expiration."),
			monitor("anomalous_activity", "Anomalous sensitive activity", "critical", []string{"dataExport", "dataLoad"}, counts["anomalous_activity"], "Open incident workflow and correlate with user/session timeline."),
		},
		AuditExcerpt: firstN(events, 20),
	}
}

func starterQueries() []models.AuditMonitoringQuery {
	return []models.AuditMonitoringQuery{
		{ID: "admin_changes", Title: "Admin changes", Description: "Permission, user, group, and marking changes.", Categories: []string{"managementPermissions", "managementUsers", "managementGroups", "managementMarkings"}, Query: `time >= now() - 24h AND categories intersects ["managementPermissions","managementUsers","managementGroups","managementMarkings"]`},
		{ID: "permission_grants", Title: "Permission grants", Description: "Resource permission changes and grants.", Categories: []string{"managementPermissions"}, Query: `time >= now() - 7d AND categories contains "managementPermissions"`},
		{ID: "marking_changes", Title: "Marking changes", Description: "Mandatory-control and marking administration.", Categories: []string{"managementMarkings"}, Query: `time >= now() - 7d AND categories contains "managementMarkings"`},
		{ID: "failed_access", Title: "Failed access", Description: "Denied auth checks and failed data loads.", Categories: []string{"authenticationCheck", "dataLoad"}, Query: `time >= now() - 24h AND (status = "denied" OR outcome in ["error","unauthorized"])`},
		{ID: "egress_use", Title: "Network egress use", Description: "Network egress policy use and lifecycle.", Categories: []string{"networkEgress"}, Query: `time >= now() - 24h AND categories contains "networkEgress"`},
		{ID: "export_events", Title: "Data exports", Description: "Exports to files, external systems, and egress routes.", Categories: []string{"dataExport"}, Query: `time >= now() - 24h AND categories contains "dataExport"`},
		{ID: "token_creation", Title: "Token creation", Description: "Token generation and management activity.", Categories: []string{"tokenGeneration", "managementTokens"}, Query: `time >= now() - 24h AND categories intersects ["tokenGeneration","managementTokens"]`},
		{ID: "anomalous_activity", Title: "Anomalous activity", Description: "Critical events, sensitive labels, and high-risk export sequences.", Categories: []string{"dataLoad", "dataExport"}, Query: `time >= now() - 24h AND (severity in ["high","critical"] OR labels contains "contains-sensitive-data")`},
	}
}

func starterDashboards() []models.AuditMonitoringDashboard {
	return []models.AuditMonitoringDashboard{{
		ID:          "security_operations_overview",
		Title:       "Security operations overview",
		Description: "Category-first monitoring dashboard for audit.3 logs, external SIEM parity, and in-platform triage.",
		Widgets: []models.AuditMonitoringWidget{
			{ID: "exports", Title: "Exports and egress", Description: "Data export plus network egress events by actor/resource.", QueryID: "export_events", Chart: "timeseries_bar"},
			{ID: "failed", Title: "Failed access", Description: "Denied or failed auth/access attempts grouped by actor and origin.", QueryID: "failed_access", Chart: "table"},
			{ID: "admin", Title: "Admin changes", Description: "Permission, user/group, and marking changes.", QueryID: "admin_changes", Chart: "timeline"},
			{ID: "tokens", Title: "Token activity", Description: "Generated, revoked, or managed tokens.", QueryID: "token_creation", Chart: "counter"},
		},
	}}
}

func monitor(id, title, severity string, categories []string, count int, action string) models.AuditMonitorDefinition {
	return models.AuditMonitorDefinition{ID: id, Title: title, Description: "Starter monitor backed by audit category filters.", Severity: severity, Categories: categories, QueryID: id, Schedule: "5m", RecommendedAction: action, CurrentCount: count}
}

func countMatching(events []models.AuditEvent, pred func(models.AuditEvent) bool) int {
	count := 0
	for _, event := range events {
		if pred(event) {
			count++
		}
	}
	return count
}

func hasAnyCategory(categories ...string) func(models.AuditEvent) bool {
	wanted := map[string]struct{}{}
	for _, c := range categories {
		wanted[strings.ToLower(c)] = struct{}{}
	}
	return func(event models.AuditEvent) bool {
		for _, c := range event.Categories {
			if _, ok := wanted[strings.ToLower(c)]; ok {
				return true
			}
		}
		return false
	}
}

func failedAccess(event models.AuditEvent) bool {
	return event.Status == string(models.StatusDenied) || strings.EqualFold(event.Outcome, "unauthorized") || strings.EqualFold(event.Outcome, "error")
}

func firstN(events []models.AuditEvent, n int) []models.AuditEvent {
	if len(events) <= n {
		return events
	}
	return events[:n]
}
