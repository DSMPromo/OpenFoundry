// Package joblifecycle ports services/pipeline-build-service/src/domain/job_lifecycle.rs
// 1:1: the Foundry "Builds.md § Job states" state machine.
//
//	WAITING ──┬─→ RUN_PENDING ─→ RUNNING ─┬─→ COMPLETED
//	          │                            ├─→ FAILED
//	          │                            └─→ ABORT_PENDING ─→ ABORTED
//	          └────────────────────────────→ ABORTED       (cascading abort)
//
// Any transition not listed above is rejected by IsValidTransition
// and propagated as ErrInvalidTransition. The high-level TransitionJob
// helper performs the row update and the audit-trail insert in a
// single transaction so `job_state_transitions` stays in sync with
// `jobs.state`.
package joblifecycle

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// ErrInvalidTransition is returned when a TransitionJob call asks for
// an edge not present in the lifecycle diagram.
type ErrInvalidTransition struct {
	From models.JobState
	To   models.JobState
}

func (e *ErrInvalidTransition) Error() string {
	return fmt.Sprintf("invalid job state transition %s → %s", e.From, e.To)
}

// ErrNotFound is returned when the requested job_id is missing.
type ErrNotFound struct{ ID uuid.UUID }

func (e *ErrNotFound) Error() string { return "job " + e.ID.String() + " not found" }

// IsValidTransition is the source of truth for the lifecycle diagram.
// It must agree with the Postgres CHECK constraint on `jobs.state`.
func IsValidTransition(from, to models.JobState) bool {
	type pair struct{ from, to models.JobState }
	allowed := map[pair]bool{
		{models.JobWaiting, models.JobRunPending}:      true,
		{models.JobWaiting, models.JobAborted}:         true,
		{models.JobWaiting, models.JobAbortPending}:    true,
		{models.JobRunPending, models.JobRunning}:      true,
		{models.JobRunPending, models.JobAbortPending}: true,
		{models.JobRunPending, models.JobFailed}:       true,
		{models.JobRunning, models.JobCompleted}:       true,
		{models.JobRunning, models.JobFailed}:          true,
		{models.JobRunning, models.JobAbortPending}:    true,
		{models.JobAbortPending, models.JobAborted}:    true,
	}
	return allowed[pair{from, to}]
}

// TransitionJobInTx applies a transition + audit insert atomically
// against an open transaction. The caller is responsible for committing.
func TransitionJobInTx(
	ctx context.Context,
	tx pgx.Tx,
	jobID uuid.UUID,
	expectedFrom *models.JobState,
	to models.JobState,
	reason *string,
) (models.JobState, error) {
	row := tx.QueryRow(ctx, "SELECT state FROM jobs WHERE id = $1 FOR UPDATE", jobID)
	var stateStr string
	if err := row.Scan(&stateStr); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", &ErrNotFound{ID: jobID}
		}
		return "", err
	}
	from, err := models.ParseJobState(stateStr)
	if err != nil {
		return "", err
	}

	if expectedFrom != nil && *expectedFrom != from {
		return "", &ErrInvalidTransition{From: from, To: to}
	}

	if from == to {
		// Idempotent retries — same as the Rust impl.
		return from, nil
	}

	if !IsValidTransition(from, to) {
		return "", &ErrInvalidTransition{From: from, To: to}
	}

	if _, err := tx.Exec(ctx,
		"UPDATE jobs SET state = $1, state_changed_at = $2 WHERE id = $3",
		to, time.Now().UTC(), jobID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO job_state_transitions (job_id, from_state, to_state, reason)
         VALUES ($1, $2, $3, $4)`,
		jobID, from, to, reason); err != nil {
		return "", err
	}
	return from, nil
}

// TransitionJob is the convenience wrapper that opens its own transaction.
func TransitionJob(
	ctx context.Context,
	pool *pgxpool.Pool,
	jobID uuid.UUID,
	expectedFrom *models.JobState,
	to models.JobState,
	reason *string,
) (models.JobState, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	from, err := TransitionJobInTx(ctx, tx, jobID, expectedFrom, to, reason)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return from, nil
}
