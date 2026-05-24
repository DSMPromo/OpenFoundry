package models

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// SDC.46 — Stream replay controls (safe replay after breaking processing logic
// changes).
//
// Pure planner that takes a replay request (from/to offsets, downstream
// inventory, operator-supplied acknowledgements) and produces a structured
// `StreamReplayPlan` describing every downstream impact (streaming exports,
// CDC archive views, object indexing pipelines, duplicate-tolerant consumers),
// the aggregate status (ready / requires_confirmation / blocked), and the
// list of acknowledgement IDs the operator still has to satisfy. Active
// streaming exports always require an explicit acknowledgement so replays
// cannot silently double-export.

type StreamReplayDownstreamKind string

const (
	StreamReplayDownstreamExport      StreamReplayDownstreamKind = "streaming_export"
	StreamReplayDownstreamCDCView     StreamReplayDownstreamKind = "cdc_archive_view"
	StreamReplayDownstreamObjectIndex StreamReplayDownstreamKind = "object_index"
	StreamReplayDownstreamConsumer    StreamReplayDownstreamKind = "duplicate_tolerant_consumer"
)

type StreamReplayImpactSeverity string

const (
	StreamReplayImpactSeverityBlock StreamReplayImpactSeverity = "block"
	StreamReplayImpactSeverityWarn  StreamReplayImpactSeverity = "warn"
	StreamReplayImpactSeverityInfo  StreamReplayImpactSeverity = "info"
)

type StreamReplayActiveExport struct {
	ExportID           string `json:"export_id"`
	ExportName         string `json:"export_name,omitempty"`
	Status             string `json:"status"` // "running" | "stopped" | "scheduled"
	ReplayBehavior     string `json:"replay_behavior,omitempty"`
	HasActiveConsumers bool   `json:"has_active_consumers,omitempty"`
}

type StreamReplayCDCView struct {
	ViewID         string `json:"view_id"`
	ViewName       string `json:"view_name,omitempty"`
	OrderingColumn string `json:"ordering_column,omitempty"`
	DeletionColumn string `json:"deletion_column,omitempty"`
}

type StreamReplayObjectIndex struct {
	IndexID    string `json:"index_id"`
	ObjectType string `json:"object_type,omitempty"`
	KeyByField string `json:"key_by_field,omitempty"`
}

type StreamReplayConsumer struct {
	ConsumerID      string `json:"consumer_id"`
	ConsumerName    string `json:"consumer_name,omitempty"`
	ConsumerGroup   string `json:"consumer_group,omitempty"`
	IdempotencyMode string `json:"idempotency_mode,omitempty"` // "duplicate_tolerant" | "exactly_once" | "unknown"
}

type StreamReplayPlanRequest struct {
	StreamID         string                     `json:"stream_id"`
	StreamRID        string                     `json:"stream_rid,omitempty"`
	StreamName       string                     `json:"stream_name,omitempty"`
	FromOffset       *int64                     `json:"from_offset,omitempty"`
	ToOffset         *int64                     `json:"to_offset,omitempty"`
	EarliestOffset   *int64                     `json:"earliest_offset,omitempty"`
	LatestOffset     *int64                     `json:"latest_offset,omitempty"`
	Reason           string                     `json:"reason"`
	RequestedBy      string                     `json:"requested_by,omitempty"`
	Acknowledgements []string                   `json:"acknowledgements,omitempty"`
	Exports          []StreamReplayActiveExport `json:"exports,omitempty"`
	CDCViews         []StreamReplayCDCView      `json:"cdc_views,omitempty"`
	ObjectIndices    []StreamReplayObjectIndex  `json:"object_indices,omitempty"`
	Consumers        []StreamReplayConsumer     `json:"consumers,omitempty"`
	ComputedAt       time.Time                  `json:"computed_at,omitempty"`
}

