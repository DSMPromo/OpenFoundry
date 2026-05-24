package logs

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/domain/executor"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// IsBuildTerminal reports whether the audit event signals a build
// reaching a terminal state. Build-terminal events have JobID =
// uuid.Nil — the executor emits them after every node has settled.
func IsBuildTerminal(event executor.AuditEvent) bool {
	if event.BuildID == uuid.Nil || event.JobID != uuid.Nil {
		return false
	}
	switch models.BuildState(event.To) {
	case models.BuildCompleted, models.BuildFailed, models.BuildAborted:
		return true
	default:
		return false
	}
}

// ArchivingAuditSink wraps an executor.AuditSink. After the delegate
// successfully records a build-terminal event, the wrapper kicks the
// Finalizer for that build — best-effort: a finalizer failure is
// logged but never propagated, because we don't want a missing object
// store to break the state-machine write that's already landed.
//
// Wiring lives in cmd/pipeline-build-service/main.go: when the S3
// archiver is configured, ExecutionPorts.Audit is set to this wrapper
// instead of the raw postgres.Repository.
type ArchivingAuditSink struct {
	Delegate  executor.AuditSink
	Finalizer *Finalizer
	Logger    *slog.Logger
}

// Record forwards to the delegate, then (on a successful terminal
// transition) runs Finalize synchronously. Synchronous is the right
// default: tests + integration smoke want deterministic ordering, and
// the archive itself is bounded by the history size we already hold.
func (a *ArchivingAuditSink) Record(ctx context.Context, event executor.AuditEvent) error {
	if a == nil || a.Delegate == nil {
		return nil
	}
	if err := a.Delegate.Record(ctx, event); err != nil {
		return err
	}
	if a.Finalizer == nil || !IsBuildTerminal(event) {
		return nil
	}
	uri, ferr := a.Finalizer.Finalize(ctx, event.BuildID)
	if ferr != nil {
		if a.Logger != nil {
			a.Logger.WarnContext(ctx, "build log archive failed",
				slog.String("build_id", event.BuildID.String()),
				slog.String("error", ferr.Error()))
		}
		return nil
	}
	if uri != "" && a.Logger != nil {
		a.Logger.InfoContext(ctx, "build log archived",
			slog.String("build_id", event.BuildID.String()),
			slog.String("log_uri", uri))
	}
	return nil
}
