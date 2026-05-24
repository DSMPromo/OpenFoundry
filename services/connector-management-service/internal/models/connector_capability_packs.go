package models

import (
	"sort"
	"strings"
)

// SDC.44 — Connector-specific capability packs.
//
// Declarative capability + validation manifests for the high-value connector
// families called out in the public Foundry docs. Each pack carries typed
// support flags for every sync/export/virtual/CDC/webhook/exploration
// capability, optional worker-specific overrides (e.g., agent worker drops
// table export), the CDC input kind discriminator from SDC.21, and a
// structured list of source-specific validation rules that the UI can render
// alongside the capability chips so operators see *why* a capability is or
// isn't available.

type ConnectorCapabilityFlags struct {
	BatchSync       bool `json:"batch_sync"`
	FileSync        bool `json:"file_sync"`
	TableSync       bool `json:"table_sync"`
	StreamingSync   bool `json:"streaming_sync"`
	CdcSync         bool `json:"cdc_sync"`
	MediaSync       bool `json:"media_sync"`
	FileExport      bool `json:"file_export"`
	TableExport     bool `json:"table_export"`
	StreamingExport bool `json:"streaming_export"`
	VirtualTable    bool `json:"virtual_table"`
	Webhook         bool `json:"webhook"`
	Exploration     bool `json:"exploration"`
}

type ConnectorCapabilityFamily string

const (
	ConnectorCapabilityFamilyRelational   ConnectorCapabilityFamily = "relational_database"
	ConnectorCapabilityFamilyWarehouse    ConnectorCapabilityFamily = "data_warehouse"
	ConnectorCapabilityFamilyObjectStore  ConnectorCapabilityFamily = "object_store"
	ConnectorCapabilityFamilyFileTransfer ConnectorCapabilityFamily = "file_transfer"
	ConnectorCapabilityFamilyEventStream  ConnectorCapabilityFamily = "event_stream"
	ConnectorCapabilityFamilyMessageQueue ConnectorCapabilityFamily = "message_queue"
	ConnectorCapabilityFamilyRestAPI      ConnectorCapabilityFamily = "rest_api"
	ConnectorCapabilityFamilyFoundry      ConnectorCapabilityFamily = "foundry_to_foundry"
)

type ConnectorValidationRule struct {
	ID          string `json:"id"`
	Capability  string `json:"capability"`
	Severity    string `json:"severity"` // "required" | "recommended" | "informational"
	Description string `json:"description"`
}

type ConnectorCapabilityPack struct {
	ConnectorType   string                              `json:"connector_type"`
	DisplayName     string                              `json:"display_name"`
	Family          ConnectorCapabilityFamily           `json:"family"`
	Capabilities    ConnectorCapabilityFlags            `json:"capabilities"`
	WorkerOverrides map[string]ConnectorCapabilityFlags `json:"worker_overrides,omitempty"`
	CdcInputKind    string                              `json:"cdc_input_kind,omitempty"`
	ValidationRules []ConnectorValidationRule           `json:"validation_rules"`
	Notes           []string                            `json:"notes,omitempty"`
	DocsURL         string                              `json:"docs_url"`
}

// CapabilityList returns the capability names that are set to true on the
// pack, in the canonical order used by the connector catalog. The list is
// useful both for chips in the UI and for serializing back into the existing
// ConnectorCapability frontend enum.
func (p ConnectorCapabilityPack) CapabilityList() []string {
	return capabilityFlagsToList(p.Capabilities)
}

// EffectiveCapabilitiesForWorker collapses the base flags with the worker
// override, returning the worker-effective capability flags. Workers that have
// no override inherit the pack's base capabilities. Unknown workers also fall
// back to the base capabilities — the descriptor never invents support.
func (p ConnectorCapabilityPack) EffectiveCapabilitiesForWorker(worker string) ConnectorCapabilityFlags {
	if p.WorkerOverrides == nil {
		return p.Capabilities
	}
	if override, ok := p.WorkerOverrides[strings.ToLower(strings.TrimSpace(worker))]; ok {
		return override
	}
	return p.Capabilities
}

