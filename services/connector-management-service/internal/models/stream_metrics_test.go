package models

import (
	"strings"
	"testing"
	"time"
)

func TestStreamMetricsWindowSeconds(t *testing.T) {
	cases := map[StreamMetricsWindow]int{
		StreamMetricsWindow1Minute:  60,
		StreamMetricsWindow5Minutes: 300,
		StreamMetricsWindow1Hour:    3600,
		StreamMetricsWindow1Day:     86400,
		"unknown":                   60,
		"":                          60,
	}
	for window, want := range cases {
		if got := window.Seconds(); got != want {
			t.Fatalf("window %q: got %d, want %d", window, got, want)
		}
	}
}

func TestBuildStreamMetricsSnapshot_BasicRates(t *testing.T) {
	capturedAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	snapshot := BuildStreamMetricsSnapshot(StreamMetricsInput{
		StreamID:          "stream-1",
		StreamRID:         "ri.streams.main.stream-1",
		StreamName:        "events",
		Window:            StreamMetricsWindow1Minute,
		CapturedAt:        capturedAt,
		IngestedRecords:   6000,
		IngestedBytes:     60000,
		ConsumedRecords:   3000,
		ConsumedBytes:     30000,
		StreamLagRecords:  3000,
		HotBufferRecords:  5000,
		HotBufferBytes:    500000,
		ArchiveLagRecords: 100,
		ProcessingLag:     200,
	})

	if snapshot.Ingestion.RecordsPerSecond != 100 {
		t.Fatalf("ingestion rate: got %v, want 100/s", snapshot.Ingestion.RecordsPerSecond)
	}
	if snapshot.Consumption.RecordsPerSecond != 50 {
		t.Fatalf("consumption rate: got %v, want 50/s", snapshot.Consumption.RecordsPerSecond)
	}
	if snapshot.Lag.StreamLagRecords != 3000 || snapshot.Lag.HotBufferRecords != 5000 ||
		snapshot.Lag.ArchiveLagRecords != 100 || snapshot.Lag.ProcessingLagRecords != 200 {
		t.Fatalf("lag summary unexpected: %+v", snapshot.Lag)
	}
	if snapshot.Window != StreamMetricsWindow1Minute {
		t.Fatalf("window not preserved: %s", snapshot.Window)
	}
	if snapshot.Ingestion.WindowStartedAt == nil {
		t.Fatalf("ingestion window_started_at must be populated")
	}
	expectedStart := capturedAt.Add(-60 * time.Second)
	if !snapshot.Ingestion.WindowStartedAt.Equal(expectedStart) {
		t.Fatalf("window started_at: got %v, want %v", snapshot.Ingestion.WindowStartedAt, expectedStart)
	}
}

func TestBuildStreamMetricsSnapshot_CheckpointAggregation(t *testing.T) {
	now := time.Now().UTC()
	snapshot := BuildStreamMetricsSnapshot(StreamMetricsInput{
		StreamID: "stream-1",
		Window:   StreamMetricsWindow5Minutes,
		Checkpoints: []StreamCheckpointSample{
			{ID: "c1", Status: "completed", DurationMs: 100, SizeBytes: 1000, CreatedAt: now.Add(-3 * time.Minute)},
			{ID: "c2", Status: "completed", DurationMs: 200, SizeBytes: 2000, CreatedAt: now.Add(-2 * time.Minute)},
			{ID: "c3", Status: "failed", DurationMs: 0, SizeBytes: 0, CreatedAt: now.Add(-90 * time.Second)},
			{ID: "c4", Status: "completed", DurationMs: 400, SizeBytes: 4000, CreatedAt: now.Add(-1 * time.Minute)},
		},
	})
	if snapshot.Checkpoint.CheckpointCount != 3 {
		t.Fatalf("checkpoint count: got %d, want 3", snapshot.Checkpoint.CheckpointCount)
	}
	if snapshot.Checkpoint.FailureCount != 1 {
		t.Fatalf("failure count: got %d, want 1", snapshot.Checkpoint.FailureCount)
	}
	if snapshot.Checkpoint.AverageDurationMs != (100+200+400)/3 {
		t.Fatalf("average duration: got %d", snapshot.Checkpoint.AverageDurationMs)
	}
	if snapshot.Checkpoint.MaxDurationMs != 400 {
		t.Fatalf("max duration: got %d", snapshot.Checkpoint.MaxDurationMs)
	}
	if snapshot.Checkpoint.LastDurationMs != 400 || snapshot.Checkpoint.LastSizeBytes != 4000 {
		t.Fatalf("last checkpoint mismatch: %+v", snapshot.Checkpoint)
	}
}

