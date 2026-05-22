package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/domain/resolver"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

func TestCreateBuildHappyPathWithFakes(t *testing.T) {
	jobSpecs := newHandlerJobSpecRepo()
	versioning := newHandlerDatasetRepo()
	locks := newHandlerLockRepo()
	builds := &recordingBuildRepo{}
	jobSpecs.add(handlerJobSpec("ri.spec.main", []string{"raw.users"}, []string{"out.users"}))
	versioning.addBranch("raw.users", "master")
	versioning.addBranch("out.users", "master")
	restore := SetBuildLifecyclePorts(BuildLifecyclePorts{JobSpecs: jobSpecs, Versioning: versioning, Locks: locks, Builds: builds})
	defer restore()

	rr := httptest.NewRecorder()
	CreateBuild(rr, httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewReader([]byte(`{"pipeline_rid":"ri.pipeline.1","build_branch":"master","output_dataset_rids":["out.users"]}`))))

	res := rr.Result()
	defer res.Body.Close()
	require.Equal(t, http.StatusAccepted, res.StatusCode)
	require.Equal(t, 1, builds.opened)
	require.Equal(t, 1, builds.persisted)
	require.Len(t, builds.last.Jobs, 1)
	require.Len(t, builds.last.OpenedTransactions, 1)
	require.Equal(t, "out.users", builds.last.OpenedTransactions[0].DatasetRID)
	var payload map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&payload))
	require.Equal(t, "BUILD_RESOLUTION", payload["state"])
	require.Equal(t, float64(1), payload["job_count"])
	require.NotContains(t, rr.Body.String(), "not_implemented")
}

func TestCreateBuildIsIdempotentOnRepeatedKey(t *testing.T) {
	jobSpecs := newHandlerJobSpecRepo()
	versioning := newHandlerDatasetRepo()
	locks := newHandlerLockRepo()
	builds := &recordingBuildRepo{}
	jobSpecs.add(handlerJobSpec("ri.spec.main", []string{"raw.users"}, []string{"out.users"}))
	versioning.addBranch("raw.users", "master")
	versioning.addBranch("out.users", "master")
	restore := SetBuildLifecyclePorts(BuildLifecyclePorts{JobSpecs: jobSpecs, Versioning: versioning, Locks: locks, Builds: builds})
	defer restore()

	body := `{"pipeline_rid":"ri.pipeline.1","build_branch":"master","output_dataset_rids":["out.users"]}`
	post := func() *http.Response {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewReader([]byte(body)))
		req.Header.Set("Idempotency-Key", "build-key-abc")
		rr := httptest.NewRecorder()
		CreateBuild(rr, req)
		return rr.Result()
	}

	first := post()
	defer first.Body.Close()
	require.Equal(t, http.StatusAccepted, first.StatusCode)
	var firstPayload map[string]any
	require.NoError(t, json.NewDecoder(first.Body).Decode(&firstPayload))

	second := post()
	defer second.Body.Close()
	require.Equal(t, http.StatusOK, second.StatusCode)
	var secondPayload map[string]any
	require.NoError(t, json.NewDecoder(second.Body).Decode(&secondPayload))

	require.Equal(t, true, secondPayload["idempotent_replay"])
	require.Equal(t, firstPayload["build_id"], secondPayload["build_id"])
	// The repeated key must not open or persist a second build.
	require.Equal(t, 1, builds.opened)
	require.Equal(t, 1, builds.persisted)
}

func TestCreateBuildMissingJobSpec(t *testing.T) {
	versioning := newHandlerDatasetRepo()
	builds := &recordingBuildRepo{}
	restore := SetBuildLifecyclePorts(BuildLifecyclePorts{JobSpecs: newHandlerJobSpecRepo(), Versioning: versioning, Locks: newHandlerLockRepo(), Builds: builds})
	defer restore()

	rr := httptest.NewRecorder()
	CreateBuild(rr, httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewReader([]byte(`{"pipeline_rid":"ri.pipeline.1","build_branch":"feature","job_spec_fallback":["master"],"output_dataset_rids":["missing.out"]}`))))

	require.Equal(t, http.StatusBadRequest, rr.Result().StatusCode)
	require.Contains(t, rr.Body.String(), "missing JobSpec")
	require.Equal(t, 1, builds.opened)
	require.Equal(t, 0, builds.persisted)
	require.Equal(t, 1, builds.failed)
}

