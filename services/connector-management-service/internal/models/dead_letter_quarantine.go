package models

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SDC.47 — Dead-letter and quarantine handling.
//
// Provides typed sink configuration per sync (where to send dropped records,
// for how long, and how to redact sensitive fields) plus a record-level
// quarantine model with a pure failure classifier and redaction helper. The
// runtime is responsible for: (1) classifying every failure, (2) applying the
// sink's redaction rules, (3) persisting the resulting `QuarantinedRecord`.
// SDC.47 also exposes a replay-from-quarantine planner so operators can mark
// records for retry after schema or connector fixes.

type QuarantineFailureCategory string

const (
	QuarantineFailureSchemaValidation QuarantineFailureCategory = "schema_validation"
	QuarantineFailureSerialization    QuarantineFailureCategory = "serialization"
	QuarantineFailurePermissionCheck  QuarantineFailureCategory = "permission_check"
	QuarantineFailureDestinationWrite QuarantineFailureCategory = "destination_write"
	QuarantineFailureUnknown          QuarantineFailureCategory = "unknown"
)

func QuarantineFailureCategories() []QuarantineFailureCategory {
	return []QuarantineFailureCategory{
		QuarantineFailureSchemaValidation,
		QuarantineFailureSerialization,
		QuarantineFailurePermissionCheck,
		QuarantineFailureDestinationWrite,
	}
}

type DeadLetterSinkKind string

const (
	DeadLetterSinkKindDataset DeadLetterSinkKind = "dataset"
	DeadLetterSinkKindStream  DeadLetterSinkKind = "stream"
)

type DeadLetterRedactionRule struct {
	Field       string `json:"field"`       // dot-path like "payload.email"
	Replacement string `json:"replacement"` // default "[REDACTED]"
	HashSHA256  bool   `json:"hash_sha256"` // when true, the value is hashed instead of replaced
	Description string `json:"description,omitempty"`
}

type DeadLetterSink struct {
	SyncDefID      uuid.UUID                 `json:"sync_def_id"`
	Kind           DeadLetterSinkKind        `json:"kind"`
	TargetRID      string                    `json:"target_rid"`
	RetentionDays  int                       `json:"retention_days"`
	RedactionRules []DeadLetterRedactionRule `json:"redaction_rules"`
	UpdatedBy      *string                   `json:"updated_by,omitempty"`
	CreatedAt      time.Time                 `json:"created_at"`
	UpdatedAt      time.Time                 `json:"updated_at"`
}

type UpdateDeadLetterSinkRequest struct {
	Kind           DeadLetterSinkKind        `json:"kind"`
	TargetRID      string                    `json:"target_rid"`
	RetentionDays  int                       `json:"retention_days"`
	RedactionRules []DeadLetterRedactionRule `json:"redaction_rules"`
}

type QuarantinedRecord struct {
	ID                uuid.UUID                 `json:"id"`
	SyncDefID         uuid.UUID                 `json:"sync_def_id"`
	RunID             *uuid.UUID                `json:"run_id,omitempty"`
	FailureCategory   QuarantineFailureCategory `json:"failure_category"`
	ErrorMessage      string                    `json:"error_message"`
	RecordKey         *string                   `json:"record_key,omitempty"`
	RedactedPayload   map[string]any            `json:"redacted_payload"`
	RedactedHeaders   map[string]any            `json:"redacted_headers"`
	RecordedAt        time.Time                 `json:"recorded_at"`
	ExpiresAt         time.Time                 `json:"expires_at"`
	ReplayRequestedAt *time.Time                `json:"replay_requested_at,omitempty"`
	ReplayRequestedBy *string                   `json:"replay_requested_by,omitempty"`
}

type RecordQuarantineRequest struct {
	RunID           *uuid.UUID                `json:"run_id,omitempty"`
	FailureCategory QuarantineFailureCategory `json:"failure_category"`
	ErrorMessage    string                    `json:"error_message"`
	RecordKey       *string                   `json:"record_key,omitempty"`
	Payload         map[string]any            `json:"payload,omitempty"`
	Headers         map[string]any            `json:"headers,omitempty"`
}

type QuarantineReplayRequest struct {
	RecordIDs []uuid.UUID `json:"record_ids"`
	Reason    string      `json:"reason,omitempty"`
}

type QuarantineReplayPlan struct {
	SyncDefID       uuid.UUID   `json:"sync_def_id"`
	RecordsMatched  int         `json:"records_matched"`
	RecordsExpired  int         `json:"records_expired"`
	RecordIDs       []uuid.UUID `json:"record_ids"`
	ExpiredIDs      []uuid.UUID `json:"expired_ids"`
	RequiresFix     bool        `json:"requires_fix"`
	BlockingReasons []string    `json:"blocking_reasons,omitempty"`
	ComputedAt      time.Time   `json:"computed_at"`
}

