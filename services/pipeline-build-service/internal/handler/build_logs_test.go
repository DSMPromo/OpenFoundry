package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	livellogs "github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/logs"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// buildLogsFakeRepo returns a canned []models.Job list for any
// build-id lookup. The other BuildQueryRepository methods aren't
// reached by ListBuildLogs / StreamBuildLogs.
type buildLogsFakeRepo struct{ jobs []models.Job }

func (f *buildLogsFakeRepo) ListBuilds(context.Context, models.ListBuildsQuery) ([]models.BuildEnvelope, error) {
	return nil, nil
}
func (f *buildLogsFakeRepo) GetBuild(context.Context, string) (*models.BuildEnvelope, error) {
	return nil, nil
}
func (f *buildLogsFakeRepo) ListJobsForBuildID(context.Context, string) ([]models.Job, error) {
	return f.jobs, nil
}
func (f *buildLogsFakeRepo) GetJob(context.Context, string) (*models.Job, error) { return nil, nil }

func TestListBuildLogsMergesHistoryAcrossJobs(t *testing.T) {
	mem := livellogs.NewMemoryService()
	// Two jobs, three entries — interleaved timestamps to prove merge order.
	mem.Emit("ri.foundry.main.job.a", livellogs.LogInfo, "a1", nil)
	time.Sleep(2 * time.Millisecond)
	mem.Emit("ri.foundry.main.job.b", livellogs.LogInfo, "b1", nil)
	time.Sleep(2 * time.Millisecond)
	mem.Emit("ri.foundry.main.job.a", livellogs.LogInfo, "a2", nil)

	restoreSvc := SetJobLogService(&livellogs.Service{Store: mem, Subscriber: mem})
	defer restoreSvc()
	restoreRepo := SetBuildQueryRepository(&buildLogsFakeRepo{jobs: []models.Job{
		{ID: uuid.New(), RID: "ri.foundry.main.job.a"},
		{ID: uuid.New(), RID: "ri.foundry.main.job.b"},
	}})
	defer restoreRepo()

	r := chi.NewRouter()
	r.Get("/api/v1/builds/{id}/logs", ListBuildLogs)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+uuid.New().String()+"/logs", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Data  []livellogs.RowDTO `json:"data"`
		Total int                `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, 3, got.Total)
	require.Equal(t, []string{"a1", "b1", "a2"}, []string{got.Data[0].Message, got.Data[1].Message, got.Data[2].Message})
}

func TestListBuildLogsEmptyWhenNoJobs(t *testing.T) {
	mem := livellogs.NewMemoryService()
	restoreSvc := SetJobLogService(&livellogs.Service{Store: mem, Subscriber: mem})
	defer restoreSvc()
	restoreRepo := SetBuildQueryRepository(&buildLogsFakeRepo{jobs: nil})
	defer restoreRepo()

	r := chi.NewRouter()
	r.Get("/api/v1/builds/{id}/logs", ListBuildLogs)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+uuid.New().String()+"/logs", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"total":0`)
}

func TestListBuildLogs503WhenLogStoreMissing(t *testing.T) {
	restoreSvc := SetJobLogService(nil)
	defer restoreSvc()
	restoreRepo := SetBuildQueryRepository(&buildLogsFakeRepo{})
	defer restoreRepo()

	r := chi.NewRouter()
	r.Get("/api/v1/builds/{id}/logs", ListBuildLogs)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+uuid.New().String()+"/logs", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestStreamBuildLogsFansInHistoryAndLive(t *testing.T) {
	mem := livellogs.NewMemoryService()
	jobA := "ri.foundry.main.job." + uuid.New().String()
	jobB := "ri.foundry.main.job." + uuid.New().String()
	mem.Emit(jobA, livellogs.LogInfo, "history-a", nil)
	mem.Emit(jobB, livellogs.LogInfo, "history-b", nil)

	restoreSvc := SetJobLogService(&livellogs.Service{Store: mem, Subscriber: mem})
	defer restoreSvc()
	restoreRepo := SetBuildQueryRepository(&buildLogsFakeRepo{jobs: []models.Job{
		{ID: uuid.New(), RID: jobA},
		{ID: uuid.New(), RID: jobB},
	}})
	defer restoreRepo()
	restoreCfg := SetJobLogStreamConfig(0, time.Millisecond)
	defer restoreCfg()

	r := chi.NewRouter()
	r.Get("/api/v1/builds/{id}/logs/stream", StreamBuildLogs)
	server := httptest.NewServer(r)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/builds/" + uuid.New().String() + "/logs/stream?follow=true")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")

	reader := bufio.NewReader(resp.Body)
	// Heartbeat first.
	require.Contains(t, readSSEEvent(t, reader), "event: heartbeat")
	// Then both history entries — order is by ts so the first emit wins.
	first := readSSEEvent(t, reader)
	require.Contains(t, first, "event: log")
	require.Contains(t, first, `"message":"history-a"`)
	second := readSSEEvent(t, reader)
	require.Contains(t, second, `"message":"history-b"`)

	// Now drive a live event on jobB — fan-in should deliver it.
	mem.Emit(jobB, livellogs.LogWarn, "live-b", nil)
	live := readSSEEvent(t, reader)
	require.Contains(t, live, `"message":"live-b"`)
	require.Contains(t, live, `"level":"WARN"`)
}

func TestStreamBuildLogs503WhenLogStoreMissing(t *testing.T) {
	restoreSvc := SetJobLogService(nil)
	defer restoreSvc()
	restoreRepo := SetBuildQueryRepository(&buildLogsFakeRepo{})
	defer restoreRepo()

	r := chi.NewRouter()
	r.Get("/api/v1/builds/{id}/logs/stream", StreamBuildLogs)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+uuid.New().String()+"/logs/stream", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.True(t, strings.Contains(rec.Body.String(), "live_logs_not_configured"))
}