// ValidationRulesForCapability returns the subset of rules attached to the
// given capability (e.g. "cdc_sync"). It is the contract the UI uses to render
// "PostgreSQL requires logical decoding" hints next to the CDC chip.
func (p ConnectorCapabilityPack) ValidationRulesForCapability(capability string) []ConnectorValidationRule {
	if capability == "" {
		return nil
	}
	out := make([]ConnectorValidationRule, 0)
	for _, rule := range p.ValidationRules {
		if rule.Capability == capability {
			out = append(out, rule)
		}
	}
	return out
}

func capabilityFlagsToList(flags ConnectorCapabilityFlags) []string {
	out := []string{}
	if flags.BatchSync {
		out = append(out, "batch_sync")
	}
	if flags.FileSync {
		out = append(out, "file_sync")
	}
	if flags.TableSync {
		out = append(out, "table_sync")
	}
	if flags.StreamingSync {
		out = append(out, "streaming_sync")
	}
	if flags.CdcSync {
		out = append(out, "cdc_sync")
	}
	if flags.MediaSync {
		out = append(out, "media_sync")
	}
	if flags.FileExport {
		out = append(out, "file_export")
	}
	if flags.TableExport {
		out = append(out, "table_export")
	}
	if flags.StreamingExport {
		out = append(out, "streaming_export")
	}
	if flags.VirtualTable {
		out = append(out, "virtual_table")
	}
	if flags.Webhook {
		out = append(out, "webhook")
	}
	if flags.Exploration {
		out = append(out, "exploration")
	}
	return out
}

// BuildConnectorCapabilityPacks emits the canonical SDC.44 capability packs.
// The list is intentionally hand-curated rather than derived from the older
// ConnectorContractProfile templates so each entry can carry connector-
// specific validation rules and worker overrides.
func BuildConnectorCapabilityPacks() []ConnectorCapabilityPack {
	packs := []ConnectorCapabilityPack{
		relationalPack("postgresql", "PostgreSQL",
			"Requires logical replication slot, replication user, and tracked publication for CDC syncs.",
			"Streaming syncs are not supported; use CDC for changelog ingestion."),
		relationalPack("mssql", "Microsoft SQL Server",
			"CDC requires SQL Server Agent and CDC enabled on each tracked table.",
			"Change Tracking is not supported; only the CDC framework is used."),
		relationalPack("oracle", "Oracle Database",
			"CDC requires Oracle LogMiner or GoldenGate; supplemental logging must cover the tracked columns.",
			"Snowflake-style zero-copy is not supported by Oracle Database."),
		relationalPack("db2", "IBM Db2",
			"CDC requires CDC for Db2 (formerly InfoSphere) configured against the LUW/iSeries instance.",
			"Batch syncs prefer the JDBC adapter for portability across LUW and Db2 for i."),

		warehousePack("snowflake", "Snowflake",
			"Streams can drive CDC syncs only when the source object emits change rows compatible with the connector.",
			"Virtual tables are zero-copy backed by the source warehouse."),
		warehousePack("bigquery", "Google BigQuery",
			"Streaming export requires BigQuery Storage Write API; legacy streaming inserts are not supported.",
			"Virtual tables read through BigLake / BigQuery Storage Read API."),
		warehousePack("databricks", "Databricks",
			"Virtual tables use Unity Catalog; Delta tables on legacy Hive metastores are not supported.",
			"Streaming export pushes through Auto Loader or Delta sink jobs."),

		objectStorePackWithMedia("s3", "Amazon S3",
			"Bucket and prefix must be reachable from the worker; cross-account access requires an explicit policy.",
			"Media sync handoff registers items under the source's marking/org policy (SDC.41/SDC.42)."),
		objectStorePackWithMedia("onelake", "Microsoft OneLake",
			"Workspace identity must be granted the Reader role on the OneLake namespace.",
			"Streaming sync is not supported."),
		objectStorePackWithMedia("abfs", "Azure Data Lake Storage Gen2 (ABFS)",
			"Service principal must hold Storage Blob Data Reader on the target container.",
			"Media sync handoff is wired but virtual media handoff is blocked (SDC.42)."),
		objectStorePack("gcs", "Google Cloud Storage",
			"Workload identity federation is required for agentless OpenFoundry workers."),

		filesystemPack("sftp", "SFTP / FTPS",
			"Host fingerprint must be configured before syncs run; password and key auth are supported.",
			"Webhook surface is unavailable; SFTP is pull-only."),

		eventStreamPack("kafka", "Apache Kafka", "streaming_middleware_changelog",
			"CDC requires the upstream producer to emit Debezium-shaped changelog events.",
			"Schema is inferred from the configured Schema Registry or the first batch of events."),
		eventStreamPack("kinesis", "Amazon Kinesis", "streaming_middleware_changelog",
			"Shard iterator type defaults to TRIM_HORIZON; LATEST and AT_TIMESTAMP must be configured explicitly.",
			"Streaming exports cap throughput at 1MB/s per shard."),
		messageQueuePack("streaming_sqs", "Amazon SQS",
			"FIFO queues are not supported; standard queues only.",
			"At-least-once delivery is the only available consistency mode."),
		messageQueuePack("streaming_pubsub", "Google Cloud Pub/Sub",
			"Subscription must be pull-mode; push subscriptions are not supported.",
			"Acknowledgement deadline must be at least the configured checkpoint interval."),

		restPack("rest_api", "Generic REST API"),
		foundryPack("foundry_to_foundry", "Foundry-to-Foundry"),
	}

	for i := range packs {
		sort.SliceStable(packs[i].ValidationRules, func(a, b int) bool {
			return packs[i].ValidationRules[a].ID < packs[i].ValidationRules[b].ID
		})
	}
	return packs
}

