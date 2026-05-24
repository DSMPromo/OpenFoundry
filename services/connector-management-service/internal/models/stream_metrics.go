package models

import (
	"sort"
	"strings"
	"time"
)

// SDC.45 — Stream lag and throughput metrics.
//
// A pure aggregator that turns raw ingestion/consumption/checkpoint/retry
// counters into a `StreamMetricsSnapshot` with breakdowns by streaming sync,
// streaming export, topic/partition, and consumer. The helper is intentionally
// stateless so it can run in the connector-management-service handler (POST
// /streams/metrics:compute) and again client-side against the existing
// DataConnectionStreamResource without diverging.

type StreamMetricsWindow string

const (
	StreamMetricsWindow1Minute  StreamMetricsWindow = "1m"
	StreamMetricsWindow5Minutes StreamMetricsWindow = "5m"
	StreamMetricsWindow1Hour    StreamMetricsWindow = "1h"
	StreamMetricsWindow1Day     StreamMetricsWindow = "1d"
)

type StreamThroughputSummary struct {
	RecordsPerSecond float64    `json:"records_per_second"`
	BytesPerSecond   float64    `json:"bytes_per_second"`
	WindowSeconds    int        `json:"window_seconds"`
	WindowStartedAt  *time.Time `json:"window_started_at,omitempty"`
}

type StreamLagSummary struct {
	StreamLagRecords     int64 `json:"stream_lag_records"`
	HotBufferRecords     int64 `json:"hot_buffer_records"`
	HotBufferBytes       int64 `json:"hot_buffer_bytes"`
	ArchiveLagRecords    int64 `json:"archive_lag_records"`
	ProcessingLagRecords int64 `json:"processing_lag_records"`
}

type StreamCheckpointMetrics struct {
	CheckpointCount   uint32 `json:"checkpoint_count"`
	AverageDurationMs int64  `json:"average_duration_ms"`
	MaxDurationMs     int64  `json:"max_duration_ms"`
	LastDurationMs    int64  `json:"last_duration_ms"`
	AverageSizeBytes  int64  `json:"average_size_bytes"`
	LastSizeBytes     int64  `json:"last_size_bytes"`
	FailureCount      uint32 `json:"failure_count"`
}

type StreamRetryMetrics struct {
	TotalRetries      uint32 `json:"total_retries"`
	DroppedRecords    uint32 `json:"dropped_records"`
	DuplicateWarnings uint32 `json:"duplicate_warnings"`
	RecentFailures    uint32 `json:"recent_failures"`
}

type StreamPartitionMetrics struct {
	PartitionKey string                  `json:"partition_key"`
	Topic        string                  `json:"topic,omitempty"`
	Lag          int64                   `json:"lag"`
	Ingestion    StreamThroughputSummary `json:"ingestion"`
	Consumption  StreamThroughputSummary `json:"consumption"`
}

type StreamConsumerMetrics struct {
	ConsumerID    string                  `json:"consumer_id"`
	ConsumerName  string                  `json:"consumer_name,omitempty"`
	ConsumerGroup string                  `json:"consumer_group,omitempty"`
	Status        string                  `json:"status,omitempty"`
	Lag           int64                   `json:"lag"`
	Consumption   StreamThroughputSummary `json:"consumption"`
}

type StreamSyncMetrics struct {
	SyncID        string                  `json:"sync_id"`
	SyncName      string                  `json:"sync_name,omitempty"`
	LastRunStatus string                  `json:"last_run_status,omitempty"`
	Ingestion     StreamThroughputSummary `json:"ingestion"`
	Retries       uint32                  `json:"retries"`
}

type StreamExportMetrics struct {
	ExportID        string                  `json:"export_id"`
	ExportName      string                  `json:"export_name,omitempty"`
	LastRunStatus   string                  `json:"last_run_status,omitempty"`
	Consumption     StreamThroughputSummary `json:"consumption"`
	Retries         uint32                  `json:"retries"`
	DuplicateRisk   bool                    `json:"duplicate_risk"`
	DropRisk        bool                    `json:"drop_risk"`
	RecordsExported int64                   `json:"records_exported"`
}

