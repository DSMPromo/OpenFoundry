package logs

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// BuildJobLookup is the subset of the postgres Repository surface the
// finalizer needs. Kept narrow so the unit tests can stand up a tiny
// fake without dragging in pgx.
type BuildJobLookup interface {
	JobRIDsForBuild(ctx context.Context, buildID uuid.UUID) ([]string, error)
	SetBuildLogURI(ctx context.Context, buildID uuid.UUID, uri string) error
}

// Finalizer drains every job's log history for a finished build,
// uploads the merged stream through the configured Archiver, and
// stamps `builds.log_uri` with the resulting URI.
//
// Idempotent: re-running Finalize on the same build replays the same
// key (S3Archiver overwrites in place) and re-stamps the same URI on
// the column. Skips entirely when the archiver is nil or returns the
// empty URI (NoopArchiver), so deployments without object-store
// credentials degrade to "log archival disabled" rather than failing
// every terminal transition.
type Finalizer struct {
	Repo     BuildJobLookup
	Store    LogStore
	Archiver Archiver
}

// Finalize archives the merged log history for buildID and persists
// the resulting URI on the builds row. Returns the URI on success,
// or the empty string when the archiver is disabled.
func (f *Finalizer) Finalize(ctx context.Context, buildID uuid.UUID) (string, error) {
	if f == nil {
		return "", errors.New("logs finalizer: nil receiver")
	}
	if f.Archiver == nil {
		return "", nil
	}
	if f.Repo == nil {
		return "", errors.New("logs finalizer: missing BuildJobLookup")
	}
	if f.Store == nil {
		return "", errors.New("logs finalizer: missing LogStore")
	}

	jobRIDs, err := f.Repo.JobRIDsForBuild(ctx, buildID)
	if err != nil {
		return "", fmt.Errorf("logs finalizer: list jobs for build %s: %w", buildID, err)
	}
	slices := make([][]LogEntry, 0, len(jobRIDs))
	for _, rid := range jobRIDs {
		entries, err := f.Store.History(ctx, rid, Query{})
		if err != nil {
			return "", fmt.Errorf("logs finalizer: history %s: %w", rid, err)
		}
		slices = append(slices, entries)
	}
	merged := MergeHistory(slices, 0)

	uri, err := f.Archiver.Archive(ctx, buildID, merged)
	if err != nil {
		return "", fmt.Errorf("logs finalizer: archive build %s: %w", buildID, err)
	}
	if uri == "" {
		// NoopArchiver path — archival disabled, nothing to persist.
		return "", nil
	}
	if err := f.Repo.SetBuildLogURI(ctx, buildID, uri); err != nil {
		return uri, fmt.Errorf("logs finalizer: stamp log_uri on build %s: %w", buildID, err)
	}
	return uri, nil
}
