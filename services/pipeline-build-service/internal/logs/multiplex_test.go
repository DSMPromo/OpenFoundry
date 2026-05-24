package logs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMergeHistorySortsByTimestampThenJobRIDThenSequence(t *testing.T) {
	t0 := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	a := []LogEntry{
		{Sequence: 1, JobRID: "job-a", TS: t0.Add(1 * time.Second), Level: LogInfo, Message: "a1"},
		{Sequence: 2, JobRID: "job-a", TS: t0.Add(3 * time.Second), Level: LogInfo, Message: "a2"},
	}
	b := []LogEntry{
		{Sequence: 1, JobRID: "job-b", TS: t0.Add(2 * time.Second), Level: LogInfo, Message: "b1"},
		{Sequence: 2, JobRID: "job-b", TS: t0.Add(3 * time.Second), Level: LogInfo, Message: "b2"}, // tied ts w/ a2 — JobRID breaks tie
	}
	got := MergeHistory([][]LogEntry{a, b}, 0)
	want := []string{"a1", "b1", "a2", "b2"} // ts ascending; a2 (job-a) < b2 (job-b) on the tie
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i, entry := range got {
		if entry.Message != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, entry.Message, want[i])
		}
	}
}

func TestMergeHistoryAppliesLimitFromTail(t *testing.T) {
	t0 := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	entries := []LogEntry{
		{Sequence: 1, JobRID: "j", TS: t0, Message: "old"},
		{Sequence: 2, JobRID: "j", TS: t0.Add(time.Second), Message: "mid"},
		{Sequence: 3, JobRID: "j", TS: t0.Add(2 * time.Second), Message: "new"},
	}
	got := MergeHistory([][]LogEntry{entries}, 2)
	if len(got) != 2 || got[0].Message != "mid" || got[1].Message != "new" {
		t.Errorf("limit=2 should keep tail, got %+v", got)
	}
}

// errSubscriber returns an error on the second Subscribe call so we can
// exercise the rollback path in FanInLive.
type errSubscriber struct {
	delegate LogSubscriber
	failNth  int
	calls    int
}

func (e *errSubscriber) Subscribe(ctx context.Context, jobRID string) (<-chan LogEntry, func(), error) {
	e.calls++
	if e.failNth > 0 && e.calls == e.failNth {
		return nil, nil, errors.New("subscribe failed")
	}
	return e.delegate.Subscribe(ctx, jobRID)
}

func TestFanInLiveMultiplexesAndShutsDown(t *testing.T) {
	mem := NewMemoryService()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := uuid.New().String()
	b := uuid.New().String()

	ch, teardown, err := FanInLive(ctx, mem, []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	// Drive both jobs.
	mem.Emit(a, LogInfo, "hi from a", nil)
	mem.Emit(b, LogWarn, "hi from b", nil)

	gotA, gotB := false, false
	timeout := time.After(2 * time.Second)
	for !gotA || !gotB {
		select {
		case entry := <-ch:
			switch entry.JobRID {
			case a:
				gotA = true
			case b:
				gotB = true
			}
		case <-timeout:
			t.Fatalf("did not receive both entries; gotA=%v gotB=%v", gotA, gotB)
		}
	}
}

func TestFanInLiveRollsBackOnSubscribeError(t *testing.T) {
	mem := NewMemoryService()
	sub := &errSubscriber{delegate: mem, failNth: 2}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := uuid.New().String()
	b := uuid.New().String()

	_, _, err := FanInLive(ctx, sub, []string{a, b})
	if err == nil {
		t.Fatal("expected error from second Subscribe")
	}
	// First subscriber must have been cleaned up — emitting after error
	// must not leave a dangling goroutine receiving. We can only
	// inspect via the package's invariant: the memory service drops
	// the subscriber on cancel/close. Emitting now should not panic.
	mem.Emit(a, LogInfo, "after error", nil)
}