type StreamMetricsSnapshot struct {
	StreamID         string                   `json:"stream_id"`
	StreamRID        string                   `json:"stream_rid,omitempty"`
	StreamName       string                   `json:"stream_name,omitempty"`
	Window           StreamMetricsWindow      `json:"window"`
	CapturedAt       time.Time                `json:"captured_at"`
	Ingestion        StreamThroughputSummary  `json:"ingestion"`
	Consumption      StreamThroughputSummary  `json:"consumption"`
	Lag              StreamLagSummary         `json:"lag"`
	Checkpoint       StreamCheckpointMetrics  `json:"checkpoint"`
	Retries          StreamRetryMetrics       `json:"retries"`
	Partitions       []StreamPartitionMetrics `json:"partitions"`
	Consumers        []StreamConsumerMetrics  `json:"consumers"`
	StreamingSyncs   []StreamSyncMetrics      `json:"streaming_syncs"`
	StreamingExports []StreamExportMetrics    `json:"streaming_exports"`
	Warnings         []string                 `json:"warnings,omitempty"`
}

// --- Builder inputs -------------------------------------------------------

type StreamCheckpointSample struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	DurationMs int64     `json:"duration_ms"`
	SizeBytes  int64     `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

type StreamConsumerSample struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ConsumerGroup string `json:"consumer_group,omitempty"`
	Status        string `json:"status,omitempty"`
	Lag           int64  `json:"lag"`
	RecordsRead   int64  `json:"records_read"`
	BytesRead     int64  `json:"bytes_read"`
}

type StreamPartitionSample struct {
	PartitionKey    string `json:"partition_key"`
	Topic           string `json:"topic,omitempty"`
	Lag             int64  `json:"lag"`
	IngestedRecords int64  `json:"ingested_records"`
	IngestedBytes   int64  `json:"ingested_bytes"`
	ConsumedRecords int64  `json:"consumed_records"`
	ConsumedBytes   int64  `json:"consumed_bytes"`
}

type StreamSyncSample struct {
	SyncID          string `json:"sync_id"`
	SyncName        string `json:"sync_name,omitempty"`
	LastRunStatus   string `json:"last_run_status,omitempty"`
	RecordsIngested int64  `json:"records_ingested"`
	BytesIngested   int64  `json:"bytes_ingested"`
	Retries         uint32 `json:"retries"`
}

type StreamExportSample struct {
	ExportID        string `json:"export_id"`
	ExportName      string `json:"export_name,omitempty"`
	LastRunStatus   string `json:"last_run_status,omitempty"`
	RecordsExported int64  `json:"records_exported"`
	BytesExported   int64  `json:"bytes_exported"`
	Retries         uint32 `json:"retries"`
	DuplicateRisk   bool   `json:"duplicate_risk"`
	DropRisk        bool   `json:"drop_risk"`
}

type StreamMetricsInput struct {
	StreamID          string                   `json:"stream_id"`
	StreamRID         string                   `json:"stream_rid,omitempty"`
	StreamName        string                   `json:"stream_name,omitempty"`
	Window            StreamMetricsWindow      `json:"window,omitempty"`
	CapturedAt        time.Time                `json:"captured_at,omitempty"`
	StreamLagRecords  int64                    `json:"stream_lag_records"`
	HotBufferRecords  int64                    `json:"hot_buffer_records"`
	HotBufferBytes    int64                    `json:"hot_buffer_bytes"`
	ArchiveLagRecords int64                    `json:"archive_lag_records"`
	ProcessingLag     int64                    `json:"processing_lag_records"`
	IngestedRecords   int64                    `json:"ingested_records"`
	IngestedBytes     int64                    `json:"ingested_bytes"`
	ConsumedRecords   int64                    `json:"consumed_records"`
	ConsumedBytes     int64                    `json:"consumed_bytes"`
	Retries           uint32                   `json:"retries"`
	DroppedRecords    uint32                   `json:"dropped_records"`
	DuplicateWarnings uint32                   `json:"duplicate_warnings"`
	RecentFailures    uint32                   `json:"recent_failures"`
	Checkpoints       []StreamCheckpointSample `json:"checkpoints,omitempty"`
	Consumers         []StreamConsumerSample   `json:"consumers,omitempty"`
	Partitions        []StreamPartitionSample  `json:"partitions,omitempty"`
	Syncs             []StreamSyncSample       `json:"streaming_syncs,omitempty"`
	Exports           []StreamExportSample     `json:"streaming_exports,omitempty"`
}

// --- Helpers --------------------------------------------------------------

// WindowSeconds maps the canonical metric windows to a duration in seconds.
// Unknown windows fall back to 1 minute so downstream throughput math stays
// safe even when the caller forgets to set the field.
func (w StreamMetricsWindow) Seconds() int {
	switch w {
	case StreamMetricsWindow5Minutes:
		return 300
	case StreamMetricsWindow1Hour:
		return 3600
	case StreamMetricsWindow1Day:
		return 86400
	default:
		return 60
	}
}

func computeThroughput(records, bytes int64, window StreamMetricsWindow, capturedAt time.Time) StreamThroughputSummary {
	secs := window.Seconds()
	out := StreamThroughputSummary{WindowSeconds: secs}
	if secs > 0 {
		out.RecordsPerSecond = float64(records) / float64(secs)
		out.BytesPerSecond = float64(bytes) / float64(secs)
	}
	if !capturedAt.IsZero() {
		start := capturedAt.Add(-time.Duration(secs) * time.Second)
		out.WindowStartedAt = &start
	}
	return out
}

func summarizeCheckpoints(samples []StreamCheckpointSample) StreamCheckpointMetrics {
	out := StreamCheckpointMetrics{}
	if len(samples) == 0 {
		return out
	}
	sorted := append([]StreamCheckpointSample(nil), samples...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].CreatedAt.Before(sorted[j].CreatedAt) })

	var (
		totalDuration int64
		totalSize     int64
		durationCount int64
		sizeCount     int64
	)
	for _, sample := range sorted {
		status := strings.ToLower(strings.TrimSpace(sample.Status))
		if status == "failed" || status == "error" || status == "expired" {
			out.FailureCount++
			continue
		}
		out.CheckpointCount++
		if sample.DurationMs > 0 {
			totalDuration += sample.DurationMs
			durationCount++
			if sample.DurationMs > out.MaxDurationMs {
				out.MaxDurationMs = sample.DurationMs
			}
		}
		if sample.SizeBytes > 0 {
			totalSize += sample.SizeBytes
			sizeCount++
		}
	}
	if durationCount > 0 {
		out.AverageDurationMs = totalDuration / durationCount
	}
	if sizeCount > 0 {
		out.AverageSizeBytes = totalSize / sizeCount
	}
	// Last completed checkpoint is the most recent non-failure sample.
	for i := len(sorted) - 1; i >= 0; i-- {
		status := strings.ToLower(strings.TrimSpace(sorted[i].Status))
		if status == "failed" || status == "error" || status == "expired" {
			continue
		}
		out.LastDurationMs = sorted[i].DurationMs
		out.LastSizeBytes = sorted[i].SizeBytes
		break
	}
	return out
}

func summarizeConsumers(samples []StreamConsumerSample, window StreamMetricsWindow, capturedAt time.Time) []StreamConsumerMetrics {
	out := make([]StreamConsumerMetrics, 0, len(samples))
	for _, sample := range samples {
		out = append(out, StreamConsumerMetrics{
			ConsumerID:    sample.ID,
			ConsumerName:  sample.Name,
			ConsumerGroup: sample.ConsumerGroup,
			Status:        sample.Status,
			Lag:           sample.Lag,
			Consumption:   computeThroughput(sample.RecordsRead, sample.BytesRead, window, capturedAt),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Lag > out[j].Lag })
	return out
}

func summarizePartitions(samples []StreamPartitionSample, window StreamMetricsWindow, capturedAt time.Time) []StreamPartitionMetrics {
	out := make([]StreamPartitionMetrics, 0, len(samples))
	for _, sample := range samples {
		out = append(out, StreamPartitionMetrics{
			PartitionKey: sample.PartitionKey,
			Topic:        sample.Topic,
			Lag:          sample.Lag,
			Ingestion:    computeThroughput(sample.IngestedRecords, sample.IngestedBytes, window, capturedAt),
			Consumption:  computeThroughput(sample.ConsumedRecords, sample.ConsumedBytes, window, capturedAt),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Lag > out[j].Lag })
	return out
}

func summarizeSyncs(samples []StreamSyncSample, window StreamMetricsWindow, capturedAt time.Time) []StreamSyncMetrics {
	out := make([]StreamSyncMetrics, 0, len(samples))
	for _, sample := range samples {
		out = append(out, StreamSyncMetrics{
			SyncID:        sample.SyncID,
			SyncName:      sample.SyncName,
			LastRunStatus: sample.LastRunStatus,
			Ingestion:     computeThroughput(sample.RecordsIngested, sample.BytesIngested, window, capturedAt),
			Retries:       sample.Retries,
		})
	}
	return out
}

func summarizeExports(samples []StreamExportSample, window StreamMetricsWindow, capturedAt time.Time) []StreamExportMetrics {
	out := make([]StreamExportMetrics, 0, len(samples))
	for _, sample := range samples {
		out = append(out, StreamExportMetrics{
			ExportID:        sample.ExportID,
			ExportName:      sample.ExportName,
			LastRunStatus:   sample.LastRunStatus,
			Consumption:     computeThroughput(sample.RecordsExported, sample.BytesExported, window, capturedAt),
			Retries:         sample.Retries,
			DuplicateRisk:   sample.DuplicateRisk,
			DropRisk:        sample.DropRisk,
			RecordsExported: sample.RecordsExported,
		})
	}
	return out
}

func collectStreamWarnings(input StreamMetricsInput) []string {
	warnings := []string{}
	if input.DroppedRecords > 0 {
		warnings = append(warnings, "Stream dropped records were reported during the metric window.")
	}
	if input.DuplicateWarnings > 0 {
		warnings = append(warnings, "Duplicate records were detected during the metric window; downstream consumers must remain idempotent.")
	}
	if input.StreamLagRecords > 0 {
		consumed := input.ConsumedRecords
		if consumed == 0 {
			consumed = 1 // avoid divide-by-zero and still flag a stalled consumer
		}
		ratio := float64(input.StreamLagRecords) / float64(consumed)
		if ratio > 1 {
			warnings = append(warnings, "Stream lag exceeds the most recent consumption rate; scale consumers or replay from a safe offset.")
		}
	}
	for _, export := range input.Exports {
		if export.DropRisk {
			warnings = append(warnings, "Streaming export "+export.ExportID+" reports a drop risk on the configured replay behavior.")
		}
		if export.DuplicateRisk {
			warnings = append(warnings, "Streaming export "+export.ExportID+" reports a duplicate risk on the configured replay behavior.")
		}
	}
	return warnings
}

// BuildStreamMetricsSnapshot turns the raw inputs into a snapshot suitable for
// rendering in the Source Detail Streams tab and feeding Data Health checks.
func BuildStreamMetricsSnapshot(input StreamMetricsInput) StreamMetricsSnapshot {
	window := input.Window
	if window == "" {
		window = StreamMetricsWindow1Minute
	}
	capturedAt := input.CapturedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}

	snapshot := StreamMetricsSnapshot{
		StreamID:    input.StreamID,
		StreamRID:   input.StreamRID,
		StreamName:  input.StreamName,
		Window:      window,
		CapturedAt:  capturedAt,
		Ingestion:   computeThroughput(input.IngestedRecords, input.IngestedBytes, window, capturedAt),
		Consumption: computeThroughput(input.ConsumedRecords, input.ConsumedBytes, window, capturedAt),
		Lag: StreamLagSummary{
			StreamLagRecords:     input.StreamLagRecords,
			HotBufferRecords:     input.HotBufferRecords,
			HotBufferBytes:       input.HotBufferBytes,
			ArchiveLagRecords:    input.ArchiveLagRecords,
			ProcessingLagRecords: input.ProcessingLag,
		},
		Checkpoint: summarizeCheckpoints(input.Checkpoints),
		Retries: StreamRetryMetrics{
			TotalRetries:      input.Retries,
			DroppedRecords:    input.DroppedRecords,
			DuplicateWarnings: input.DuplicateWarnings,
			RecentFailures:    input.RecentFailures,
		},
		Partitions:       summarizePartitions(input.Partitions, window, capturedAt),
		Consumers:        summarizeConsumers(input.Consumers, window, capturedAt),
		StreamingSyncs:   summarizeSyncs(input.Syncs, window, capturedAt),
		StreamingExports: summarizeExports(input.Exports, window, capturedAt),
		Warnings:         collectStreamWarnings(input),
	}
	return snapshot
}
