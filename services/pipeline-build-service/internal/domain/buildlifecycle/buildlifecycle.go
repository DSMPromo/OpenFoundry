// Package buildlifecycle is the formal state machine for a
// pipeline-build-service build — the build-level twin of the
// joblifecycle package, which does the same job for jobs.
//
//	BUILD_RESOLUTION ──┬─→ BUILD_QUEUED ─→ BUILD_RUNNING ─→ BUILD_COMPLETED
//	                   │        │                │
//	                   ├────────┴────────────────┴──────→ BUILD_FAILED
//	                   └────────┴────────────────┴──────→ BUILD_ABORTING ─→ BUILD_ABORTED
//
// Every non-terminal state (RESOLUTION, QUEUED, RUNNING) may also fail
// (BUILD_FAILED) or begin aborting (BUILD_ABORTING). BUILD_COMPLETED,
// BUILD_FAILED and BUILD_ABORTED are terminal — see
// models.BuildState.IsTerminal.
//
// IsValidTransition is the single source of truth for this diagram and
// must agree with any Postgres CHECK constraint on builds.state.
package buildlifecycle

import (
	"fmt"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// ErrInvalidTransition is returned when a build is asked to move along
// an edge that is not present in the lifecycle diagram.
type ErrInvalidTransition struct {
	From models.BuildState
	To   models.BuildState
}

func (e *ErrInvalidTransition) Error() string {
	return fmt.Sprintf("invalid build state transition %s → %s", e.From, e.To)
}

type transition struct{ from, to models.BuildState }

// validTransitions is the legal build-state edge set.
var validTransitions = map[transition]bool{
	{models.BuildResolution, models.BuildQueued}:   true,
	{models.BuildResolution, models.BuildFailed}:   true,
	{models.BuildResolution, models.BuildAborting}: true,
	{models.BuildQueued, models.BuildRunning}:      true,
	{models.BuildQueued, models.BuildFailed}:       true,
	{models.BuildQueued, models.BuildAborting}:     true,
	{models.BuildRunning, models.BuildCompleted}:   true,
	{models.BuildRunning, models.BuildFailed}:      true,
	{models.BuildRunning, models.BuildAborting}:    true,
	{models.BuildAborting, models.BuildAborted}:    true,
}

// IsValidTransition reports whether moving a build from `from` to `to`
// is a legal edge. A no-op (from == to) is not a transition and returns
// false; callers handle idempotent retries separately.
func IsValidTransition(from, to models.BuildState) bool {
	return validTransitions[transition{from, to}]
}

// CheckTransition returns an *ErrInvalidTransition when the edge is
// illegal and nil when it is allowed.
func CheckTransition(from, to models.BuildState) error {
	if !IsValidTransition(from, to) {
		return &ErrInvalidTransition{From: from, To: to}
	}
	return nil
}
