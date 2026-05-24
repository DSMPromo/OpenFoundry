// Package schedule ports
// `services/pipeline-build-service/src/pipeline_schedule/*` —
// schedule + trigger model, trigger evaluation, auto-pause supervisor,
// cron window planner.
//
// **Phase D scope** (in-process algorithms):
//
//   - Trigger / TriggerKind union (Time / Event / Compound) +
//     ScheduleTarget union (PipelineBuild / DatasetBuild / SyncRun /
//     HealthCheck) + Schedule struct.
//   - Trigger evaluator: `EvaluateTrigger(trigger, ctx)` returns the
//     next fire time + a boolean signalling whether the trigger fires
//     for the given evaluation context.
//   - Cron window planner (`BuildScheduleWindows`) — emits the
//     [scheduled_for, window_start, window_end] tuple list the
//     backfill API consumes.
//   - Auto-pause supervisor (`ShouldAutoPause`,
//     `AutoPauseExempt`) — Foundry rule of thumb: pause after N
//     consecutive failures unless the schedule carries the
//     `auto_pause_exempt` flag.
//   - Pipeline-table-backed legacy due-run helpers (DueRunRecord,
//     ScheduleTargetKindLegacy) shared with the cron-dispatch loop.
//
// The DB-backed schedule_store / run_store / version_store, the
// dispatcher state machine, the Kafka event listener and the
// notification-client live in their own follow-ups.
package schedule

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	cron "github.com/openfoundry/openfoundry-go/libs/scheduling-cron"
)

// ── Trigger model ──────────────────────────────────────────────────

// TriggerKindTag mirrors the Rust `enum TriggerKind` discriminator.
type TriggerKindTag int

const (
	TriggerKindTime TriggerKindTag = iota
	TriggerKindEvent
	TriggerKindCompound
)

// CronFlavor mirrors `enum CronFlavor`.
type CronFlavor string

const (
	CronFlavorUnix5   CronFlavor = "UNIX5"
	CronFlavorQuartz6 CronFlavor = "QUARTZ6"
)

// EventType mirrors `enum EventType`.
type EventType string

const (
	EventTypeNewLogic                EventType = "NEW_LOGIC"
	EventTypeDataUpdated             EventType = "DATA_UPDATED"
	EventTypeJobSucceeded            EventType = "JOB_SUCCEEDED"
	EventTypeScheduleRanSuccessfully EventType = "SCHEDULE_RAN_SUCCESSFULLY"
)

// CompoundOp mirrors `enum CompoundOp`.
type CompoundOp string

const (
	CompoundOpAnd CompoundOp = "AND"
	CompoundOpOr  CompoundOp = "OR"
)

// Trigger mirrors `pub struct Trigger`. The discriminator is `Kind`;
// exactly one inner pointer is populated.
type Trigger struct {
	Kind     TriggerKindTag
	Time     *TimeTrigger
	Event    *EventTrigger
	Compound *CompoundTrigger
}

// TimeTrigger mirrors `pub struct TimeTrigger`.
type TimeTrigger struct {
	Cron     string     `json:"cron"`
	TimeZone string     `json:"time_zone"`
	Flavor   CronFlavor `json:"flavor"`
}

// EventTrigger mirrors `pub struct EventTrigger`.
type EventTrigger struct {
	EventType    EventType `json:"type"`
	TargetRID    string    `json:"target_rid"`
	BranchFilter []string  `json:"branch_filter,omitempty"`
}

// CompoundTrigger mirrors `pub struct CompoundTrigger`.
type CompoundTrigger struct {
	Op         CompoundOp `json:"op"`
	Components []Trigger  `json:"components"`
}

// MarshalJSON renders the tagged union with the snake_case `kind`
// discriminator that matches the Rust serde representation.
func (t Trigger) MarshalJSON() ([]byte, error) {
	switch t.Kind {
	case TriggerKindTime:
		return json.Marshal(map[string]any{"kind": "time", "data": t.Time})
	case TriggerKindEvent:
		return json.Marshal(map[string]any{"kind": "event", "data": t.Event})
	case TriggerKindCompound:
		return json.Marshal(map[string]any{"kind": "compound", "data": t.Compound})
	}
	return nil, fmt.Errorf("invalid trigger kind: %d", t.Kind)
}