func TestBuildStreamMetricsSnapshot_BreakdownsAndOrdering(t *testing.T) {
	snapshot := BuildStreamMetricsSnapshot(StreamMetricsInput{
		StreamID: "stream-1",
		Window:   StreamMetricsWindow1Minute,
		Consumers: []StreamConsumerSample{
			{ID: "a", Name: "consumer-a", Lag: 10, RecordsRead: 60, BytesRead: 600},
			{ID: "b", Name: "consumer-b", Lag: 500, RecordsRead: 600, BytesRead: 6000},
			{ID: "c", Name: "consumer-c", Lag: 100, RecordsRead: 300, BytesRead: 3000},
		},
		Partitions: []StreamPartitionSample{
			{PartitionKey: "p1", Topic: "t", Lag: 5, IngestedRecords: 60, ConsumedRecords: 60},
			{PartitionKey: "p2", Topic: "t", Lag: 80, IngestedRecords: 1200, ConsumedRecords: 1120},
		},
		Syncs: []StreamSyncSample{
			{SyncID: "s1", SyncName: "kafka-sync", LastRunStatus: "succeeded", RecordsIngested: 1200, BytesIngested: 12000, Retries: 1},
		},
		Exports: []StreamExportSample{
			{ExportID: "e1", ExportName: "kafka-export", LastRunStatus: "running", RecordsExported: 600, BytesExported: 6000, DuplicateRisk: true},
		},
	})

	if len(snapshot.Consumers) != 3 || snapshot.Consumers[0].ConsumerID != "b" {
		t.Fatalf("consumers should be sorted by lag desc, got %+v", snapshot.Consumers)
	}
	if len(snapshot.Partitions) != 2 || snapshot.Partitions[0].PartitionKey != "p2" {
		t.Fatalf("partitions should be sorted by lag desc, got %+v", snapshot.Partitions)
	}
	if len(snapshot.StreamingSyncs) != 1 || snapshot.StreamingSyncs[0].Ingestion.RecordsPerSecond != 20 {
		t.Fatalf("sync rate: got %+v", snapshot.StreamingSyncs)
	}
	if len(snapshot.StreamingExports) != 1 || !snapshot.StreamingExports[0].DuplicateRisk {
		t.Fatalf("export duplicate risk lost: %+v", snapshot.StreamingExports)
	}
}

func TestBuildStreamMetricsSnapshot_Warnings(t *testing.T) {
	snapshot := BuildStreamMetricsSnapshot(StreamMetricsInput{
		StreamID:          "stream-1",
		Window:            StreamMetricsWindow1Minute,
		IngestedRecords:   100,
		ConsumedRecords:   10,
		StreamLagRecords:  500,
		DroppedRecords:    3,
		DuplicateWarnings: 2,
		Exports: []StreamExportSample{
			{ExportID: "e1", DropRisk: true},
		},
	})
	if len(snapshot.Warnings) == 0 {
		t.Fatalf("expected warnings to be emitted, got none")
	}
	wantSubstrings := []string{
		"dropped",
		"Duplicate",
		"Stream lag exceeds",
		"drop risk",
	}
	for _, want := range wantSubstrings {
		found := false
		for _, w := range snapshot.Warnings {
			if strings.Contains(w, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing warning substring %q in %v", want, snapshot.Warnings)
		}
	}
}

func TestBuildStreamMetricsSnapshot_DefaultsZero(t *testing.T) {
	snapshot := BuildStreamMetricsSnapshot(StreamMetricsInput{StreamID: "stream-1"})
	if snapshot.Window != StreamMetricsWindow1Minute {
		t.Fatalf("default window should be 1m, got %s", snapshot.Window)
	}
	if snapshot.Ingestion.RecordsPerSecond != 0 {
		t.Fatalf("ingestion rate must default to zero, got %v", snapshot.Ingestion.RecordsPerSecond)
	}
	if len(snapshot.Warnings) != 0 {
		t.Fatalf("no warnings expected for empty input, got %v", snapshot.Warnings)
	}
	if snapshot.CapturedAt.IsZero() {
		t.Fatalf("captured_at must default to now")
	}
}