// ConnectorCapabilityPackFor returns the canonical pack for a connector type
// or nil if SDC.44 has not yet defined one. Connector types are normalized to
// lowercase before lookup.
func ConnectorCapabilityPackFor(connectorType string) *ConnectorCapabilityPack {
	target := strings.ToLower(strings.TrimSpace(connectorType))
	if target == "" {
		return nil
	}
	for _, pack := range BuildConnectorCapabilityPacks() {
		if pack.ConnectorType == target {
			copied := pack
			return &copied
		}
	}
	return nil
}

// --- family helpers ---------------------------------------------------------

func relationalPack(connectorType, displayName string, notes ...string) ConnectorCapabilityPack {
	flags := ConnectorCapabilityFlags{
		BatchSync:   true,
		TableSync:   true,
		CdcSync:     true,
		TableExport: true,
		Exploration: true,
	}
	return ConnectorCapabilityPack{
		ConnectorType: connectorType,
		DisplayName:   displayName,
		Family:        ConnectorCapabilityFamilyRelational,
		Capabilities:  flags,
		WorkerOverrides: map[string]ConnectorCapabilityFlags{
			"agent": dropFlags(flags, func(f *ConnectorCapabilityFlags) {
				f.TableExport = false
			}),
		},
		CdcInputKind: "relational_connector",
		ValidationRules: []ConnectorValidationRule{
			{ID: "cdc-requires-pk", Capability: "cdc_sync", Severity: "required", Description: "CDC syncs require at least one primary key column on the source table."},
			{ID: "cdc-requires-ordering", Capability: "cdc_sync", Severity: "required", Description: "CDC syncs require an ordering column (LSN, SCN, or commit timestamp) to resolve the archive view."},
			{ID: "table-export-parquet-input", Capability: "table_export", Severity: "required", Description: "Table exports require the input dataset to be Parquet-backed with declared schema."},
			{ID: "exploration-discovery", Capability: "exploration", Severity: "informational", Description: "Discovery enumerates schemas and tables visible to the configured credential."},
		},
		Notes:   notes,
		DocsURL: "https://www.palantir.com/docs/foundry/data-integration/change-data-capture",
	}
}

func warehousePack(connectorType, displayName string, notes ...string) ConnectorCapabilityPack {
	flags := ConnectorCapabilityFlags{
		BatchSync:       true,
		TableSync:       true,
		TableExport:     true,
		StreamingExport: connectorType != "snowflake", // Snowflake streaming export blocked by SDC.31 settings
		VirtualTable:    true,
		Exploration:     true,
	}
	if connectorType == "snowflake" {
		flags.StreamingExport = false
	}
	pack := ConnectorCapabilityPack{
		ConnectorType: connectorType,
		DisplayName:   displayName,
		Family:        ConnectorCapabilityFamilyWarehouse,
		Capabilities:  flags,
		ValidationRules: []ConnectorValidationRule{
			{ID: "virtual-table-zero-copy", Capability: "virtual_table", Severity: "informational", Description: "Virtual tables are zero-copy; queries execute against the warehouse and bill the warehouse account."},
			{ID: "table-export-schema-match", Capability: "table_export", Severity: "required", Description: "Destination table schema must match the input dataset column names and types exactly."},
		},
		Notes:   notes,
		DocsURL: "https://www.palantir.com/docs/foundry/data-integration/virtual-tables/",
	}
	return pack
}