// UnmarshalJSON decodes the tagged union shape matching the Rust
// serde-internally-tagged emission.
func (t *Trigger) UnmarshalJSON(data []byte) error {
	var env struct {
		Kind string          `json:"kind"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	switch env.Kind {
	case "time":
		var inner TimeTrigger
		if err := json.Unmarshal(env.Data, &inner); err != nil {
			return err
		}
		if inner.TimeZone == "" {
			inner.TimeZone = "UTC"
		}
		*t = Trigger{Kind: TriggerKindTime, Time: &inner}
		return nil
	case "event":
		var inner EventTrigger
		if err := json.Unmarshal(env.Data, &inner); err != nil {
			return err
		}
		*t = Trigger{Kind: TriggerKindEvent, Event: &inner}
		return nil
	case "compound":
		var inner CompoundTrigger
		if err := json.Unmarshal(env.Data, &inner); err != nil {
			return err
		}
		*t = Trigger{Kind: TriggerKindCompound, Compound: &inner}
		return nil
	}
	return fmt.Errorf("unknown trigger kind: %s", env.Kind)
}

// ── ScheduleTarget ────────────────────────────────────────────────

// ScheduleTargetKindTag mirrors the Rust `ScheduleTargetKind`
// discriminator.
type ScheduleTargetKindTag int

const (
	ScheduleTargetPipelineBuild ScheduleTargetKindTag = iota
	ScheduleTargetDatasetBuild
	ScheduleTargetSyncRun
	ScheduleTargetHealthCheck
)

// ScheduleTarget mirrors `pub struct ScheduleTarget`.
type ScheduleTarget struct {
	Kind          ScheduleTargetKindTag
	PipelineBuild *PipelineBuildTarget
	DatasetBuild  *DatasetBuildTarget
	SyncRun       *SyncRunTarget
	HealthCheck   *HealthCheckTarget
}

// PipelineBuildTarget mirrors `pub struct PipelineBuildTarget`.
type PipelineBuildTarget struct {
	PipelineRID     string   `json:"pipeline_rid"`
	BuildBranch     string   `json:"build_branch"`
	JobSpecFallback []string `json:"job_spec_fallback,omitempty"`
	ForceBuild      bool     `json:"force_build"`
	AbortPolicy     *string  `json:"abort_policy,omitempty"`
}

// DatasetBuildTarget mirrors `pub struct DatasetBuildTarget`.
type DatasetBuildTarget struct {
	DatasetRID  string `json:"dataset_rid"`
	BuildBranch string `json:"build_branch"`
	ForceBuild  bool   `json:"force_build"`
}

// SyncRunTarget mirrors `pub struct SyncRunTarget`.
type SyncRunTarget struct {
	SyncRID   string `json:"sync_rid"`
	SourceRID string `json:"source_rid"`
}

// HealthCheckTarget mirrors `pub struct HealthCheckTarget`.
type HealthCheckTarget struct {
	CheckRID string `json:"check_rid"`
}

// ── Schedule ──────────────────────────────────────────────────────

// PauseReason mirrors the canonical Rust strings.
const (
	PauseReasonManual                  = "MANUAL"
	PauseReasonAutoPausedAfterFailures = "AUTO_PAUSED_AFTER_FAILURES"
)

// Schedule mirrors `pub struct Schedule`.
type Schedule struct {
	ID          uuid.UUID      `json:"id"`
	RID         string         `json:"rid"`
	ProjectRID  string         `json:"project_rid"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Trigger     Trigger        `json:"trigger"`
	Target      ScheduleTarget `json:"target"`
	Paused      bool           `json:"paused"`
	PauseReason *string        `json:"pause_reason,omitempty"`
	Version     int32          `json:"version"`
	CreatedBy   string         `json:"created_by"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	LastRunAt   *time.Time     `json:"last_run_at,omitempty"`
}

// ── Cron window planner ───────────────────────────────────────────

// ScheduleWindow mirrors `pub struct ScheduleWindow`.
type ScheduleWindow struct {
	ScheduledFor time.Time `json:"scheduled_for"`
	WindowStart  time.Time `json:"window_start"`
	WindowEnd    time.Time `json:"window_end"`
}

// DefaultBackfillLimit / MaxBackfillLimit mirror the Rust constants.
const (
	DefaultBackfillLimit = 50
	MaxBackfillLimit     = 200
)

// ClampLimit mirrors `pub fn clamp_limit`.
func ClampLimit(limit *int) int {
	if limit == nil {
		return DefaultBackfillLimit
	}
	if *limit < 1 {
		return 1
	}
	if *limit > MaxBackfillLimit {
		return MaxBackfillLimit
	}
	return *limit
}

// BuildScheduleWindows mirrors `pub fn build_schedule_windows`. Walks
// the cron expression's occurrences inside [start_at, end_at] and
// returns up to `limit` windows. Each window's `WindowEnd` defaults
// to the next occurrence (or `end_at` when there is no next).
//
// Uses the workspace `libs/scheduling-cron` parser + evaluator so
// the cron semantics exactly match the production scheduler (DST-
// aware, supports both Unix5 and Quartz6 flavors).
func BuildScheduleWindows(expression string, startAt, endAt time.Time, limit int, flavor CronFlavor, tz *time.Location) ([]ScheduleWindow, error) {
	if endAt.Before(startAt) {
		return nil, fmt.Errorf("end_at must be greater than or equal to start_at")
	}
	if tz == nil {
		tz = time.UTC
	}
	parsed, err := cron.ParseCron(expression, mapFlavor(flavor), tz)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression '%s': %w", expression, err)
	}

	occurrences := []time.Time{}
	cursor := startAt.Add(-time.Second)
	for len(occurrences) < limit+1 {
		next, ok := cron.NextFireAfter(&parsed, cursor)
		if !ok || next.After(endAt) {
			break
		}
		occurrences = append(occurrences, next)
		cursor = next
	}

	windows := make([]ScheduleWindow, 0, limit)
	for i := 0; i < len(occurrences) && i < limit; i++ {
		scheduled := occurrences[i]
		windowEnd := endAt
		if i+1 < len(occurrences) {
			windowEnd = occurrences[i+1]
		}
		if windowEnd.Before(scheduled) {
			windowEnd = scheduled
		}
		if windowEnd.After(endAt) {
			windowEnd = endAt
		}
		windows = append(windows, ScheduleWindow{
			ScheduledFor: scheduled,
			WindowStart:  scheduled,
			WindowEnd:    windowEnd,
		})
	}
	return windows, nil
}

func mapFlavor(f CronFlavor) cron.CronFlavor {
	switch f {
	case CronFlavorQuartz6:
		return cron.Quartz6
	}
	return cron.Unix5
}

// ── Trigger evaluation ────────────────────────────────────────────

// TriggerEvent is one input the event-listener feeds the trigger
// evaluator. Mirrors the Rust event-driven dispatcher's input.
type TriggerEvent struct {
	EventType  EventType
	TargetRID  string
	Branch     string
	OccurredAt time.Time
}

// EvaluationContext mirrors the implicit context the Rust evaluator
// reads (`now`, `last_run_at`, optional event).
type EvaluationContext struct {
	Now       time.Time
	LastRunAt *time.Time
	Event     *TriggerEvent
}

// EvaluateTrigger mirrors the evaluator path that determines whether
// a trigger fires for the given `ctx`.
//
// Semantics:
//
//   - TimeTrigger: fires when the next cron occurrence after
//     LastRunAt (or epoch when nil) is <= Now.
//   - EventTrigger: fires when the event matches `target_rid` AND
//     (when configured) `branch_filter`. Returns true on match.
//   - Compound{AND, [...]}: fires when every component fires.
//   - Compound{OR, [...]}: fires when at least one component fires.
//
// Returns `(fires, nextFire)` — the next fire time is meaningful for
// time-based components and surfaces the nearest scheduled instant.
func EvaluateTrigger(trigger *Trigger, ctx EvaluationContext) (bool, *time.Time, error) {
	if trigger == nil {
		return false, nil, fmt.Errorf("trigger is nil")
	}
	switch trigger.Kind {
	case TriggerKindTime:
		return evaluateTime(trigger.Time, ctx)
	case TriggerKindEvent:
		return evaluateEvent(trigger.Event, ctx), nil, nil
	case TriggerKindCompound:
		return evaluateCompound(trigger.Compound, ctx)
	}
	return false, nil, fmt.Errorf("unknown trigger kind: %d", trigger.Kind)
}

func evaluateTime(t *TimeTrigger, ctx EvaluationContext) (bool, *time.Time, error) {
	if t == nil {
		return false, nil, fmt.Errorf("time trigger is nil")
	}
	tz := time.UTC
	if t.TimeZone != "" {
		loaded, err := time.LoadLocation(t.TimeZone)
		if err != nil {
			return false, nil, fmt.Errorf("invalid time_zone '%s': %w", t.TimeZone, err)
		}
		tz = loaded
	}
	parsed, err := cron.ParseCron(t.Cron, mapFlavor(t.Flavor), tz)
	if err != nil {
		return false, nil, fmt.Errorf("invalid cron expression '%s': %w", t.Cron, err)
	}
	cursor := time.Time{}
	if ctx.LastRunAt != nil {
		cursor = *ctx.LastRunAt
	}
	next, ok := cron.NextFireAfter(&parsed, cursor)
	if !ok {
		return false, nil, nil
	}
	fires := !next.After(ctx.Now)
	return fires, &next, nil
}

func evaluateEvent(t *EventTrigger, ctx EvaluationContext) bool {
	if t == nil || ctx.Event == nil {
		return false
	}
	if ctx.Event.EventType != t.EventType {
		return false
	}
	if ctx.Event.TargetRID != t.TargetRID {
		return false
	}
	if len(t.BranchFilter) == 0 {
		return true
	}
	for _, branch := range t.BranchFilter {
		if branch == ctx.Event.Branch {
			return true
		}
	}
	return false
}

func evaluateCompound(t *CompoundTrigger, ctx EvaluationContext) (bool, *time.Time, error) {
	if t == nil || len(t.Components) == 0 {
		return false, nil, nil
	}
	switch t.Op {
	case CompoundOpAnd:
		var earliest *time.Time
		for i := range t.Components {
			fires, next, err := EvaluateTrigger(&t.Components[i], ctx)
			if err != nil {
				return false, nil, err
			}
			if !fires {
				return false, next, nil
			}
			if next != nil && (earliest == nil || next.Before(*earliest)) {
				earliest = next
			}
		}
		return true, earliest, nil
	case CompoundOpOr:
		var earliest *time.Time
		anyFires := false
		for i := range t.Components {
			fires, next, err := EvaluateTrigger(&t.Components[i], ctx)
			if err != nil {
				return false, nil, err
			}
			if fires {
				anyFires = true
			}
			if next != nil && (earliest == nil || next.Before(*earliest)) {
				earliest = next
			}
		}
		return anyFires, earliest, nil
	}
	return false, nil, fmt.Errorf("unknown compound op: %s", t.Op)
}

// ── Auto-pause supervisor ─────────────────────────────────────────

// AutoPauseFailureThreshold mirrors the Foundry rule-of-thumb:
// pause after 5 consecutive failures unless the schedule carries
// the `auto_pause_exempt` flag.
const AutoPauseFailureThreshold = 5

// RunOutcome mirrors the slimmest path of `RunOutcome` the auto-
// pause supervisor reads. `Succeeded` resets the failure counter.
type RunOutcome struct {
	Succeeded  bool
	OccurredAt time.Time
}

// ShouldAutoPause mirrors `should_auto_pause`. Walks the most
// recent `RunOutcome`s and returns true when the trailing
// consecutive-failures count crosses the threshold.
func ShouldAutoPause(history []RunOutcome) bool {
	streak := 0
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Succeeded {
			break
		}
		streak++
		if streak >= AutoPauseFailureThreshold {
			return true
		}
	}
	return streak >= AutoPauseFailureThreshold
}

// AutoPauseExempt mirrors `auto_pause_exempt`. Schedules carrying
// the canonical `"auto_pause_exempt"` tag in their description or
// metadata bypass the auto-pause supervisor.
//
// In Foundry the exempt flag lives on the schedule's metadata; the
// Go port reads the description field for now (matches Rust 0.x) —
// the metadata-driven variant lands when the schedule_store ports.
func AutoPauseExempt(schedule *Schedule) bool {
	if schedule == nil {
		return false
	}
	return strings.Contains(strings.ToLower(schedule.Description), "auto_pause_exempt")
}

// ── Legacy pipeline-table due-run helpers ─────────────────────────

// ScheduleTargetKindLegacy mirrors `enum ScheduleTargetKind` from
// `models/schedule.rs` (the legacy pipeline-table-backed scheduler
// used by the cron-dispatch loop).
type ScheduleTargetKindLegacy string

const (
	ScheduleTargetKindLegacyPipeline ScheduleTargetKindLegacy = "pipeline"
	ScheduleTargetKindLegacyWorkflow ScheduleTargetKindLegacy = "workflow"
)

// DueRunRecord mirrors `pub struct DueRunRecord`.
type DueRunRecord struct {
	TargetKind         ScheduleTargetKindLegacy `json:"target_kind"`
	TargetID           uuid.UUID                `json:"target_id"`
	Name               string                   `json:"name"`
	DueAt              time.Time                `json:"due_at"`
	ScheduleExpression string                   `json:"schedule_expression"`
	TriggerType        string                   `json:"trigger_type"`
}