type StreamReplayDownstreamImpact struct {
	Kind         StreamReplayDownstreamKind `json:"kind"`
	ResourceID   string                     `json:"resource_id"`
	ResourceName string                     `json:"resource_name,omitempty"`
	Severity     StreamReplayImpactSeverity `json:"severity"`
	Implication  string                     `json:"implication"`
	Mitigation   string                     `json:"mitigation,omitempty"`
	WarningID    string                     `json:"warning_id,omitempty"`
}

type StreamReplayPlan struct {
	StreamID                  string                         `json:"stream_id"`
	StreamRID                 string                         `json:"stream_rid,omitempty"`
	StreamName                string                         `json:"stream_name,omitempty"`
	Status                    string                         `json:"status"` // "ready" | "requires_confirmation" | "blocked"
	Reason                    string                         `json:"reason,omitempty"`
	RequestedBy               string                         `json:"requested_by,omitempty"`
	FromOffset                *int64                         `json:"from_offset,omitempty"`
	ToOffset                  *int64                         `json:"to_offset,omitempty"`
	EstimatedRecords          *int64                         `json:"estimated_records,omitempty"`
	ConfirmationRequired      bool                           `json:"confirmation_required"`
	AcknowledgementsRequired  []string                       `json:"acknowledgements_required"`
	AcknowledgementsSatisfied []string                       `json:"acknowledgements_satisfied"`
	AcknowledgementsMissing   []string                       `json:"acknowledgements_missing"`
	PreconditionsSatisfied    []string                       `json:"preconditions_satisfied"`
	PreconditionsBlocking     []string                       `json:"preconditions_blocking"`
	Impacts                   []StreamReplayDownstreamImpact `json:"impacts"`
	ComputedAt                time.Time                      `json:"computed_at"`
}

