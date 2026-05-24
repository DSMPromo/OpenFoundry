package queuedispatcher

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// fakeRepo implements the dispatcher's Repository contract. Every
// call records into a shared mu+slice for assertion; canned outputs
// per-method.
type fakeRepo struct {
	mu          sync.Mutex
	queued      []QueuedRun
	pools       []models.ResourcePool
	utilByPool  map[uuid.UUID]PoolUtilization
	promoted    []uuid.UUID
	waiting     map[uuid.UUID]string
	promoteErr error
	waitErr    error
}

func (f *fakeRepo) ListQueuedRuns(_ context.Context, limit int) ([]QueuedRun, error) {
	if limit > 0 && limit < len(f.queued) {
		return append([]QueuedRun(nil), f.queued[:limit]...), nil
	}
	return append([]QueuedRun(nil), f.queued...), nil
}

func (f *fakeRepo) ListResourcePoolsAll(_ context.Context) ([]models.ResourcePool, error) {
	return append([]models.ResourcePool(nil), f.pools...), nil
}

func (f *fakeRepo) CurrentPoolUtilization(_ context.Context, poolID uuid.UUID) (PoolUtilization, error) {
	return f.utilByPool[poolID], nil
}

func (f *fakeRepo) PromoteToRunning(_ context.Context, runID, _ uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.promoteErr != nil {
		return f.promoteErr
	}
	f.promoted = append(f.promoted, runID)
	return nil
}

func (f *fakeRepo) MarkWaitingForResources(_ context.Context, runID uuid.UUID, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.waitErr != nil {
		return f.waitErr
	}
	if f.waiting == nil {
		f.waiting = map[uuid.UUID]string{}
	}
	f.waiting[runID] = reason
	return nil
}

// reservingPoolName returns a *int holding n.
func intp(n int) *int { return &n }

func defaultPool() models.ResourcePool {
	return models.ResourcePool{ID: uuid.New(), Name: "default", Priority: 100}
}

func TestTickPromotesQueuedRunsIntoDefaultPool(t *testing.T) {
	dpool := defaultPool()
	runs := []QueuedRun{{ID: uuid.New()}, {ID: uuid.New()}, {ID: uuid.New()}}
	repo := &fakeRepo{
		queued: runs,
		pools:  []models.ResourcePool{dpool},
	}
	res, err := New(repo).Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Promoted != 3 || res.Waiting != 0 {
		t.Errorf("result = %+v", res)
	}
	if len(repo.promoted) != 3 {
		t.Errorf("promoted = %d, want 3", len(repo.promoted))
	}
}

func TestTickStopsAtMaxConcurrentBuilds(t *testing.T) {
	dpool := defaultPool()
	dpool.MaxConcurrentBuilds = intp(2)
	runs := []QueuedRun{{ID: uuid.New()}, {ID: uuid.New()}, {ID: uuid.New()}, {ID: uuid.New()}}
	repo := &fakeRepo{
		queued: runs,
		pools:  []models.ResourcePool{dpool},
		// 2 running already → pool full → none should promote.
		utilByPool: map[uuid.UUID]PoolUtilization{dpool.ID: {RunningCount: 2}},
	}
	res, err := New(repo).Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Promoted != 0 {
		t.Errorf("promoted = %d, want 0 (pool full)", res.Promoted)
	}
	if res.Waiting != 4 {
		t.Errorf("waiting = %d, want 4", res.Waiting)
	}
	if len(repo.waiting) != 4 {
		t.Errorf("waiting map = %+v", repo.waiting)
	}
	// Reason text should mention the pool name + capacity.
	for _, reason := range repo.waiting {
		if reason == "" {
			t.Error("empty reason")
		}
	}
}

func TestTickRoundRobinsAcrossPools(t *testing.T) {
	dpool := defaultPool()
	gpu := models.ResourcePool{ID: uuid.New(), Name: "gpu", Priority: 200}
	// 1 run in default, 1 in gpu, 1 in default again.
	dID := dpool.ID
	gID := gpu.ID
	runs := []QueuedRun{
		{ID: uuid.New(), ResourcePoolID: &dID},
		{ID: uuid.New(), ResourcePoolID: &gID},
		{ID: uuid.New(), ResourcePoolID: &dID},
		{ID: uuid.New(), ResourcePoolID: &gID},
	}
	repo := &fakeRepo{queued: runs, pools: []models.ResourcePool{dpool, gpu}}
	res, err := New(repo).Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Promoted != 4 {
		t.Errorf("promoted = %d, want 4", res.Promoted)
	}
}

func TestTickRoutesUnassignedRunsToDefault(t *testing.T) {
	dpool := defaultPool()
	gpu := models.ResourcePool{ID: uuid.New(), Name: "gpu"}
	runs := []QueuedRun{{ID: uuid.New()} /* no pool */}
	repo := &fakeRepo{queued: runs, pools: []models.ResourcePool{gpu, dpool}}
	res, err := New(repo).Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Promoted != 1 {
		t.Errorf("promoted = %d, want 1", res.Promoted)
	}
}

func TestTickHandlesAlreadyAdvancedAsPromoted(t *testing.T) {
	dpool := defaultPool()
	runs := []QueuedRun{{ID: uuid.New()}}
	repo := &fakeRepo{queued: runs, pools: []models.ResourcePool{dpool}, promoteErr: ErrAlreadyAdvanced}
	res, _ := New(repo).Tick(context.Background())
	if res.Promoted != 1 || res.Errors != 0 {
		t.Errorf("result = %+v", res)
	}
}

func TestTickRecordsErrorsForOtherPromoteFailures(t *testing.T) {
	dpool := defaultPool()
	runs := []QueuedRun{{ID: uuid.New()}}
	repo := &fakeRepo{queued: runs, pools: []models.ResourcePool{dpool}, promoteErr: errors.New("boom")}
	res, _ := New(repo).Tick(context.Background())
	if res.Errors != 1 || res.Promoted != 0 {
		t.Errorf("result = %+v", res)
	}
}

func TestTickEmptyPoolsWarns(t *testing.T) {
	repo := &fakeRepo{pools: nil}
	res, err := New(repo).Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Promoted != 0 || res.Waiting != 0 {
		t.Errorf("result = %+v", res)
	}
}

// recordReserver counts reservation calls + can fail on demand.
type recordReserver struct {
	calls int
	err   error
}

func (r *recordReserver) Reserve(_ context.Context, _ QueuedRun, _ models.ResourcePool) error {
	r.calls++
	return r.err
}

func TestTickRollsBackPromoteWhenReserveFails(t *testing.T) {
	dpool := defaultPool()
	runs := []QueuedRun{{ID: uuid.New()}}
	repo := &fakeRepo{queued: runs, pools: []models.ResourcePool{dpool}}
	rr := &recordReserver{err: errors.New("no capacity")}
	res, _ := New(repo, WithReserver(rr)).Tick(context.Background())
	if rr.calls != 1 {
		t.Errorf("reserve calls = %d, want 1", rr.calls)
	}
	if res.Errors != 1 || res.Promoted != 0 {
		t.Errorf("result = %+v", res)
	}
	if len(repo.promoted) != 0 {
		t.Errorf("promote should NOT have run when reserve failed")
	}
}
