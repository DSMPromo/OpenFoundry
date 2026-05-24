package logs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// fakeBuildJobLookup captures SetBuildLogURI calls and serves a canned
// jobs list for any build.
type fakeBuildJobLookup struct {
	jobs       []string
	setBuild   uuid.UUID
	setURI     string
	setCalls   int
	listErr    error
	setErr     error
}

func (f *fakeBuildJobLookup) JobRIDsForBuild(_ context.Context, _ uuid.UUID) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]string(nil), f.jobs...), nil
}

func (f *fakeBuildJobLookup) SetBuildLogURI(_ context.Context, buildID uuid.UUID, uri string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.setBuild = buildID
	f.setURI = uri
	f.setCalls++
	return nil
}

// stubArchiver records the inputs to Archive so tests can assert.
type stubArchiver struct {
	uri     string
	err     error
	calls   int
	gotHist []LogEntry
}

func (s *stubArchiver) Archive(_ context.Context, _ uuid.UUID, history []LogEntry) (string, error) {
	s.calls++
	s.gotHist = append([]LogEntry(nil), history...)
	if s.err != nil {
		return "", s.err
	}
	return s.uri, nil
}

func TestFinalizerArchivesAndStampsLogURI(t *testing.T) {
	mem := NewMemoryService()
	mem.Emit("ri.job.a", LogInfo, "from-a", nil)
	mem.Emit("ri.job.b", LogInfo, "from-b", nil)
	mem.Emit("ri.job.a", LogError, "from-a-2", nil)

	repo := &fakeBuildJobLookup{jobs: []string{"ri.job.a", "ri.job.b"}}
	archiver := &stubArchiver{uri: "s3://logs/builds/x/driver.log"}
	f := &Finalizer{Repo: repo, Store: mem, Archiver: archiver}

	uri, err := f.Finalize(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if uri != archiver.uri {
		t.Errorf("uri = %q, want %q", uri, archiver.uri)
	}
	if archiver.calls != 1 {
		t.Errorf("archiver.calls = %d, want 1", archiver.calls)
	}
	if len(archiver.gotHist) != 3 {
		t.Errorf("got %d entries, want 3 merged", len(archiver.gotHist))
	}
	if repo.setCalls != 1 || repo.setURI != archiver.uri {
		t.Errorf("SetBuildLogURI not stamped: calls=%d uri=%q", repo.setCalls, repo.setURI)
	}
}

func TestFinalizerNoopArchiverSkipsURIStamp(t *testing.T) {
	mem := NewMemoryService()
	repo := &fakeBuildJobLookup{jobs: []string{"ri.job.a"}}
	f := &Finalizer{Repo: repo, Store: mem, Archiver: NoopArchiver{}}

	uri, err := f.Finalize(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if uri != "" {
		t.Errorf("uri = %q, want empty (noop)", uri)
	}
	if repo.setCalls != 0 {
		t.Errorf("SetBuildLogURI should NOT be called for noop archiver, got %d", repo.setCalls)
	}
}

func TestFinalizerSkipsWhenArchiverNil(t *testing.T) {
	f := &Finalizer{Repo: &fakeBuildJobLookup{}, Store: NewMemoryService(), Archiver: nil}
	uri, err := f.Finalize(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if uri != "" {
		t.Errorf("uri = %q, want empty (nil archiver)", uri)
	}
}

func TestFinalizerPropagatesArchiveErr(t *testing.T) {
	mem := NewMemoryService()
	repo := &fakeBuildJobLookup{jobs: []string{"ri.job.a"}}
	archiver := &stubArchiver{err: errors.New("503")}
	f := &Finalizer{Repo: repo, Store: mem, Archiver: archiver}

	_, err := f.Finalize(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFinalizerIdempotent(t *testing.T) {
	mem := NewMemoryService()
	repo := &fakeBuildJobLookup{jobs: []string{"ri.job.a"}}
	archiver := &stubArchiver{uri: "s3://logs/builds/x/driver.log"}
	f := &Finalizer{Repo: repo, Store: mem, Archiver: archiver}
	buildID := uuid.New()

	_, _ = f.Finalize(context.Background(), buildID)
	_, _ = f.Finalize(context.Background(), buildID)
	if archiver.calls != 2 {
		t.Errorf("archive should run each time (idempotency lives in the archiver itself); calls = %d", archiver.calls)
	}
	if repo.setCalls != 2 || repo.setURI != archiver.uri {
		t.Errorf("each finalize should re-stamp the same URI; calls=%d uri=%q", repo.setCalls, repo.setURI)
	}
}