// BuildStreamReplayPlan classifies every downstream dependency, raises the
// appropriate acknowledgement ids, and returns the aggregate status. The
// helper is intentionally pure so it can also run client-side from the
// existing DataConnectionStreamResource if/when richer wiring lands.
func BuildStreamReplayPlan(req StreamReplayPlanRequest) StreamReplayPlan {
	now := req.ComputedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	plan := StreamReplayPlan{
		StreamID:                  strings.TrimSpace(req.StreamID),
		StreamRID:                 strings.TrimSpace(req.StreamRID),
		StreamName:                strings.TrimSpace(req.StreamName),
		Reason:                    strings.TrimSpace(req.Reason),
		RequestedBy:               strings.TrimSpace(req.RequestedBy),
		FromOffset:                req.FromOffset,
		ToOffset:                  req.ToOffset,
		ComputedAt:                now,
		Impacts:                   []StreamReplayDownstreamImpact{},
		PreconditionsSatisfied:    []string{},
		PreconditionsBlocking:     []string{},
		AcknowledgementsRequired:  []string{},
		AcknowledgementsSatisfied: []string{},
		AcknowledgementsMissing:   []string{},
	}

	// Required preconditions.
	if plan.Reason == "" {
		plan.PreconditionsBlocking = append(plan.PreconditionsBlocking, "replay_reason_required")
	} else {
		plan.PreconditionsSatisfied = append(plan.PreconditionsSatisfied, "replay_reason_provided")
	}
	if plan.StreamID == "" {
		plan.PreconditionsBlocking = append(plan.PreconditionsBlocking, "stream_id_required")
	}

	// Offset validation. If both endpoints are supplied, "from" must be <= "to";
	// each endpoint must also fall within [earliest, latest] when those bounds
	// are known.
	if req.FromOffset != nil && req.ToOffset != nil && *req.FromOffset > *req.ToOffset {
		plan.PreconditionsBlocking = append(plan.PreconditionsBlocking, "replay_offsets_inverted")
	}
	if req.FromOffset != nil && req.EarliestOffset != nil && *req.FromOffset < *req.EarliestOffset {
		plan.PreconditionsBlocking = append(plan.PreconditionsBlocking, "replay_from_offset_before_earliest")
	}
	if req.ToOffset != nil && req.LatestOffset != nil && *req.ToOffset > *req.LatestOffset {
		plan.PreconditionsBlocking = append(plan.PreconditionsBlocking, "replay_to_offset_after_latest")
	}
	if req.FromOffset != nil && req.ToOffset != nil && *req.FromOffset <= *req.ToOffset {
		estimated := *req.ToOffset - *req.FromOffset + 1
		plan.EstimatedRecords = &estimated
	}

	ackSatisfied := map[string]bool{}
	for _, ack := range req.Acknowledgements {
		ack = strings.TrimSpace(ack)
		if ack != "" {
			ackSatisfied[ack] = true
		}
	}

	addImpact := func(impact StreamReplayDownstreamImpact) {
		impact.ResourceID = strings.TrimSpace(impact.ResourceID)
		impact.ResourceName = strings.TrimSpace(impact.ResourceName)
		plan.Impacts = append(plan.Impacts, impact)
		if impact.WarningID != "" {
			plan.AcknowledgementsRequired = append(plan.AcknowledgementsRequired, impact.WarningID)
		}
	}

	// Streaming exports — explicit confirmation rule from SDC.46.
	for _, exp := range req.Exports {
		status := strings.ToLower(strings.TrimSpace(exp.Status))
		severity := StreamReplayImpactSeverityWarn
		implication := "Replayed records will be re-exported; downstream consumers may see duplicates."
		mitigation := "Switch the export replay_behavior to skip_replayed_records or stop the export before replaying."
		warningID := "ack_streaming_export_" + exp.ExportID
		if status == "running" || exp.HasActiveConsumers {
			severity = StreamReplayImpactSeverityBlock
			implication = "Active streaming export will double-export records during the replay window."
			mitigation = "Stop the export or pass the explicit acknowledgement before retrying the plan."
		}
		if strings.EqualFold(exp.ReplayBehavior, "skip_replayed_records") {
			severity = StreamReplayImpactSeverityWarn
			implication = "Export is configured to skip replayed records; consumers may miss the replay window entirely."
			mitigation = "Confirm consumers downstream of the export can tolerate the skip, or switch replay_behavior to export_replayed_records."
		}
		addImpact(StreamReplayDownstreamImpact{
			Kind:         StreamReplayDownstreamExport,
			ResourceID:   exp.ExportID,
			ResourceName: exp.ExportName,
			Severity:     severity,
			Implication:  implication,
			Mitigation:   mitigation,
			WarningID:    warningID,
		})
	}

	// CDC archive views.
	for _, view := range req.CDCViews {
		ordering := strings.TrimSpace(view.OrderingColumn)
		warningID := "ack_cdc_archive_view_" + view.ViewID
		if ordering == "" {
			addImpact(StreamReplayDownstreamImpact{
				Kind:         StreamReplayDownstreamCDCView,
				ResourceID:   view.ViewID,
				ResourceName: view.ViewName,
				Severity:     StreamReplayImpactSeverityBlock,
				Implication:  "CDC archive view has no ordering column; replay would corrupt the current-state resolution.",
				Mitigation:   "Set the ordering column on the CDC sync before replaying.",
				WarningID:    warningID,
			})
			continue
		}
		addImpact(StreamReplayDownstreamImpact{
			Kind:         StreamReplayDownstreamCDCView,
			ResourceID:   view.ViewID,
			ResourceName: view.ViewName,
			Severity:     StreamReplayImpactSeverityWarn,
			Implication:  fmt.Sprintf("CDC archive view will re-resolve current state using %s; expect transient incorrect rows until replay completes.", ordering),
			Mitigation:   "Coordinate with downstream consumers of the archive view; consider replaying outside business hours.",
			WarningID:    warningID,
		})
	}

	// Object indexing pipelines.
	for _, index := range req.ObjectIndices {
		warningID := "ack_object_index_" + index.IndexID
		addImpact(StreamReplayDownstreamImpact{
			Kind:         StreamReplayDownstreamObjectIndex,
			ResourceID:   index.IndexID,
			ResourceName: index.ObjectType,
			Severity:     StreamReplayImpactSeverityWarn,
			Implication:  "Object indexing will reapply changes for the replay window; indexed objects may oscillate while the replay drains.",
			Mitigation:   "Pause downstream object indexing or accept the transient drift; key-by " + index.KeyByField + " preserves ordering when the stream CDC metadata is intact.",
			WarningID:    warningID,
		})
	}

	// Duplicate-tolerant consumers.
	for _, consumer := range req.Consumers {
		mode := strings.ToLower(strings.TrimSpace(consumer.IdempotencyMode))
		if mode == "" {
			mode = "unknown"
		}
		severity := StreamReplayImpactSeverityInfo
		implication := "Consumer is declared duplicate-tolerant and will absorb the replay safely."
		mitigation := ""
		warningID := ""
		if mode != "duplicate_tolerant" {
			severity = StreamReplayImpactSeverityWarn
			implication = "Consumer is not declared duplicate-tolerant; replay may emit duplicate downstream effects."
			mitigation = "Pause the consumer or migrate to a duplicate-tolerant consumer pattern before replaying."
			warningID = "ack_consumer_" + consumer.ConsumerID
		}
		addImpact(StreamReplayDownstreamImpact{
			Kind:         StreamReplayDownstreamConsumer,
			ResourceID:   consumer.ConsumerID,
			ResourceName: consumer.ConsumerName,
			Severity:     severity,
			Implication:  implication,
			Mitigation:   mitigation,
			WarningID:    warningID,
		})
	}

	// Resolve acknowledgement status now that all impacts are collected.
	requiredSet := map[string]bool{}
	requiredOrdered := []string{}
	for _, ack := range plan.AcknowledgementsRequired {
		if ack == "" || requiredSet[ack] {
			continue
		}
		requiredSet[ack] = true
		requiredOrdered = append(requiredOrdered, ack)
	}
	plan.AcknowledgementsRequired = requiredOrdered

	for _, ack := range requiredOrdered {
		if ackSatisfied[ack] {
			plan.AcknowledgementsSatisfied = append(plan.AcknowledgementsSatisfied, ack)
		} else {
			plan.AcknowledgementsMissing = append(plan.AcknowledgementsMissing, ack)
		}
	}
	sort.Strings(plan.AcknowledgementsSatisfied)
	sort.Strings(plan.AcknowledgementsMissing)

	// Aggregate status.
	hasBlock := false
	for _, impact := range plan.Impacts {
		if impact.Severity == StreamReplayImpactSeverityBlock {
			hasBlock = true
			break
		}
	}
	switch {
	case len(plan.PreconditionsBlocking) > 0:
		plan.Status = "blocked"
		plan.ConfirmationRequired = true
	case hasBlock && len(plan.AcknowledgementsMissing) > 0:
		plan.Status = "blocked"
		plan.ConfirmationRequired = true
	case len(plan.AcknowledgementsMissing) > 0:
		plan.Status = "requires_confirmation"
		plan.ConfirmationRequired = true
	default:
		plan.Status = "ready"
		plan.ConfirmationRequired = false
	}

	return plan
}

// SortStreamReplayImpactsBySeverity returns a stable ordering with block first,
// then warn, then info. Useful for the UI rendering.
func SortStreamReplayImpactsBySeverity(impacts []StreamReplayDownstreamImpact) []StreamReplayDownstreamImpact {
	out := append([]StreamReplayDownstreamImpact(nil), impacts...)
	rank := func(sev StreamReplayImpactSeverity) int {
		switch sev {
		case StreamReplayImpactSeverityBlock:
			return 0
		case StreamReplayImpactSeverityWarn:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i].Severity) < rank(out[j].Severity) })
	return out
}