func TestCreateBuildLockHeld(t *testing.T) {
	jobSpecs := newHandlerJobSpecRepo()
	versioning := newHandlerDatasetRepo()
	locks := newHandlerLockRepo()
	builds := &recordingBuildRepo{}
	jobSpecs.add(handlerJobSpec("ri.spec.main", nil, []string{"out.locked"}))
	versioning.addBranch("out.locked", "master")
	locks.hold("out.locked", uuid.MustParse("11111111-1111-1111-1111-111111111111"))
	restore := SetBuildLifecyclePorts(BuildLifecyclePorts{JobSpecs: jobSpecs, Versioning: versioning, Locks: locks, Builds: builds})
	defer restore()

	rr := httptest.NewRecorder()
	CreateBuild(rr, httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewReader([]byte(`{"pipeline_rid":"ri.pipeline.1","build_branch":"master","output_dataset_rids":["out.locked"]}`))))

	require.Equal(t, http.StatusBadRequest, rr.Result().StatusCode)
	require.Contains(t, rr.Body.String(), "already locked")
	require.Equal(t, 1, builds.failed)
}

func TestCreateBuildInputBranchMissing(t *testing.T) {
	jobSpecs := newHandlerJobSpecRepo()
	versioning := newHandlerDatasetRepo()
	builds := &recordingBuildRepo{}
	jobSpecs.add(handlerJobSpec("ri.spec.main", []string{"raw.missing_branch"}, []string{"out.users"}))
	versioning.addBranch("raw.missing_branch", "develop")
	versioning.addBranch("out.users", "master")
	restore := SetBuildLifecyclePorts(BuildLifecyclePorts{JobSpecs: jobSpecs, Versioning: versioning, Locks: newHandlerLockRepo(), Builds: builds})
	defer restore()

	rr := httptest.NewRecorder()
	CreateBuild(rr, httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewReader([]byte(`{"pipeline_rid":"ri.pipeline.1","build_branch":"feature","output_dataset_rids":["out.users"]}`))))

	require.Equal(t, http.StatusBadRequest, rr.Result().StatusCode)
	require.Contains(t, rr.Body.String(), "has no branch matching")
	require.Equal(t, 1, builds.failed)
}

func TestDryRunResolveDoesNotPersist(t *testing.T) {
	jobSpecs := newHandlerJobSpecRepo()
	versioning := newHandlerDatasetRepo()
	builds := &recordingBuildRepo{}
	jobSpecs.add(handlerJobSpec("ri.spec.main", []string{"raw.users"}, []string{"out.users"}))
	versioning.addBranch("raw.users", "master")
	versioning.addBranch("out.users", "master")
	restore := SetBuildLifecyclePorts(BuildLifecyclePorts{JobSpecs: jobSpecs, Versioning: versioning, Locks: newHandlerLockRepo(), Builds: builds})
	defer restore()

	rr := httptest.NewRecorder()
	DryRunResolve(rr, httptest.NewRequest(http.MethodPost, "/api/v1/dry-run/resolve", bytes.NewReader([]byte(`{"pipeline_rid":"ri.pipeline.1","build_branch":"master","output_dataset_rids":["out.users"]}`))))

	require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	require.Equal(t, 0, builds.opened)
	require.Equal(t, 0, builds.persisted)
	require.Equal(t, 0, versioning.openedTransactions)
	var payload dryRunResolveResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&payload))
	require.Len(t, payload.Jobs, 1)
	require.Empty(t, payload.Errors)
}

func TestCreateBuildValidation(t *testing.T) {
	restore := SetBuildLifecyclePorts(BuildLifecyclePorts{JobSpecs: newHandlerJobSpecRepo(), Versioning: newHandlerDatasetRepo(), Locks: newHandlerLockRepo(), Builds: &recordingBuildRepo{}})
	defer restore()

	rr := httptest.NewRecorder()
	CreateBuild(rr, httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewReader([]byte(`{"pipeline_rid":"ri.pipeline.1","build_branch":"master"}`))))

	require.Equal(t, http.StatusBadRequest, rr.Result().StatusCode)
	require.Contains(t, rr.Body.String(), "output_dataset_rids")
}

type recordingBuildRepo struct {
	opened    int
	persisted int
	failed    int
	last      *models.ResolvedBuild
	idem      map[string]uuid.UUID
}