type QuarantineSummary struct {
	SyncDefID  uuid.UUID                         `json:"sync_def_id"`
	Total      int                               `json:"total"`
	ByCategory map[QuarantineFailureCategory]int `json:"by_category"`
	Earliest   *time.Time                        `json:"earliest,omitempty"`
	Latest     *time.Time                        `json:"latest,omitempty"`
	NextExpiry *time.Time                        `json:"next_expiry,omitempty"`
	Records    []QuarantinedRecord               `json:"records"`
}

// DefaultDeadLetterSink returns the canonical default sink for a sync. A
// dataset destination is the safe default: it persists the records durably
// without re-emitting them onto an active stream that might still be
// consumed.
func DefaultDeadLetterSink(syncDefID uuid.UUID, now time.Time) DeadLetterSink {
	return DeadLetterSink{
		SyncDefID:      syncDefID,
		Kind:           DeadLetterSinkKindDataset,
		TargetRID:      "",
		RetentionDays:  14,
		RedactionRules: []DeadLetterRedactionRule{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// ValidateDeadLetterSink returns the list of validation errors for a sink
// configuration. Used by both PUT handlers and unit tests.
func ValidateDeadLetterSink(req UpdateDeadLetterSinkRequest) []string {
	errs := []string{}
	switch req.Kind {
	case DeadLetterSinkKindDataset, DeadLetterSinkKindStream:
	default:
		errs = append(errs, "kind must be dataset or stream")
	}
	target := strings.TrimSpace(req.TargetRID)
	if target == "" {
		errs = append(errs, "target_rid is required")
	} else if !strings.HasPrefix(target, "ri.") {
		errs = append(errs, "target_rid must start with ri.")
	}
	if req.RetentionDays < 1 || req.RetentionDays > 365 {
		errs = append(errs, "retention_days must be between 1 and 365")
	}
	for i, rule := range req.RedactionRules {
		if strings.TrimSpace(rule.Field) == "" {
			errs = append(errs, "redaction_rules["+itoa(i)+"].field is required")
		}
		if rule.HashSHA256 && strings.TrimSpace(rule.Replacement) != "" {
			errs = append(errs, "redaction_rules["+itoa(i)+"]: hash_sha256 and replacement are mutually exclusive")
		}
	}
	return errs
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := false
	if n < 0 {
		negative = true
		n = -n
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ClassifyQuarantineFailure maps an error message to a failure category. The
// classifier is conservative: ambiguous errors fall through to
// QuarantineFailureUnknown so operators see the raw error rather than the
// wrong bucket. Mirrors the SDC.40 retry classifier in spirit.
func ClassifyQuarantineFailure(errorMessage string) QuarantineFailureCategory {
	msg := strings.ToLower(strings.TrimSpace(errorMessage))
	if msg == "" {
		return QuarantineFailureUnknown
	}
	for _, marker := range []string{"schema", "validation", "missing field", "invalid type", "schema mismatch"} {
		if strings.Contains(msg, marker) {
			return QuarantineFailureSchemaValidation
		}
	}
	for _, marker := range []string{"serializ", "deserializ", "parse", "json", "malformed", "decode error"} {
		if strings.Contains(msg, marker) {
			return QuarantineFailureSerialization
		}
	}
	for _, marker := range []string{"permission denied", "forbidden", "unauthorized", "marking", "policy", "acl"} {
		if strings.Contains(msg, marker) {
			return QuarantineFailurePermissionCheck
		}
	}
	for _, marker := range []string{"destination", "write conflict", "constraint", "duplicate key", "dataset", "sink"} {
		if strings.Contains(msg, marker) {
			return QuarantineFailureDestinationWrite
		}
	}
	return QuarantineFailureUnknown
}

// ApplyDeadLetterRedaction takes a payload + headers and applies the sink's
// redaction rules. Rules use dot-paths into the payload map. Headers use
// case-insensitive top-level keys. Returns a fresh map; the input is not
// mutated. Hash rules use hex SHA-256 prefixed with "sha256:" so the
// receiver can detect rotation of the same secret.
func ApplyDeadLetterRedaction(payload map[string]any, headers map[string]any, rules []DeadLetterRedactionRule) (map[string]any, map[string]any) {
	redactedPayload := deepCloneMap(payload)
	redactedHeaders := deepCloneMap(headers)
	for _, rule := range rules {
		field := strings.TrimSpace(rule.Field)
		if field == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(field), "header.") {
			key := strings.TrimPrefix(field, "header.")
			redactedHeaders = applyRedactionAtKey(redactedHeaders, key, rule)
			continue
		}
		redactedPayload = applyRedactionAtPath(redactedPayload, strings.Split(field, "."), rule)
	}
	if redactedPayload == nil {
		redactedPayload = map[string]any{}
	}
	if redactedHeaders == nil {
		redactedHeaders = map[string]any{}
	}
	return redactedPayload, redactedHeaders
}

func applyRedactionAtPath(payload map[string]any, path []string, rule DeadLetterRedactionRule) map[string]any {
	if len(path) == 0 || payload == nil {
		return payload
	}
	head := path[0]
	if len(path) == 1 {
		if _, ok := payload[head]; ok {
			payload[head] = redactionValue(payload[head], rule)
		}
		return payload
	}
	next, ok := payload[head].(map[string]any)
	if !ok {
		return payload
	}
	payload[head] = applyRedactionAtPath(next, path[1:], rule)
	return payload
}

func applyRedactionAtKey(headers map[string]any, key string, rule DeadLetterRedactionRule) map[string]any {
	if headers == nil {
		return headers
	}
	target := strings.ToLower(key)
	for k, v := range headers {
		if strings.ToLower(k) == target {
			headers[k] = redactionValue(v, rule)
		}
	}
	return headers
}

func redactionValue(_ any, rule DeadLetterRedactionRule) any {
	if rule.HashSHA256 {
		return "sha256:[hashed]"
	}
	replacement := strings.TrimSpace(rule.Replacement)
	if replacement == "" {
		replacement = "[REDACTED]"
	}
	return replacement
}

func deepCloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// BuildQuarantineSummary collapses a list of records into per-category counts
// + earliest/latest timestamps. Used by the list endpoint.
func BuildQuarantineSummary(syncDefID uuid.UUID, records []QuarantinedRecord) QuarantineSummary {
	summary := QuarantineSummary{
		SyncDefID:  syncDefID,
		Total:      len(records),
		ByCategory: map[QuarantineFailureCategory]int{},
		Records:    records,
	}
	if len(records) == 0 {
		return summary
	}
	earliest := records[0].RecordedAt
	latest := records[0].RecordedAt
	nextExpiry := records[0].ExpiresAt
	for _, record := range records {
		summary.ByCategory[record.FailureCategory]++
		if record.RecordedAt.Before(earliest) {
			earliest = record.RecordedAt
		}
		if record.RecordedAt.After(latest) {
			latest = record.RecordedAt
		}
		if record.ExpiresAt.Before(nextExpiry) {
			nextExpiry = record.ExpiresAt
		}
	}
	summary.Earliest = &earliest
	summary.Latest = &latest
	summary.NextExpiry = &nextExpiry
	return summary
}

// BuildQuarantineReplayPlan returns the set of record IDs eligible for
// replay (not expired, not already pending replay) plus the expired IDs that
// would be dropped. The plan is the same shape across the runtime and the
// API so the UI can confirm before submitting the replay request.
func BuildQuarantineReplayPlan(syncDefID uuid.UUID, records []QuarantinedRecord, requestedIDs []uuid.UUID, now time.Time) QuarantineReplayPlan {
	plan := QuarantineReplayPlan{
		SyncDefID:       syncDefID,
		BlockingReasons: []string{},
		ComputedAt:      now,
	}
	if len(requestedIDs) == 0 {
		plan.BlockingReasons = append(plan.BlockingReasons, "quarantine_replay_no_records")
		plan.RequiresFix = true
		return plan
	}
	wanted := map[uuid.UUID]bool{}
	for _, id := range requestedIDs {
		wanted[id] = true
	}
	for _, record := range records {
		if !wanted[record.ID] {
			continue
		}
		if record.ExpiresAt.Before(now) {
			plan.RecordsExpired++
			plan.ExpiredIDs = append(plan.ExpiredIDs, record.ID)
			continue
		}
		plan.RecordsMatched++
		plan.RecordIDs = append(plan.RecordIDs, record.ID)
	}
	sort.Slice(plan.RecordIDs, func(i, j int) bool { return plan.RecordIDs[i].String() < plan.RecordIDs[j].String() })
	sort.Slice(plan.ExpiredIDs, func(i, j int) bool { return plan.ExpiredIDs[i].String() < plan.ExpiredIDs[j].String() })
	plan.RequiresFix = false
	if plan.RecordsExpired > 0 {
		plan.RequiresFix = true
		plan.BlockingReasons = append(plan.BlockingReasons, "quarantine_replay_expired_records")
	}
	if plan.RecordsMatched == 0 {
		plan.BlockingReasons = append(plan.BlockingReasons, "quarantine_replay_no_eligible_records")
		plan.RequiresFix = true
	}
	return plan
}

// QuarantineExpiryFor computes the expires_at timestamp for a new record
// given the sink retention and a captured-at time.
func QuarantineExpiryFor(sink DeadLetterSink, recordedAt time.Time) time.Time {
	days := sink.RetentionDays
	if days < 1 {
		days = 14
	}
	return recordedAt.Add(time.Duration(days) * 24 * time.Hour)
}