func objectStorePackWithMedia(connectorType, displayName string, notes ...string) ConnectorCapabilityPack {
	flags := ConnectorCapabilityFlags{
		BatchSync:   true,
		FileSync:    true,
		MediaSync:   true,
		FileExport:  true,
		Exploration: true,
	}
	return ConnectorCapabilityPack{
		ConnectorType: connectorType,
		DisplayName:   displayName,
		Family:        ConnectorCapabilityFamilyObjectStore,
		Capabilities:  flags,
		ValidationRules: []ConnectorValidationRule{
			{ID: "file-sync-glob", Capability: "file_sync", Severity: "informational", Description: "File syncs respect include/exclude globs and the configured file size limit."},
			{ID: "media-sync-mime", Capability: "media_sync", Severity: "required", Description: "Media syncs declare the allowed MIME types; mismatched files are skipped or recorded as schema mismatches (SDC.41)."},
			{ID: "file-export-modified-since", Capability: "file_export", Severity: "informational", Description: "File exports default to modified-since-last-success; full re-exports require an explicit override."},
		},
		Notes:   notes,
		DocsURL: "https://www.palantir.com/docs/foundry/data-connection/file-based-syncs/",
	}
}

func objectStorePack(connectorType, displayName string, notes ...string) ConnectorCapabilityPack {
	flags := ConnectorCapabilityFlags{
		BatchSync:    true,
		FileSync:     true,
		VirtualTable: true,
		FileExport:   true,
		Exploration:  true,
	}
	return ConnectorCapabilityPack{
		ConnectorType: connectorType,
		DisplayName:   displayName,
		Family:        ConnectorCapabilityFamilyObjectStore,
		Capabilities:  flags,
		ValidationRules: []ConnectorValidationRule{
			{ID: "file-sync-glob", Capability: "file_sync", Severity: "informational", Description: "File syncs respect include/exclude globs and the configured file size limit."},
			{ID: "virtual-table-format", Capability: "virtual_table", Severity: "required", Description: "Virtual tables require Parquet, Iceberg, or Delta-formatted objects with discoverable schema."},
		},
		Notes:   notes,
		DocsURL: "https://www.palantir.com/docs/foundry/data-connection/file-based-syncs/",
	}
}

func filesystemPack(connectorType, displayName string, notes ...string) ConnectorCapabilityPack {
	flags := ConnectorCapabilityFlags{
		BatchSync:   true,
		FileSync:    true,
		FileExport:  true,
		Exploration: true,
	}
	return ConnectorCapabilityPack{
		ConnectorType: connectorType,
		DisplayName:   displayName,
		Family:        ConnectorCapabilityFamilyFileTransfer,
		Capabilities:  flags,
		ValidationRules: []ConnectorValidationRule{
			{ID: "sftp-host-key", Capability: "exploration", Severity: "required", Description: "SFTP/FTPS sources require a configured host key fingerprint before exploration or sync."},
			{ID: "file-sync-resume", Capability: "file_sync", Severity: "informational", Description: "File syncs are resumable through the exclude-already-synced filter."},
		},
		Notes:   notes,
		DocsURL: "https://www.palantir.com/docs/foundry/data-connection/file-based-syncs/",
	}
}

