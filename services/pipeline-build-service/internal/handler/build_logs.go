package handler

import (
	"net/http"
	"strings"
	"time"

	livellogs "github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/logs"
)

// ListBuildLogs returns merged-by-timestamp history for every job in
// the build at `GET /api/v1/builds/{id}/logs`. The JSON shape mirrors
// the single-job ListJobLogs response (`data` + `total`) so the
// frontend can use the same fetch helper.
//
// The merge order is deterministic: see logs.MergeHistory — sort by
// (timestamp, jobRID, sequence). The optional `limit` query param trims
// from the tail (most-recent N entries), matching the "show me the
// last 5000 lines" intent.
func ListBuildLogs(w http.ResponseWriter, r *http.Request) {
	service, ok := requireJobLogStore(w, "ListBuildLogs requires DATABASE_URL-backed log store wiring")
	if !ok {
		return
	}
	repo, ok := requireBuildQueryRepository(w, "ListBuildLogs requires DATABASE_URL-backed repository wiring")
	if !ok {
		return
	}
	query, err := parseLogsQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_logs_query", "detail": err.Error()})
		return
	}
	query.Follow = false

	jobs, err := repo.ListJobsForBuildID(r.Context(), buildIDParam(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list_jobs_failed", "detail": err.Error()})
		return
	}
	if len(jobs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"data": []livellogs.RowDTO{}, "total": 0})
		return
	}

	slices := make([][]livellogs.LogEntry, 0, len(jobs))
	for _, job := range jobs {
		// History query is per-job — sequence + limit apply per job and
		// the merge re-applies the limit at the build level. That means
		// a tight `limit=10` won't accidentally starve any one job's
		// history; the per-job pull stays generous.
		perJob := query
		perJob.Limit = 0 // unbounded per-job; bound at merge time.
		history, err := service.Store.History(r.Context(), job.RID, perJob)
		if err != nil {
			writeLogStoreUnavailable(w, "log_store_unavailable", err)
			return
		}
		slices = append(slices, history)
	}

	limit := int(query.Limit)
	merged := livellogs.MergeHistory(slices, limit)
	rows := make([]livellogs.RowDTO, 0, len(merged))
	for _, entry := range merged {
		rows = append(rows, entry.RowDTO())
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows, "total": len(rows)})
}

// StreamBuildLogs is the SSE multi-job fan-in at
// `GET /api/v1/builds/{id}/logs/stream?follow=true`. History phase
// drains a merged catch-up window first, then (if follow) subscribes
// to every job and multiplexes the live channels through
// logs.FanInLive.
//
// The wire format is identical to StreamJobLogs (event: log / event:
// heartbeat) so the frontend can swap endpoints without changing its
// consumer.
func StreamBuildLogs(w http.ResponseWriter, r *http.Request) {
	service, ok := requireJobLogStore(w, "live logs not configured")
	if !ok {
		return
	}
	repo, ok := requireBuildQueryRepository(w, "StreamBuildLogs requires DATABASE_URL-backed repository wiring")
	if !ok {
		return
	}
	buildID := buildIDParam(r)
	if strings.TrimSpace(buildID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_build_id"})
		return
	}
	query, err := parseLogsQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_logs_query", "detail": err.Error()})
		return
	}

	jobs, err := repo.ListJobsForBuildID(r.Context(), buildID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list_jobs_failed", "detail": err.Error()})
		return
	}
	jobRIDs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		if job.RID != "" {
			jobRIDs = append(jobRIDs, job.RID)
		}
	}

	// History pass — gather every job's catch-up window, merge by ts.
	historySlices := make([][]livellogs.LogEntry, 0, len(jobRIDs))
	historyQuery := query
	historyQuery.Limit = 0 // unbounded per-job; bound at merge.
	for _, rid := range jobRIDs {
		entries, err := service.Store.History(r.Context(), rid, historyQuery)
		if err != nil {
			writeLogStoreUnavailable(w, "log_store_unavailable", err)
			return
		}
		historySlices = append(historySlices, entries)
	}
	history := livellogs.MergeHistory(historySlices, int(query.Limit))

	// Live subscription — only if requested AND we actually have jobs.
	var live <-chan livellogs.LogEntry
	var teardown func()
	if query.Follow && len(jobRIDs) > 0 {
		if service.Subscriber == nil {
			writeLogSubscriberUnavailable(w, "log subscriber not configured")
			return
		}
		live, teardown, err = livellogs.FanInLive(r.Context(), service.Subscriber, jobRIDs)
		if err != nil {
			writeLogStoreUnavailable(w, "log_subscriber_unavailable", err)
			return
		}
		defer teardown()
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming_not_supported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	cfg := streamConfig.Load().(jobLogStreamConfig)
	writeHeartbeat(w, flusher, cfg.InitialDelay, "Build logs are merged across every job and streamed in timestamp order.")
	if !waitInitialDelay(r.Context(), w, flusher, cfg) {
		return
	}
	for _, entry := range history {
		if !writeLogEvent(w, flusher, entry) {
			return
		}
	}
	if !query.Follow || live == nil {
		return
	}

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			_, _ = w.Write([]byte(": keep-alive\n\n"))
			flusher.Flush()
		case entry, ok := <-live:
			if !ok {
				return
			}
			if !queryAllowsLive(query, entry) {
				continue
			}
			if !writeLogEvent(w, flusher, entry) {
				return
			}
		}
	}
}
