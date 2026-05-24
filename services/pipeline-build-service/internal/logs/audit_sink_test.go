package logs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/domain/executor"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

type recordingDelegate struct {
	mu     sync.Mutex
	events []executor.AuditEvent
	err    error
}

func (r *recordingDelegate) Record(_ context.Context, event executor.AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return r.err
}

func TestIsBuildTerminalRecognizesEveryTerminalState(t *testing.T) {
	cases := []struct {
		name string
		ev   executor.AuditEvent
		want bool
	}{
		{"completed", executor.AuditEvent{BuildID: uuid.New(), To: executor.NodeState(models.BuildCompleted)}, true},
		{"failed", executor.AuditEvent{BuildID: uuid.New(), To: executor.NodeState(models.BuildFailed)}, true},
		{"aborted", executor.AuditEvent{BuildID: uuid.New(), To: executor.NodeState(models.BuildAborted)}, true},
		{"job-level event", executor.AuditEvent{BuildID: uuid.New(), JobID: uuid.New(), To: executor.NodeState(models.BuildCompleted)}, false},
		{"running not terminal", executor.AuditEvent{BuildID: uuid.New(), To: executor.NodeState(models.BuildRunning)}, false},
		{"no build id", executor.AuditEvent{To: executor.NodeState(models.BuildCompleted)}, false},
	}
	for _, tc := range cases {
		if got := IsBuildTerminal(tc.ev); got != tc.want {
			t.Errorf("%s: IsBuildTerminal = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestArchivingAuditSinkFinalizesOnTerminal(t *testing.T) {
	delegate := &recordingDelegate{}
	mem := NewMemoryService()
	repo := &fakeBuildJobLookup{jobs: []string{"ri.job.a"}}
	archiver := &stubArchiver{uri: "s3://logs/builds/x/driver.log"}
	finalizer := &Finalizer{Repo: repo, Store: mem, Archiver: archiver}
	sink := &ArchivingAuditSink{Delegate: delegate, Finalizer: finalizer}

	buildID := uuid.New()
	if err := sink.Record(context.Background(), executor.AuditEvent{BuildID: buildID, To: executor.NodeState(models.BuildCompleted), Reason: "build terminal"}); err != nil {
		t.Fatal(err)
	}
	if len(delegate.events) != 1 {
		t.Errorf("delegate should still receive the event, got %d", len(delegate.events))
	}
	if archiver.calls != 1 {
		t.Errorf("archiver should fire on terminal, got %d", archiver.calls)
	}
	if repo.setURI != archiver.uri {
		t.Errorf("SetBuildLogURI not stamped: got %q", repo.setURI)
	}
}

func TestArchivingAuditSinkSkipsNonTerminal(t *testing.T) {
	delegate := &recordingDelegate{}
	archiver := &stubArchiver{uri: "s3://x"}
	sink := &ArchivingAuditSink{
		Delegate: delegate,
		Finalizer: &Finalizer{
			Repo:     &fakeBuildJobLookup{},
			Store:    NewMemoryService(),
			Archiver: archiver,
		},
	}

	// job-level event with To=COMPLETED — must not fire archive
	_ = sink.Record(context.Background(), executor.AuditEvent{
		BuildID: uuid.New(), JobID: uuid.New(),
		To: executor.NodeState(models.BuildCompleted), Reason: "job done",
	})
	if archiver.calls != 0 {
		t.Errorf("job-level event should not trigger archive, got %d", archiver.calls)
	}
}

func TestArchivingAuditSinkLogsFinalizerErrorButReturnsNil(t *testing.T) {
	delegate := &recordingDelegate{}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	archiver := &stubArchiver{err: errors.New("network down")}
	sink := &ArchivingAuditSink{
		Delegate: delegate,
		Finalizer: &Finalizer{
			Repo:     &fakeBuildJobLookup{jobs: []string{"a"}},
			Store:    NewMemoryService(),
			Archiver: archiver,
		},
		Logger: logger,
	}

	err := sink.Record(context.Background(), executor.AuditEvent{
		BuildID: uuid.New(),
		To:      executor.NodeState(models.BuildCompleted),
	})
	if err != nil {
		t.Errorf("archive failure must not propagate, got %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("build log archive failed")) {
		t.Errorf("expected warning to be logged, got %q", buf.String())
	}
}

func TestArchivingAuditSinkPropagatesDelegateError(t *testing.T) {
	delegate := &recordingDelegate{err: errors.New("postgres down")}
	sink := &ArchivingAuditSink{Delegate: delegate}
	err := sink.Record(context.Background(), executor.AuditEvent{BuildID: uuid.New(), To: executor.NodeState(models.BuildCompleted)})
	if err == nil {
		t.Fatal("delegate error must propagate")
	}
}