func eventStreamPack(connectorType, displayName, cdcInputKind string, notes ...string) ConnectorCapabilityPack {
	flags := ConnectorCapabilityFlags{
		StreamingSync:   true,
		StreamingExport: true,
		CdcSync:         cdcInputKind != "",
		Exploration:     true,
	}
	pack := ConnectorCapabilityPack{
		ConnectorType: connectorType,
		DisplayName:   displayName,
		Family:        ConnectorCapabilityFamilyEventStream,
		Capabilities:  flags,
		CdcInputKind:  cdcInputKind,
		ValidationRules: []ConnectorValidationRule{
			{ID: "streaming-consumer-group", Capability: "streaming_sync", Severity: "required", Description: "Streaming syncs require a consumer group / shard iterator type / subscription identifier."},
			{ID: "streaming-checkpoint", Capability: "streaming_sync", Severity: "required", Description: "Streaming syncs persist checkpoints; restart resumes from the last completed checkpoint (SDC.15)."},
		},
		Notes:   notes,
		DocsURL: "https://www.palantir.com/docs/foundry/data-integration/streams/",
	}
	if cdcInputKind != "" {
		pack.ValidationRules = append(pack.ValidationRules, ConnectorValidationRule{
			ID:          "cdc-changelog-shape",
			Capability:  "cdc_sync",
			Severity:    "required",
			Description: "CDC over an event stream requires changelog-shaped payloads with primary key, ordering, and deletion markers preserved.",
		})
	}
	return pack
}

func messageQueuePack(connectorType, displayName string, notes ...string) ConnectorCapabilityPack {
	flags := ConnectorCapabilityFlags{
		StreamingSync: true,
		Exploration:   true,
	}
	return ConnectorCapabilityPack{
		ConnectorType: connectorType,
		DisplayName:   displayName,
		Family:        ConnectorCapabilityFamilyMessageQueue,
		Capabilities:  flags,
		ValidationRules: []ConnectorValidationRule{
			{ID: "queue-at-least-once", Capability: "streaming_sync", Severity: "informational", Description: "Message queues deliver at-least-once; consumers must be idempotent."},
		},
		Notes:   notes,
		DocsURL: "https://www.palantir.com/docs/foundry/data-integration/streams/",
	}
}

func restPack(connectorType, displayName string) ConnectorCapabilityPack {
	flags := ConnectorCapabilityFlags{
		BatchSync:   true,
		Webhook:     true,
		Exploration: true,
	}
	return ConnectorCapabilityPack{
		ConnectorType: connectorType,
		DisplayName:   displayName,
		Family:        ConnectorCapabilityFamilyRestAPI,
		Capabilities:  flags,
		ValidationRules: []ConnectorValidationRule{
			{ID: "rest-auth", Capability: "exploration", Severity: "required", Description: "REST API sources require an auth configuration before connection tests can run."},
			{ID: "rest-webhook-secret", Capability: "webhook", Severity: "required", Description: "Webhooks require an authorization reference or shared secret stored as a credential."},
		},
		Notes:   []string{"Generic REST is the fallback connector for any HTTP/JSON API not covered by a dedicated adapter."},
		DocsURL: "https://www.palantir.com/docs/foundry/available-connectors/other-source-types/",
	}
}

func foundryPack(connectorType, displayName string) ConnectorCapabilityPack {
	flags := ConnectorCapabilityFlags{
		BatchSync:    true,
		TableSync:    true,
		TableExport:  true,
		VirtualTable: true,
		Exploration:  true,
	}
	return ConnectorCapabilityPack{
		ConnectorType: connectorType,
		DisplayName:   displayName,
		Family:        ConnectorCapabilityFamilyFoundry,
		Capabilities:  flags,
		ValidationRules: []ConnectorValidationRule{
			{ID: "f2f-marking-policy", Capability: "table_export", Severity: "required", Description: "Foundry-to-Foundry exports require the source export policy to include the remote tenant's marking/org."},
			{ID: "f2f-zero-copy", Capability: "virtual_table", Severity: "informational", Description: "Virtual tables across Foundry tenants run via the remote query gateway and respect remote tenant ACLs."},
		},
		Notes:   []string{"Foundry-to-Foundry uses signed remote tenant tokens and respects export controls on both sides."},
		DocsURL: "https://www.palantir.com/docs/foundry/data-integration/source-type-overview/",
	}
}

func dropFlags(base ConnectorCapabilityFlags, mutate func(*ConnectorCapabilityFlags)) ConnectorCapabilityFlags {
	copied := base
	if mutate != nil {
		mutate(&copied)
	}
	return copied
}