func (r *recordingBuildRepo) OpenBuild(_ context.Context, _ resolver.ResolveBuildArgs, _ uuid.UUID) error {
	r.opened++
	return nil
}

func (r *recordingBuildRepo) PersistResolvedBuild(_ context.Context, resolved *models.ResolvedBuild) error {
	r.persisted++
	r.last = resolved
	return nil
}

func (r *recordingBuildRepo) MarkBuildFailed(_ context.Context, _ uuid.UUID, _ string) error {
	r.failed++
	return nil
}

func (r *recordingBuildRepo) ClaimBuildIdempotency(_ context.Context, subject, key string, buildID uuid.UUID) (uuid.UUID, bool, error) {
	if r.idem == nil {
		r.idem = map[string]uuid.UUID{}
	}
	k := subject + "\x00" + key
	if owner, ok := r.idem[k]; ok {
		return owner, false, nil
	}
	r.idem[k] = buildID
	return buildID, true, nil
}

func (r *recordingBuildRepo) ReleaseBuildIdempotency(_ context.Context, subject, key string) error {
	delete(r.idem, subject+"\x00"+key)
	return nil
}

type handlerJobSpecRepo struct {
	mu    sync.Mutex
	specs map[string]models.JobSpec
}

func newHandlerJobSpecRepo() *handlerJobSpecRepo {
	return &handlerJobSpecRepo{specs: map[string]models.JobSpec{}}
}

func (r *handlerJobSpecRepo) add(spec models.JobSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, output := range spec.OutputDatasetRIDs {
		r.specs[output] = spec
	}
}

func (r *handlerJobSpecRepo) Lookup(_ context.Context, _, outputDatasetRID, _ string, _ []string) (*models.JobSpec, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	spec, ok := r.specs[outputDatasetRID]
	if !ok {
		return nil, nil
	}
	return &spec, nil
}

type handlerDatasetRepo struct {
	mu                 sync.Mutex
	branches           map[string][]models.BranchSnapshot
	openedTransactions int
}

func newHandlerDatasetRepo() *handlerDatasetRepo {
	return &handlerDatasetRepo{branches: map[string][]models.BranchSnapshot{}}
}

func (r *handlerDatasetRepo) addBranch(datasetRID, branch string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.branches[datasetRID] = append(r.branches[datasetRID], models.BranchSnapshot{Name: branch})
}

func (r *handlerDatasetRepo) ListBranches(_ context.Context, datasetRID string) ([]models.BranchSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]models.BranchSnapshot(nil), r.branches[datasetRID]...), nil
}

func (r *handlerDatasetRepo) OpenTransaction(_ context.Context, datasetRID, branch string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.openedTransactions++
	return fmt.Sprintf("txn.%s.%s.%d", datasetRID, branch, r.openedTransactions), nil
}

func (r *handlerDatasetRepo) ViewSchema(context.Context, string, string) (json.RawMessage, error) {
	return json.RawMessage(`{"fields":[]}`), nil
}

type handlerLockRepo struct {
	mu    sync.Mutex
	locks map[string]uuid.UUID
}

func newHandlerLockRepo() *handlerLockRepo { return &handlerLockRepo{locks: map[string]uuid.UUID{}} }

func (r *handlerLockRepo) hold(outputDatasetRID string, buildID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.locks[outputDatasetRID] = buildID
}

func (r *handlerLockRepo) TryAcquire(_ context.Context, outputDatasetRID string, buildID uuid.UUID, _ string) (uuid.UUID, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if holder, ok := r.locks[outputDatasetRID]; ok {
		return holder, false, nil
	}
	r.locks[outputDatasetRID] = buildID
	return uuid.Nil, true, nil
}

func (r *handlerLockRepo) HasUpstreamInProgress(context.Context, []string, uuid.UUID) (bool, error) {
	return false, nil
}

func handlerJobSpec(rid string, inputs, outputs []string) models.JobSpec {
	inputSpecs := make([]models.InputSpec, 0, len(inputs))
	for _, input := range inputs {
		inputSpecs = append(inputSpecs, models.InputSpec{DatasetRID: input, FallbackChain: []string{"master"}})
	}
	return models.JobSpec{RID: rid, PipelineRID: "ri.pipeline.1", BranchName: "master", Inputs: inputSpecs, OutputDatasetRIDs: append([]string(nil), outputs...), LogicKind: "TRANSFORM", LogicPayload: json.RawMessage(`null`), ContentHash: "hash-" + rid}
}
